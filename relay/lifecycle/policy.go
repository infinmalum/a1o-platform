package lifecycle

import (
	"net/http"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
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
		return channel, nil
	}
	var channel *model.Channel
	var err error
	if input.Type >= 0 {
		channel, err = model.GetRandomSatisfiedChannelByTypeExcluding(input.Group, input.Model, input.Type, input.IgnorePriority, input.Excluded, input.Source)
	} else if len(input.Excluded) == 0 {
		channel, err = model.CacheGetRandomSatisfiedChannel(input.Group, input.Model, input.IgnorePriority, input.Source)
	} else {
		channel, err = model.GetRandomSatisfiedChannelExcluding(input.Group, input.Model, input.IgnorePriority, input.Excluded, input.Source)
	}
	if err != nil || channel == nil {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "no compatible channel for this model"}
	}
	return channel, nil
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

// Retryable applies equally to native and legacy forwarding. A pinned
// channel, cancelled request, or response that has started cannot be retried.
func Retryable(status int, outputStarted, pinned, cancelled bool) bool {
	if outputStarted || pinned || cancelled {
		return false
	}
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
}
