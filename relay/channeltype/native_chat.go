package channeltype

import "github.com/infinmalum/a1o-platform/relay/relaymode"

// NativeChatCompatible identifies channels whose Chat Completions endpoint
// accepts the OpenAI JSON and SSE wire format. Every other vendor kind was
// collapsed onto OpenAICompatible, where the admin enters the full
// version-prefixed base URL; Minimax keeps its own adapter because its chat
// path (/v1/text/chatcompletion_v2) cannot be expressed as a custom endpoint.
func NativeChatCompatible(kind int) bool {
	switch kind {
	case OpenAI, Azure, Minimax, OpenAICompatible:
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
		relaymode.AudioTranscription, relaymode.AudioTranslation, relaymode.Edits:
		return true
	default:
		return false
	}
}
