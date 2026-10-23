package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiToChatRequestConvertsConversationAndTools(t *testing.T) {
	input := []byte(`{
		"systemInstruction": {"parts": [{"text": "be brief"}]},
		"contents": [
			{"role": "user", "parts": [{"text": "find it"}, {"inlineData": {"mimeType": "image/png", "data": "aGk="}}]},
			{"role": "model", "parts": [{"text": "Searching"}, {"functionCall": {"name": "search", "args": {"q": "test"}}}]},
			{"role": "user", "parts": [{"functionResponse": {"name": "search", "response": {"result": "found"}}}]}
		],
		"generationConfig": {"maxOutputTokens": 64, "temperature": 0.5, "stopSequences": ["done"]},
		"tools": [{"functionDeclarations": [{"name": "search", "description": "look up", "parameters": {"type": "object"}}]}],
		"toolConfig": {"functionCallingConfig": {"mode": "ANY"}}
	}`)
	converted, err := GeminiToChatRequest(input, "upstream-model")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Model    string `json:"model"`
		MaxStars int64  `json:"max_tokens"`
		Messages []struct {
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
		Tools      []any    `json:"tools"`
		ToolChoice string   `json:"tool_choice"`
		Stop       []string `json:"stop"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.Model != "upstream-model" || result.MaxStars != 64 || result.ToolChoice != "required" || len(result.Stop) != 1 || result.Stop[0] != "done" || len(result.Tools) != 1 {
		t.Fatalf("converted request lost envelope fields: %s", converted)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("expected system, user, assistant, tool messages: %s", converted)
	}
	if result.Messages[0].Role != "system" || string(result.Messages[0].Content) != `"be brief"` {
		t.Fatalf("system instruction was lost: %s", converted)
	}
	if result.Messages[1].Role != "user" || !strings.Contains(string(result.Messages[1].Content), "data:image/png;base64,aGk=") {
		t.Fatalf("user media content was lost: %s", converted)
	}
	if result.Messages[2].Role != "assistant" || result.Messages[2].ToolCalls[0].Function.Name != "search" || !strings.Contains(result.Messages[2].ToolCalls[0].Function.Arguments, `"q"`) {
		t.Fatalf("assistant function call was lost: %s", converted)
	}
	var toolContent string
	if err := json.Unmarshal(result.Messages[3].Content, &toolContent); err != nil || !strings.Contains(toolContent, "found") {
		t.Fatalf("function response did not become a tool message: %s", converted)
	}
	if result.Messages[3].Role != "tool" || result.Messages[3].ToolCallID != result.Messages[2].ToolCalls[0].ID {
		t.Fatalf("function response did not become a tool message: %s", converted)
	}
}

func TestGeminiToChatRequestRejectsUnrepresentable(t *testing.T) {
	for _, body := range []string{
		`{"contents":[{"role":"user","parts":[{"fileData":{"fileUri":"gs://bucket/x.png"}}]}]}`,
		`{"contents":[{"role":"system","parts":[{"text":"hi"}]}]}`,
		`{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"nope","response":{"result":1}}}]}]}`,
		`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"application/pdf","data":"aGk="}}]}]}`,
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"tools":[{"googleSearch":{}}]}`,
	} {
		if _, err := GeminiToChatRequest([]byte(body), "m"); err == nil {
			t.Fatalf("unrepresentable Gemini request was accepted: %s", body)
		}
	}
}

func TestGeminiToChatRequestSkipsThoughtHistory(t *testing.T) {
	input := []byte(`{"contents":[{"role":"model","parts":[{"text":"secret","thought":true}]},{"role":"model","parts":[{"text":"answer"}]},{"role":"user","parts":[{"text":"ok"}]}]}`)
	converted, err := GeminiToChatRequest(input, "m")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(converted), "secret") {
		t.Fatalf("thought history leaked into the Chat request: %s", converted)
	}
	var result struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("thought-only turn was not skipped: %s", converted)
	}
}

func TestChatToGeminiResponseConverts(t *testing.T) {
	input := []byte(`{"id":"chat_1","object":"chat.completion","system_fingerprint":"fp","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","reasoning_content":"chain","content":"Searching","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"test\"}"}}]}}],"usage":{"prompt_tokens":8,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2}}}`)
	converted, usage, err := ChatToGeminiResponse(input, "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Seen || usage.Input != 8 || usage.Output != 3 || !usage.Complete {
		t.Fatalf("usage was not preserved: %+v", usage)
	}
	body := string(converted)
	for _, want := range []string{
		`"responseId":"chat_1"`,
		`"modelVersion":"client-model"`,
		`"text":"chain","thought":true`,
		`"text":"Searching"`,
		`"functionCall"`, `"id":"call_1"`, `"name":"search"`, `"args":{"q":"test"}`,
		`"finishReason":"STOP"`,
		`"promptTokenCount":8`,
		`"cachedContentTokenCount":2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Gemini response is missing %s: %s", want, body)
		}
	}
	if _, _, err := ChatToGeminiResponse([]byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`), "m"); err != nil {
		t.Fatalf("length finish was not mapped: %v", err)
	}
	if _, _, err := ChatToGeminiResponse([]byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"finish_reason":"weird","message":{"role":"assistant","content":"x"}}]}`), "m"); err == nil {
		t.Fatal("unknown finish reason was accepted")
	}
}

func TestChatToGeminiResponseWithoutUsage(t *testing.T) {
	converted, usage, err := ChatToGeminiResponse([]byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`), "m")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Seen || usage.Input != 0 || !usage.Complete {
		t.Fatalf("usage-less response claimed provider usage: %+v", usage)
	}
	if strings.Contains(string(converted), "usageMetadata") {
		t.Fatalf("usage-less response emitted usageMetadata: %s", converted)
	}
}

func TestChatStreamToGeminiEvents(t *testing.T) {
	fixture := "data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"think\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"q\\\":1}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":9,\"prompt_cache_hit_tokens\":1}}\n\n" +
		"data: [DONE]\n\n"
	writer := &recordingWriter{}
	usage, err := (geminiChat{}).Stream(writer, strings.NewReader(fixture), "client-alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Seen || usage.Input != 4 || usage.Output != 9 || !usage.Complete {
		t.Fatalf("stream usage is wrong: %+v", usage)
	}
	body := writer.buffer.String()
	for _, want := range []string{
		`"text":"think","thought":true`,
		`"text":"hello"`,
		`"functionCall"`, `"id":"call_1"`, `"name":"search"`, `"args":{"q":1}`,
		`"finishReason":"STOP"`,
		`"promptTokenCount":4`,
		`"cachedContentTokenCount":1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Gemini stream is missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "gateway_stream_error") {
		t.Fatalf("successful stream reported an error: %s", body)
	}
}

func TestChatStreamToGeminiRejectsTruncation(t *testing.T) {
	fixture := "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"
	writer := &recordingWriter{}
	if _, err := (geminiChat{}).Stream(writer, strings.NewReader(fixture), "alias"); err == nil {
		t.Fatal("truncated Chat stream was accepted")
	}
	if !strings.Contains(writer.buffer.String(), `"error"`) {
		t.Fatalf("conversion failure was not surfaced to the client: %s", writer.buffer.String())
	}
}

func TestEnsureChatStreamOptionsIsIdempotent(t *testing.T) {
	first, err := EnsureChatStreamOptions([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `"stream":true`) || !strings.Contains(string(first), `"include_usage":true`) {
		t.Fatalf("stream options were not restored: %s", first)
	}
	second, err := EnsureChatStreamOptions(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("EnsureChatStreamOptions is not idempotent: %s vs %s", first, second)
	}
}
