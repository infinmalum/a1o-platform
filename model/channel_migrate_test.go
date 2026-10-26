package model

import (
	"testing"

	"github.com/infinmalum/one-gateway/relay/channeltype"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupChannelMigrateTest(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
}

func insertMigrateChannel(t *testing.T, channelType int, baseURL, key string) int {
	t.Helper()
	channel := Channel{Type: channelType, BaseURL: &baseURL, Key: key, Models: "test-model", Group: "default"}
	require.NoError(t, DB.Create(&channel).Error)
	return channel.Id
}

func migratedChannel(t *testing.T, id int) Channel {
	t.Helper()
	var channel Channel
	require.NoError(t, DB.First(&channel, "id = ?", id).Error)
	return channel
}

func TestMigrateChannelTypesRewritesRetiredKinds(t *testing.T) {
	setupChannelMigrateTest(t)
	palm := insertMigrateChannel(t, channeltype.PaLM, "", "google-key")
	baidu := insertMigrateChannel(t, channeltype.Baidu, "https://aip.baidubce.com", "old-signature")
	api2d := insertMigrateChannel(t, channeltype.API2D, "", "api2d-key")
	// Custom base URLs survive the rewrite and gain the version prefix.
	api2dMirror := insertMigrateChannel(t, channeltype.API2D, "https://mirror.example.com", "api2d-mirror-key")
	custom := insertMigrateChannel(t, channeltype.Custom, "https://relay.example.com/", "custom-key")
	// A base that already carries the prefix is left alone.
	prefixed := insertMigrateChannel(t, channeltype.Custom, "https://prefixed.example.com/v1", "custom-prefixed-key")
	ollama := insertMigrateChannel(t, channeltype.Ollama, "http://192.168.1.5:11434", "ollama-key")
	zhipu := insertMigrateChannel(t, channeltype.Zhipu, "", "zhipu-key")
	tencentDefault := insertMigrateChannel(t, channeltype.Tencent, "https://api.hunyuan.cloud.tencent.com", "signature-pair")
	tencentCustom := insertMigrateChannel(t, channeltype.Tencent, "https://hunyuan.internal.example.com", "signature-pair")
	geminiCompat := insertMigrateChannel(t, channeltype.GeminiOpenAICompatible, "https://generativelanguage.googleapis.com/v1beta/openai/", "gemini-key")
	// Modern kinds are left untouched.
	openai := insertMigrateChannel(t, channeltype.OpenAI, "", "openai-key")

	MigrateChannelTypes()

	palmRow := migratedChannel(t, palm)
	require.Equal(t, channeltype.OpenAICompatible, palmRow.Type)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/openai", *palmRow.BaseURL)
	require.Equal(t, "google-key", palmRow.Key)

	baiduRow := migratedChannel(t, baidu)
	require.Equal(t, channeltype.OpenAICompatible, baiduRow.Type)
	require.Equal(t, "https://qianfan.baidubce.com/v2", *baiduRow.BaseURL)
	require.Empty(t, baiduRow.Key, "incompatible credentials are dropped")

	api2dRow := migratedChannel(t, api2d)
	require.Equal(t, channeltype.OpenAICompatible, api2dRow.Type)
	require.Equal(t, "https://oa.api2d.net/v1", *api2dRow.BaseURL)

	require.Equal(t, "https://mirror.example.com/v1", *migratedChannel(t, api2dMirror).BaseURL)
	require.Equal(t, "https://relay.example.com/v1", *migratedChannel(t, custom).BaseURL)
	require.Equal(t, "https://prefixed.example.com/v1", *migratedChannel(t, prefixed).BaseURL)
	require.Equal(t, "http://192.168.1.5:11434/v1", *migratedChannel(t, ollama).BaseURL)

	zhipuRow := migratedChannel(t, zhipu)
	require.Equal(t, channeltype.OpenAICompatible, zhipuRow.Type)
	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4", *zhipuRow.BaseURL)

	tencentRow := migratedChannel(t, tencentDefault)
	require.Equal(t, "https://api.hunyuan.cloud.tencent.com/v1", *tencentRow.BaseURL)
	require.Empty(t, tencentRow.Key, "the TencentCloud signature pair is dropped")
	require.Equal(t, "https://hunyuan.internal.example.com/v1", *migratedChannel(t, tencentCustom).BaseURL)

	geminiRow := migratedChannel(t, geminiCompat)
	require.Equal(t, channeltype.OpenAICompatible, geminiRow.Type)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/openai", *geminiRow.BaseURL)

	require.Equal(t, channeltype.OpenAI, migratedChannel(t, openai).Type)

	// Re-running is a no-op.
	MigrateChannelTypes()
	var compatibleCount int64
	DB.Model(&Channel{}).Where("type = ?", channeltype.OpenAICompatible).Count(&compatibleCount)
	require.EqualValues(t, 11, compatibleCount)
	var openaiCount int64
	DB.Model(&Channel{}).Where("type = ?", channeltype.OpenAI).Count(&openaiCount)
	require.EqualValues(t, 1, openaiCount)
}
