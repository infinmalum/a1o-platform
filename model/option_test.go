package model

import (
	"testing"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestUpdateOptionMapBindings pins the declarative binding tables: each
// mechanical option key updates its config variable and the option map, and
// unknown keys only enter the option map.
func TestUpdateOptionMapBindings(t *testing.T) {
	restoredMap := config.OptionMap
	if config.OptionMap == nil {
		config.OptionMap = map[string]string{}
	}
	restored := struct {
		retryTimes       int
		passwordLogin    bool
		quotaForNewUser  int64
		quotaPerUnit     float64
		smtpServer       string
		upstreamPolicy   string
		chatMaxFileBytes int64
		sandboxTimeout   int
		channelDisable   float64
		automaticDisable bool
	}{
		retryTimes:       config.RetryTimes,
		passwordLogin:    config.PasswordLoginEnabled,
		quotaForNewUser:  config.QuotaForNewUser,
		quotaPerUnit:     config.QuotaPerUnit,
		smtpServer:       config.SMTPServer,
		upstreamPolicy:   config.UpstreamRoutingPolicy,
		chatMaxFileBytes: config.ChatMaxFileBytes,
		sandboxTimeout:   config.ChatSandboxTimeoutSeconds,
		channelDisable:   config.ChannelDisableThreshold,
		automaticDisable: config.AutomaticDisableChannelEnabled,
	}
	defer func() {
		config.RetryTimes = restored.retryTimes
		config.PasswordLoginEnabled = restored.passwordLogin
		config.QuotaForNewUser = restored.quotaForNewUser
		config.QuotaPerUnit = restored.quotaPerUnit
		config.SMTPServer = restored.smtpServer
		config.UpstreamRoutingPolicy = restored.upstreamPolicy
		config.ChatMaxFileBytes = restored.chatMaxFileBytes
		config.ChatSandboxTimeoutSeconds = restored.sandboxTimeout
		config.ChannelDisableThreshold = restored.channelDisable
		config.AutomaticDisableChannelEnabled = restored.automaticDisable
		config.OptionMap = restoredMap
	}()

	require.NoError(t, updateOptionMap("RetryTimes", "7"))
	require.EqualValues(t, 7, config.RetryTimes)
	require.Equal(t, "7", config.OptionMap["RetryTimes"])

	updateOptionMap("PasswordLoginEnabled", "false")
	require.False(t, config.PasswordLoginEnabled)

	require.NoError(t, updateOptionMap("QuotaForNewUser", "123456"))
	require.EqualValues(t, 123456, config.QuotaForNewUser)

	require.NoError(t, updateOptionMap("QuotaPerUnit", "1.5"))
	require.EqualValues(t, 1.5, config.QuotaPerUnit)

	require.NoError(t, updateOptionMap("SMTPServer", "smtp.example.com"))
	require.Equal(t, "smtp.example.com", config.SMTPServer)

	require.NoError(t, updateOptionMap("ChatMaxFileBytes", "4096"))
	require.EqualValues(t, 4096, config.ChatMaxFileBytes)

	require.NoError(t, updateOptionMap("ChatSandboxTimeoutSeconds", "99"))
	require.EqualValues(t, 99, config.ChatSandboxTimeoutSeconds)

	require.NoError(t, updateOptionMap("ChannelDisableThreshold", "2.5"))
	require.EqualValues(t, 2.5, config.ChannelDisableThreshold)

	updateOptionMap("AutomaticDisableChannelEnabled", "true")
	require.True(t, config.AutomaticDisableChannelEnabled)

	// The routing policy validates its value and leaves the config untouched
	// on failure; a rejected value must not enter the option map either.
	require.Error(t, updateOptionMap("UpstreamRoutingPolicy", "nope"))
	require.NotEqual(t, "nope", config.UpstreamRoutingPolicy)
	require.NotEqual(t, "nope", config.OptionMap["UpstreamRoutingPolicy"])

	// A non-integer value for an integer option is rejected outright: it must
	// neither zero the config variable (the old discarded-parse bug) nor land
	// in the option map.
	require.Error(t, updateOptionMap("QuotaForNewUser", "2.56"))
	require.EqualValues(t, 123456, config.QuotaForNewUser)
	require.NotEqual(t, "2.56", config.OptionMap["QuotaForNewUser"])

	require.Error(t, updateOptionMap("RetryTimes", "abc"))
	require.EqualValues(t, 7, config.RetryTimes)

	require.Error(t, updateOptionMap("QuotaPerUnit", "not-a-float"))
	require.EqualValues(t, 1.5, config.QuotaPerUnit)

	// Unknown keys are option-map-only display state.
	require.NoError(t, updateOptionMap("Notice", "hello"))
	require.Equal(t, "hello", config.OptionMap["Notice"])
}

// TestUpdateOptionRejectsInvalidValueBeforePersist pins the API-path repair:
// a value the bindings reject is returned as an error and never written to
// the options table, while a valid value is persisted.
func TestUpdateOptionRejectsInvalidValueBeforePersist(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })

	previousQuota := config.QuotaForNewUser
	restoredMap := config.OptionMap
	if config.OptionMap == nil {
		config.OptionMap = map[string]string{}
	}
	t.Cleanup(func() {
		config.QuotaForNewUser = previousQuota
		config.OptionMap = restoredMap
	})

	require.Error(t, UpdateOption("QuotaForNewUser", "2.56"))
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "QuotaForNewUser").Count(&count).Error)
	require.EqualValues(t, 0, count)

	require.NoError(t, UpdateOption("QuotaForNewUser", "123456"))
	require.EqualValues(t, 123456, config.QuotaForNewUser)
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "QuotaForNewUser").Count(&count).Error)
	require.EqualValues(t, 1, count)
	var stored Option
	require.NoError(t, DB.First(&stored, "key = ?", "QuotaForNewUser").Error)
	require.Equal(t, "123456", stored.Value)
}
