package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/native"
	"github.com/stretchr/testify/require"
)

func TestChatToAnthropicPreservesToolsAndConversation(t *testing.T) {
	input := []byte(`{"model":"alias","max_completion_tokens":24,"messages":[{"role":"system","content":"system"},{"role":"developer","content":"developer"},{"role":"user","content":[{"type":"text","text":"find it"}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"test\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"found"}],"tools":[{"type":"function","function":{"name":"search","description":"Search","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}],"tool_choice":"required","stop":["done"]}`)
	converted, err := ChatToAnthropic(input, "claude-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Model     string `json:"model"`
		MaxTokens int64  `json:"max_tokens"`
		System    string `json:"system"`
		Messages  []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		Tools      []map[string]any `json:"tools"`
		ToolChoice map[string]any   `json:"tool_choice"`
		Stop       []string         `json:"stop_sequences"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.Model != "claude-upstream" || result.MaxTokens != 24 || result.System != "system\n\ndeveloper" || len(result.Messages) != 3 || result.Messages[1].Content[0]["type"] != "tool_use" || result.Messages[2].Content[0]["tool_use_id"] != "call_1" || result.ToolChoice["type"] != "any" || len(result.Tools) != 1 || result.Stop[0] != "done" {
		t.Fatalf("Chat request conversion lost fields: %s", converted)
	}
}

func TestChatToAnthropicRejectsLossyFeatures(t *testing.T) {
	for _, body := range []string{
		`{"model":"alias","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"high"}}]}]}`,
		`{"model":"alias","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`,
		`{"model":"alias","messages":[{"role":"user","content":"hi"}],"n":2}`,
	} {
		if _, err := ChatToAnthropic([]byte(body), "claude-upstream"); err == nil {
			t.Fatalf("lossy Chat request was accepted: %s", body)
		}
	}
}

func TestChatToAnthropicForwardsStreamFlag(t *testing.T) {
	converted, err := ChatToAnthropic([]byte(`{"model":"alias","messages":[{"role":"user","content":"hi"}],"stream":true}`), "claude-upstream")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(converted, &result); err != nil || !result.Stream {
		t.Fatalf("stream flag was not forwarded: %s err %v", converted, err)
	}
}

func TestAnthropicToChatConvertsToolsUsageAndRejectsThinking(t *testing.T) {
	input := []byte(`{"id":"msg_1","type":"message","stop_reason":"tool_use","content":[{"type":"text","text":"Searching"},{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"test"}}],"usage":{"input_tokens":3,"cache_read_input_tokens":2,"output_tokens":7}}`)
	converted, usage, err := AnthropicToChat(input, "client-alias")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(converted, &result); err != nil {
		t.Fatal(err)
	}
	if result.Model != "client-alias" || result.Choices[0].FinishReason != "tool_calls" || result.Choices[0].Message.Content != "Searching" || result.Choices[0].Message.ToolCalls[0].ID != "toolu_1" || result.Choices[0].Message.ToolCalls[0].Function.Name != "search" || !strings.Contains(result.Choices[0].Message.ToolCalls[0].Function.Arguments, `"q":"test"`) || !usage.Seen || usage.Input != 5 || usage.Output != 7 {
		t.Fatalf("Anthropic response conversion lost fields: %s usage %+v", converted, usage)
	}
	// Visible thinking maps onto reasoning_content; redacted thinking cannot
	// round-trip through the Chat wire and fails loudly.
	withThinking, _, err := AnthropicToChat([]byte(`{"id":"msg_2","type":"message","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"ok"}]}`), "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withThinking), `"reasoning_content":"private"`) {
		t.Fatalf("thinking block was not carried as reasoning_content: %s", withThinking)
	}
	if _, _, err := AnthropicToChat([]byte(`{"id":"msg_3","type":"message","stop_reason":"end_turn","content":[{"type":"redacted_thinking","data":"blob"}]}`), "alias"); err == nil {
		t.Fatal("redacted thinking was accepted on the Chat wire")
	}
}

func TestAnthropicToChatResponseFieldTolerance(t *testing.T) {
	// Additive provider envelope fields (tool accounting, sandbox containers)
	// ride provider_metadata instead of failing or vanishing. Citations have
	// no Chat slot and fail loudly so the client can pick the Anthropic wire.
	for _, body := range []string{
		`{"id":"msg_1","type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"output_tokens":1,"server_tool_use":{"web_search_requests":1}}}`,
		`{"id":"msg_1","type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"container":{"id":"x"}}`,
	} {
		converted, _, err := AnthropicToChat([]byte(body), "alias")
		if err != nil {
			t.Fatalf("additive Anthropic response field was rejected: %s err %v", body, err)
		}
		if !strings.Contains(string(converted), `"content":"ok"`) {
			t.Fatalf("tolerated response lost content: %s", converted)
		}
	}
	if _, _, err := AnthropicToChat([]byte(`{"id":"msg_1","type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"ok","citations":[{"type":"web_search_result_location"}]}]}`), "alias"); err == nil {
		t.Fatal("citations were silently dropped on the Chat wire")
	}
	// Content-shape problems still fail: the data would be lost or reordered.
	for _, body := range []string{
		`{"id":"msg_1","type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{}},{"type":"text","text":"after tool"}]}`,
		`{"id":"msg_1","type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":true}]}`,
	} {
		if _, _, err := AnthropicToChat([]byte(body), "alias"); err == nil {
			t.Fatalf("lossy Anthropic response was accepted: %s", body)
		}
	}
}

// TestAnthropicToChatAcceptsStopSequence pins that a response which hit a
// client-configured stop word converts with finish "stop" instead of failing:
// the request side forwards stop_sequences, so the response side must accept
// the matching stop_sequence report.
func TestAnthropicToChatAcceptsStopSequence(t *testing.T) {
	input := []byte(`{"id":"msg_1","type":"message","stop_reason":"stop_sequence","stop_sequence":"END","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"output_tokens":1}}`)
	converted, _, err := AnthropicToChat(input, "alias")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(converted), `"finish_reason":"stop"`) {
		t.Fatalf("stop_sequence response did not convert to finish stop: %s", converted)
	}
}

