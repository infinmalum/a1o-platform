package model

import (
	"fmt"

	"github.com/infinmalum/one-gateway/common/logger"
	"github.com/infinmalum/one-gateway/relay/channeltype"
)

// channelTypeMigration rewrites channels whose kind rode a removed per-vendor
// adapter onto the OpenAI-compatible kind that replaced it. Every statement
// uses bound parameters; each rule is idempotent because it rewrites only
// rows still sitting on the retired type.
type channelTypeMigration struct {
	from int
	to   int
	// baseURL replaces base_url for rows that left it empty or still on the
	// retired type's default; custom base URLs are kept.
	oldDefault string
	newBaseURL string
	// clearKey drops stored credentials that only worked against the retired
	// native protocol (signature pairs, scoped tokens) so the channel fails
	// an explicit auth error instead of a confusing one.
	clearKey bool
}

var channelTypeMigrations = []channelTypeMigration{
	// PaLM died with Google's generative language v1; the Gemini OpenAI
	// compatibility layer is the successor and needs no base URL (the target
	// default carries the /v1beta/openai/ prefix).
	{from: channeltype.PaLM, to: channeltype.GeminiOpenAICompatible, oldDefault: "https://generativelanguage.googleapis.com", newBaseURL: ""},
	// Qianfan v2 / Spark OpenAI / Bailian compatible-mode all take new keys.
	{from: channeltype.Baidu, to: channeltype.BaiduV2, oldDefault: "https://aip.baidubce.com", newBaseURL: "https://qianfan.baidubce.com", clearKey: true},
	{from: channeltype.Xunfei, to: channeltype.XunfeiV2, oldDefault: "", newBaseURL: "https://spark-api-open.xf-yun.com", clearKey: true},
	{from: channeltype.Ali, to: channeltype.AliBailian, oldDefault: "https://dashscope.aliyuncs.com", newBaseURL: "https://dashscope.aliyuncs.com/compatible-mode", clearKey: true},
	// Hunyuan's OpenAI-compatible endpoint uses console-issued API keys, not
	// the old TencentCloud signature pair.
	{from: channeltype.Tencent, to: channeltype.Tencent, oldDefault: "https://hunyuan.tencentcloudapi.com", newBaseURL: "https://api.hunyuan.cloud.tencent.com", clearKey: true},
	// The aggregator and proxy kinds were plain OpenAI wire all along; they
	// only move to the explicit custom kind with a full base URL.
	{from: channeltype.API2D, to: channeltype.OpenAICompatible, oldDefault: "https://oa.api2d.net", newBaseURL: "https://oa.api2d.net/v1"},
	{from: channeltype.CloseAI, to: channeltype.OpenAICompatible, oldDefault: "https://api.closeai-proxy.xyz", newBaseURL: "https://api.closeai-proxy.xyz/v1"},
	{from: channeltype.OpenAISB, to: channeltype.OpenAICompatible, oldDefault: "https://api.openai-sb.com", newBaseURL: "https://api.openai-sb.com/v1"},
	{from: channeltype.OpenAIMax, to: channeltype.OpenAICompatible, oldDefault: "https://api.openaimax.com", newBaseURL: "https://api.openaimax.com/v1"},
	{from: channeltype.OhMyGPT, to: channeltype.OpenAICompatible, oldDefault: "https://api.ohmygpt.com", newBaseURL: "https://api.ohmygpt.com/v1"},
	{from: channeltype.Ails, to: channeltype.OpenAICompatible, oldDefault: "https://api.caipacity.com", newBaseURL: "https://api.caipacity.com/v1"},
	{from: channeltype.AIProxy, to: channeltype.OpenAICompatible, oldDefault: "https://api.aiproxy.io", newBaseURL: "https://api.aiproxy.io/v1"},
	{from: channeltype.API2GPT, to: channeltype.OpenAICompatible, oldDefault: "https://api.api2gpt.com", newBaseURL: "https://api.api2gpt.com/v1"},
	{from: channeltype.AIGC2D, to: channeltype.OpenAICompatible, oldDefault: "https://api.aigc2d.com", newBaseURL: "https://api.aigc2d.com/v1"},
	// The embedded AI Proxy library kind becomes a custom OpenAI-compatible
	// channel pointed at the same host.
	{from: channeltype.AIProxyLibrary, to: channeltype.OpenAICompatible, oldDefault: "https://api.aiproxy.io", newBaseURL: "https://api.aiproxy.io/v1"},
}

// RetiredChannelType reports whether kind no longer has its own relay
// implementation: a channel created on it is silently rewritten to a
// different kind at the next boot, so channel creation must refuse it up
// front. Tencent is exempt — its migration row only rewrites legacy base
// URLs, the kind itself still accepts new channels.
func RetiredChannelType(kind int) (from int, to int, ok bool) {
	for _, m := range channelTypeMigrations {
		if m.from == kind && m.to != kind {
			return m.from, m.to, true
		}
	}
	return 0, 0, false
}

// MigrateChannelTypes moves channels off retired channel kinds. It runs once
// per boot on the master node after AutoMigrate and logs what it touched.
func MigrateChannelTypes() {
	for _, m := range channelTypeMigrations {
		rewritten := map[string]interface{}{"type": m.to}
		if m.clearKey {
			rewritten["key"] = ""
		}
		withDefault := map[string]interface{}{
			"type":     m.to,
			"base_url": m.newBaseURL,
		}
		if m.clearKey {
			withDefault["key"] = ""
		}

		// Rows on the retired default (or empty) base URL adopt the new
		// default; rows with a custom base URL keep it.
		result := DB.Model(&Channel{}).
			Where("type = ? AND (base_url IS NULL OR base_url = '' OR base_url = ?)", m.from, m.oldDefault).
			Updates(withDefault)
		if result.Error != nil {
			logger.SysError("channel type migration failed for type " + fmt.Sprintf("%d", m.from) + ": " + result.Error.Error())
			continue
		}
		if result.RowsAffected > 0 {
			logger.SysLog(fmt.Sprintf("migrated %d channel(s) from type %d to %d", result.RowsAffected, m.from, m.to))
		}

		remaining := DB.Model(&Channel{}).Where("type = ?", m.from).Updates(rewritten)
		if remaining.Error != nil {
			logger.SysError("channel type migration failed for type " + fmt.Sprintf("%d", m.from) + ": " + remaining.Error.Error())
			continue
		}
		if remaining.RowsAffected > 0 {
			logger.SysLog(fmt.Sprintf("migrated %d channel(s) from type %d to %d (custom base URL kept)", remaining.RowsAffected, m.from, m.to))
		}
	}
}
