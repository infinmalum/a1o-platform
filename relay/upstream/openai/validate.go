package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The ChatGPT plan profile serves the public Responses endpoint under the
// documented preview limitations. Requests are validated before forwarding so
// unsupported semantics fail explicitly instead of degrading silently.
var forbiddenResponsesFields = []string{
	"background", "conversation", "max_output_tokens", "max_tool_calls",
	"metadata", "moderation", "multi_agent", "prompt", "prompt_cache_retention",
	"safety_identifier", "temperature", "top_logprobs", "top_p", "truncation",
	"user", "previous_response_id", "programmatic_tool_calling",
}

var supportedResponsesTools = map[string]bool{
	"function":           true,
	"custom":             true,
	"web_search":         true,
	"web_search_preview": true,
}

// ValidateResponsesRequest enforces the subscription capability matrix on one
// upstream Responses request and returns the normalized body: the profile
// requires stateless streaming requests, so store=false is injected when the
// client did not opt into storing. Unsupported fields, tools, and inputs fail
// with an explicit error.
func ValidateResponsesRequest(body []byte, stream bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid OpenAI Responses JSON request")
	}
	if raw, ok := fields["stream"]; ok {
		var requested bool
		if json.Unmarshal(raw, &requested) == nil && !requested {
			return nil, errors.New("this account profile currently supports streaming responses only")
		}
	}
	if !stream {
		return nil, errors.New("this account profile currently supports streaming responses only")
	}
	var unsupported []string
	for _, name := range forbiddenResponsesFields {
		if raw, ok := fields[name]; ok && !isNull(raw) {
			unsupported = append(unsupported, name)
		}
	}
	if len(unsupported) > 0 {
		return nil, fmt.Errorf("field(s) %s are not supported by this account profile", strings.Join(unsupported, ", "))
	}
	if raw, ok := fields["input"]; ok && !isNull(raw) {
		if !isArray(raw) {
			return nil, errors.New("input must be an array of items for this account profile")
		}
		if err := validateResponsesInput(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := fields["tools"]; ok && !isNull(raw) {
		if err := validateResponsesTools(raw); err != nil {
			return nil, err
		}
	}
	if raw, ok := fields["store"]; ok {
		var store bool
		if json.Unmarshal(raw, &store) == nil && store {
			return nil, errors.New("stored responses are not supported by this account profile")
		}
	}
	fields["store"] = []byte("false")
	return json.Marshal(fields)
}

func isNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func isArray(raw json.RawMessage) bool {
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte("["))
}

func validateResponsesInput(raw json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return errors.New("input must be an array of items")
	}
	for _, item := range items {
		var fields struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(item, &fields) != nil {
			continue
		}
		if fields.Type == "message" && fields.Role == "system" {
			return errors.New("system message items are rejected by this account profile; use instructions or a developer message")
		}
		if err := rejectUnsupportedContent(fields.Content); err != nil {
			return err
		}
	}
	return nil
}

// rejectUnsupportedContent scans message content parts; string content and
// unknown shapes are left to the upstream to judge.
func rejectUnsupportedContent(raw json.RawMessage) error {
	if len(raw) == 0 || isNull(raw) {
		return nil
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	for _, part := range parts {
		var kind string
		if raw, ok := part["type"]; ok {
			_ = json.Unmarshal(raw, &kind)
		}
		switch kind {
		case "input_audio", "input_video":
			return fmt.Errorf("%s input is not supported by this account profile", kind)
		}
	}
	return nil
}

func validateResponsesTools(raw json.RawMessage) error {
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return errors.New("tools must be an array")
	}
	for _, tool := range tools {
		var kind string
		if raw, ok := tool["type"]; ok {
			_ = json.Unmarshal(raw, &kind)
		}
		if kind == "" {
			continue
		}
		if !supportedResponsesTools[kind] {
			return fmt.Errorf("tool type %q is not supported by this account profile", kind)
		}
	}
	return nil
}
