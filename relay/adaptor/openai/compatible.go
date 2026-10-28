package openai

import (
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// CompatibleChannels lists the channel kinds whose OpenAI-compatible presets
// are served through this adapter's per-kind metadata. Every other vendor
// kind collapsed onto channeltype.OpenAICompatible, where admins enter the
// full version-prefixed base URL and fetch model lists from the upstream.
var CompatibleChannels = []int{
	channeltype.Azure,
	channeltype.Minimax,
}

// GetCompatibleChannelMeta returns the vendor display name used as the
// owned_by value in /v1/models. Model presets are gone: admins fetch the
// live list from the upstream or type model names themselves.
func GetCompatibleChannelMeta(channelType int) (string, []string) {
	switch channelType {
	case channeltype.Azure:
		return "azure", nil
	case channeltype.Minimax:
		return "minimax", nil
	case channeltype.OpenAICompatible:
		return "openai_compatible", nil
	default:
		return "openai", nil
	}
}
