package agent

import (
	"os"
	"path/filepath"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
)

// openWorkTestDB opens an isolated database with the work runtime tables.
func openWorkTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := openChatTestDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WorkTask{}, &model.RunInput{}); err != nil {
		t.Fatal(err)
	}
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
}

// newWorkFixture creates a task and a queued work run whose initial context
// carries the goal as one user message.
func newWorkFixture(t *testing.T, userId int) (*model.WorkTask, *model.AgentRun) {
	t.Helper()
	task := &model.WorkTask{UserId: userId, Title: "整理季度数据", Status: model.WorkTaskStatusActive}
	if err := model.CreateWorkTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	inputItems, err := json.Marshal([]Item{{
		Type: ItemMessage, Role: "user",
		Content: []ContentPart{{Type: PartInputText, Text: "把上季度销售数据整理成表格"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	frozen := string(inputItems)
	run := &model.AgentRun{UserId: userId, Kind: "work", Status: model.RunStatusQueued,
		TaskId: task.Id, Model: "test-model", InputItems: &frozen, ContextItems: &frozen}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	return task, run
}

// claimFixture moves the run into the running state like a scheduler claim
// would, returning the fencing token the engine receives.
func claimFixture(t *testing.T, run *model.AgentRun) int64 {
	t.Helper()
	claimed, err := model.ClaimNextWorkRun("test-instance", 60, 1)
	if err != nil || claimed == nil || claimed.Id != run.Id {
		t.Fatalf("claim failed: run=%+v err=%v", claimed, err)
	}
	*run = *claimed
	return claimed.FencingToken
}

func assistantLeg(text string) InvokeResult {
	return InvokeResult{Items: []Item{
		{Type: ItemMessage, Role: "assistant",
			Content: []ContentPart{{Type: PartOutputText, Text: text}}},
	}}
}

func TestRunWorkCompletesViaCompleteTask(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{
			{Type: ItemMessage, Role: "assistant",
				Content: []ContentPart{{Type: PartOutputText, Text: "已经整理完成。"}}},
			{Type: ItemFunctionCall, CallID: "c1", Name: CompleteTaskToolName,
				Arguments: `{"summary":"四季度销售汇总表已完成"}`},
		}},
	}}
	var events []EventEnvelope
	engine := &Engine{}
	err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	})
	if err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	fresh, err := model.GetAgentRunById(run.Id)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != model.RunStatusCompleted {
		t.Fatalf("run must complete, got %s", fresh.Status)
	}
	reloadedTask, err := model.GetWorkTask(task.Id, task.UserId)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedTask.Summary != "四季度销售汇总表已完成" {
		t.Fatalf("task summary = %q", reloadedTask.Summary)
	}
	types := eventTypes(events)
	if !contains(types, model.RunEventRunCompleted) {
		t.Fatalf("missing run.completed: %v", types)
	}
	completed := payloadMap(t, events[len(events)-1])
	if completed["summary"] != "四季度销售汇总表已完成" {
		t.Fatalf("completion payload = %v", completed)
	}
	// The leg output and the tool result must both be replayable context.
	var contextItems []Item
	if err := json.Unmarshal([]byte(*fresh.ContextItems), &contextItems); err != nil {
		t.Fatal(err)
	}
	last := contextItems[len(contextItems)-1]
	if last.Type != ItemFunctionCallOutput || last.CallID != "c1" {
		t.Fatalf("context must end with the tool output, got %+v", last)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestRunWorkPausesWaitingInput(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	executor := &fakeExecutor{responses: []InvokeResult{assistantLeg("需要确认表格的列设置。")}}
	var events []EventEnvelope
	engine := &Engine{}
	err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	})
	if err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	fresh, _ := model.GetAgentRunById(run.Id)
	if fresh.Status != model.RunStatusWaitingInput {
		t.Fatalf("run must park in waiting_input, got %s", fresh.Status)
	}
	if !contains(eventTypes(events), model.RunEventRunWaitingInput) {
		t.Fatalf("missing run.waiting_input event: %v", eventTypes(events))
	}
}

