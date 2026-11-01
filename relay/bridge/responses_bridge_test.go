package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// TestResponsesChatConverter covers the Responses→Chat direct pair: request
// projection, response reconstruction, and the chunk-to-event stream.
func TestResponsesChatConverter(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.OpenAIChat)
	if err != nil || converter == nil {
		t.Fatalf("Responses to Chat pair is not registered: %v", err)
	}
	request, err := converter.Request([]byte(`{
		"model":"ignored","input":[
			{"type":"message","role":"system","content":"be brief"},
			{"type":"message","role":"user","content":"hi"}
		],
		"instructions":"use tools","max_output_tokens":64,
		"tools":[{"type":"function","name":"weather","parameters":{"type":"object"}}],
		"tool_choice":"auto","temperature":0.2,"stream":true}`), "gpt-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var chat struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		MaxTokens     int64 `json:"max_tokens"`
		Stream        bool  `json:"stream"`
		Tools         []any `json:"tools"`
		ToolChoice    any   `json:"tool_choice"`
		Temperature   any   `json:"temperature"`
		InstructionsX any   `json:"instructions"`
	}
	if err := json.Unmarshal(request, &chat); err != nil {
		t.Fatal(err)
	}
	if chat.Model != "gpt-upstream" || !chat.Stream || chat.MaxTokens != 64 || chat.Temperature == nil {
		t.Fatalf("request head converted wrong: %s", request)
	}
	if len(chat.Messages) != 3 || chat.Messages[0].Role != "system" || chat.Messages[2].Role != "user" {
		t.Fatalf("messages converted wrong: %s", request)
	}
	if len(chat.Tools) != 1 || chat.ToolChoice != "auto" {
		t.Fatalf("tools converted wrong: %s", request)
	}
	// Fields with no Chat representation must be rejected, not dropped.
	if _, err := converter.Request([]byte(`{"model":"a","input":"hi","reasoning":{"effort":"low"}}`), "m"); err == nil {
		t.Fatal("reasoning field was not rejected")
	}

	response, usage, err := converter.Response([]byte(`{
		"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"upstream",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`), "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Input != 5 || usage.Output != 7 || !usage.Complete {
		t.Fatalf("usage lost: %+v", usage)
	}
	var responsesObject struct {
		Object string `json:"object"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(response, &responsesObject); err != nil {
		t.Fatal(err)
	}
	if responsesObject.Object != "response" || responsesObject.Status != "completed" ||
		len(responsesObject.Output) != 1 || responsesObject.Output[0].Content[0].Text != "hello" {
		t.Fatalf("response converted wrong: %s", response)
	}
}

// TestResponsesChatStream exercises the chunk-to-Responses state machine,
// including the tool-call argument path and the terminal completed event.
func TestResponsesChatStream(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.OpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := converter.(StreamConverter)
	if !ok {
		t.Fatal("Responses to Chat conversion does not stream")
	}
	upstream := "data: {\"id\":\"chatcmpl-9\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"up\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"he\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-9\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"up\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{\\\"a\\\":\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-9\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"up\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-9\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"up\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"
	writer := &testStreamWriter{}
	if _, err := streamer.Stream(writer, strings.NewReader(upstream), "client-model"); err != nil {
		t.Fatal(err)
	}
	output := writer.String()
	for _, want := range []string{
		"response.created", "response.output_text.delta", `"delta":"he"`,
		"response.output_item.added", `"type":"function_call"`, `"name":"f"`,
		`"delta":"{\"a\":"`, `"delta":"1}"`,
		"response.output_item.done", "response.completed", `"input_tokens":2`, `"output_tokens":3`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stream is missing %s: %s", want, output)
		}
	}
}

// TestResponsesAnthropicConverter covers the Responses→Anthropic pair.
func TestResponsesAnthropicConverter(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.Anthropic)
	if err != nil || converter == nil {
		t.Fatalf("Responses to Anthropic pair is not registered: %v", err)
	}
	request, err := converter.Request([]byte(`{
		"model":"ignored","instructions":"be brief",
		"input":[
			{"type":"message","role":"user","content":"hi"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"doing"}]},
			{"type":"function_call","call_id":"call_1","name":"f","arguments":"{\"a\":1}"},
			{"type":"function_call_output","call_id":"call_1","output":"done"}
		],
		"max_output_tokens":64,
		"tools":[{"type":"function","name":"f","description":"d","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"f"},"stream":true}`), "claude-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var messages struct {
		Model     string `json:"model"`
		MaxTokens int64  `json:"max_tokens"`
		System    string `json:"system"`
		Stream    bool   `json:"stream"`
		Messages  []struct {
			Role    string `json:"role"`
			Content []struct {
				Type  string         `json:"type"`
				Text  string         `json:"text"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"messages"`
		Tools      []any           `json:"tools"`
		ToolChoice map[string]any  `json:"tool_choice"`
		Extra      json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(request, &messages); err != nil {
		t.Fatal(err)
	}
	if messages.Model != "claude-upstream" || messages.MaxTokens != 64 || !messages.Stream || messages.System != "be brief" {
		t.Fatalf("request head converted wrong: %s", request)
	}
	if len(messages.Messages) != 4 ||
		messages.Messages[0].Content[0].Text != "hi" ||
		messages.Messages[1].Content[0].Text != "doing" ||
		messages.Messages[2].Content[0].Type != "tool_use" ||
		messages.Messages[3].Content[0].Type != "tool_result" {
		t.Fatalf("messages converted wrong: %s", request)
	}
	if messages.ToolChoice["type"] != "tool" || messages.ToolChoice["name"] != "f" {
		t.Fatalf("tool_choice converted wrong: %s", request)
	}
	// Extended thinking has no Messages representation on this pair.
	if _, err := converter.Request([]byte(`{"model":"a","input":[{"type":"reasoning"}],"max_output_tokens":1}`), "m"); err == nil {
		t.Fatal("reasoning item was not rejected")
	}

	response, usage, err := converter.Response([]byte(`{
		"id":"msg_1","type":"message","role":"assistant","model":"up",
		"content":[{"type":"text","text":"hi there"},{"type":"tool_use","id":"toolu_1","name":"f","input":{"a":1}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":9,"output_tokens":4,"cache_read_input_tokens":2}}`), "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Input != 11 || usage.Output != 4 {
		t.Fatalf("usage did not fold cache tokens: %+v", usage)
	}
	var responsesObject struct {
		Object string `json:"object"`
		Status string `json:"status"`
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Arguments string `json:"arguments"`
		} `json:"output"`
		Usage struct {
			InputTokens        int64 `json:"input_tokens"`
			InputTokensDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response, &responsesObject); err != nil {
		t.Fatal(err)
	}
	if responsesObject.Status != "completed" || len(responsesObject.Output) != 2 ||
		responsesObject.Output[1].Type != "function_call" || responsesObject.Output[1].CallID != "toolu_1" {
		t.Fatalf("response converted wrong: %s", response)
	}
	if responsesObject.Usage.InputTokensDetails.CachedTokens != 2 {
		t.Fatalf("cache detail lost: %s", response)
	}
}

// TestResponsesAnthropicStream exercises the Anthropic-SSE→Responses-SSE state
// machine, including the tool_use block index routing and terminal usage.
func TestResponsesAnthropicStream(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.Anthropic)
	if err != nil {
		t.Fatal(err)
	}
	streamer := converter.(StreamConverter)
	upstream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_s\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"up\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":6,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hey\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_7\",\"name\":\"f\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"x\\\":\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"2}\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":9}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	writer := &testStreamWriter{}
	if _, err := streamer.Stream(writer, strings.NewReader(upstream), "client-model"); err != nil {
		t.Fatal(err)
	}
	output := writer.String()
	for _, want := range []string{
		"response.created", "response.output_text.delta", `"delta":"hey"`,
		`"call_id":"toolu_7"`, `"delta":"{\"x\":"`, `"delta":"2}"`,
		"response.completed", `"input_tokens":6`, `"output_tokens":9`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stream is missing %s: %s", want, output)
		}
	}
}

// TestAnthropicResponsesConverter covers the Messages→Responses pair.
func TestAnthropicResponsesConverter(t *testing.T) {
	converter, err := For(native.Anthropic, native.OpenAIResponses)
	if err != nil || converter == nil {
		t.Fatalf("Anthropic to Responses pair is not registered: %v", err)
	}
	request, err := converter.Request([]byte(`{
		"model":"ignored","max_tokens":64,"system":"be brief","stream":true,
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"f","input":{"a":1}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]}
		],
		"tools":[{"name":"f","description":"d","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"any"}}`), "gpt-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var responsesRequest struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		MaxOutput    int64  `json:"max_output_tokens"`
		Stream       bool   `json:"stream"`
		Input        []struct {
			Type     string `json:"type"`
			CallID   string `json:"call_id"`
			Contents any    `json:"content"`
		} `json:"input"`
		Tools      []any `json:"tools"`
		ToolChoice any   `json:"tool_choice"`
	}
	if err := json.Unmarshal(request, &responsesRequest); err != nil {
		t.Fatal(err)
	}
	if responsesRequest.Model != "gpt-upstream" || responsesRequest.Instructions != "be brief" ||
		responsesRequest.MaxOutput != 64 || !responsesRequest.Stream || responsesRequest.ToolChoice != "required" {
		t.Fatalf("request head converted wrong: %s", request)
	}
	if len(responsesRequest.Input) != 3 ||
		responsesRequest.Input[1].Type != "function_call" || responsesRequest.Input[1].CallID != "toolu_1" ||
		responsesRequest.Input[2].Type != "function_call_output" {
		t.Fatalf("input items converted wrong: %s", request)
	}
	// stop_sequences has no Responses representation.
	if _, err := converter.Request([]byte(`{"model":"a","max_tokens":1,"messages":[{"role":"user","content":"hi"}],"stop_sequences":["END"]}`), "m"); err == nil {
		t.Fatal("stop_sequences was not rejected")
	}

	response, usage, err := converter.Response([]byte(`{
		"id":"resp_1","object":"response","status":"completed","created_at":1700000000,
		"output":[
			{"type":"reasoning","summary":[]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
			{"type":"function_call","call_id":"call_2","name":"f","arguments":"{\"b\":2}"}
		],
		"usage":{"input_tokens":8,"output_tokens":5,"input_tokens_details":{"cached_tokens":3}}}`), "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Input != 8 || usage.Output != 5 || !usage.Complete {
		t.Fatalf("usage lost: %+v", usage)
	}
	var messages struct {
		Type       string `json:"type"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			ID   string `json:"id"`
		} `json:"content"`
		Usage struct {
			InputTokens          int64 `json:"input_tokens"`
			CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
			OutputTokens         int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response, &messages); err != nil {
		t.Fatal(err)
	}
	if messages.Type != "message" || messages.StopReason != "tool_use" ||
		messages.Content[0].Text != "hello" || messages.Content[1].ID != "call_2" {
		t.Fatalf("response converted wrong: %s", response)
	}
	if messages.Usage.CacheReadInputTokens != 3 || messages.Usage.InputTokens != 5 {
		t.Fatalf("cache detail not split: %s", response)
	}
}

// TestAnthropicResponsesStream exercises the Responses-SSE→Anthropic-SSE state
// machine, including refusal-as-text and the terminal message pair.
func TestAnthropicResponsesStream(t *testing.T) {
	converter, err := For(native.Anthropic, native.OpenAIResponses)
	if err != nil {
		t.Fatal(err)
	}
	streamer := converter.(StreamConverter)
	upstream := "data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_5\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"sequence_number\":1,\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"in_progress\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.content_part.added\",\"sequence_number\":2,\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"sequence_number\":3,\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hola\"}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"sequence_number\":4,\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_9\",\"name\":\"f\",\"arguments\":\"\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"sequence_number\":5,\"item_id\":\"fc_1\",\"output_index\":1,\"delta\":\"{\\\"y\\\":1}\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"sequence_number\":6,\"response\":{\"id\":\"resp_5\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":6}}}\n\n"
	writer := &testStreamWriter{}
	if _, err := streamer.Stream(writer, strings.NewReader(upstream), "client-model"); err != nil {
		t.Fatal(err)
	}
	output := writer.String()
	for _, want := range []string{
		"event: message_start", `"id":"resp_5"`, "text_delta", `"text":"hola"`,
		`"type":"tool_use"`, `"id":"call_9"`, `"partial_json":"{\"y\":1}"`,
		"event: message_delta", `"stop_reason":"tool_use"`, "event: message_stop",
		`"input_tokens":4`, `"output_tokens":6`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stream is missing %s: %s", want, output)
		}
	}
}

// TestResponsesGeminiConverter covers the Responses→Gemini pair.
func TestResponsesGeminiConverter(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.Gemini)
	if err != nil || converter == nil {
		t.Fatalf("Responses to Gemini pair is not registered: %v", err)
	}
	request, err := converter.Request([]byte(`{
		"model":"ignored","instructions":"be brief",
		"input":[
			{"type":"message","role":"user","content":"hi"},
			{"type":"function_call","call_id":"call_1","name":"f","arguments":"{\"a\":1}"},
			{"type":"function_call_output","call_id":"call_1","output":"done"}
		],
		"max_output_tokens":64,
		"tools":[{"type":"function","name":"f","parameters":{"type":"object"}}],
		"tool_choice":"required","stream":false}`), "gemini-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var geminiRequest struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text             string         `json:"text"`
				FunctionCall     map[string]any `json:"functionCall"`
				FunctionResponse map[string]any `json:"functionResponse"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig map[string]any `json:"generationConfig"`
		Tools            []any          `json:"tools"`
		ToolConfig       struct {
			FunctionCallingConfig map[string]any `json:"functionCallingConfig"`
		} `json:"toolConfig"`
	}
	if err := json.Unmarshal(request, &geminiRequest); err != nil {
		t.Fatal(err)
	}
	if geminiRequest.SystemInstruction.Parts[0].Text != "be brief" ||
		geminiRequest.GenerationConfig["maxOutputTokens"] != float64(64) ||
		len(geminiRequest.Tools) != 1 ||
		geminiRequest.ToolConfig.FunctionCallingConfig["mode"] != "ANY" {
		t.Fatalf("request head converted wrong: %s", request)
	}
	if len(geminiRequest.Contents) != 3 ||
		geminiRequest.Contents[1].Parts[0].FunctionCall == nil ||
		geminiRequest.Contents[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("contents converted wrong: %s", request)
	}
	// Gemini cannot fetch remote images.
	if _, err := converter.Request([]byte(`{"model":"a","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/i.png"}]}]}`), "m"); err == nil {
		t.Fatal("remote image URL was not rejected")
	}

	response, usage, err := converter.Response([]byte(`{
		"responseId":"g1","modelVersion":"gemini-upstream",
		"candidates":[{"index":0,"finishReason":"STOP","content":{"role":"model","parts":[{"text":"bonjour"}]}}],
		"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"totalTokenCount":10}}`), "client-model")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Input != 7 || usage.Output != 3 || !usage.Complete {
		t.Fatalf("usage lost: %+v", usage)
	}
	var responsesObject struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(response, &responsesObject); err != nil {
		t.Fatal(err)
	}
	if responsesObject.Status != "completed" || len(responsesObject.Output) != 1 ||
		responsesObject.Output[0].Content[0].Text != "bonjour" {
		t.Fatalf("response converted wrong: %s", response)
	}
}

// TestResponsesGeminiStream exercises the Gemini-SSE→Responses-SSE state
// machine, including the accumulated function call and terminal status.
func TestResponsesGeminiStream(t *testing.T) {
	converter, err := For(native.OpenAIResponses, native.Gemini)
	if err != nil {
		t.Fatal(err)
	}
	streamer := converter.(StreamConverter)
	upstream := "data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"salut\"}]}}],\"responseId\":\"g9\"}\n\n" +
		"data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"f\",\"args\":{\"z\":1},\"id\":\"call_3\"}}]}}]}\n\n" +
		"data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2,\"totalTokenCount\":7}}\n\n"
	writer := &testStreamWriter{}
	if _, err := streamer.Stream(writer, strings.NewReader(upstream), "client-model"); err != nil {
		t.Fatal(err)
	}
	output := writer.String()
	for _, want := range []string{
		"response.created", `"delta":"salut"`, `"call_id":"call_3"`, `"arguments":"{\"z\":1}"`,
		"response.completed", `"input_tokens":5`, `"output_tokens":2`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("stream is missing %s: %s", want, output)
		}
	}
}
