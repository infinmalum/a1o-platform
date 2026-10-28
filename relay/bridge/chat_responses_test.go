package bridge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/native"
	"github.com/infinmalum/a1o-platform/relay/upstream/openai"
)

type testStreamWriter struct{ bytes.Buffer }

func (w *testStreamWriter) Flush() {}

func chatResponsesConverter() *chatResponses { return &chatResponses{} }

func TestChatToResponsesMapsConversationAndTools(t *testing.T) {
	input := []byte(`{"model":"alias","max_completion_tokens":32,"messages":[{"role":"system","content":"be brief"},{"role":"developer","content":"cite sources"},{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"low"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"x\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"}],"tools":[{"type":"function","function":{"name":"search","description":"Search","parameters":{"type":"object"},"strict":true}}],"tool_choice":{"type":"function","function":{"name":"search"}},"parallel_tool_calls":true,"temperature":0.5}`)
	converted, err := chatResponsesConverter().Request(input, "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Model        string           `json:"model"`
		Stream       bool             `json:"stream"`
		Store        bool             `json:"store"`
		Instructions string           `json:"instructions"`
		MaxOutput    int64            `json:"max_output_tokens"`
		Temperature  float64          `json:"temperature"`
		Parallel     bool             `json:"parallel_tool_calls"`
		Input        []map[string]any `json:"input"`
		Tools        []map[string]any `json:"tools"`
		ToolChoice   map[string]any   `json:"tool_choice"`
		MaxTokens    any              `json:"max_tokens"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.Model != "gpt-5" || !result.Stream || result.Store || result.Instructions != "be brief\n\ncite sources" ||
		result.MaxOutput != 32 || result.Temperature != 0.5 || !result.Parallel || result.MaxTokens != nil {
		t.Fatalf("Chat to Responses request lost or kept wrong fields: %s", converted)
	}
	if len(result.Input) != 3 {
		t.Fatalf("expected three input items, got %d: %s", len(result.Input), converted)
	}
	user := result.Input[0]
	if user["role"] != "user" {
		t.Fatalf("expected user message item: %s", converted)
	}
	parts := user["content"].([]any)
	if parts[0].(map[string]any)["type"] != "input_text" || parts[1].(map[string]any)["type"] != "input_image" || parts[1].(map[string]any)["detail"] != "low" {
		t.Fatalf("multimodal parts were not converted: %s", converted)
	}
	if result.Input[1]["type"] != "function_call" || result.Input[1]["call_id"] != "call_1" {
		t.Fatalf("assistant tool_calls did not convert: %s", converted)
	}
	if result.Input[2]["type"] != "function_call_output" || result.Input[2]["call_id"] != "call_1" {
		t.Fatalf("tool result did not convert: %s", converted)
	}
	if result.Tools[0]["name"] != "search" || result.Tools[0]["strict"] != true {
		t.Fatalf("function tool was not flattened: %s", converted)
	}
	if result.ToolChoice["name"] != "search" || result.ToolChoice["type"] != "function" {
		t.Fatalf("named tool_choice did not convert: %s", converted)
	}
}

func TestChatToResponsesMapsResponseFormat(t *testing.T) {
	converted, err := chatResponsesConverter().Request([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"out","schema":{"type":"object"},"strict":false}}}`), "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Text struct {
			Format map[string]any `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	format := result.Text.Format
	if format["type"] != "json_schema" || format["name"] != "out" || format["strict"] != false {
		t.Fatalf("response_format did not convert to text.format: %s", converted)
	}
}

func TestChatToResponsesRejectsUnrepresentableRequests(t *testing.T) {
	for _, body := range []string{
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"stop":["done"]}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"frequency_penalty":0.5}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"logit_bias":{"50256":-100}}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"},{"role":"system","content":"late"}]}`,
		`{"model":"a","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"x"}}]}]}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search"}]}`,
		`{"model":"a","messages":[{"role":"user","content":"hi"}],"tool_choice":"required","stream_options":{"unknown":true}}`,
		`{"model":"a","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"not json"}}]},{"role":"user","content":"hi"}]}`,
	} {
		if _, err := chatResponsesConverter().Request([]byte(body), "gpt-5"); err == nil {
			t.Fatalf("unrepresentable Chat request was accepted: %s", body)
		}
	}
}