func TestRunWorkConsumesAppendedInputs(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	if _, _, err := model.CreateRunInput(&model.RunInput{RunId: run.Id, UserId: 1,
		IdempotencyKey: "in-1", Content: "用数据透视表的形式"}); err != nil {
		t.Fatal(err)
	}

	executor := &fakeExecutor{responses: []InvokeResult{assistantLeg("好的。")}}
	var events []EventEnvelope
	engine := &Engine{}
	if err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	}); err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("expected one leg, got %d", len(executor.requests))
	}
	items := executor.requests[0].Items
	var sawInput bool
	for _, item := range items {
		if item.Type == ItemMessage && item.Role == "user" && messageItemTextWork(item) == "用数据透视表的形式" {
			sawInput = true
		}
	}
	if !sawInput {
		t.Fatalf("appended input must reach the leg request: %+v", items)
	}
	pending, _ := model.ListPendingRunInputs(run.Id)
	if len(pending) != 0 {
		t.Fatalf("input must be consumed: %+v", pending)
	}
	if !contains(eventTypes(events), model.RunEventInputAppended) {
		t.Fatal("missing input.appended event")
	}
}

func messageItemTextWork(item Item) string {
	text := ""
	for _, part := range item.Content {
		if part.Type == PartInputText || part.Type == PartOutputText {
			text += part.Text
		}
	}
	return text
}

func TestRunWorkStopsWhenLeaseLost(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	// The executor bumps the fencing token mid-flight — another instance
	// claimed the run while this one worked.
	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{{Type: ItemFunctionCall, CallID: "c1", Name: "no_such_tool",
			Arguments: `{}`}}},
	}}
	executor.hook = func() {
		if err := model.DB.Model(&model.AgentRun{}).Where("id = ?", run.Id).
			Update("fencing_token", fencing+1).Error; err != nil {
			t.Fatal(err)
		}
	}
	var events []EventEnvelope
	engine := &Engine{}
	err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	})
	if !errors.Is(err, errLeaseLost) {
		t.Fatalf("expected errLeaseLost, got %v", err)
	}
	fresh, _ := model.GetAgentRunById(run.Id)
	if fresh.Status != model.RunStatusRunning || fresh.FencingToken != fencing+1 {
		t.Fatalf("stale worker must not touch run state: %+v", fresh)
	}
	for _, event := range events {
		switch event.Type {
		case model.RunEventRunFailed, model.RunEventRunCompleted, model.RunEventRunCancelled:
			t.Fatalf("stale worker must not emit terminal events: %v", eventTypes(events))
		}
	}
}

func TestRunWorkReconcilesUnknownUpstreamOutcome(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	executor := &fakeExecutor{errs: []error{errors.New("model stream failed")}}
	executor.responses = []InvokeResult{{ResponseID: "resp_1"}}
	var events []EventEnvelope
	engine := &Engine{}
	if err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	}); err == nil {
		t.Fatal("unknown outcome must surface as an error")
	}
	fresh, _ := model.GetAgentRunById(run.Id)
	if fresh.Status != model.RunStatusNeedsReconciliation {
		t.Fatalf("engaged-but-unverifiable leg must reconcile, got %s", fresh.Status)
	}
	if !contains(eventTypes(events), model.RunEventRunNeedsReconcile) {
		t.Fatal("missing needs_reconciliation event")
	}
}

func TestRunWorkFailsOnModelError(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	executor := &fakeExecutor{errs: []error{errors.New("no compatible channel")}}
	var events []EventEnvelope
	engine := &Engine{}
	if err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: collectEvents(&events),
	}); err == nil {
		t.Fatal("model failure must surface as an error")
	}
	fresh, _ := model.GetAgentRunById(run.Id)
	if fresh.Status != model.RunStatusFailed {
		t.Fatalf("run must fail, got %s", fresh.Status)
	}
	if !contains(eventTypes(events), model.RunEventRunFailed) {
		t.Fatal("missing run.failed event")
	}
}

