package deepl

import (
	"errors"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/model"
)

// https://developers.deepl.com/docs/getting-started/your-first-api-request

// ConvertRequest maps a single user message onto the /v2/translate request.
// DeepL is a text-to-text translation RPC, not a conversation model: system
// prompts and multi-turn history have no translation semantics, so anything
// beyond one user message fails loudly instead of being silently dropped.
func ConvertRequest(textRequest model.TextRequest) (*Request, string, error) {
	if len(textRequest.Messages) == 0 {
		return nil, "", errors.New("conversation messages are required")
	}
	for _, message := range textRequest.Messages {
		if message.Role == "system" || message.Role == "developer" {
			return nil, "", errors.New("DeepL translates a single user message; system prompts have no translation semantics")
		}
		if message.Role != "user" {
			return nil, "", errors.New("DeepL translates a single user message; conversation history has no translation semantics")
		}
	}
	if len(textRequest.Messages) > 1 {
		return nil, "", errors.New("DeepL translates a single user message; conversation history has no translation semantics")
	}
	text, err := messageText(textRequest.Messages[0].Content)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(text) == "" {
		return nil, "", errors.New("user message requires text content")
	}
	targetLang, err := parseLangFromModelName(textRequest.Model)
	if err != nil {
		return nil, "", err
	}
	return &Request{
		ShowBilledCharacters: true,
		Text:                 []string{text},
		TargetLang:           targetLang,
	}, text, nil
}

// messageText extracts text-only content, rejecting the parts a translation
// request cannot carry.
func messageText(raw any) (string, error) {
	switch content := raw.(type) {
	case string:
		return content, nil
	case []any:
		var text strings.Builder
		for _, item := range content {
			part, ok := item.(map[string]any)
			if !ok || part["type"] != "text" {
				return "", errors.New("DeepL translation requires text content")
			}
			value, ok := part["text"].(string)
			if !ok {
				return "", errors.New("Chat text part requires text")
			}
			text.WriteString(value)
		}
		return text.String(), nil
	default:
		return "", errors.New("DeepL translation requires text content")
	}
}
