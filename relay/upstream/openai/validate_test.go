package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validResponsesBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model":        "gpt-5",
		"stream":       true,
		"instructions": "be brief",
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "hello"},
		},
	})
	require.NoError(t, err)
	return body
}

func TestValidateResponsesRequestNormalizesStatelessStreaming(t *testing.T) {
	normalized, err := ValidateResponsesRequest(validResponsesBody(t), true)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(normalized, &fields))
	require.Equal(t, "false", string(fields["store"]))
	require.NotEmpty(t, fields["input"])
	require.NotEmpty(t, fields["instructions"])
}

func TestValidateResponsesRequestRejectsSyncRequests(t *testing.T) {
	_, err := ValidateResponsesRequest(validResponsesBody(t), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "streaming")

	raw, err := json.Marshal(map[string]any{"model": "gpt-5", "stream": false, "input": []any{}})
	require.NoError(t, err)
	_, err = ValidateResponsesRequest(raw, false)
	require.Error(t, err)
}

func TestValidateResponsesRequestRejectsForbiddenFields(t *testing.T) {
	for _, field := range []string{
		"background", "max_output_tokens", "temperature", "top_p",
		"previous_response_id", "metadata", "truncation", "user", "prompt_cache_retention",
	} {
		body := map[string]any{"model": "gpt-5", "stream": true, "input": []any{}}
		body[field] = 1
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		_, err = ValidateResponsesRequest(raw, true)
		require.Error(t, err, "field %s must be rejected", field)
		require.Contains(t, err.Error(), field)
	}
}

func TestValidateResponsesRequestRejectsStoredResponses(t *testing.T) {
	body := map[string]any{"model": "gpt-5", "stream": true, "input": []any{}, "store": true}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	_, err = ValidateResponsesRequest(raw, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stored responses")
}

func TestValidateResponsesRequestRejectsNonArrayInput(t *testing.T) {
	body := map[string]any{"model": "gpt-5", "stream": true, "input": "hello"}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	_, err = ValidateResponsesRequest(raw, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "array")
}

func TestValidateResponsesRequestRejectsSystemMessages(t *testing.T) {
	body := map[string]any{
		"model":  "gpt-5",
		"stream": true,
		"input": []map[string]any{
			{"type": "message", "role": "system", "content": "you are helpful"},
			{"type": "message", "role": "user", "content": "hello"},
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	_, err = ValidateResponsesRequest(raw, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "system message")
}

func TestValidateResponsesRequestRejectsAudioVideoInput(t *testing.T) {
	body := map[string]any{
		"model":  "gpt-5",
		"stream": true,
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_audio", "input_audio": "data"}}},
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	_, err = ValidateResponsesRequest(raw, true)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "input_audio"))
}

func TestValidateResponsesRequestToolMatrix(t *testing.T) {
	for _, toolType := range []string{"image_generation", "file_search", "code_interpreter", "computer_use_preview", "mcp", "tool_search"} {
		body := map[string]any{
			"model": "gpt-5", "stream": true, "input": []any{},
			"tools": []map[string]any{{"type": toolType, "name": "t"}},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		_, err = ValidateResponsesRequest(raw, true)
		require.Error(t, err, "tool %s must be rejected", toolType)
		require.Contains(t, err.Error(), toolType)
	}
	for _, toolType := range []string{"function", "custom", "web_search"} {
		body := map[string]any{
			"model": "gpt-5", "stream": true, "input": []any{},
			"tools": []map[string]any{{"type": toolType, "name": "t"}},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		_, err = ValidateResponsesRequest(raw, true)
		require.NoError(t, err, "tool %s is supported", toolType)
	}
}