func TestRunWorkContextCarriesToolOutputsAcrossLegs(t *testing.T) {
	openWorkTestDB(t)
	task, run := newWorkFixture(t, 1)
	fencing := claimFixture(t, run)

	tools := newToolSet()
	tools.add(Tool{Name: "echo", Description: "原样返回"}, func(ctx context.Context, req toolRequest) toolResult {
		return toolResult{Content: "echo-ok"}
	})
	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{{Type: ItemFunctionCall, CallID: "c1", Name: "echo", Arguments: `{}`}}},
		assistantLeg("完成。"),
	}}
	// The second leg completes the task via complete_task.
	executor.responses[1].Items = append(executor.responses[1].Items,
		Item{Type: ItemFunctionCall, CallID: "c2", Name: CompleteTaskToolName,
			Arguments: `{"summary":"done"}`})

	engine := &Engine{extraTools: tools}
	if err := engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, Executor: executor,
		Emit: func(EventEnvelope) error { return nil },
	}); err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	second := executor.requests[1].Items
	found := false
	for _, item := range second {
		if item.Type == ItemFunctionCallOutput && item.CallID == "c1" && item.Output == "echo-ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("second leg must replay the persisted tool output: %+v", second)
	}
}

func TestSchedulerClaimsAndCompletesWork(t *testing.T) {
	openWorkTestDB(t)
	_, run := newWorkFixture(t, 1)

	scheduler := NewScheduler(&Engine{})
	scheduler.newExecutor = func(userId int) ModelInvoker {
		return &fakeExecutor{responses: []InvokeResult{{
			Items: []Item{{Type: ItemFunctionCall, CallID: "c1", Name: CompleteTaskToolName,
				Arguments: `{"summary":"任务完成"}`}},
		}}}
	}
	scheduler.claimAvailable()
	waitForRunStatus(t, run.Id, model.RunStatusCompleted, 5*time.Second)

	fresh, _ := model.GetAgentRunById(run.Id)
	if fresh.LeaseOwner == "" || fresh.FencingToken != 1 {
		t.Fatalf("claim bookkeeping missing: %+v", fresh)
	}
	reloadedTask, _ := model.GetWorkTask(fresh.TaskId, 1)
	if reloadedTask.Summary != "任务完成" {
		t.Fatalf("task summary = %q", reloadedTask.Summary)
	}
	var events []model.RunEvent
	if err := model.DB.Where("run_id = ?", run.Id).Order("sequence ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	if !contains(types, model.RunEventRunStarted) || !contains(types, model.RunEventRunCompleted) {
		t.Fatalf("persisted events incomplete: %v", types)
	}
}