func TestResponsesToChatConvertsTextAndUsage(t *testing.T) {
	body := []byte(`{"id":"resp_1","object":"response","created_at":1730000000,"status":"completed","output":[{"type":"reasoning","id":"rs","summary":[]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	converted, usage, err := chatResponsesConverter().Response(body, "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Seen || !usage.Complete || usage.Input != 11 || usage.Output != 7 {
		t.Fatalf("usage was not observed: %+v", usage)
	}
	var result struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Message      map[string]any `json:"message"`
			FinishReason string         `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]int64 `json:"usage"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "resp_1" || result.Object != "chat.completion" || result.Model != "alias" || result.Created != 1730000000 {
		t.Fatalf("Chat envelope was not built from the Responses object: %s", converted)
	}
	if result.Choices[0].FinishReason != "stop" || result.Choices[0].Message["content"] != "hello" {
		t.Fatalf("message conversion failed: %s", converted)
	}
	if result.Usage["prompt_tokens"] != 11 || result.Usage["completion_tokens"] != 7 || result.Usage["total_tokens"] != 18 {
		t.Fatalf("usage was not converted: %s", converted)
	}
}

func TestResponsesToChatConvertsToolCallsAndIncomplete(t *testing.T) {
	body := []byte(`{"id":"resp_2","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","call_id":"call_9","name":"search","arguments":"{\"q\":1}"}],"usage":{"input_tokens":3,"output_tokens":4}}`)
	converted, _, err := chatResponsesConverter().Response(body, "alias")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Choices []struct {
			Message struct {
				ToolCalls []map[string]any `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.Choices[0].FinishReason != "length" || len(result.Choices[0].Message.ToolCalls) != 1 || result.Choices[0].Message.ToolCalls[0]["id"] != "call_9" {
		t.Fatalf("incomplete tool call response did not convert: %s", converted)
	}
}

func TestResponsesToChatRejectsUnrepresentableResponses(t *testing.T) {
	for _, body := range []string{
		`{"id":"r","status":"in_progress","output":[]}`,
		`{"id":"r","status":"incomplete","incomplete_details":{"reason":"context_window"},"output":[]}`,
		`{"id":"r","status":"failed","error":{"message":"policy"}}`,
		`{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"x","annotations":[{"type":"url_citation","url":"https://x"}]}]}]}`,
		`{"id":"r","status":"completed","output":[{"type":"web_search_call","id":"w"}]}`,
		`{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"unknown_part"}]}]}`,
	} {
		if _, _, err := chatResponsesConverter().Response([]byte(body), "alias"); err == nil {
			t.Fatalf("unrepresentable Responses object was accepted: %s", body)
		}
	}
}

