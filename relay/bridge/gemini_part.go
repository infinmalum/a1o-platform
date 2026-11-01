package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
)

type geminiPartKind int

const (
	geminiPartText geminiPartKind = iota
	geminiPartThought
	geminiPartFunctionCall
	geminiPartSignature
)

// geminiPart is the converted view of one Gemini candidate content part.
// Parts parse with their thought signature intact; each consuming converter
// decides whether the target wire can represent the kind plus signature and
// rejects or carries accordingly, so nothing is silently stripped.
type geminiPart struct {
	kind      geminiPartKind
	text      string
	signature string
	callID    string
	name      string
	args      json.RawMessage
}

// parseGeminiResponsePart parses one candidate content part. Thought parts may
// carry the thought signature that must be echoed back on later requests; a
// bare signature part attaches to the preceding thinking content; display text
// and function calls may also carry signatures, which thinking-model function
// calling requires for multi-turn replay. Parts mixing mapped keys with
// unmapped media keys (inlineData, fileData, ...) fail loudly instead of
// losing the media.
func parseGeminiResponsePart(raw json.RawMessage) (geminiPart, error) {
	fields, extras, err := rawObject(raw, "text", "thought", "thoughtSignature", "functionCall")
	if err != nil {
		return geminiPart{}, err
	}
	if len(extras) > 0 {
		return geminiPart{}, fmt.Errorf("Gemini content part carries fields the target wire cannot represent: %s", sortExtrasNames(extras))
	}
	if len(fields["functionCall"]) != 0 {
		if len(fields["text"]) != 0 || len(fields["thought"]) != 0 {
			return geminiPart{}, errors.New("Gemini part combines a function call with other content")
		}
		call, err := object(fields["functionCall"], "name", "args", "id")
		if err != nil {
			return geminiPart{}, err
		}
		name := rawString(call["name"])
		if name == "" {
			return geminiPart{}, errors.New("Gemini function call requires a name")
		}
		args := json.RawMessage("{}")
		if len(call["args"]) != 0 && string(call["args"]) != "null" {
			var input map[string]any
			if json.Unmarshal(call["args"], &input) != nil || input == nil {
				return geminiPart{}, errors.New("Gemini function call args must be an object")
			}
			args = call["args"]
		}
		return geminiPart{kind: geminiPartFunctionCall, name: name, args: args, callID: rawString(call["id"]), signature: rawString(fields["thoughtSignature"])}, nil
	}
	var thought bool
	if len(fields["thought"]) != 0 {
		if json.Unmarshal(fields["thought"], &thought) != nil {
			return geminiPart{}, errors.New("Gemini thought flag must be boolean")
		}
	}
	signature := rawString(fields["thoughtSignature"])
	if thought {
		if rawString(fields["text"]) == "" && signature == "" {
			return geminiPart{}, errors.New("Gemini thought part requires text or a signature")
		}
		return geminiPart{kind: geminiPartThought, text: rawString(fields["text"]), signature: signature}, nil
	}
	if len(fields["text"]) != 0 {
		return geminiPart{kind: geminiPartText, text: rawString(fields["text"]), signature: signature}, nil
	}
	if signature != "" {
		return geminiPart{kind: geminiPartSignature, signature: signature}, nil
	}
	return geminiPart{}, errors.New("Gemini part cannot be represented")
}
