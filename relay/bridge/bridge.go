package bridge

import (
	"errors"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Converter defines the boundary between a client's JSON protocol and the
// selected upstream JSON protocol. Streaming uses the separate StreamConverter
// path.
type Converter interface {
	Request(body []byte, upstreamModel string) ([]byte, error)
	Response(body []byte, requestedModel string) ([]byte, native.Usage, error)
}

func For(inbound, upstream native.Protocol) (Converter, error) {
	// Share only the documented common items/events conversion; retain the
	// separate protocol identity in routing and transport. Raw extensions remain intact.
	if inbound == native.OpenResponses {
		inbound = native.OpenAIResponses
	}
	if upstream == native.OpenResponses {
		upstream = native.OpenAIResponses
	}
	if inbound == upstream {
		return nil, nil
	}
	if inbound == native.OpenAIChat && upstream == native.Anthropic {
		return chatAnthropic{}, nil
	}
	if inbound == native.Anthropic && upstream == native.OpenAIChat {
		return anthropicChat{}, nil
	}
	if inbound == native.OpenAIChat && upstream == native.Gemini {
		return chatGemini{}, nil
	}
	if inbound == native.Gemini && upstream == native.OpenAIChat {
		return geminiChat{}, nil
	}
	if inbound == native.Anthropic && upstream == native.Gemini {
		return messagesGemini{}, nil
	}
	if inbound == native.OpenAIChat && upstream == native.OpenAIResponses {
		return &chatResponses{}, nil
	}
	if inbound == native.OpenAIResponses && upstream == native.OpenAIChat {
		return responsesChat{}, nil
	}
	if inbound == native.OpenAIResponses && upstream == native.Anthropic {
		return responsesAnthropic{}, nil
	}
	if inbound == native.Anthropic && upstream == native.OpenAIResponses {
		return anthropicResponses{}, nil
	}
	if inbound == native.OpenAIResponses && upstream == native.Gemini {
		return responsesGemini{}, nil
	}
	return nil, errors.New("unsupported protocol conversion")
}

type chatAnthropic struct{}

func (chatAnthropic) Request(body []byte, model string) ([]byte, error) {
	return ChatToAnthropic(body, model)
}

func (chatAnthropic) Response(body []byte, model string) ([]byte, native.Usage, error) {
	return AnthropicToChat(body, model)
}

type anthropicChat struct{}

func (anthropicChat) Request(body []byte, model string) ([]byte, error) {
	return AnthropicToChatRequest(body, model)
}

func (anthropicChat) Response(body []byte, model string) ([]byte, native.Usage, error) {
	return ChatToAnthropicResponse(body, model)
}

type chatGemini struct{}

func (chatGemini) Request(body []byte, model string) ([]byte, error) {
	return ChatToGemini(body, model)
}

func (chatGemini) Response(body []byte, model string) ([]byte, native.Usage, error) {
	return GeminiToChat(body, model)
}

type messagesGemini struct{}

func (messagesGemini) Request(body []byte, model string) ([]byte, error) {
	return MessagesToGemini(body, model)
}

func (messagesGemini) Response(body []byte, model string) ([]byte, native.Usage, error) {
	return GeminiToMessages(body, model)
}

type geminiChat struct{}

func (geminiChat) Request(body []byte, model string) ([]byte, error) {
	return GeminiToChatRequest(body, model)
}

func (geminiChat) Response(body []byte, model string) ([]byte, native.Usage, error) {
	return ChatToGeminiResponse(body, model)
}