func TestResponsesToChatAggregatesEventStreams(t *testing.T) {
	stream := "event: response.created\n" +
		`data: {"type":"response.created","response":{"id":"resp_3"}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"he"}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_3","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":5,"output_tokens":2}}}` + "\n\n"
	converted, usage, err := chatResponsesConverter().Response([]byte(stream), "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Seen || usage.Input != 5 || usage.Output != 2 {
		t.Fatalf("aggregated usage was not observed: %+v", usage)
	}
	var result struct {
		ID      string `json:"id"`
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "resp_3" || result.Choices[0].Message["content"] != "hello" {
		t.Fatalf("aggregated stream did not convert: %s", converted)
	}

	failed := "event: response.failed\n" +
		`data: {"type":"response.failed","response":{"status":"failed","error":{"message":"backend overloaded"}}}` + "\n\n"
	if _, _, err := chatResponsesConverter().Response([]byte(failed), "alias"); err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("failed stream was not reported: %v", err)
	}
	if _, _, err := chatResponsesConverter().Response([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n"), "alias"); err == nil {
		t.Fatal("truncated stream was accepted")
	}
}

func TestResponsesStreamConvertsTextAndToolCalls(t *testing.T) {
	stream := "" +
		`data: {"type":"response.created","sequence_number":1,"response":{"id":"resp_4","status":"in_progress"}}` + "\n\n" +
		`data: {"type":"response.in_progress","sequence_number":2,"response":{"id":"resp_4"}}` + "\n\n" +
		`data: {"type":"response.output_item.added","sequence_number":3,"output_index":0,"item":{"type":"reasoning","id":"rs"}}` + "\n\n" +
		`data: {"type":"response.reasoning_summary_text.delta","sequence_number":4,"delta":"thinking"}` + "\n\n" +
		`data: {"type":"response.output_item.added","sequence_number":5,"output_index":1,"item":{"type":"message","role":"assistant"}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","sequence_number":6,"output_index":1,"content_index":0,"delta":"he","obfuscation":"pad"}` + "\n\n" +
		`data: {"type":"response.output_text.delta","sequence_number":7,"output_index":1,"content_index":0,"delta":"llo"}` + "\n\n" +
		`data: {"type":"response.output_item.added","sequence_number":8,"output_index":2,"item":{"type":"function_call","call_id":"call_5","name":"search"}}` + "\n\n" +
		`data: {"type":"response.function_call_arguments.delta","sequence_number":9,"output_index":2,"delta":"{\"q\":"}` + "\n\n" +
		`data: {"type":"response.function_call_arguments.delta","sequence_number":10,"output_index":2,"delta":"\"x\"}"}` + "\n\n" +
		`data: {"type":"response.completed","sequence_number":11,"response":{"id":"resp_4","status":"completed","usage":{"input_tokens":9,"output_tokens":4}}}` + "\n\n"
	writer := &testStreamWriter{}
	usage, err := chatResponsesConverter().Stream(writer, strings.NewReader(stream), "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Seen || !usage.Complete || usage.Input != 9 || usage.Output != 4 {
		t.Fatalf("stream usage was not observed: %+v", usage)
	}
	var chunks []struct {
		Choices []struct {
			Delta        map[string]any `json:"delta"`
			FinishReason *string        `json:"finish_reason"`
		} `json:"choices"`
	}
	for _, line := range strings.Split(writer.String(), "\n\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "data: [DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta        map[string]any `json:"delta"`
				FinishReason *string        `json:"finish_reason"`
			} `json:"choices"`
		}
		payload := strings.TrimPrefix(line, "data: ")
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("invalid converted chunk %q: %v", payload, err)
		}
		chunks = append(chunks, chunk)
	}
	if !strings.HasSuffix(writer.String(), "data: [DONE]\n\n") {
		t.Fatalf("stream did not end with [DONE]: %q", writer.String())
	}
	var text string
	var toolCalls []any
	finish := ""
	for _, chunk := range chunks {
		if len(chunk.Choices) == 0 {
			continue
		}
		if value, ok := chunk.Choices[0].Delta["content"].(string); ok {
			text += value
		}
		if value, ok := chunk.Choices[0].Delta["tool_calls"].([]any); ok {
			toolCalls = append(toolCalls, value...)
		}
		if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != "" {
			finish = *chunk.Choices[0].FinishReason
		}
	}
	if text != "hello" {
		t.Fatalf("text deltas were lost: %q in %s", text, writer.String())
	}
	if finish != "tool_calls" || len(toolCalls) != 3 {
		t.Fatalf("tool call stream did not convert: finish %q calls %v in %s", finish, toolCalls, writer.String())
	}
}

