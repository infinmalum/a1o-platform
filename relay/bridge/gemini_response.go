package bridge

import (
	"encoding/json"
	"errors"
)

// geminiResponseFields lists the top-level GenerateContent response envelope
// fields the converters understand. It is shared by the sync response
// converters and the stream event parser so a new provider field is handled
// in exactly one place.
var geminiResponseFields = []string{"candidates", "promptFeedback", "usageMetadata", "modelVersion", "responseId", "modelStatus"}

// geminiCandidateFields lists the fields of one response candidate the
// converters understand.
var geminiCandidateFields = []string{"content", "finishReason", "safetyRatings", "citationMetadata", "tokenCount", "groundingAttributions", "groundingMetadata", "avgLogprobs", "logprobsResult", "urlContextMetadata", "index", "finishMessage"}

// geminiCandidateIndex rejects candidates whose index is not zero: the
// converters represent exactly one choice.
func geminiCandidateIndex(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var index int
	if json.Unmarshal(raw, &index) != nil || index != 0 {
		return errors.New("Gemini candidate index must be zero")
	}
	return nil
}

// geminiProviderMetadata collects the envelope and candidate fields that are
// provider state rather than conversation content, so converted responses can
// carry them back to the client unchanged.
func geminiProviderMetadata(fields, candidate map[string]json.RawMessage) map[string]json.RawMessage {
	metadata := map[string]json.RawMessage{"usageMetadata": fields["usageMetadata"]}
	for _, name := range []string{"modelVersion", "modelStatus", "promptFeedback"} {
		if len(fields[name]) != 0 {
			metadata[name] = fields[name]
		}
	}
	for name, value := range candidate {
		switch name {
		case "content", "finishReason", "index":
		default:
			metadata[name] = value
		}
	}
	return metadata
}
