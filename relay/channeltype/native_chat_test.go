package channeltype

import (
	"testing"

	"github.com/infinmalum/one-gateway/relay/apitype"
	"github.com/infinmalum/one-gateway/relay/relaymode"
)

// retiredChannelTypes are the kinds whose native adapters were removed and
// whose official OpenAI-compatible endpoints need a different request path
// than the default /v1 one. Channels of these kinds are migrated to the
// matching OpenAI-compatible kind at startup (migrateChannelTypes); the
// constants stay in the iota table for numbering stability only.
var retiredChannelTypes = map[int]bool{
	PaLM:           true,
	Baidu:          true,
	Ali:            true,
	Xunfei:         true,
	AIProxyLibrary: true,
}

func TestAllOpenAIAdapterChannelsUseNativeChat(t *testing.T) {
	for kind := OpenAI; kind < Dummy; kind++ {
		if retiredChannelTypes[kind] {
			continue
		}
		if got, want := NativeChatCompatible(kind), ToAPIType(kind) == apitype.OpenAI; got != want {
			t.Errorf("channel type %d: native Chat eligibility %t, OpenAI adapter %t", kind, got, want)
		}
	}
}

func TestOpenAIWireSupportsOperation(t *testing.T) {
	for kind := OpenAI; kind < Dummy; kind++ {
		if !NativeChatCompatible(kind) || kind == Zhipu || kind == Tencent || kind == Ollama || kind == Cohere {
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
	// The retired vendor compatible endpoints mirror chat completions and
	// embeddings; Zhipu additionally serves image generations.
	for _, kind := range []int{Zhipu, Tencent, Ollama, Cohere} {
		if !OpenAIWireSupportsOperation(kind, relaymode.ChatCompletions) || !OpenAIWireSupportsOperation(kind, relaymode.Embeddings) {
			t.Errorf("channel type %d: chat and embeddings must ride the compatible wire", kind)
		}
		for _, mode := range []int{
			relaymode.Completions, relaymode.Moderations, relaymode.Edits,
			relaymode.AudioSpeech, relaymode.AudioTranscription, relaymode.AudioTranslation,
		} {
			if OpenAIWireSupportsOperation(kind, mode) {
				t.Errorf("channel type %d: wire operation %d must be refused before the upstream", kind, mode)
			}
		}
		if OpenAIWireSupportsOperation(kind, relaymode.ImagesGenerations) != (kind == Zhipu) {
			t.Errorf("channel type %d: image support mismatch", kind)
		}
	}
	if OpenAIWireSupportsOperation(Anthropic, relaymode.ChatCompletions) {
		t.Errorf("non OpenAI wire channels must stay on the provider adapters")
	}
}
