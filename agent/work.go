package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

// Work execution (R3): one claimed AgentRun driven to a terminal state by
// the scheduler's worker context — never by an HTTP request. The run's
// accumulated Item context lives in AgentRun.ContextItems and is the single
// fact source: the executor appends each leg's output there, the engine
// appends consumed inputs and tool outputs at the safe boundaries, and every
// leg re-reads it. User inputs queue in RunInput.
//
// Completion semantics: a leg with no tool calls ends the current reasoning
// step, not the task. The run completes only through the complete_task tool;
// a turn that ends without it parks the run in waiting_input until the user
// appends input (which requeues the run).

// CompleteTaskToolName is the built-in tool the model calls to declare a
// work run finished.
const CompleteTaskToolName = "complete_task"

// errLeaseLost reports that a fenced write found the run no longer owned by
// this worker. The scheduler walks away without touching the run; a new
// owner (or the reconciliation pass) owns its state from there.
var errLeaseLost = errors.New("work run lease lost")

// WorkTurnRequest carries one claimed work run into the engine.
type WorkTurnRequest struct {
	// Run is the claimed run row (status running, LeaseOwner/FencingToken
	// set by the claim).
	Run *model.AgentRun
	// Fencing is the claim's fencing token; every guarded write carries it.
	Fencing int64
	// Task is the owning work task (title feeds the instructions).
	Task *model.WorkTask
	// Skills are the run's selected, enabled skills.
	Skills []*model.ChatSkill
	// AgentSpec is the frozen configuration of the agent version this run
	// pinned, or nil for runs without an agent. Its instructions join the
	// work contract and its sandbox flag gates code execution.
	AgentSpec *model.AgentVersionSpec
	// Executor performs the model legs with the run owner's billing identity.
	Executor ModelInvoker
	// Emit receives every envelope (persisted events and transient deltas);
	// the scheduler tees it into the live bus.
	Emit Emitter
}

// RunWork drives one work run to a terminal state. It returns nil only when
// it transitioned the run to completed or waiting_input; every other exit
// leaves the resolution to the scheduler's finalizer (cancel, timeout, lost
// lease) or was already transitioned in-band (failed, needs_reconciliation).
func (e *Engine) RunWork(ctx context.Context, req WorkTurnRequest) error {
	if req.Emit == nil {
		req.Emit = func(EventEnvelope) error { return nil }
	}
	if req.Run.ContextItems == nil || *req.Run.ContextItems == "" {
		return e.workFail(req, errors.New("运行缺少初始输入"))
	}
	_ = e.emitEvent(req.Run, 0, model.RunEventRunStarted,
		map[string]any{"task_id": req.Run.TaskId}, true, req.Emit)

	tools := e.resolveWorkTools(ctx, req)
	summary := ""
	tools.add(completeTaskToolDef(), func(ctx context.Context, toolReq toolRequest) toolResult {
		var args struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(toolReq.Arguments), &args); err != nil || strings.TrimSpace(args.Summary) == "" {
			return toolResult{Content: "complete_task 需要非空的 summary 参数", IsError: true}
		}
		summary = args.Summary
		return toolResult{Content: "任务完成已记录。"}
	})

	for iteration := 0; ; iteration++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if iteration > config.WorkMaxToolIterations {
			return e.workFail(req, fmt.Errorf("已达工具调用步数上限（%d）", config.WorkMaxToolIterations))
		}

		// Safe boundary: pull the persisted context, then consume queued
		// user inputs into it before the next model call, so appended input
		// never rewrites an in-flight leg.
		items, err := loadWorkContext(req)
		if err != nil {
			return e.workFail(req, fmt.Errorf("运行上下文损坏: %w", err))
		}
		inputItems, err := e.consumePendingInputs(req)
		if err != nil {
			return e.workFail(req, fmt.Errorf("消费追加输入失败: %w", err))
		}
		items = append(items, inputItems...)

		leg, legErr := e.workLeg(ctx, req, items, tools)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if legErr != nil {
			if leg.ResponseID != "" {
				// The upstream engaged (a response arrived) but the outcome
				// cannot be verified from here; report reconciliation
				// instead of guessing success or double-invoking.
				return e.workReconcile(req, legErr)
			}
			return e.workFail(req, legErr)
		}

		calls := resultToolCalls(leg.Items)
		outputs := make([]Item, 0, len(calls))
		for _, call := range calls {
			callResult := e.executeTool(ctx, tools, call)
			_ = e.emitEvent(req.Run, leg.InvocationID, model.RunEventToolCalled,
				map[string]any{"tool_call_id": call.CallID, "name": call.Name, "arguments": call.Arguments},
				true, req.Emit)
			_ = e.emitEvent(req.Run, leg.InvocationID, model.RunEventToolResult,
				map[string]any{"tool_call_id": call.CallID, "name": call.Name,
					"content": callResult.Content, "is_error": callResult.IsError},
				true, req.Emit)
			outputs = append(outputs, Item{Type: ItemFunctionCallOutput,
				CallID: call.CallID, Output: callResult.Content})
		}
		// Checkpoint the tool batch: the executor already appended the leg's
		// function_call items to the run context, so only the outputs are
		// missing; persisting them keeps the context replayable after a
		// crash or pause.
		if len(outputs) > 0 {
			if err := e.appendWorkContext(req, outputs); err != nil {
				return err
			}
		}

		if summary != "" {
			return e.workComplete(req, summary)
		}
		if len(calls) == 0 {
			// Safe boundary before pausing: inputs that arrived during the
			// final leg keep the run going instead of parking it.
			pending, err := model.ListPendingRunInputs(req.Run.Id)
			if err != nil {
				return e.workFail(req, fmt.Errorf("读取追加输入失败: %w", err))
			}
			if len(pending) > 0 {
				continue
			}
			return e.workPause(req)
		}
	}
}