func TestSchedulerCancelRunningWork(t *testing.T) {
	openWorkTestDB(t)
	_, run := newWorkFixture(t, 1)

	started := make(chan struct{})
	scheduler := NewScheduler(&Engine{})
	scheduler.newExecutor = func(userId int) ModelInvoker {
		return &blockingExecutor{started: started}
	}
	scheduler.claimAvailable()
	<-started

	if _, _, err := model.RequestRunCancel(run.Id); err != nil {
		t.Fatal(err)
	}
	scheduler.NotifyCancel(run.Id)
	waitForRunStatus(t, run.Id, model.RunStatusCancelled, 5*time.Second)

	var events []model.RunEvent
	if err := model.DB.Where("run_id = ?", run.Id).Order("sequence ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	var cancelled bool
	for _, event := range events {
		if event.Type == model.RunEventRunCancelled {
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatalf("missing run.cancelled event: %+v", events)
	}
}

// blockingExecutor blocks inside Invoke until its context is cancelled —
// the shape of a model call in flight when the cancel request arrives.
type blockingExecutor struct {
	started chan struct{}
}

func (b *blockingExecutor) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	close(b.started)
	<-ctx.Done()
	return InvokeResult{}, ctx.Err()
}

func waitForRunStatus(t *testing.T, runId int, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fresh, err := model.GetAgentRunById(runId)
		if err == nil && fresh.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d never reached %s", runId, status)
}

func TestSubscribeRunDeliversAndStops(t *testing.T) {
	events, cancel := SubscribeRun(42)
	defer cancel()
	PublishRun(42, EventEnvelope{RunID: 42, Type: "response.output_text.delta",
		Payload: json.RawMessage(`{"delta":"hi"}`)})
	select {
	case event := <-events:
		if event.Type != "response.output_text.delta" {
			t.Fatalf("unexpected event %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("subscribed channel missed the publish")
	}
	cancel()
	// A closed subscription must not receive further publishes or panic.
	PublishRun(42, EventEnvelope{RunID: 42, Type: "run.started"})
}

// migrateAgentDefs adds the R4 agent definition tables to a work test DB.
func migrateAgentDefs(t *testing.T) {
	t.Helper()
	if err := model.DB.AutoMigrate(&model.AgentDefinition{}, &model.AgentVersion{}); err != nil {
		t.Fatal(err)
	}
}

func toolSetHas(set *toolSet, name string) bool {
	for _, def := range set.defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func TestRunWorkAppliesAgentSpec(t *testing.T) {
	openWorkTestDB(t)
	migrateAgentDefs(t)
	task, run := newWorkFixture(t, 1)
	def := &model.AgentDefinition{Name: "数据整理员"}
	if err := model.CreateAgentDefinition(def, 1, &model.AgentVersionSpec{
		Instructions:   "始终先核对数据来源。",
		SandboxEnabled: false,
		Modes:          []string{model.AgentModeWork},
	}); err != nil {
		t.Fatal(err)
	}
	version, err := model.GetLatestAgentVersion(def.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Model(&model.AgentRun{}).Where("id = ?", run.Id).
		Update("agent_version_id", version.Id).Error; err != nil {
		t.Fatal(err)
	}
	fencing := claimFixture(t, run)

	spec := &model.AgentVersionSpec{Instructions: "始终先核对数据来源。", Modes: []string{model.AgentModeWork}}
	prevSandbox := config.ChatSandboxEnabled
	config.ChatSandboxEnabled = true
	t.Cleanup(func() { config.ChatSandboxEnabled = prevSandbox })
	engine := &Engine{Sandbox: &Sandbox{}}
	if engine.Sandbox.Available() {
		tools := engine.resolveWorkTools(context.Background(), WorkTurnRequest{
			Run: run, Fencing: fencing, Task: task, AgentSpec: spec})
		if toolSetHas(tools, "sandbox") {
			t.Fatal("agent spec denying sandbox must exclude the sandbox tool")
		}
	}

	executor := &fakeExecutor{responses: []InvokeResult{{
		Items: []Item{{Type: ItemFunctionCall, CallID: "c1", Name: CompleteTaskToolName,
			Arguments: `{"summary":"完成"}`}},
	}}}
	err = engine.RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task, AgentSpec: spec,
		Executor: executor, Emit: func(EventEnvelope) error { return nil },
	})
	if err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	if len(executor.requests) == 0 {
		t.Fatal("no model leg ran")
	}
	instructions := executor.requests[0].Instructions
	if !strings.Contains(instructions, "始终先核对数据来源。") {
		t.Fatalf("agent instructions missing from leg request: %q", instructions)
	}
	if !strings.Contains(instructions, "complete_task") {
		t.Fatalf("work contract missing from leg request: %q", instructions)
	}
	fresh, err := model.GetAgentRunById(run.Id)
	if err != nil || fresh.Status != model.RunStatusCompleted {
		t.Fatalf("run must complete, got %+v err=%v", fresh, err)
	}
}

func TestSchedulerBuildTurnRequestResolvesAgent(t *testing.T) {
	openWorkTestDB(t)
	migrateAgentDefs(t)
	_, run := newWorkFixture(t, 1)
	enabled := &model.ChatSkill{OwnerId: 1, Name: "表格规范", Instructions: "用表格输出。", Enabled: true}
	if err := model.DB.Create(enabled).Error; err != nil {
		t.Fatal(err)
	}
	def := &model.AgentDefinition{Name: "报告员"}
	if err := model.CreateAgentDefinition(def, 1, &model.AgentVersionSpec{
		Instructions: "写报告。", Modes: []string{model.AgentModeWork},
		SkillIds:      []int{enabled.Id},
	}); err != nil {
		t.Fatal(err)
	}
	version, err := model.GetLatestAgentVersion(def.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Model(&model.AgentRun{}).Where("id = ?", run.Id).
		Update("agent_version_id", version.Id).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := model.ClaimNextWorkRun("test-instance", 60, 1)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}

	scheduler := NewScheduler(&Engine{})
	scheduler.newExecutor = func(userId int) ModelInvoker {
		return &fakeExecutor{}
	}
	req, err := scheduler.buildTurnRequest(claimed, func(EventEnvelope) error { return nil })
	if err != nil {
		t.Fatalf("buildTurnRequest failed: %v", err)
	}
	if req == nil || req.AgentSpec == nil || req.AgentSpec.Instructions != "写报告。" {
		t.Fatalf("agent spec not resolved: %+v", req)
	}
	if len(req.Skills) != 1 || req.Skills[0].Id != enabled.Id {
		t.Fatalf("skills intersection wrong: %+v", req.Skills)
	}

	// A definition archived between queueing and claiming fails the run
	// start with the explainable cause.
	if err := model.SetAgentDefinitionArchived(def.Id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.buildTurnRequest(claimed, func(EventEnvelope) error { return nil }); err == nil {
		t.Fatal("archived agent must fail the build")
	}
}

// TestRunWorkInjectsReferenceFiles pins that the agent version's reference
// files reach the work leg as a user preamble item carrying the file
// content, ahead of the task input.
func TestRunWorkInjectsReferenceFiles(t *testing.T) {
	openWorkTestDB(t)
	migrateAgentDefs(t)
	task, run := newWorkFixture(t, 1)
	dir := t.TempDir()
	specPath := filepath.Join(dir, "rules.txt")
	if err := os.WriteFile(specPath, []byte("报告必须以结论开头。"), 0600); err != nil {
		t.Fatal(err)
	}
	file := &model.ChatFile{UserId: 1, Name: "rules.txt", Kind: model.ChatFileKindText,
		Size: int64(len("报告必须以结论开头。")), Path: specPath}
	if err := model.DB.Create(file).Error; err != nil {
		t.Fatal(err)
	}
	def := &model.AgentDefinition{Name: "文件助手"}
	if err := model.CreateAgentDefinition(def, 1, &model.AgentVersionSpec{
		Instructions: "按参考文件执行。", Modes: []string{model.AgentModeWork},
		FileIds:       []int{file.Id},
	}); err != nil {
		t.Fatal(err)
	}
	version, err := model.GetLatestAgentVersion(def.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Model(&model.AgentRun{}).Where("id = ?", run.Id).
		Update("agent_version_id", version.Id).Error; err != nil {
		t.Fatal(err)
	}
	fencing := claimFixture(t, run)

	executor := &fakeExecutor{responses: []InvokeResult{{
		Items: []Item{{Type: ItemFunctionCall, CallID: "c1", Name: CompleteTaskToolName,
			Arguments: `{"summary":"完成"}`}},
	}}}
	err = (&Engine{}).RunWork(context.Background(), WorkTurnRequest{
		Run: run, Fencing: fencing, Task: task,
		AgentSpec:      &model.AgentVersionSpec{Instructions: "按参考文件执行。", Modes: []string{model.AgentModeWork}, FileIds: []int{file.Id}},
		ReferenceFiles: []*model.ChatFile{file},
		Executor:       executor, Emit: func(EventEnvelope) error { return nil },
	})
	if err != nil {
		t.Fatalf("RunWork failed: %v", err)
	}
	if len(executor.requests) == 0 {
		t.Fatal("no model leg ran")
	}
	items := executor.requests[0].Items
	if len(items) < 2 {
		t.Fatalf("leg items = %d, want at least preamble + task", len(items))
	}
	preamble := items[0]
	if preamble.Type != ItemMessage || preamble.Role != "user" || len(preamble.Content) != 2 {
		t.Fatalf("preamble item = %+v", preamble)
	}
	if !strings.Contains(preamble.Content[0].Text, "参考文件") ||
		!strings.Contains(preamble.Content[1].Text, "报告必须以结论开头。") {
		t.Fatalf("preamble content = %+v", preamble.Content)
	}
}
