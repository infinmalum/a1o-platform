package channeltype

import "github.com/infinmalum/a1o-platform/relay/relaymode"

// NativeChatCompatible identifies channels whose Chat Completions endpoint
// accepts the OpenAI JSON and SSE wire format: OpenAI, Azure (whose URL is
// deployment-shaped), and OpenRouter whose fixed unified base serves the
// same wire.
func NativeChatCompatible(kind int) bool {
	switch kind {
	case OpenAI, Azure, OpenRouter:
		return true
	default:
		return false
	}
}

// OpenAIWireSupportsOperation reports whether the channel kind serves a relay
// mode on the OpenAI JSON wire, so unsupported operations are refused before
// a request is forwarded to an endpoint that does not exist.
func OpenAIWireSupportsOperation(kind int, mode int) bool {
	if !NativeChatCompatible(kind) {
		return false
	}
	switch mode {
	case relaymode.ChatCompletions, relaymode.Embeddings, relaymode.ImagesGenerations,
		relaymode.Completions, relaymode.Moderations, relaymode.AudioSpeech,
		relaymode.AudioTranscription, relaymode.AudioTranslation:
		return true
	default:
		return false
	}
}
