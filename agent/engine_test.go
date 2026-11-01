package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// collectEvents returns an Emitter appending every envelope to the slice.
func collectEvents(out *[]EventEnvelope) Emitter {
	return func(event EventEnvelope) error {
		*out = append(*out, event)
		return nil
	}
}

// payloadMap decodes one envelope's payload for assertions.
func payloadMap(t *testing.T, envelope EventEnvelope) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("payload %s: %v", envelope.Payload, err)
	}
	return payload
}

func eventTypes(events []EventEnvelope) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

// fakeExecutor scripts Invoke results and records every request. hook, when
// set, runs after the request is recorded (tests use it to mutate run state
// mid-leg, e.g. simulating a fencing takeover).
type fakeExecutor struct {
	responses []InvokeResult
	errs      []error
	requests  []InvokeRequest
	hook      func()
}

func (f *fakeExecutor) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	f.requests = append(f.requests, req)
	if f.hook != nil {
		f.hook()
	}
	index := len(f.requests) - 1
	if index < len(f.errs) && f.errs[index] != nil {
		// A paired response carries partial state (e.g. the upstream's
		// response id) so tests can script engaged-but-failed legs.
		result := InvokeResult{}
		if index < len(f.responses) {
			result = f.responses[index]
		}
		return result, f.errs[index]
	}
	if index < len(f.responses) {
		response := f.responses[index]
		if req.OnTextDelta != nil {
			if text := messageText(response.Items); text != "" {
				req.OnTextDelta(text)
			}
		}
		return response, nil
	}
	return InvokeResult{}, nil
}

// newTurnFixture creates the session, user message, and run rows for one
// engine turn.
func newTurnFixture(t *testing.T, systemPrompt string) (*model.ChatSession, *model.AgentRun, []*model.ChatMessage) {
	t.Helper()
	session := &model.ChatSession{UserId: 1, Model: "test-model", SystemPrompt: systemPrompt}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "算一下 1+1"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)
	run := &model.AgentRun{UserId: session.UserId, SessionId: session.Id, Kind: "chat",
		Status: model.RunStatusQueued, Model: session.Model}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	return session, run, userMessageAsHistory(userMessage)
}