func TestResponsesStreamHonorsIncludeUsageAndRefusal(t *testing.T) {
	converter := chatResponsesConverter()
	if _, err := converter.Request([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}],"stream_options":{"include_usage":true}}`), "gpt-5"); err != nil {
		t.Fatal(err)
	}
	stream := "" +
		`data: {"type":"response.created","response":{"id":"resp_5"}}` + "\n\n" +
		`data: {"type":"response.refusal.delta","delta":"no"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"resp_5","status":"completed","usage":{"input_tokens":2,"output_tokens":1}}}` + "\n\n"
	writer := &testStreamWriter{}
	usage, err := converter.Stream(writer, strings.NewReader(stream), "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !usage.Complete {
		t.Fatalf("stream was not complete: %+v", usage)
	}
	output := writer.String()
	if !strings.Contains(output, `"refusal":"no"`) {
		t.Fatalf("refusal delta was lost: %s", output)
	}
	var usageChunk struct {
		Choices []any            `json:"choices"`
		Usage   map[string]int64 `json:"usage"`
	}
	found := false
	for _, line := range strings.Split(output, "\n\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, `"choices":[]`) {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &usageChunk); err != nil {
			t.Fatal(err)
		}
		found = usageChunk.Usage["total_tokens"] == 3
	}
	if !found {
		t.Fatalf("include_usage chunk missing: %s", output)
	}
}

func TestResponsesStreamReportsFailuresAndTruncation(t *testing.T) {
	failed := `data: {"type":"response.failed","response":{"status":"failed","error":{"message":"quota exceeded"}}}` + "\n\n"
	writer := &testStreamWriter{}
	if _, err := chatResponsesConverter().Stream(writer, strings.NewReader(failed), "alias"); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("failure event was not reported: %v", err)
	}
	if !strings.Contains(writer.String(), "gateway_stream_error") {
		t.Fatalf("client was not told the stream failed: %s", writer.String())
	}

	writer = &testStreamWriter{}
	if _, err := chatResponsesConverter().Stream(writer, strings.NewReader(`data: {"type":"response.created","response":{"id":"r"}}`+"\n\n"), "alias"); err == nil {
		t.Fatal("truncated stream was accepted")
	}

	writer = &testStreamWriter{}
	if _, err := chatResponsesConverter().Stream(writer, strings.NewReader(`data: {"type":"response.created","response":{"id":"r"}}`+"\n\n"+`data: {"type":"mystery.event"}`+"\n\n"), "alias"); err == nil {
		t.Fatal("unknown event was accepted")
	}
}

func TestResponsesStreamCompletesArgumentsWithoutDeltas(t *testing.T) {
	stream := "" +
		`data: {"type":"response.created","response":{"id":"resp_6"}}` + "\n\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"f"}}` + "\n\n" +
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"f","arguments":"{}"}}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"resp_6","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n\n"
	writer := &testStreamWriter{}
	if _, err := chatResponsesConverter().Stream(writer, strings.NewReader(stream), "alias"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(writer.String(), `"arguments":"{}"`) {
		t.Fatalf("arguments without deltas were lost: %s", writer.String())
	}
}

func TestChatResponsesBridgeRegistrationAndValidation(t *testing.T) {
	converter, err := For(native.OpenAIChat, native.OpenAIResponses)
	if err != nil || converter == nil {
		t.Fatalf("Chat to Responses pair is not registered: %v", err)
	}
	if _, ok := converter.(StreamConverter); !ok {
		t.Fatal("Chat to Responses conversion does not stream")
	}
	if _, err := For(native.OpenAIResponses, native.OpenAIChat); err == nil {
		t.Fatal("reverse conversion should not exist")
	}
	// A converted Chat request must satisfy the subscription capability
	// matrix, while profile-forbidden fields fail explicitly.
	clean, err := chatResponsesConverter().Request([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}]}`), "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openai.ValidateResponsesRequest(clean); err != nil {
		t.Fatalf("converted request was rejected by the profile validator: %v", err)
	}
	restricted, err := chatResponsesConverter().Request([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}],"temperature":0.3}`), "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openai.ValidateResponsesRequest(restricted); err == nil || !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("profile-forbidden field was not rejected: %v", err)
	}
}
