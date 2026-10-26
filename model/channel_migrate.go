package model

import (
	"fmt"
	"strings"

	"github.com/infinmalum/one-gateway/common/logger"
	"github.com/infinmalum/one-gateway/relay/channeltype"
)

// channelTypeMigration rewrites channels whose kind rode a removed per-vendor
// adapter onto the generic OpenAI-compatible kind (50). The destination
// convention carries the full version prefix in base_url and strips /v1 from
// the request path, so retired kinds whose wire mounted chat under a version
// path must rebase their URLs. Every statement uses bound parameters; each
// rule is idempotent because it rewrites only rows still sitting on the
// retired type.
type channelTypeMigration struct {
	from int
	to   int
	// oldDefaults lists the retired kind's default base URLs (including the
	// value a previous migration may have written); rows on one of them, or
	// with an empty base URL, adopt newBaseURL. Custom base URLs are kept and
	// rebased instead.
	oldDefaults []string
	newBaseURL  string
	// appendPath is appended to surviving custom base URLs (after trimming a
	// trailing slash) so they carry the version prefix the destination kind
	// expects. Empty means the kind's custom bases already follow the
	// destination convention.
	appendPath string
	// clearKey drops stored credentials that only worked against the retired
	// native protocol (signature pairs, scoped tokens) so the channel fails
	// an explicit auth error instead of a confusing one.
	clearKey bool
}

const geminiOpenAICompatBase = "https://generativelanguage.googleapis.com/v1beta/openai"

