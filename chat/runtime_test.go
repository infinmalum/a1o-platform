package chat

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
)

// collectEvents returns an Emitter appending every event to the slice.
func collectEvents(out *[]Event) Emitter {
	return func(event Event) error {
		*out = append(*out, event)
		return nil
	}
}

func TestRunLoopExecutesToolRoundTrip(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model", SystemPrompt: "回答简短"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "算一下 1+1"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	leg := &fakeLeg{responses: []fakeLegResponse{
		{result: LegResult{Content: "我用工具算一下。", ToolCalls: []LegToolCall{
			{ID: "c1", Name: "echo", Arguments: `{"value":"1+1"}`},
		}}},
		{result: LegResult{Content: "等于 2。", PromptTokens: 20, CompletionTokens: 5}},
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

	runtime := &Runtime{}
	var events []Event
	err := runtime.runLoop(context.Background(), collectEvents(&events),
		TurnRequest{Session: session, History: userMessageAsHistory(userMessage), Leg: leg}, tools)
	if err != nil {
		t.Fatalf("runLoop failed: %v", err)
	}

	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	joined := strings.Join(types, ",")
	want := EventMessageStart + "," + EventDelta + "," + EventUsage + "," +
		EventToolCall + "," + EventToolResult + "," +
		EventMessageStart + "," + EventDelta + "," + EventUsage + "," + EventDone
	if joined != want {
		t.Fatalf("event sequence = %q, want %q", joined, want)
	}
	if events[1].Delta != "我用工具算一下。" || events[6].Delta != "等于 2。" {
		t.Fatalf("deltas wrong: %+v %+v", events[1], events[6])
	}
	if events[3].ToolCallID != "c1" || events[3].Name != "echo" {
		t.Fatalf("tool_call event = %+v", events[3])
	}
	if events[4].Content != "结果: 1+1" {
		t.Fatalf("tool_result event = %+v", events[4])
	}
	if events[len(events)-1].CompletionTokens != 5 {
		t.Fatalf("done event usage = %+v", events[len(events)-1])
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
	// The second leg's request must carry the tool result as history.
	second := leg.requests[1]
	var body struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(second.Body, &body); err != nil {
		t.Fatalf("second leg body: %v", err)
	}
	if len(body.Messages) != 4 || body.Messages[3].Role != "tool" || body.Messages[3].ToolCallID != "c1" {
		t.Fatalf("second leg messages wrong: %+v", body.Messages)
	}
}

// userMessageAsHistory wraps one message for turn requests.
func userMessageAsHistory(message *model.ChatMessage) []*model.ChatMessage {
	return []*model.ChatMessage{message}
}

func TestRunLoopReportsLegFailure(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "hi"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	leg := &fakeLeg{responses: []fakeLegResponse{
		{result: LegResult{Content: "部分"}, err: context.Canceled},
	}}
	runtime := &Runtime{}
	var events []Event
	err := runtime.runLoop(context.Background(), collectEvents(&events),
		TurnRequest{Session: session, History: userMessageAsHistory(userMessage), Leg: leg}, newToolSet())
	if err == nil {
		t.Fatalf("runLoop should surface the leg error")
	}
	last := events[len(events)-1]
	if last.Type != EventDone {
		t.Fatalf("last event = %s, want done", last.Type)
	}
	if events[len(events)-2].Type != EventError || events[len(events)-2].Error == "" {
		t.Fatalf("error event missing: %+v", events[len(events)-2])
	}
}

func TestRunLoopHonorsMaxToolIterations(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "循环调用"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	calls := []LegToolCall{{ID: "c1", Name: "echo", Arguments: "{}"}}
	leg := &fakeLeg{responses: []fakeLegResponse{
		{result: LegResult{ToolCalls: calls}},
		{result: LegResult{ToolCalls: calls}},
		{result: LegResult{ToolCalls: calls}},
	}}
	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "ok"}
	})

	runtime := &Runtime{}
	var events []Event
	err := runtime.runLoop(context.Background(), collectEvents(&events),
		TurnRequest{Session: session, History: userMessageAsHistory(userMessage),
			Leg: leg, MaxToolIterations: 2}, tools)
	if err == nil || !strings.Contains(err.Error(), "轮数上限") {
		t.Fatalf("err = %v, want 轮数上限", err)
	}
	if len(leg.requests) != 3 { // iterations 0..2, then the bound rejects
		t.Fatalf("legs executed = %d, want 3", len(leg.requests))
	}
	last := events[len(events)-1]
	if last.Type != EventDone {
		t.Fatalf("last event = %s, want done", last.Type)
	}
}