// workLeg streams one model leg. The executor appends the leg's output to
// the run's fenced context; fencing propagates so a stale worker's write
// fails instead of clobbering the new owner's state.
func (e *Engine) workLeg(ctx context.Context, req WorkTurnRequest, items []Item, tools *toolSet) (InvokeResult, error) {
	result, err := req.Executor.Invoke(ctx, InvokeRequest{
		RunID:           req.Run.Id,
		Model:           req.Run.Model,
		Instructions:    workInstructions(req.Task, req.Skills, req.AgentSpec),
		Items:           items,
		Tools:           tools.defs,
		MaxOutputTokens: DefaultMaxOutputTokens,
		FencingToken:    req.Fencing,
		OnTextDelta: func(delta string) {
			_ = e.emitEvent(req.Run, 0, "response.output_text.delta",
				map[string]any{"delta": delta}, false, req.Emit)
		},
		OnReasoningDelta: func(delta string) {
			_ = e.emitEvent(req.Run, 0, "response.reasoning_summary_text.delta",
				map[string]any{"delta": delta}, false, req.Emit)
		},
	})
	if err != nil {
		return result, err
	}
	_ = e.emitEvent(req.Run, result.InvocationID, model.RunEventInvocationUsage,
		map[string]any{"prompt_tokens": result.Usage.Input, "completion_tokens": result.Usage.Output},
		true, req.Emit)
	return result, nil
}

// loadWorkContext re-reads the run's accumulated Item list from the
// database — the persisted context, not the caller's memory, is the truth.
func loadWorkContext(req WorkTurnRequest) ([]Item, error) {
	run, err := model.GetAgentRunById(req.Run.Id)
	if err != nil {
		return nil, err
	}
	req.Run.ContextItems = run.ContextItems
	if run.ContextItems == nil || *run.ContextItems == "" {
		return nil, nil
	}
	var items []Item
	if err := json.Unmarshal([]byte(*run.ContextItems), &items); err != nil {
		return nil, err
	}
	return items, nil
}