func TestTurnLoopExecutesToolRoundTrip(t *testing.T) {
	setupChatDB(t)
	session, run, history := newTurnFixture(t, "回答简短")

	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "我用工具算一下。"}}},
			{Type: ItemFunctionCall, CallID: "c1", Name: "echo", Arguments: `{"value":"1+1"}`},
		}, Usage: native.Usage{Input: 10, Output: 4, Seen: true}},
		{Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "等于 2。"}}},
		}, Usage: native.Usage{Input: 20, Output: 5, Seen: true}},
	}}
	tools := newToolSet()
	tools.add(Tool{Name: "echo", Description: "原样返回"}, func(ctx context.Context, req toolRequest) toolResult {
		var args struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
			t.Fatalf("bad tool arguments: %v", err)
		}
		return toolResult{Content: "结果: " + args.Value}
	})

	engine := &Engine{extraTools: tools}
	var events []EventEnvelope
	err := engine.RunChat(context.Background(), collectEvents(&events),
		ChatTurnRequest{Run: run, Session: session, History: history,
			Executor: executor, ContextBudget: 100000})
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}

	want := []string{
		model.RunEventRunStarted,
		model.RunEventMessageStarted, "response.output_text.delta", model.RunEventInvocationUsage,
		model.RunEventToolCalled, model.RunEventToolResult,
		model.RunEventMessageStarted, "response.output_text.delta", model.RunEventInvocationUsage,
		model.RunEventRunCompleted,
	}
	got := eventTypes(events)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	firstDelta := payloadMap(t, events[2])
	if firstDelta["delta"] != "我用工具算一下。" {
		t.Fatalf("first delta = %v", firstDelta)
	}
	secondDelta := payloadMap(t, events[7])
	if secondDelta["delta"] != "等于 2。" {
		t.Fatalf("second delta = %v", secondDelta)
	}
	toolCalled := payloadMap(t, events[4])
	if toolCalled["tool_call_id"] != "c1" || toolCalled["name"] != "echo" {
		t.Fatalf("tool.called event = %v", toolCalled)
	}
	toolResult := payloadMap(t, events[5])
	if toolResult["content"] != "结果: 1+1" {
		t.Fatalf("tool.result event = %v", toolResult)
	}
	completed := payloadMap(t, events[len(events)-1])
	if completed["prompt_tokens"] != float64(30) || completed["completion_tokens"] != float64(9) {
		t.Fatalf("run.completed usage = %v", completed)
	}

	messages, err := model.GetSessionMessages(session.Id)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("stored messages = %d, want 4 (user, assistant, tool, assistant)", len(messages))
	}
	if messages[1].Role != model.ChatRoleAssistant || messages[1].ToolCalls == nil {
		t.Fatalf("first assistant row missing tool calls: %+v", messages[1])
	}
	if messages[2].Role != model.ChatRoleTool || messages[2].ToolCallId != "c1" ||
		messages[2].Content != "结果: 1+1" {
		t.Fatalf("tool row wrong: %+v", messages[2])
	}
	if messages[3].Content != "等于 2。" || messages[3].Tokens != 5 {
		t.Fatalf("final assistant row wrong: %+v", messages[3])
	}
	// The second leg's request must carry the tool result plus the replayed
	// assistant items in standard form.
	second := executor.requests[1]
	sawCall, sawOutput := false, false
	for _, item := range second.Items {
		if item.Type == ItemFunctionCall && item.CallID == "c1" {
			sawCall = true
		}
		if item.Type == ItemFunctionCallOutput && item.CallID == "c1" && item.Output == "结果: 1+1" {
			sawOutput = true
		}
	}
	if !sawCall || !sawOutput {
		t.Fatalf("second leg items missing tool round trip: %+v", second.Items)
	}
	// Structural events persist to the run's event log.
	storedEvents, err := model.GetRunEventsAfter(run.Id, 0)
	if err != nil || len(storedEvents) == 0 {
		t.Fatalf("persisted events = %d, err %v", len(storedEvents), err)
	}
}

// userMessageAsHistory wraps one message for turn requests.
func userMessageAsHistory(message *model.ChatMessage) []*model.ChatMessage {
	return []*model.ChatMessage{message}
}

func TestTurnLoopReportsLegFailure(t *testing.T) {
	setupChatDB(t)
	session, run, history := newTurnFixture(t, "")

	executor := &fakeExecutor{
		responses: []InvokeResult{{Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "部分"}}},
		}}},
		errs: []error{context.Canceled},
	}
	engine := &Engine{}
	var events []EventEnvelope
	err := engine.RunChat(context.Background(), collectEvents(&events),
		ChatTurnRequest{Run: run, Session: session, History: history,
			Executor: executor, ContextBudget: 100000})
	if err == nil {
		t.Fatalf("RunChat should surface the leg error")
	}
	last := events[len(events)-1]
	if last.Type != model.RunEventRunCompleted {
		t.Fatalf("last event = %s, want run.completed", last.Type)
	}
	failed := payloadMap(t, events[len(events)-2])
	if events[len(events)-2].Type != model.RunEventRunFailed || failed["error"] == "" {
		t.Fatalf("run.failed event missing: %+v", events[len(events)-2])
	}
}

