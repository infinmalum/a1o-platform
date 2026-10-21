package channeltype

import "github.com/infinmalum/one-gateway/relay/relaymode"

// NativeChatCompatible identifies channels whose Chat Completions endpoint
// accepts the OpenAI JSON and SSE wire format. Channels with provider-specific
// request conversion stay on their existing adapter until that conversion is
// moved into the shared lifecycle.
func NativeChatCompatible(kind int) bool {
	switch kind {
	case OpenAI, API2D, Azure, CloseAI, OpenAISB, OpenAIMax, OhMyGPT,
		Custom, Ails, AIProxy, API2GPT, AIGC2D, AI360, OpenRouter, FastGPT,
		Moonshot, Baichuan, Minimax, Mistral, Groq, LingYiWanWu,
		StepFun, DeepSeek, TogetherAI, Doubao, Novita, SiliconFlow,
		XAI, BaiduV2, XunfeiV2, AliBailian, OpenAICompatible,
		GeminiOpenAICompatible,
		// Retired native adapters now ride the OpenAI wire through official
		// vendor-compatible endpoints.
		Zhipu, Tencent, Ollama, Cohere:
		return true
	default:
		return false
	}
}

// OpenAIWireSupportsOperation reports whether the channel kind serves a relay
// mode on the OpenAI JSON wire, so unsupported operations are refused before
// a request is forwarded to an endpoint that does not exist. The retired
// vendor compatible endpoints mirror chat completions and embeddings, plus
// Zhipu image generations, but none of the legacy text or audio operations.
func OpenAIWireSupportsOperation(kind int, mode int) bool {
	if !NativeChatCompatible(kind) {
		return false
	}
	switch kind {
	case Zhipu, Tencent, Ollama, Cohere:
		switch mode {
		case relaymode.ChatCompletions, relaymode.Embeddings:
			return true
		case relaymode.ImagesGenerations:
			return kind == Zhipu
		default:
			return false
		}
	default:
		return true
	}
}
