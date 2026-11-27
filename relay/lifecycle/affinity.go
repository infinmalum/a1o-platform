package lifecycle

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// RequestAffinity scopes a caller's cache key or first user message to its
// gateway token. It does not retain the key or message in routing state.
func RequestAffinity(body []byte, userID, tokenID int) string {
	var request struct {
		CacheKey string          `json:"prompt_cache_key"`
		Messages json.RawMessage `json:"messages"`
		Input    json.RawMessage `json:"input"`
		Contents json.RawMessage `json:"contents"`
	}
	if json.Unmarshal(body, &request) != nil {
		return ""
	}
	seed := request.CacheKey
	kind := "key"
	if seed == "" {
		kind = "input"
		raw := request.Messages
		if len(raw) == 0 {
			raw = request.Input
		}
		isGemini := len(raw) == 0 && len(request.Contents) > 0
		if isGemini {
			raw = request.Contents
		}
		if json.Unmarshal(raw, &seed) != nil {
			var messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Parts   json.RawMessage `json:"parts"`
			}
			if json.Unmarshal(raw, &messages) != nil {
				return ""
			}
			for _, message := range messages {
				contentJSON := message.Content
				if isGemini {
					contentJSON = message.Parts
				}
				if (message.Role == "user" || isGemini && message.Role == "") && len(contentJSON) > 0 {
					// Canonicalize objects so equivalent JSON key order and
					// whitespace do not change the channel.
					var content any
					if json.Unmarshal(contentJSON, &content) == nil {
						if text, ok := content.(string); ok {
							seed = text
						} else if content != nil {
							canonical, _ := json.Marshal(content)
							seed = string(canonical)
						}
					}
					break
				}
			}
		}
	}
	if seed == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", kind, userID, tokenID, seed))))
}
