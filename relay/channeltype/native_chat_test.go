package channeltype

import (
	"testing"

	"github.com/infinmalum/a1o-platform/relay/apitype"
	"github.com/infinmalum/a1o-platform/relay/relaymode"
)

// nativeChatWhitelist is pinned literally so NativeChatCompatible can never
// drift from it.
var nativeChatWhitelist = map[int]bool{
	OpenAI:     true,
	Azure:      true,
	OpenRouter: true,
}

func TestAllOpenAIAdapterChannelsUseNativeChat(t *testing.T) {
	for kind := OpenAI; kind < Dummy; kind++ {
		if got := NativeChatCompatible(kind); got != nativeChatWhitelist[kind] {
			t.Errorf("channel type %d: native Chat eligibility %t, whitelist %t", kind, got, nativeChatWhitelist[kind])
		}
		if nativeChatWhitelist[kind] && ToAPIType(kind) != apitype.OpenAI {
			t.Errorf("channel type %d: whitelist member must ride the OpenAI adaptor", kind)
		}
	}
}

func TestOpenAIWireSupportsOperation(t *testing.T) {
	for kind := OpenAI; kind < Dummy; kind++ {
		if !NativeChatCompatible(kind) {
			continue
		}
		for _, mode := range []int{
			relaymode.ChatCompletions, relaymode.Completions, relaymode.Embeddings,
			relaymode.Moderations, relaymode.ImagesGenerations,
			relaymode.AudioSpeech, relaymode.AudioTranscription, relaymode.AudioTranslation,
		} {
			if !OpenAIWireSupportsOperation(kind, mode) {
				t.Errorf("channel type %d: wire operation %d should be forwarded", kind, mode)
			}
		}
	}
	if OpenAIWireSupportsOperation(Anthropic, relaymode.ChatCompletions) {
		t.Errorf("non OpenAI wire channels must stay on the provider adapters")
	}
}
