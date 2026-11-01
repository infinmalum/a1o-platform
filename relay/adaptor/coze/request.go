package coze

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// ConvertRequest builds the v3 chat request from whichever client wire the
// request arrived on. Coze bots accept text-only user/assistant turns, so
// every conversion rejects the richer content kinds instead of dropping them,
// and system text folds into a user turn because the v3 chat wire has no
// system role.
func ConvertRequest(in *relaymodel.ConversionInput, botID, userID string) (*Request, error) {
	request := &Request{
		BotID:           botID,
		UserID:          userID,
		Stream:          true,
		AutoSaveHistory: true,
	}
	switch in.Inbound {
	case "", native.OpenAIChat:
		if in.Request == nil {
			return nil, errors.New("request is nil")
		}
		if len(in.Request.Tools) != 0 || in.Request.ToolChoice != nil {
			return nil, errors.New("Coze bots do not accept client tools")
		}
		for _, message := range in.Request.Messages {
			if len(message.ToolCalls) != 0 || message.ToolCallId != "" {
				return nil, errors.New("Coze bots do not accept tool-call history; use a non-tool conversation")
			}
			content, err := chatTextContent(message.Content)
			if err != nil {
				return nil, err
			}
			if err := appendMessage(request, message.Role, content); err != nil {
				return nil, err
			}
		}
	case native.Anthropic:
		return convertAnthropicInput(request, in.RawBody)
	case native.OpenAIResponses:
		return convertResponsesInput(request, in.RawBody)
	default:
		return nil, fmt.Errorf("client wire %q cannot be converted", in.Inbound)
	}
	if len(request.AdditionalMsgs) == 0 {
		return nil, errors.New("Coze requires at least one conversation message")
	}
	return request, nil
}

// appendMessage appends one turn. Coze accepts user and assistant history;
// system turns fold into a user turn because the wire has no system role.
func appendMessage(request *Request, role, content string) error {
	if strings.TrimSpace(content) == "" {
		return errors.New("Coze conversation messages require text content")
	}
	switch role {
	case "system", "developer":
		request.AdditionalMsgs = append(request.AdditionalMsgs, Message{Role: "user", Content: content, ContentType: "text"})
	case "user":
		request.AdditionalMsgs = append(request.AdditionalMsgs, Message{Role: "user", Content: content, ContentType: "text"})
	case "assistant":
		request.AdditionalMsgs = append(request.AdditionalMsgs, Message{Role: "assistant", Type: messageTypeAnswer, Content: content, ContentType: "text"})
	default:
		return fmt.Errorf("Coze conversion cannot represent the %q role", role)
	}
	return nil
}

// chatTextContent extracts text-only chat content, rejecting parts Coze
// cannot represent instead of silently dropping them.
func chatTextContent(raw any) (string, error) {
	switch content := raw.(type) {
	case string:
		return content, nil
	case []any:
		var text strings.Builder
		for _, item := range content {
			part, ok := item.(map[string]any)
			if !ok {
				return "", errors.New("invalid Chat content part")
			}
			if part["type"] != "text" {
				return "", fmt.Errorf("Coze conversion cannot represent the %q content part", part["type"])
			}
			value, ok := part["text"].(string)
			if !ok {
				return "", errors.New("Chat text part requires text")
			}
			text.WriteString(value)
		}
		return text.String(), nil
	default:
		return "", errors.New("Chat content must be text or text parts")
	}
}

// convertAnthropicInput parses a Messages body onto the v3 wire. Thinking,
// tool, and image blocks fail loudly: Coze bots run server-side tools and
// accept text turns only.
func convertAnthropicInput(request *Request, body []byte) (*Request, error) {
	var fields struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, errors.New("invalid Messages JSON request")
	}
	if text, err := textOrBlocks(fields.System); err != nil {
		return nil, fmt.Errorf("system: %w", err)
	} else if text != "" {
		if err := appendMessage(request, "user", text); err != nil {
			return nil, err
		}
	}
	for _, message := range fields.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, fmt.Errorf("Messages role %q cannot be converted", message.Role)
		}
		var blocks []map[string]json.RawMessage
		var plain string
		var content string
		var err error
		if json.Unmarshal(message.Content, &plain) == nil {
			content = plain
		} else if json.Unmarshal(message.Content, &blocks) == nil {
			content, err = anthropicBlocksText(blocks)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, errors.New("Messages content must be text or a block array")
		}
		if err := appendMessage(request, message.Role, content); err != nil {
			return nil, err
		}
	}
	return request, nil
}

// anthropicBlocksText extracts text from a block array, rejecting every other
// block kind.
func anthropicBlocksText(blocks []map[string]json.RawMessage) (string, error) {
	var text strings.Builder
	for _, block := range blocks {
		var kind string
		_ = json.Unmarshal(block["type"], &kind)
		if len(block) != 2 || kind != "text" {
			return "", fmt.Errorf("Coze conversion cannot represent the %q content block", kind)
		}
		var value string
		if json.Unmarshal(block["text"], &value) != nil {
			return "", errors.New("text block text must be a string")
		}
		text.WriteString(value)
	}
	return text.String(), nil
}

// convertResponsesInput parses a Responses body onto the v3 wire. Reasoning
// items are skipped; function and image items fail loudly.
func convertResponsesInput(request *Request, body []byte) (*Request, error) {
	var fields struct {
		Instructions string `json:"instructions"`
		Input        json.RawMessage
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, errors.New("invalid Responses JSON request")
	}
	if fields.Instructions != "" {
		if err := appendMessage(request, "user", fields.Instructions); err != nil {
			return nil, err
		}
	}
	var text string
	if json.Unmarshal(fields.Input, &text) == nil {
		if err := appendMessage(request, "user", text); err != nil {
			return nil, err
		}
		return request, nil
	}
	var items []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content json.RawMessage
	}
	if json.Unmarshal(fields.Input, &items) != nil || len(items) == 0 {
		return nil, errors.New("Responses input must be a string or a non-empty item array")
	}
	for _, item := range items {
		switch item.Type {
		case "reasoning":
			// Coze bots expose no thinking capability, so replayed reasoning
			// history has no carrier here and is skipped deliberately.
			continue
		case "message":
			role := item.Role
			if role != "user" && role != "assistant" && role != "system" {
				return nil, fmt.Errorf("Responses message role %q cannot be converted", role)
			}
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			var plain string
			var content string
			if json.Unmarshal(item.Content, &plain) == nil {
				content = plain
			} else if json.Unmarshal(item.Content, &parts) == nil {
				for _, part := range parts {
					representable := (role == "user" && part.Type == "input_text") || (role != "user" && part.Type == "output_text")
					if !representable {
						return nil, fmt.Errorf("Coze conversion cannot represent the %q content part", part.Type)
					}
					content += part.Text
				}
			} else {
				return nil, errors.New("Responses message content must be text or a part array")
			}
			if appendErr := appendMessage(request, role, content); appendErr != nil {
				return nil, appendErr
			}
		default:
			return nil, fmt.Errorf("Responses input item %q cannot be converted", item.Type)
		}
	}
	return request, nil
}

// textOrBlocks reads Anthropic system content, which is a string or text
// blocks. Malformed content fails instead of silently reducing to empty text.
func textOrBlocks(raw json.RawMessage) (string, error) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return "", errors.New("system content must be text or a block array")
	}
	return anthropicBlocksText(blocks)
}