// appendWorkContext appends items to the run's persisted context under the
// claim's fencing token.
func (e *Engine) appendWorkContext(req WorkTurnRequest, extra []Item) error {
	run, err := model.GetAgentRunById(req.Run.Id)
	if err != nil {
		return err
	}
	contextItems := []Item{}
	if run.ContextItems != nil && *run.ContextItems != "" {
		if err := json.Unmarshal([]byte(*run.ContextItems), &contextItems); err != nil {
			return fmt.Errorf("运行上下文损坏: %w", err)
		}
	}
	contextItems = append(contextItems, extra...)
	encoded, err := json.Marshal(contextItems)
	if err != nil {
		return err
	}
	ok, err := model.UpdateAgentRunContextFenced(req.Run.Id, req.Fencing, string(encoded))
	if err != nil {
		return err
	}
	if !ok {
		return errLeaseLost
	}
	return nil
}

// consumePendingInputs appends every queued input to the run's persisted
// context as a user message and marks it consumed; each consumption is
// announced as input.appended. Returns the items added.
func (e *Engine) consumePendingInputs(req WorkTurnRequest) ([]Item, error) {
	pending, err := model.ListPendingRunInputs(req.Run.Id)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return nil, nil
	}
	appended := make([]Item, 0, len(pending))
	consumed := make([]int, 0, len(pending))
	for _, input := range pending {
		appended = append(appended, Item{Type: ItemMessage, Role: "user",
			Content: []ContentPart{{Type: PartInputText, Text: input.Content}}})
		consumed = append(consumed, input.Id)
		_ = e.emitEvent(req.Run, 0, model.RunEventInputAppended,
			map[string]any{"input_id": input.Id, "content": input.Content},
			true, req.Emit)
	}
	if err := e.appendWorkContext(req, appended); err != nil {
		return nil, err
	}
	if err := model.ConsumeRunInputs(req.Run.Id, consumed); err != nil {
		return nil, err
	}
	return appended, nil
}

// workComplete transitions the run to completed, records the summary on the
// task, and emits the terminal event. The summary argument is non-empty by
// construction (the tool executor validates it).
func (e *Engine) workComplete(req WorkTurnRequest, summary string) error {
	if req.Task != nil {
		if err := model.UpdateWorkTaskSummary(req.Task.Id, summary); err != nil {
			logger.SysError("failed to record work task summary: " + err.Error())
		}
	}
	ok, err := model.UpdateAgentRunStatusFenced(req.Run.Id, req.Fencing,
		model.RunStatusRunning, model.RunStatusCompleted, "")
	if err != nil {
		return err
	}
	if !ok {
		return errLeaseLost
	}
	_ = e.emitEvent(req.Run, 0, model.RunEventRunCompleted,
		map[string]any{"summary": summary}, true, req.Emit)
	return nil
}

// workPause parks the run in waiting_input: the model ended its turn without
// declaring completion and no input waits. The next user input requeues it.
func (e *Engine) workPause(req WorkTurnRequest) error {
	ok, err := model.UpdateAgentRunStatusFenced(req.Run.Id, req.Fencing,
		model.RunStatusRunning, model.RunStatusWaitingInput, "")
	if err != nil {
		return err
	}
	if !ok {
		return errLeaseLost
	}
	_ = e.emitEvent(req.Run, 0, model.RunEventRunWaitingInput,
		map[string]any{}, true, req.Emit)
	return nil
}

// workFail transitions the run to failed and emits the failure. A lost lease
// surfaces as errLeaseLost with no event — the write did not land.
func (e *Engine) workFail(req WorkTurnRequest, err error) error {
	ok, fencedErr := model.UpdateAgentRunStatusFenced(req.Run.Id, req.Fencing,
		model.RunStatusRunning, model.RunStatusFailed, err.Error())
	if fencedErr != nil {
		return fencedErr
	}
	if !ok {
		return errLeaseLost
	}
	_ = e.emitEvent(req.Run, 0, model.RunEventRunFailed,
		map[string]any{"error": err.Error()}, true, req.Emit)
	return err
}

