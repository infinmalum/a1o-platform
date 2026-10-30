package openai

import "github.com/infinmalum/a1o-platform/relay/channeltype"

// GetCompatibleChannelMeta returns the vendor display name used as the
// owned_by value in /v1/models for the kinds served through this adapter
// (OpenAI and Azure). Model presets are gone: admins fetch the live list from
// the upstream or type model names themselves.
func GetCompatibleChannelMeta(channelType int) (string, []string) {
	if channelType == channeltype.Azure {
		return "azure", nil
	}
	return "openai", nil
}