func TestTurnLoopHonorsMaxToolIterations(t *testing.T) {
	setupChatDB(t)
	session, run, history := newTurnFixture(t, "")

	calls := []Item{{Type: ItemFunctionCall, CallID: "c1", Name: "echo", Arguments: "{}"}}
	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: calls}, {Items: calls}, {Items: calls},
	}}
	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "ok"}
	})

	engine := &Engine{extraTools: tools}
	var events []EventEnvelope
	err := engine.RunChat(context.Background(), collectEvents(&events),
		ChatTurnRequest{Run: run, Session: session, History: history,
			Executor: executor, ContextBudget: 100000, MaxToolIterations: 2})
	if err == nil || !strings.Contains(err.Error(), "轮数上限") {
		t.Fatalf("err = %v, want 轮数上限", err)
	}
	if len(executor.requests) != 3 { // iterations 0..2, then the bound rejects
		t.Fatalf("legs executed = %d, want 3", len(executor.requests))
	}
	if events[len(events)-1].Type != model.RunEventRunCompleted {
		t.Fatalf("last event = %s, want run.completed", events[len(events)-1].Type)
	}
}

func TestResolveToolsSandboxToggleAndSkillFilter(t *testing.T) {
	if !(&Sandbox{}).Available() {
		t.Skip("本机没有 python3/node，跳过")
	}
	setupChatDB(t)
	previousEnabled := config.ChatSandboxEnabled
	config.ChatSandboxEnabled = true
	t.Cleanup(func() { config.ChatSandboxEnabled = previousEnabled })

	engine := Default()
	offSession := &model.ChatSession{UserId: 1, Model: "m", SandboxEnabled: false}
	if tools := engine.resolveTools(context.Background(), ChatTurnRequest{Session: offSession}); len(tools.defs) != 0 {
		t.Fatalf("sandbox tool present while session toggle off: %v", tools.defs)
	}
	onSession := &model.ChatSession{UserId: 1, Model: "m", SandboxEnabled: true}
	tools := engine.resolveTools(context.Background(), ChatTurnRequest{Session: onSession})
	if len(tools.defs) != 1 || tools.defs[0].Name != sandboxToolName {
		t.Fatalf("sandbox tool missing: %v", tools.defs)
	}

	master := &model.ChatSkill{Name: "锁定", Tools: ptrString(`["echo"]`)}
	tools = engine.resolveTools(context.Background(),
		ChatTurnRequest{Session: onSession, Skills: []*model.ChatSkill{master}})
	if len(tools.defs) != 0 {
		t.Fatalf("skill filter should drop unbound tools, got %v", tools.defs)
	}
}

func ptrString(raw string) *string { return &raw }

func TestSandboxExecutesPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("本机没有 python3，跳过")
	}
	// These checks exercise execution mechanics (output capture, exit codes),
	// not isolation, so they opt into the network-allowed path and stay
	// independent of host unshare support.
	previousNetwork := config.ChatSandboxNetworkEnabled
	config.ChatSandboxNetworkEnabled = true
	t.Cleanup(func() { config.ChatSandboxNetworkEnabled = previousNetwork })
	sandbox := &Sandbox{}
	output, err := sandbox.Execute(context.Background(), "python", "print('hello', 6*7)")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(output, "hello 42") {
		t.Fatalf("output = %q", output)
	}
	if _, err := sandbox.Execute(context.Background(), "python", "raise SystemExit(3)"); err == nil {
		t.Fatalf("non-zero exit should fail")
	}
}

func TestSandboxTimesOutInfiniteLoop(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("本机没有 python3，跳过")
	}
	previousTimeout := config.ChatSandboxTimeoutSeconds
	config.ChatSandboxTimeoutSeconds = 1
	t.Cleanup(func() { config.ChatSandboxTimeoutSeconds = previousTimeout })
	previousNetwork := config.ChatSandboxNetworkEnabled
	config.ChatSandboxNetworkEnabled = true
	t.Cleanup(func() { config.ChatSandboxNetworkEnabled = previousNetwork })

	sandbox := &Sandbox{}
	start := time.Now()
	_, err := sandbox.Execute(context.Background(), "python", "while True:\n    pass\n")
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("err = %v, want 执行超时", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestVirtualToolNameSlugifiesAndDedupes(t *testing.T) {
	used := map[string]bool{}
	name := VirtualToolName("服务器 A", "搜索！工具", used)
	if !strings.HasPrefix(name, "mcp_") || strings.ContainsAny(name, " ！") {
		t.Fatalf("unexpected name %q", name)
	}
	used[name] = true
	second := VirtualToolName("服务器 A", "搜索！工具", used)
	if second == name {
		t.Fatalf("collision not resolved: %q", second)
	}
	long := VirtualToolName(strings.Repeat("s", 80), strings.Repeat("t", 80), map[string]bool{})
	if len(long) > 64 {
		t.Fatalf("name too long: %d", len(long))
	}
}

// cancelExecutor cancels the caller's context while returning tool calls —
// the shape of a client vanishing mid-leg with tool calls already decided.
type cancelExecutor struct {
	calls   []Item
	cancels context.CancelFunc
}

func (c *cancelExecutor) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	c.cancels()
	return InvokeResult{Items: append([]Item{}, c.calls...)}, nil
}