func TestClaudeBridgeCacheControlsAndToolChoice(t *testing.T) {
	input := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral","ttl":"1h"}}]},{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"search","parameters":{}}}],"parallel_tool_calls":false}`)
	converted, err := ChatToAnthropic(input, "claude")
	require.NoError(t, err)
	require.Contains(t, string(converted), `"disable_parallel_tool_use":true`)
	require.Contains(t, string(converted), `"max_tokens":4096`)
	converted, err = ConfigureAnthropicBridge(converted, 8192, true, false)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(converted), `"cache_control"`))
	require.Contains(t, string(converted), `"ttl":"1h"`)

	plain, err := ChatToAnthropic([]byte(`{"messages":[{"role":"user","content":"hello"}]}`), "claude")
	require.NoError(t, err)
	bedrock, err := ConfigureAnthropicBridge(plain, 4096, true, true)
	require.NoError(t, err)
	require.Contains(t, string(bedrock), `"content":[{"cache_control":{"type":"ephemeral"}`)
	gemini, err := For(native.OpenAIChat, native.Gemini)
	require.NoError(t, err)
	_, err = gemini.Request([]byte(`{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"search","parameters":{}}}],"parallel_tool_calls":false}`), "gemini")
	require.ErrorContains(t, err, "cannot enforce")

	marked := []byte(`{"max_tokens":1024,"system":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"tools":[{"name":"search","input_schema":{"type":"object","properties":{"cache_control":{"type":"string"}}},"cache_control":{"type":"ephemeral"}}],"cache_control":{"type":"ephemeral"}}`)
	for _, target := range []native.Protocol{native.OpenAIChat, native.OpenAIResponses, native.Gemini} {
		converter, err := For(native.Anthropic, target)
		require.NoError(t, err)
		out, err := converter.Request(marked, "target")
		require.NoError(t, err)
		require.Equal(t, 1, strings.Count(string(out), `"cache_control"`), "tool schema property must be preserved")
		require.NotContains(t, string(out), "ephemeral")
	}
}
