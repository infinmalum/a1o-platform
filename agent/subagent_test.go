package agent

import (
	"context"
	"testing"

	"github.com/infinmalum/norma-e/llm"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

type fakeInvoker struct {
	results []InvokeResult
	calls   []InvokeRequest
}

func (f *fakeInvoker) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	f.calls = append(f.calls, req)
	result := f.results[len(f.calls)-1]
	return result, nil
}

func TestRelayProviderStreamEvents(t *testing.T) {
	invoker := &fakeInvoker{results: []InvokeResult{{
		InvocationID: 7,
		Status:       ItemStatusCompleted,
		Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "先查一下。"}}},
			{Type: ItemFunctionCall, Name: "web_search", CallID: "call_real_1", Arguments: `{"query":"norma"}`},
		},
		Usage: native.Usage{Input: 100, Output: 20, CacheRead: 40},
	}}}
	p := &relayProvider{executor: invoker, runID: 1, modelName: "gpt-test"}
	req := llm.CompletionRequest{
		System:    []string{"sys-a", "sys-b"},
		Messages:  []llm.Message{llm.UserText("开始")},
		MaxTokens: 512,
	}
	var events []llm.StreamEvent
	for ev, err := range p.Stream(context.Background(), req) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != llm.SETextDelta || events[0].Text != "先查一下。" {
		t.Fatalf("unexpected text event: %+v", events[0])
	}
	if events[1].Type != llm.SEToolUseStart || events[1].ToolID != "0" || events[1].ToolName != "web_search" {
		t.Fatalf("unexpected tool start: %+v", events[1])
	}
	if events[2].Type != llm.SEToolInputJSON || events[2].Text != `{"query":"norma"}` {
		t.Fatalf("unexpected tool input: %+v", events[2])
	}
	if events[3].Type != llm.SEMessageDelta || events[3].StopReason != "end_turn" || events[3].Usage.OutputTokens != 20 {
		t.Fatalf("unexpected message delta: %+v", events[3])
	}
	if events[4].Type != llm.SEMessageStop {
		t.Fatalf("expected message stop: %+v", events[4])
	}
	leg := invoker.calls[0]
	if leg.Instructions != "sys-a\n\nsys-b" || leg.Model != "gpt-test" || leg.RunID != 1 {
		t.Fatalf("unexpected invoke request: %+v", leg)
	}
	if leg.MaxOutputTokens != 512 || !leg.SkipContextAppend {
		t.Fatalf("unexpected invoke options: %+v", leg)
	}
}

func TestRelayProviderToolReplayMapping(t *testing.T) {
	invoker := &fakeInvoker{results: []InvokeResult{
		{Items: []Item{
			{Type: ItemFunctionCall, Name: "web_search", CallID: "call_real_1", Arguments: "{}"},
		}},
		{Items: []Item{{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "done"}}}}},
	}}
	p := &relayProvider{executor: invoker, runID: 1, modelName: "m"}
	ctx := context.Background()
	for range p.Stream(ctx, llm.CompletionRequest{Messages: []llm.Message{llm.UserText("go")}}) {
	}
	// Second leg: harness replays the assistant tool_use (positional id) and
	// the tool result under the same id.
	replay := []llm.Message{
		llm.UserText("go"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{
			Type: llm.BlockToolUse, ID: "0", Name: "web_search", Input: []byte("{}"),
		}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("0", "结果", false)}},
	}
	for range p.Stream(ctx, llm.CompletionRequest{Messages: replay}) {
	}
	items := invoker.calls[1].Items
	var call, output *Item
	for i := range items {
		switch items[i].Type {
		case ItemFunctionCall:
			call = &items[i]
		case ItemFunctionCallOutput:
			output = &items[i]
		}
	}
	if call == nil || call.CallID != "call_real_1" {
		t.Fatalf("replayed function_call should carry the real provider id: %+v", call)
	}
	if output == nil || output.CallID != "call_real_1" || output.Output != "结果" {
		t.Fatalf("replayed output should map back to the real call id: %+v", output)
	}
}

func TestAddSubagentTool(t *testing.T) {
	previousEnabled := config.WorkSubagentEnabled
	previousTurns := config.WorkSubagentMaxTurns
	config.WorkSubagentEnabled = true
	config.WorkSubagentMaxTurns = 4
	t.Cleanup(func() {
		config.WorkSubagentEnabled = previousEnabled
		config.WorkSubagentMaxTurns = previousTurns
	})
	setWebTools(t, false)
	setMemoryTools(t, false)
	engine := &Engine{}
	req := WorkTurnRequest{
		Run:      &model.AgentRun{Id: 5, UserId: 1, Model: "gpt-test"},
		Executor: &fakeInvoker{},
	}
	set := engine.resolveWorkTools(context.Background(), req)
	if !defsContain(set.defs, subagentToolName) {
		t.Fatalf("Agent tool missing from work toolset: %v", set.defs)
	}

	config.WorkSubagentEnabled = false
	set = engine.resolveWorkTools(context.Background(), req)
	if defsContain(set.defs, subagentToolName) {
		t.Fatalf("Agent tool registered while disabled")
	}
}

func TestSubagentRegistryExcludesControlTools(t *testing.T) {
	p := &relayProvider{}
	set := newToolSet()
	set.add(completeTaskToolDef(), func(context.Context, toolRequest) toolResult { return toolResult{} })
	set.add(Tool{Name: subagentToolName, Description: "self"}, nil)
	registry := subagentRegistry(set, &model.AgentRun{Id: 1}, p)
	for _, tool := range registry.List() {
		if tool.Name() == CompleteTaskToolName || tool.Name() == subagentToolName {
			t.Fatalf("control tool %q must not reach subagents", tool.Name())
		}
	}
}
