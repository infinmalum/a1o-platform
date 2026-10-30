package model

import (
	"fmt"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"strconv"
	"strings"
	"time"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	config.OptionMapRWMutex.Lock()
	config.OptionMap = make(map[string]string)
	config.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(config.PasswordLoginEnabled)
	config.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(config.PasswordRegisterEnabled)
	config.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(config.EmailVerificationEnabled)
	config.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(config.GitHubOAuthEnabled)
	config.OptionMap["OidcEnabled"] = strconv.FormatBool(config.OidcEnabled)
	config.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(config.TurnstileCheckEnabled)
	config.OptionMap["RegisterEnabled"] = strconv.FormatBool(config.RegisterEnabled)
	config.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(config.AutomaticDisableChannelEnabled)
	config.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(config.AutomaticEnableChannelEnabled)
	config.OptionMap["ApproximateTokenEnabled"] = strconv.FormatBool(config.ApproximateTokenEnabled)
	config.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(config.LogConsumeEnabled)
	config.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(config.DisplayInCurrencyEnabled)
	config.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(config.DisplayTokenStatEnabled)
	config.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(config.ChannelDisableThreshold, 'f', -1, 64)
	config.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(config.EmailDomainRestrictionEnabled)
	config.OptionMap["EmailDomainWhitelist"] = strings.Join(config.EmailDomainWhitelist, ",")
	config.OptionMap["SMTPServer"] = ""
	config.OptionMap["SMTPFrom"] = ""
	config.OptionMap["SMTPPort"] = strconv.Itoa(config.SMTPPort)
	config.OptionMap["SMTPAccount"] = ""
	config.OptionMap["SMTPToken"] = ""
	config.OptionMap["Notice"] = ""
	config.OptionMap["About"] = ""
	config.OptionMap["HomePageContent"] = ""
	config.OptionMap["Footer"] = config.Footer
	config.OptionMap["SystemName"] = config.SystemName
	config.OptionMap["Logo"] = config.Logo
	config.OptionMap["ServerAddress"] = ""
	config.OptionMap["GitHubClientId"] = ""
	config.OptionMap["GitHubClientSecret"] = ""
	config.OptionMap["MessagePusherAddress"] = ""
	config.OptionMap["MessagePusherToken"] = ""
	config.OptionMap["TurnstileSiteKey"] = ""
	config.OptionMap["TurnstileSecretKey"] = ""
	config.OptionMap["QuotaForNewUser"] = strconv.FormatInt(config.QuotaForNewUser, 10)
	config.OptionMap["QuotaForInviter"] = strconv.FormatInt(config.QuotaForInviter, 10)
	config.OptionMap["QuotaForInvitee"] = strconv.FormatInt(config.QuotaForInvitee, 10)
	config.OptionMap["QuotaRemindThreshold"] = strconv.FormatInt(config.QuotaRemindThreshold, 10)
	config.OptionMap["PreConsumedQuota"] = strconv.FormatInt(config.PreConsumedQuota, 10)
	config.OptionMap["ModelRatio"] = billingratio.ModelRatio2JSONString()
	config.OptionMap["GroupRatio"] = billingratio.GroupRatio2JSONString()
	config.OptionMap["CompletionRatio"] = billingratio.CompletionRatio2JSONString()
	config.OptionMap["TopUpLink"] = config.TopUpLink
	config.OptionMap["ChatLink"] = config.ChatLink
	config.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(config.QuotaPerUnit, 'f', -1, 64)
	config.OptionMap["RetryTimes"] = strconv.Itoa(config.RetryTimes)
	config.OptionMap["Theme"] = config.Theme
	config.OptionMap["UpstreamRoutingPolicy"] = config.UpstreamRoutingPolicy
	config.OptionMap["ChatSandboxEnabled"] = strconv.FormatBool(config.ChatSandboxEnabled)
	config.OptionMap["ChatSandboxNetworkEnabled"] = strconv.FormatBool(config.ChatSandboxNetworkEnabled)
	config.OptionMap["ChatSandboxTimeoutSeconds"] = strconv.Itoa(config.ChatSandboxTimeoutSeconds)
	config.OptionMap["ChatContextBudgetTokens"] = strconv.Itoa(config.ChatContextBudgetTokens)
	config.OptionMap["ChatMaxToolIterations"] = strconv.Itoa(config.ChatMaxToolIterations)
	config.OptionMap["ChatMaxFileBytes"] = strconv.FormatInt(config.ChatMaxFileBytes, 10)
	config.OptionMap["ChatMCPStdioEnabled"] = strconv.FormatBool(config.ChatMCPStdioEnabled)
	config.OptionMap["ChatMCPStdioAllowlist"] = config.ChatMCPStdioAllowlist
	config.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, _ := AllOption()
	for _, option := range options {
		err := updateOptionMap(option.Key, option.Value)
		if err != nil {
			logger.SysError("failed to update option map: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func UpdateOption(key string, value string) error {
	// Apply to the in-memory state first: a value the bindings reject is
	// reported back without ever reaching the database, so no half-valid
	// "2.56" can linger in the table for the next sync to trip over.
	if err := updateOptionMap(key, value); err != nil {
		return err
	}
	option := Option{
		Key: key,
	}
	// https://gorm.io/docs/update.html#Save-All-Fields
	DB.FirstOrCreate(&option, Option{Key: key})
	option.Value = value
	// Save is a combination function.
	// If save value does not contain primary key, it will execute Create,
	// otherwise it will execute Update (with all fields).
	return DB.Save(&option).Error
}

// Option binding tables map option keys onto their config variables so
// updateOptionMap stays declarative: adding an option means declaring the
// config variable and adding one binding, not another switch case.

var boolOptionBindings = map[string]*bool{
	"ApproximateTokenEnabled":        &config.ApproximateTokenEnabled,
	"AutomaticDisableChannelEnabled": &config.AutomaticDisableChannelEnabled,
	"AutomaticEnableChannelEnabled":  &config.AutomaticEnableChannelEnabled,
	"ChatMCPStdioEnabled":            &config.ChatMCPStdioEnabled,
	"ChatSandboxEnabled":             &config.ChatSandboxEnabled,
	"ChatSandboxNetworkEnabled":      &config.ChatSandboxNetworkEnabled,
	"DisplayInCurrencyEnabled":       &config.DisplayInCurrencyEnabled,
	"DisplayTokenStatEnabled":        &config.DisplayTokenStatEnabled,
	"EmailDomainRestrictionEnabled":  &config.EmailDomainRestrictionEnabled,
	"EmailVerificationEnabled":       &config.EmailVerificationEnabled,
	"GitHubOAuthEnabled":             &config.GitHubOAuthEnabled,
	"LogConsumeEnabled":              &config.LogConsumeEnabled,
	"OidcEnabled":                    &config.OidcEnabled,
	"PasswordLoginEnabled":           &config.PasswordLoginEnabled,
	"PasswordRegisterEnabled":        &config.PasswordRegisterEnabled,
	"RegisterEnabled":                &config.RegisterEnabled,
	"TurnstileCheckEnabled":          &config.TurnstileCheckEnabled,
}

var stringOptionBindings = map[string]*string{
	"ChatLink":                    &config.ChatLink,
	"ChatMCPStdioAllowlist":       &config.ChatMCPStdioAllowlist,
	"Footer":                      &config.Footer,
	"GitHubClientId":              &config.GitHubClientId,
	"GitHubClientSecret":          &config.GitHubClientSecret,
	"LarkClientId":                &config.LarkClientId,
	"LarkClientSecret":            &config.LarkClientSecret,
	"Logo":                        &config.Logo,
	"MessagePusherAddress":        &config.MessagePusherAddress,
	"MessagePusherToken":          &config.MessagePusherToken,
	"OidcAuthorizationEndpoint":   &config.OidcAuthorizationEndpoint,
	"OidcClientId":                &config.OidcClientId,
	"OidcClientSecret":            &config.OidcClientSecret,
	"OidcTokenEndpoint":           &config.OidcTokenEndpoint,
	"OidcUserinfoEndpoint":        &config.OidcUserinfoEndpoint,
	"OidcWellKnown":               &config.OidcWellKnown,
	"SMTPAccount":                 &config.SMTPAccount,
	"SMTPFrom":                    &config.SMTPFrom,
	"SMTPServer":                  &config.SMTPServer,
	"SMTPToken":                   &config.SMTPToken,
	"ServerAddress":               &config.ServerAddress,
	"SystemName":                  &config.SystemName,
	"Theme":                       &config.Theme,
	"TopUpLink":                   &config.TopUpLink,
	"TurnstileSecretKey":          &config.TurnstileSecretKey,
	"TurnstileSiteKey":            &config.TurnstileSiteKey,
}

var intOptionBindings = map[string]*int{
	"ChatContextBudgetTokens":   &config.ChatContextBudgetTokens,
	"ChatMaxToolIterations":     &config.ChatMaxToolIterations,
	"ChatSandboxTimeoutSeconds": &config.ChatSandboxTimeoutSeconds,
	"RetryTimes":                &config.RetryTimes,
	"SMTPPort":                  &config.SMTPPort,
}

var int64OptionBindings = map[string]*int64{
	"ChatMaxFileBytes":     &config.ChatMaxFileBytes,
	"PreConsumedQuota":     &config.PreConsumedQuota,
	"QuotaForInvitee":      &config.QuotaForInvitee,
	"QuotaForInviter":      &config.QuotaForInviter,
	"QuotaForNewUser":      &config.QuotaForNewUser,
	"QuotaRemindThreshold": &config.QuotaRemindThreshold,
}

var floatOptionBindings = map[string]*float64{
	"ChannelDisableThreshold": &config.ChannelDisableThreshold,
	"QuotaPerUnit":            &config.QuotaPerUnit,
}

// customOptionSetters holds the keys whose update does more than assign one
// config variable.
var customOptionSetters = map[string]func(value string) error{
	"CompletionRatio": billingratio.UpdateCompletionRatioByJSONString,
	"GroupRatio":      billingratio.UpdateGroupRatioByJSONString,
	"ModelRatio":      billingratio.UpdateModelRatioByJSONString,
	"EmailDomainWhitelist": func(value string) error {
		config.EmailDomainWhitelist = strings.Split(value, ",")
		return nil
	},
	"UpstreamRoutingPolicy": func(value string) error {
		if !config.ValidUpstreamRoutingPolicies[value] {
			return fmt.Errorf("invalid upstream routing policy: %s", value)
		}
		config.UpstreamRoutingPolicy = value
		return nil
	},
}

// updateOptionMap validates before it mutates: a value that fails its
// binding's parse leaves both the config variables and the option map
// untouched, so bad input can neither silently zero a setting nor linger in
// the display state for the next sync to trip over.
func updateOptionMap(key string, value string) (err error) {
	config.OptionMapRWMutex.Lock()
	defer config.OptionMapRWMutex.Unlock()
	if setter, ok := customOptionSetters[key]; ok {
		if err := setter(value); err != nil {
			return err
		}
		config.OptionMap[key] = value
		return nil
	}
	if binding, ok := boolOptionBindings[key]; ok {
		*binding = value == "true"
		config.OptionMap[key] = value
		return nil
	}
	if binding, ok := stringOptionBindings[key]; ok {
		*binding = value
		config.OptionMap[key] = value
		return nil
	}
	if binding, ok := intOptionBindings[key]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("option %s expects an integer, got %q", key, value)
		}
		*binding = parsed
		config.OptionMap[key] = value
		return nil
	}
	if binding, ok := int64OptionBindings[key]; ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("option %s expects an integer, got %q", key, value)
		}
		*binding = parsed
		config.OptionMap[key] = value
		return nil
	}
	if binding, ok := floatOptionBindings[key]; ok {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("option %s expects a number, got %q", key, value)
		}
		*binding = parsed
		config.OptionMap[key] = value
		return nil
	}
	config.OptionMap[key] = value
	return nil
}