// TestTurnLoopPersistsPlaceholdersWhenCancelledMidTurn pins the repair for
// interrupted tool turns: the stored history used to end with an assistant
// tool_calls row missing its tool messages, which the next turn's upstream
// rejects outright.
func TestTurnLoopPersistsPlaceholdersWhenCancelledMidTurn(t *testing.T) {
	setupChatDB(t)
	session, run, history := newTurnFixture(t, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &cancelExecutor{cancels: cancel, calls: []Item{
		{Type: ItemFunctionCall, CallID: "c1", Name: "echo", Arguments: "{}"},
		{Type: ItemFunctionCall, CallID: "c2", Name: "echo", Arguments: "{}"},
	}}
	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "ok"}
	})

	engine := &Engine{extraTools: tools}
	var events []EventEnvelope
	err := engine.RunChat(ctx, collectEvents(&events),
		ChatTurnRequest{Run: run, Session: session, History: history,
			Executor: executor, ContextBudget: 100000})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	messages, err := model.GetSessionMessages(session.Id)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	// Both tool calls are unanswered when the turn dies mid-leg, so both get
	// a placeholder: user, assistant with tool_calls, tool c1, tool c2.
	if len(messages) != 4 {
		t.Fatalf("stored messages = %d, want 4", len(messages))
	}
	if messages[1].Role != model.ChatRoleAssistant || messages[1].ToolCalls == nil {
		t.Fatalf("assistant row missing tool calls: %+v", messages[1])
	}
	if messages[2].Role != model.ChatRoleTool || messages[2].ToolCallId != "c1" ||
		messages[2].Name != "echo" {
		t.Fatalf("first placeholder row wrong: %+v", messages[2])
	}
	if messages[3].Role != model.ChatRoleTool || messages[3].ToolCallId != "c2" {
		t.Fatalf("second placeholder row wrong: %+v", messages[3])
	}
}

// TestTurnLoopDoesNotPersistHallucinatedToolCalls pins that tool calls from a
// model with no tools offered never reach the stored history: they would
// leave the same dangling tool_calls sequence the interrupted-turn repair
// guards against.
func TestTurnLoopDoesNotPersistHallucinatedToolCalls(t *testing.T) {
	setupChatDB(t)
	session, run, history := newTurnFixture(t, "")

	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "我没有工具可用。"}}},
			{Type: ItemFunctionCall, CallID: "ghost-1", Name: "not_offered", Arguments: "{}"},
		}},
	}}

	engine := &Engine{}
	var events []EventEnvelope
	err := engine.RunChat(context.Background(), collectEvents(&events),
		ChatTurnRequest{Run: run, Session: session, History: history,
			Executor: executor, ContextBudget: 100000})
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}

	messages, err := model.GetSessionMessages(session.Id)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(messages) != 2 { // user, assistant — no ghost tool row
		t.Fatalf("stored messages = %d, want 2", len(messages))
	}
	if messages[1].ToolCalls != nil {
		t.Fatalf("hallucinated tool calls persisted: %s", *messages[1].ToolCalls)
	}
}
