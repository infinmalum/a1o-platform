package lifecycle

import (
	"net/http"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/account"
	"github.com/infinmalum/one-gateway/relay/native"
	"github.com/infinmalum/one-gateway/relay/upstream"
)

// Selection describes channel requirements independently of the client
// protocol and the HTTP framework. Type < 0 accepts any enabled channel.
// Source selects the credential source of candidates; its zero value keeps
// the historical API-key-only behavior.
type Selection struct {
	Group          string
	Model          string
	Type           int
	SpecificID     int
	IgnorePriority bool
	Excluded       map[int]bool
	Source         model.ChannelSource
}

// maxReadinessProbes bounds how many unready subscription channels one
// selection skips before reporting no compatible channel.
const maxReadinessProbes = 8

func SelectChannel(input Selection) (*model.Channel, *HTTPError) {
	if input.SpecificID != 0 {
		if input.SpecificID < 0 {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "invalid channel ID"}
		}
		channel, err := model.GetChannelById(input.SpecificID, true)
		if err != nil || channel == nil {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "invalid channel ID"}
		}
		if channel.Status != model.ChannelStatusEnabled {
			return nil, &HTTPError{Status: http.StatusForbidden, Message: "selected channel is disabled"}
		}
		if input.Type >= 0 && channel.Type != input.Type {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "selected channel does not support this protocol"}
		}
		// Pinned channels follow the same source policy as the selector.
		if !sourceAccepts(input.Source, channel.UpstreamAccountID != nil) {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "selected channel is not eligible under the current routing policy"}
		}
		if readinessErr := checkChannelReadiness(channel, input.Model); readinessErr != nil {
			return nil, readinessErr
		}
		return channel, nil
	}
	excluded := input.Excluded
	probing := false
	for probe := 0; probe < maxReadinessProbes; probe++ {
		channel, err := selectOnce(input, excluded)
		if err != nil || channel == nil {
			return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "no compatible channel for this model"}
		}
		if readinessErr := checkChannelReadiness(channel, input.Model); readinessErr == nil {
			return channel, nil
		}
		// An unready subscription channel is skipped like an excluded one:
		// its account is disabled, cooling down, or not configurable. The
		// probe set stays request-local so caller state is not mutated.
		if !probing {
			excluded = make(map[int]bool, len(input.Excluded)+1)
			for id := range input.Excluded {
				excluded[id] = true
			}
			probing = true
		}
		excluded[channel.Id] = true
	}
	return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "no compatible channel for this model"}
}

// selectOnce runs one candidate selection with the given exclusion set.
func selectOnce(input Selection, excluded map[int]bool) (*model.Channel, error) {
	if input.Type >= 0 {
		return model.GetRandomSatisfiedChannelByTypeExcluding(input.Group, input.Model, input.Type, input.IgnorePriority, excluded, input.Source)
	}
	if len(excluded) == 0 {
		return model.CacheGetRandomSatisfiedChannel(input.Group, input.Model, input.IgnorePriority, input.Source)
	}
	return model.GetRandomSatisfiedChannelExcluding(input.Group, input.Model, input.IgnorePriority, excluded, input.Source)
}

// checkChannelReadiness verifies that a channel bound to a subscription
// account can serve requests right now: the account is enabled, its profile
// is registered, credential sealing is configured, and it is not cooling down
// for the requested model.
func checkChannelReadiness(channel *model.Channel, modelName string) *HTTPError {
	if channel.UpstreamAccountID == nil {
		return nil
	}
	accountID := *channel.UpstreamAccountID
	accountRow, err := model.GetUpstreamAccountById(accountID, false)
	if err != nil {
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account could not be loaded"}
	}
	if accountRow.Status != model.UpstreamAccountStatusEnabled {
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account is not enabled"}
	}
	profile, ok := upstream.Lookup(accountRow.Profile)
	if !ok || !profile.Subscription {
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account profile is not available"}
	}
	if !credentialProvider().Enabled() {
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "subscription credentials are not configured"}
	}
	if account.DefaultCooldowns.Active(accountID, modelName) {
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account is cooling down"}
	}
	return nil
}

func sourceAccepts(source model.ChannelSource, subscriptionBound bool) bool {
	switch source {
	case model.ChannelSourceAPIOnly:
		return !subscriptionBound
	case model.ChannelSourceSubscriptionOnly:
		return subscriptionBound
	}
	return true
}

// InitialSource returns the candidate source for the first attempt under the
// configured routing policy.
func InitialSource() model.ChannelSource {
	switch config.UpstreamRoutingPolicy {
	case config.RoutingPolicySubscriptionOnly, config.RoutingPolicySubscriptionThenAPI:
		return model.ChannelSourceSubscriptionOnly
	case config.RoutingPolicyMixed:
		return model.ChannelSourceAny
	default:
		return model.ChannelSourceAPIOnly
	}
}

// RetrySources returns the candidate sources for retries, in preference
// order. subscription_then_api prefers remaining subscriptions and then
// healthy API channels; the initial selection in Forward handles the
// no-subscription-at-all case.
func RetrySources() []model.ChannelSource {
	switch config.UpstreamRoutingPolicy {
	case config.RoutingPolicySubscriptionOnly:
		return []model.ChannelSource{model.ChannelSourceSubscriptionOnly}
	case config.RoutingPolicySubscriptionThenAPI:
		return []model.ChannelSource{model.ChannelSourceSubscriptionOnly, model.ChannelSourceAPIOnly}
	case config.RoutingPolicyMixed:
		return []model.ChannelSource{model.ChannelSourceAny}
	default:
		return []model.ChannelSource{model.ChannelSourceAPIOnly}
	}
}

// ChatUpstreamProtocols returns the initial upstream protocol for Chat
// Completions inbound requests and the candidates tried when no channel for
// it exists. Subscription profiles serve the Responses protocol only, so
// subscription-first routing bridges Chat onto Responses; API-first routing
// forwards Chat natively.
func ChatUpstreamProtocols() (native.Protocol, []native.Protocol) {
	switch config.UpstreamRoutingPolicy {
	case config.RoutingPolicySubscriptionOnly, config.RoutingPolicySubscriptionThenAPI:
		return native.OpenAIResponses, []native.Protocol{native.OpenAIChat}
	default:
		return native.OpenAIChat, nil
	}
}

// Retryable applies equally to native and legacy forwarding. A pinned
// channel, cancelled request, or response that has started cannot be retried.
func Retryable(status int, outputStarted, pinned, cancelled bool) bool {
	if outputStarted || pinned || cancelled {
		return false
	}
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
}
