package bridge

import (
	"encoding/json"
	"errors"
)

// Anthropic cache markers are optimization hints on a different wire. Drop
// them only at the explicit Messages envelope, block and tool boundaries.
func messagesObject(raw []byte, allowed ...string) (map[string]json.RawMessage, error) {
	fields, err := object(raw, append(allowed, "cache_control")...)
	if err != nil {
		return nil, err
	}
	if err := validateCacheControl(fields["cache_control"]); err != nil {
		return nil, err
	}
	delete(fields, "cache_control")
	return fields, nil
}

func validateCacheControl(raw json.RawMessage) error {
	if !nonNull(raw) {
		return nil
	}
	control, err := object(raw, "type", "ttl")
	if err != nil || rawString(control["type"]) != "ephemeral" {
		return errors.New("cache_control must specify type ephemeral")
	}
	if nonNull(control["ttl"]) {
		switch rawString(control["ttl"]) {
		case "5m", "1h":
		default:
			return errors.New("cache_control ttl must be 5m or 1h")
		}
	}
	return nil
}

func AnthropicBridgeLimit(body []byte, fallback int64) int64 {
	var limits struct {
		MaxTokens           int64 `json:"max_tokens"`
		MaxCompletionTokens int64 `json:"max_completion_tokens"`
		MaxOutputTokens     int64 `json:"max_output_tokens"`
	}
	_ = json.Unmarshal(body, &limits)
	if limit := max(limits.MaxTokens, limits.MaxCompletionTokens, limits.MaxOutputTokens); limit > 0 {
		return limit
	}
	return fallback
}

// ConfigureAnthropicBridge applies channel policy to the converted request.
func ConfigureAnthropicBridge(body []byte, limit int64, cache, blockCache bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	fields["max_tokens"], _ = json.Marshal(limit)
	if cache {
		var messages []struct {
			Role    string                       `json:"role"`
			Content []map[string]json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(fields["messages"], &messages); err != nil {
			return nil, err
		}
		var system []map[string]json.RawMessage
		// Plain-text system prompts stay strings until a breakpoint needs one.
		_ = json.Unmarshal(fields["system"], &system)
		for _, block := range system {
			if nonNull(block["cache_control"]) {
				return json.Marshal(fields)
			}
		}
		var lastText map[string]json.RawMessage
		for _, msg := range messages {
			for _, block := range msg.Content {
				if nonNull(block["cache_control"]) {
					return json.Marshal(fields)
				}
				if rawString(block["type"]) == "text" {
					lastText = block
				}
			}
		}
		ephemeral := json.RawMessage(`{"type":"ephemeral"}`)
		if !blockCache {
			fields["cache_control"] = ephemeral
		} else if lastText != nil {
			lastText["cache_control"] = ephemeral
			fields["messages"], _ = json.Marshal(messages)
		} else if len(system) > 0 {
			system[len(system)-1]["cache_control"] = ephemeral
			fields["system"], _ = json.Marshal(system)
		} else if text := rawString(fields["system"]); text != "" {
			fields["system"], _ = json.Marshal([]map[string]any{{"type": "text", "text": text, "cache_control": ephemeral}})
		} else {
			var tools []map[string]json.RawMessage
			if err := decode(fields["tools"], &tools); err != nil {
				return nil, err
			}
			if len(tools) > 0 {
				tools[len(tools)-1]["cache_control"] = ephemeral
				fields["tools"], _ = json.Marshal(tools)
			}
		}
	}
	return json.Marshal(fields)
}
