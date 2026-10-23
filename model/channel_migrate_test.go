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

func insertMigrateChannel(t *testing.T, channelType int, baseURL, key string) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Type: channelType, BaseURL: &baseURL, Key: key, Models: "test-model", Group: "default"}).Error)
}

func TestMigrateChannelTypesRewritesRetiredKinds(t *testing.T) {
	setupChannelMigrateTest(t)
	insertMigrateChannel(t, channeltype.PaLM, "", "google-key")
	insertMigrateChannel(t, channeltype.Baidu, "https://aip.baidubce.com", "old-signature")
	insertMigrateChannel(t, channeltype.API2D, "", "api2d-key")
	// A custom base URL must survive the rewrite.
	insertMigrateChannel(t, channeltype.API2D, "https://mirror.example.com", "api2d-key")
	// Modern kinds are left untouched.
	insertMigrateChannel(t, channeltype.OpenAI, "", "openai-key")

	MigrateChannelTypes()

	var count int64
	DB.Model(&Channel{}).Where("type = ?", channeltype.GeminiOpenAICompatible).Count(&count)
	require.EqualValues(t, 1, count)

	var baidu Channel
	require.NoError(t, DB.First(&baidu, "type = ?", channeltype.BaiduV2).Error)
	require.Empty(t, baidu.Key, "incompatible credentials are dropped")
	require.NotNil(t, baidu.BaseURL)
	require.Equal(t, "https://qianfan.baidubce.com", *baidu.BaseURL)

	var api2d Channel
	require.NoError(t, DB.First(&api2d, "type = ? AND base_url = ?", channeltype.OpenAICompatible, "https://oa.api2d.net/v1").Error)
	require.Equal(t, "api2d-key", api2d.Key)

	// A fresh dest: First folds a non-zero primary key back into the WHERE.
	var api2dMirror Channel
	require.NoError(t, DB.First(&api2dMirror, "type = ? AND base_url = ?", channeltype.OpenAICompatible, "https://mirror.example.com").Error)
	require.Equal(t, "api2d-key", api2dMirror.Key)

	DB.Model(&Channel{}).Where("type = ?", channeltype.OpenAI).Count(&count)
	require.EqualValues(t, 1, count)

	// Re-running is a no-op.
	MigrateChannelTypes()
	DB.Model(&Channel{}).Where("type = ?", channeltype.OpenAICompatible).Count(&count)
	require.EqualValues(t, 2, count)
}