var channelTypeMigrations = []channelTypeMigration{
	// PaLM died with Google's generative language v1; the Gemini OpenAI
	// compatibility layer is the successor.
	{from: channeltype.PaLM, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://generativelanguage.googleapis.com"},
		newBaseURL:  geminiOpenAICompatBase, appendPath: "/v1beta/openai"},
	// Qianfan v2 / Spark OpenAI / Bailian compatible-mode all take new keys.
	{from: channeltype.Baidu, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://aip.baidubce.com"},
		newBaseURL:  "https://qianfan.baidubce.com/v2", appendPath: "/v2", clearKey: true},
	{from: channeltype.Xunfei, to: channeltype.OpenAICompatible,
		newBaseURL: "https://spark-api-open.xf-yun.com/v1", appendPath: "/v1", clearKey: true},
	{from: channeltype.Ali, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com/compatible-mode"},
		newBaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1", appendPath: "/compatible-mode/v1", clearKey: true},
	// Hunyuan's OpenAI-compatible endpoint uses console-issued API keys, not
	// the old TencentCloud signature pair.
	{from: channeltype.Tencent, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.hunyuan.cloud.tencent.com", "https://hunyuan.tencentcloudapi.com"},
		newBaseURL:  "https://api.hunyuan.cloud.tencent.com/v1", appendPath: "/v1", clearKey: true},

	// The aggregator and proxy kinds were plain OpenAI wire all along; they
	// only move to the explicit compatible kind, whose convention folds the
	// /v1 version prefix into base_url.
	{from: channeltype.API2D, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://oa.api2d.net"},
		newBaseURL:  "https://oa.api2d.net/v1", appendPath: "/v1"},
	{from: channeltype.CloseAI, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.closeai-proxy.xyz"},
		newBaseURL:  "https://api.closeai-proxy.xyz/v1", appendPath: "/v1"},
	{from: channeltype.OpenAISB, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.openai-sb.com"},
		newBaseURL:  "https://api.openai-sb.com/v1", appendPath: "/v1"},
	{from: channeltype.OpenAIMax, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.openaimax.com"},
		newBaseURL:  "https://api.openaimax.com/v1", appendPath: "/v1"},
	{from: channeltype.OhMyGPT, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.ohmygpt.com"},
		newBaseURL:  "https://api.ohmygpt.com/v1", appendPath: "/v1"},
	{from: channeltype.Ails, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.caipacity.com"},
		newBaseURL:  "https://api.caipacity.com/v1", appendPath: "/v1"},
	{from: channeltype.AIProxy, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.aiproxy.io"},
		newBaseURL:  "https://api.aiproxy.io/v1", appendPath: "/v1"},
	{from: channeltype.API2GPT, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.api2gpt.com"},
		newBaseURL:  "https://api.api2gpt.com/v1", appendPath: "/v1"},
	{from: channeltype.AIGC2D, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.aigc2d.com"},
		newBaseURL:  "https://api.aigc2d.com/v1", appendPath: "/v1"},
	// The embedded AI Proxy library kind becomes a custom OpenAI-compatible
	// channel pointed at the same host.
	{from: channeltype.AIProxyLibrary, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.aiproxy.io"},
		newBaseURL:  "https://api.aiproxy.io/v1", appendPath: "/v1"},

	// Vendor kinds whose official OpenAI-compatible endpoints either sit on
	// the bare host (append /v1) or mount the wire under a version path
	// (append that path); the destination kind strips /v1 from the request
	// path instead of rewriting it per kind.
	{from: channeltype.AI360, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://ai.360.cn"},
		newBaseURL:  "https://ai.360.cn/v1", appendPath: "/v1"},
	{from: channeltype.OpenRouter, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://openrouter.ai/api"},
		newBaseURL:  "https://openrouter.ai/api/v1", appendPath: "/v1"},
	{from: channeltype.FastGPT, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://fastgpt.run/api/openapi"},
		newBaseURL:  "https://fastgpt.run/api/openapi/v1", appendPath: "/v1"},
	{from: channeltype.Moonshot, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.moonshot.cn"},
		newBaseURL:  "https://api.moonshot.cn/v1", appendPath: "/v1"},
	{from: channeltype.Baichuan, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.baichuan-ai.com"},
		newBaseURL:  "https://api.baichuan-ai.com/v1", appendPath: "/v1"},
	{from: channeltype.Mistral, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.mistral.ai"},
		newBaseURL:  "https://api.mistral.ai/v1", appendPath: "/v1"},
	{from: channeltype.Groq, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.groq.com/openai"},
		newBaseURL:  "https://api.groq.com/openai/v1", appendPath: "/v1"},
	{from: channeltype.Ollama, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"http://localhost:11434"},
		newBaseURL:  "http://localhost:11434/v1", appendPath: "/v1"},
	{from: channeltype.LingYiWanWu, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.lingyiwanwu.com"},
		newBaseURL:  "https://api.lingyiwanwu.com/v1", appendPath: "/v1"},
	{from: channeltype.StepFun, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.stepfun.com"},
		newBaseURL:  "https://api.stepfun.com/v1", appendPath: "/v1"},
	{from: channeltype.DeepSeek, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.deepseek.com"},
		newBaseURL:  "https://api.deepseek.com/v1", appendPath: "/v1"},
	{from: channeltype.TogetherAI, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.together.xyz"},
		newBaseURL:  "https://api.together.xyz/v1", appendPath: "/v1"},
	{from: channeltype.SiliconFlow, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.siliconflow.cn"},
		newBaseURL:  "https://api.siliconflow.cn/v1", appendPath: "/v1"},
	{from: channeltype.XAI, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.x.ai"},
		newBaseURL:  "https://api.x.ai/v1", appendPath: "/v1"},
	// The plain custom kind already pointed at the bare host; its rows gain
	// the /v1 prefix the compatible kind expects.
	{from: channeltype.Custom, to: channeltype.OpenAICompatible,
		newBaseURL: "", appendPath: "/v1"},
	{from: channeltype.Doubao, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://ark.cn-beijing.volces.com"},
		newBaseURL:  "https://ark.cn-beijing.volces.com/api/v3", appendPath: "/api/v3"},
	// Novita's default base already carries the full path prefix.
	{from: channeltype.Novita, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.novita.ai/v3/openai"},
		newBaseURL:  "https://api.novita.ai/v3/openai"},
	{from: channeltype.BaiduV2, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://qianfan.baidubce.com"},
		newBaseURL:  "https://qianfan.baidubce.com/v2", appendPath: "/v2"},
	{from: channeltype.XunfeiV2, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://spark-api-open.xf-yun.com"},
		newBaseURL:  "https://spark-api-open.xf-yun.com/v1", appendPath: "/v1"},
	{from: channeltype.AliBailian, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com/compatible-mode"},
		newBaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1", appendPath: "/compatible-mode/v1"},
	// The Gemini OpenAI-compatible kind follows the destination convention
	// already; only the kind number moves.
	{from: channeltype.GeminiOpenAICompatible, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://generativelanguage.googleapis.com/v1beta/openai/"},
		newBaseURL:  geminiOpenAICompatBase},
	{from: channeltype.Zhipu, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://open.bigmodel.cn"},
		newBaseURL:  "https://open.bigmodel.cn/api/paas/v4", appendPath: "/api/paas/v4"},
	{from: channeltype.Cohere, to: channeltype.OpenAICompatible,
		oldDefaults: []string{"https://api.cohere.ai"},
		newBaseURL:  "https://api.cohere.ai/compatibility/v1", appendPath: "/compatibility/v1"},
}

// RetiredChannelType reports whether kind no longer has its own relay
// implementation: a channel created on it is silently rewritten to a
// different kind at the next boot, so channel creation must refuse it up
// front. Minimax keeps its own adapter and stays creatable.
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
		migrateDefaultBase(m)
		migrateCustomBase(m)
	}
}