func TestComposeBodyShape(t *testing.T) {
	temperature := 0.3
	session := &model.ChatSession{Model: "m1", Temperature: &temperature, MaxTokens: 77}
	messages := []openAIMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}
	tools := []Tool{{Name: "t1", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}

	raw, err := composeBody(session, messages, tools, 77)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "m1" || body["max_tokens"] != float64(77) || body["temperature"] != 0.3 {
		t.Fatalf("body scalars wrong: %v", body)
	}
	if body["stream"] != true {
		t.Fatalf("stream must be true: %v", body["stream"])
	}
	options, ok := body["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("stream_options wrong: %v", body["stream_options"])
	}
	toolDefs, ok := body["tools"].([]any)
	if !ok || len(toolDefs) != 1 {
		t.Fatalf("tools wrong: %v", body["tools"])
	}
	if body["tool_choice"] != "auto" {
		t.Fatalf("tool_choice wrong: %v", body["tool_choice"])
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

	runtime := Default()
	offSession := &model.ChatSession{UserId: 1, Model: "m", SandboxEnabled: false}
	if tools := runtime.resolveTools(context.Background(), TurnRequest{Session: offSession}); len(tools.defs) != 0 {
		t.Fatalf("sandbox tool present while session toggle off: %v", tools.defs)
	}
	onSession := &model.ChatSession{UserId: 1, Model: "m", SandboxEnabled: true}
	tools := runtime.resolveTools(context.Background(), TurnRequest{Session: onSession})
	if len(tools.defs) != 1 || tools.defs[0].Name != sandboxToolName {
		t.Fatalf("sandbox tool missing: %v", tools.defs)
	}

	master := &model.ChatSkill{Name: "锁定", Tools: ptrString(`["echo"]`)}
	tools = runtime.resolveTools(context.Background(),
		TurnRequest{Session: onSession, Skills: []*model.ChatSkill{master}})
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

// disconnectLeg cancels the caller's context while streaming, before the
// runtime persists the assistant row — the shape of a client vanishing
// mid-leg with tool calls already decided.
type disconnectLeg struct {
	calls   []LegToolCall
	cancels context.CancelFunc
}

func (l *disconnectLeg) RunLeg(ctx context.Context, req LegRequest, onDelta func(string)) (LegResult, error) {
	l.cancels()
	return LegResult{ToolCalls: l.calls}, nil
}

// TestRunLoopPersistsPlaceholdersWhenCancelledMidTurn pins the repair for
// interrupted tool turns: the stored history used to end with an assistant
// tool_calls row missing its tool messages, which the next turn's upstream
// rejects outright.
func TestRunLoopPersistsPlaceholdersWhenCancelledMidTurn(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "查一下"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leg := &disconnectLeg{cancels: cancel, calls: []LegToolCall{
		{ID: "c1", Name: "echo", Arguments: "{}"},
		{ID: "c2", Name: "echo", Arguments: "{}"},
	}}
	tools := newToolSet()
	tools.add(Tool{Name: "echo"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "ok"}
	})

	runtime := &Runtime{}
	var events []Event
	err := runtime.runLoop(ctx, collectEvents(&events),
		TurnRequest{Session: session, History: userMessageAsHistory(userMessage), Leg: leg}, tools)
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

// TestRunLoopDoesNotPersistHallucinatedToolCalls pins that tool calls from a
// model with no tools offered never reach the stored history: they would
// leave the same dangling tool_calls sequence the interrupted-turn repair
// guards against.
func TestRunLoopDoesNotPersistHallucinatedToolCalls(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	userMessage := &model.ChatMessage{Role: model.ChatRoleUser, Content: "你好"}
	insertChatFixture(t, session, userMessageAsHistory(userMessage)...)

	leg := &fakeLeg{responses: []fakeLegResponse{
		{result: LegResult{Content: "我没有工具可用。", ToolCalls: []LegToolCall{
			{ID: "ghost-1", Name: "not_offered", Arguments: "{}"},
		}}},
	}}

	runtime := &Runtime{}
	var events []Event
	err := runtime.runLoop(context.Background(), collectEvents(&events),
		TurnRequest{Session: session, History: userMessageAsHistory(userMessage), Leg: leg},
		newToolSet())
	if err != nil {
		t.Fatalf("runLoop failed: %v", err)
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
