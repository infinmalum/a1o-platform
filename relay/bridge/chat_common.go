package bridge

import (
	"encoding/json"
	"errors"
)

// chatRequestHead holds the request envelope shared by every Chat-to-X
// converter: the strict field whitelist, the stream flag, the mutually
// consistent token limit, and the message array.
type chatRequestHead struct {
	fields   map[string]json.RawMessage
	stream   bool
	messages []json.RawMessage
	limit    int64
}

// parseChatRequestHead validates the envelope fields every Chat conversion
// needs. extraFields are appended to the shared whitelist so converters keep
// rejecting fields they cannot represent.
func parseChatRequestHead(body []byte, model string, extraFields ...string) (*chatRequestHead, error) {
	if model == "" {
		return nil, errors.New("model is required")
	}
	permitted := append([]string{"model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "tools", "tool_choice"}, extraFields...)
	fields, err := object(body, permitted...)
	if err != nil {
		return nil, err
	}
	var stream bool
	if err := decode(fields["stream"], &stream); err != nil {
		return nil, errors.New("stream must be boolean")
	}
	var maxTokens, maxCompletion int64
	if err := decode(fields["max_tokens"], &maxTokens); err != nil || maxTokens < 0 {
		return nil, errors.New("max_tokens must not be negative")
	}
	if err := decode(fields["max_completion_tokens"], &maxCompletion); err != nil || maxCompletion < 0 {
		return nil, errors.New("max_completion_tokens must not be negative")
	}
	if maxTokens > 0 && maxCompletion > 0 && maxTokens != maxCompletion {
		return nil, errors.New("conflicting Chat token limits")
	}
	var messages []json.RawMessage
	if json.Unmarshal(fields["messages"], &messages) != nil || len(messages) == 0 {
		return nil, errors.New("messages must be a non-empty array")
	}
	return &chatRequestHead{
		fields:   fields,
		stream:   stream,
		messages: messages,
		limit:    max(maxTokens, maxCompletion),
	}, nil
}

// chatToolCall is the converted view of one OpenAI tool call entry.
type chatToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// chatToolCalls parses the tool_calls array of a Chat message or response.
// The arguments string is returned verbatim; callers decide whether an object
// or any valid JSON is required via chatToolArgsObject.
func chatToolCalls(raw json.RawMessage) ([]chatToolCall, error) {
	var calls []json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return nil, errors.New("tool_calls must be an array")
	}
	var result []chatToolCall
	for _, callRaw := range calls {
		call, err := object(callRaw, "id", "type", "function")
		if err != nil {
			return nil, err
		}
		function, err := object(call["function"], "name", "arguments")
		if err != nil {
			return nil, err
		}
		converted := chatToolCall{
			ID:        rawString(call["id"]),
			Name:      rawString(function["name"]),
			Arguments: rawString(function["arguments"]),
		}
		if rawString(call["type"]) != "function" || converted.ID == "" || converted.Name == "" {
			return nil, errors.New("only named function tool calls can be converted")
		}
		result = append(result, converted)
	}
	return result, nil
}

// chatToolArgsObject requires tool call arguments to decode into a JSON
// object, which is the only form the Anthropic and Gemini tool formats accept.
func chatToolArgsObject(arguments string) (map[string]any, error) {
	var input map[string]any
	if json.Unmarshal([]byte(arguments), &input) != nil || input == nil {
		return nil, errors.New("function tool arguments must be a JSON object")
	}
	return input, nil
}

// chatStopSequences reads the Chat stop field, which is either a single string
// or an array of strings.
func chatStopSequences(raw json.RawMessage) ([]string, error) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, nil
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil {
		return nil, errors.New("stop must be text or a list of text")
	}
	return many, nil
}

// chatTextPartList decodes an array of {"type":"text","text":...} parts with
// strict validation, returning each part's text in order. Malformed or
// non-text parts fail instead of being dropped as empty text.
func chatTextPartList(raw json.RawMessage) ([]string, error) {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil, errors.New("Chat content must be text or text parts")
	}
	var result []string
	for _, partRaw := range parts {
		part, err := object(partRaw, "type", "text")
		if err != nil {
			return nil, err
		}
		if rawString(part["type"]) != "text" {
			return nil, errors.New("Chat content supports only text parts")
		}
		var value string
		if len(part) != 2 || json.Unmarshal(part["text"], &value) != nil {
			return nil, errors.New("Chat text part requires a text field")
		}
		result = append(result, value)
	}
	return result, nil
}

// chatContentString decodes Chat content given as a plain JSON string. Like
// json.Unmarshal into a string, a JSON null counts as the empty string so
// assistant messages that carry only tool_calls keep converting.
func chatContentString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	return "", false
}

// chatTextBlocks converts text-only Chat content into Anthropic text blocks.
func chatTextBlocks(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if text, ok := chatContentString(raw); ok {
		if text == "" {
			return nil, nil
		}
		return []any{map[string]string{"type": "text", "text": text}}, nil
	}
	parts, err := chatTextPartList(raw)
	if err != nil {
		return nil, err
	}
	var result []any
	for _, text := range parts {
		result = append(result, map[string]string{"type": "text", "text": text})
	}
	return result, nil
}