// migrateDefaultBase rewrites rows sitting on the retired kind's default (or
// an empty) base URL onto the destination default.
func migrateDefaultBase(m channelTypeMigration) {
	condition := "(base_url IS NULL OR base_url = '')"
	for range m.oldDefaults {
		condition += " OR base_url = ?"
	}
	args := make([]interface{}, 0, len(m.oldDefaults)+1)
	args = append(args, m.from)
	for _, d := range m.oldDefaults {
		args = append(args, d)
	}
	updates := map[string]interface{}{
		"type":     m.to,
		"base_url": m.newBaseURL,
	}
	if m.clearKey {
		updates["key"] = ""
	}
	result := DB.Model(&Channel{}).Where("type = ? AND "+condition, args...).Updates(updates)
	if result.Error != nil {
		logger.SysError("channel type migration failed for type " + fmt.Sprintf("%d", m.from) + ": " + result.Error.Error())
		return
	}
	if result.RowsAffected > 0 {
		logger.SysLog(fmt.Sprintf("migrated %d channel(s) from type %d to %d", result.RowsAffected, m.from, m.to))
	}
}

// migrateCustomBase flips the kind of rows on a custom base URL, appending
// the destination kind's version prefix. The transform runs in Go so the
// string concatenation stays portable across SQLite, MySQL, and Postgres.
func migrateCustomBase(m channelTypeMigration) {
	var leftovers []Channel
	if err := DB.Where("type = ?", m.from).Find(&leftovers).Error; err != nil {
		logger.SysError("channel type migration failed for type " + fmt.Sprintf("%d", m.from) + ": " + err.Error())
		return
	}
	migrated := 0
	for _, row := range leftovers {
		updates := map[string]interface{}{"type": m.to}
		if m.clearKey {
			updates["key"] = ""
		}
		if m.appendPath != "" {
			base := ""
			if row.BaseURL != nil {
				base = *row.BaseURL
			}
			base = strings.TrimSuffix(base, "/")
			if !strings.HasSuffix(base, m.appendPath) {
				base += m.appendPath
			}
			updates["base_url"] = base
		}
		if err := DB.Model(&Channel{}).Where("id = ?", row.Id).Updates(updates).Error; err != nil {
			logger.SysError("channel type migration failed for channel " + fmt.Sprintf("%d", row.Id) + ": " + err.Error())
			continue
		}
		migrated++
	}
	if migrated > 0 {
		logger.SysLog(fmt.Sprintf("migrated %d channel(s) from type %d to %d (custom base URL kept)", migrated, m.from, m.to))
	}
}
