package openai

import (
	"github.com/infinmalum/one-gateway/relay/adaptor/minimax"
	"github.com/infinmalum/one-gateway/relay/channeltype"
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
// owned_by value in /v1/models and the preset model list for a kind.
// OpenAICompatible ships no presets: the admin fetches them from the
// upstream instead.
func GetCompatibleChannelMeta(channelType int) (string, []string) {
	switch channelType {
	case channeltype.Azure:
		return "azure", ModelList
	case channeltype.Minimax:
		return "minimax", minimax.ModelList
	case channeltype.OpenAICompatible:
		return "openai_compatible", nil
	default:
		return "openai", ModelList
	}
}
