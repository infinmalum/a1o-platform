package channeltype

import (
	"testing"

	"github.com/infinmalum/one-gateway/relay/apitype"
	"github.com/infinmalum/one-gateway/relay/relaymode"
)

// nativeChatWhitelist is pinned literally so NativeChatCompatible can never
// drift from it. Widening the whitelist to a retired vendor kind would route
// live traffic onto a kind whose channels are rewritten at boot.
var nativeChatWhitelist = map[int]bool{
	OpenAI:           true,
	Azure:            true,
	Minimax:          true,
	OpenAICompatible: true,
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
			relaymode.Moderations, relaymode.ImagesGenerations, relaymode.Edits,
			relaymode.AudioSpeech, relaymode.AudioTranscription, relaymode.AudioTranslation,
		} {
			if !OpenAIWireSupportsOperation(kind, mode) {
				t.Errorf("channel type %d: wire operation %d should be forwarded", kind, mode)
			}
		}
	}
	// Retired vendor kinds must never serve the wire: their channels are
	// rewritten to the compatible kind at boot, so accepting an operation on
	// them would keep stale channels routable.
	retired := []int{PaLM, Baidu, Ali, Xunfei, Tencent, AIProxyLibrary, Custom,
		Zhipu, Cohere, Ollama, BaiduV2, AliBailian, GeminiOpenAICompatible}
	for _, kind := range retired {
		for _, mode := range []int{
			relaymode.ChatCompletions, relaymode.Completions, relaymode.Embeddings,
			relaymode.Moderations, relaymode.ImagesGenerations, relaymode.Edits,
			relaymode.AudioSpeech, relaymode.AudioTranscription, relaymode.AudioTranslation,
		} {
			if OpenAIWireSupportsOperation(kind, mode) {
				t.Errorf("channel type %d: retired wire operation %d must be refused", kind, mode)
			}
		}
	}
	if OpenAIWireSupportsOperation(Anthropic, relaymode.ChatCompletions) {
		t.Errorf("non OpenAI wire channels must stay on the provider adapters")
	}
}
