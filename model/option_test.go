package model

import (
	"testing"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/stretchr/testify/require"
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
	// on failure, while the option map still records what was sent.
	require.Error(t, updateOptionMap("UpstreamRoutingPolicy", "nope"))
	require.NotEqual(t, "nope", config.UpstreamRoutingPolicy)
	require.Equal(t, "nope", config.OptionMap["UpstreamRoutingPolicy"])

	// Unknown keys are option-map-only display state.
	require.NoError(t, updateOptionMap("Notice", "hello"))
	require.Equal(t, "hello", config.OptionMap["Notice"])
}
