package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// feedParser writes bytes in odd-sized chunks to exercise line buffering.
func feedParser(t *testing.T, parser *chatStreamParser, payload string) {
	t.Helper()
	rest := []byte(payload)
	for len(rest) > 0 {
		n := 1
		if len(rest) > 3 {
			n = 3
		}
		if _, err := parser.Write(rest[:n]); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		rest = rest[n:]
	}
}

func TestChatStreamParserAccumulatesContentAndUsage(t *testing.T) {
	var deltas []string
	parser := newChatStreamParser(func(delta string) { deltas = append(deltas, delta) })
	feedParser(t, parser,
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":null}]}\n\n"+
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\n"+
			"data: [DONE]\n\n")
	result, err := parser.finalize()
	if err != nil {
		t.Fatalf("finalize returned error: %v", err)
	}
	if result.Content != "你好" {
		t.Fatalf("content = %q, want %q", result.Content, "你好")
	}
	if result.PromptTokens != 12 || result.CompletionTokens != 3 {
		t.Fatalf("usage = %d/%d, want 12/3", result.PromptTokens, result.CompletionTokens)
	}
	if result.FinishReason != "" {
		t.Fatalf("finish reason = %q, want empty", result.FinishReason)
	}
	if strings.Join(deltas, "|") != "你|好" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestChatStreamParserMergesToolCallDeltas(t *testing.T) {
	parser := newChatStreamParser(nil)
	feedParser(t, parser,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"x\\\":\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1}\"}},{\"index\":1,\"id\":\"call_b\",\"function\":{\"name\":\"add\",\"arguments\":\"{}\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"+
			"data: [DONE]\n\n")
	result, err := parser.finalize()
	if err != nil {
		t.Fatalf("finalize returned error: %v", err)
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ID != "call_a" || result.ToolCalls[0].Name != "echo" || result.ToolCalls[0].Arguments != `{"x":1}` {
		t.Fatalf("first call = %+v", result.ToolCalls[0])
	}
	if result.ToolCalls[1].ID != "call_b" || result.ToolCalls[1].Name != "add" {
		t.Fatalf("second call = %+v", result.ToolCalls[1])
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", result.FinishReason)
	}
}

func TestChatStreamParserGeneratesMissingToolCallIDs(t *testing.T) {
	parser := newChatStreamParser(nil)
	feedParser(t, parser,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"a\",\"arguments\":\"{}\"}}]}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"name\":\"b\",\"arguments\":\"{}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n")
	result, err := parser.finalize()
	if err != nil {
		t.Fatalf("finalize returned error: %v", err)
	}
	if result.ToolCalls[0].ID != "call_1" || result.ToolCalls[1].ID != "call_2" {
		t.Fatalf("generated IDs = %q, %q", result.ToolCalls[0].ID, result.ToolCalls[1].ID)
	}
}

func TestChatStreamParserReportsInBandError(t *testing.T) {
	parser := newChatStreamParser(nil)
	feedParser(t, parser,
		"data: {\"choices\":[{\"delta\":{\"content\":\"部分\"}}]}\n\n"+
			"data: {\"error\":{\"message\":\"上游不可用\"}}\n\n")
	result, err := parser.finalize()
	if err == nil || err.Error() != "上游不可用" {
		t.Fatalf("err = %v, want 上游不可用", err)
	}
	if result.Content != "部分" {
		t.Fatalf("partial content lost: %q", result.Content)
	}
}

func TestChatStreamParserIgnoresForeignLines(t *testing.T) {
	parser := newChatStreamParser(nil)
	feedParser(t, parser,
		": keep-alive comment\n\n"+
			"event: ping\n\n"+
			"data: not json\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\r\n"+
			"data: [DONE]\n\n")
	result, err := parser.finalize()
	if err != nil {
		t.Fatalf("finalize returned error: %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("content = %q, want ok", result.Content)
	}
}

// fakeLeg replays scripted leg results and records requests.
type fakeLeg struct {
	responses []fakeLegResponse
	requests  []LegRequest
	deltas    []string
}

type fakeLegResponse struct {
	result LegResult
	err    error
}

func (f *fakeLeg) RunLeg(ctx context.Context, req LegRequest, onDelta func(string)) (LegResult, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return LegResult{}, errors.New("没有更多脚本响应")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	if onDelta != nil && response.result.Content != "" {
		onDelta(response.result.Content)
		f.deltas = append(f.deltas, response.result.Content)
	}
	return response.result, response.err
}
