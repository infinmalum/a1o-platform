package native

import "encoding/json"

const LatestAnthropicVersion = "2026-10-21"

// The new version removes sampling parameters and some history block shapes.
// Honor explicit versions. Legacy requests stay on the official compatibility
// version instead of silently losing supplied controls or history.
func anthropicAPIVersion(input Request) string {
	if version := input.Headers.Get("anthropic-version"); version != "" {
		return version
	}
	if input.Version == "2023-06-01" || input.Version == LatestAnthropicVersion {
		return input.Version
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(input.Body, &fields) == nil {
		for _, key := range []string{"temperature", "top_p", "top_k"} {
			if _, ok := fields[key]; ok {
				return "2023-06-01"
			}
		}
		var messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(fields["messages"], &messages)
		for _, message := range messages {
			var blocks []map[string]json.RawMessage
			if json.Unmarshal(message.Content, &blocks) != nil {
				continue
			}
			for _, block := range blocks {
				if message.Role == "assistant" && string(block["type"]) == `"document"` {
					return "2023-06-01"
				}
				if message.Role == "user" && legacyCitations(block) {
					return "2023-06-01"
				}
			}
		}
	}
	return LatestAnthropicVersion
}
func legacyCitations(block map[string]json.RawMessage) bool {
	var citations []json.RawMessage
	if json.Unmarshal(block["citations"], &citations) == nil && len(citations) > 0 {
		return true
	}
	var children []map[string]json.RawMessage
	if json.Unmarshal(block["content"], &children) == nil {
		for _, child := range children {
			if legacyCitations(child) {
				return true
			}
		}
	}
	return false
}