// workReconcile parks the run in needs_reconciliation: the model call's
// outcome is unknowable from this process.
func (e *Engine) workReconcile(req WorkTurnRequest, cause error) error {
	ok, err := model.UpdateAgentRunStatusFenced(req.Run.Id, req.Fencing,
		model.RunStatusRunning, model.RunStatusNeedsReconciliation, cause.Error())
	if err != nil {
		return err
	}
	if !ok {
		return errLeaseLost
	}
	_ = e.emitEvent(req.Run, 0, model.RunEventRunNeedsReconcile,
		map[string]any{"error": cause.Error()}, true, req.Emit)
	return cause
}

// workInstructions renders the run-level system prompt: the work contract,
// the frozen agent instructions when the run pins a version, the task title,
// then the selected skills.
func workInstructions(task *model.WorkTask, skills []*model.ChatSkill, spec *model.AgentVersionSpec) string {
	parts := []string{
		"你是 A1O 平台的持久任务执行助手，在后台独立运行，用户可能不在线。" +
			"持续推进任务目标，把关键结论写进回复，让用户随时回来都能看到进展。",
		"任务目标已完成后，必须调用 complete_task 工具并提交结果总结；" +
			"需要用户补充信息或做出决策时，直接结束本轮回复并提出问题，" +
			"运行会进入等待输入状态，用户回复后自动继续。",
	}
	if spec != nil && strings.TrimSpace(spec.Instructions) != "" {
		parts = append(parts, strings.TrimSpace(spec.Instructions))
	}
	if task != nil && strings.TrimSpace(task.Title) != "" {
		parts = append(parts, "## 任务\n"+strings.TrimSpace(task.Title))
	}
	for _, skill := range skills {
		if strings.TrimSpace(skill.Instructions) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("## Skill: %s\n%s", skill.Name, strings.TrimSpace(skill.Instructions)))
	}
	return strings.Join(parts, "\n\n")
}

// completeTaskToolDef renders the completion tool definition.
func completeTaskToolDef() Tool {
	return Tool{
		Name:        CompleteTaskToolName,
		Description: "声明任务目标已全部完成。summary 是给用户的结果总结，说明完成了什么与最终产出。",
		Parameters: json.RawMessage(`{"type":"object","properties":{` +
			`"summary":{"type":"string","description":"任务结果总结"}},` +
			`"required":["summary"]}`),
	}
}

// resolveWorkTools builds the work run's tool set: on-demand code execution
// (no pre-created environment — the tool only exists when sandboxing is
// enabled and the pinned agent version, if any, grants it), the run owner's
// visible MCP tools, then the skill filter. The completion tool is added
// after the filter by RunWork: skill tool lists narrow capability tools,
// never the run's completion mechanism.
func (e *Engine) resolveWorkTools(ctx context.Context, req WorkTurnRequest) *toolSet {
	set := newToolSet()
	sandboxAllowed := req.AgentSpec == nil || req.AgentSpec.SandboxEnabled
	if sandboxAllowed && config.ChatSandboxEnabled && e.Sandbox != nil {
		if languages := e.Sandbox.Languages(); len(languages) > 0 {
			set.add(sandboxToolDef(languages), e.runSandbox)
		}
	}
	if e.MCP != nil {
		for _, tool := range e.MCP.ListTools(ctx, req.Run.UserId) {
			parameters := marshalInputSchema(tool.Tool.InputSchema)
			set.add(Tool{
				Name:        tool.VirtualName,
				Description: fmt.Sprintf("[MCP %s] %s", tool.Server.Name, tool.Tool.Description),
				Parameters:  parameters,
			}, func(ctx context.Context, toolReq toolRequest) toolResult {
				content, isError, err := e.MCP.CallTool(ctx, tool.Server, tool.Tool.Name, toolReq.Arguments)
				if err != nil {
					return toolResult{Content: "[MCP 错误] " + err.Error(), IsError: true}
				}
				return toolResult{Content: content, IsError: isError}
			})
		}
	}
	if e.extraTools != nil {
		for _, def := range e.extraTools.defs {
			set.add(def, e.extraTools.exec[def.Name])
		}
	}
	set.filterBySkills(req.Skills)
	return set
}
