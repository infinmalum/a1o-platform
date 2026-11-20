package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/infinmalum/Lapua/harness"
	"github.com/infinmalum/Lapua/llm"
	"github.com/infinmalum/Lapua/permission"
	lapuatool "github.com/infinmalum/Lapua/tool"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

// lapuaLoop delegates turn progression and tool dispatch to Lapua. The host
// supplies the next model leg from its own Item history, retaining native
// provider state and the existing gateway's streaming and billing behavior.
func lapuaLoop(ctx context.Context, run *model.AgentRun, tools *toolSet, maxLegs int,
	nextLeg func(context.Context) (InvokeResult, []Item, error),
	onToolResult func(InvokeResult, Item, toolResult) error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	var activeTool sync.Mutex
	defer func() {
		cancel()
		// Lapua can emit synthetic cancellation results before the tool's
		// cleanup returns. Keep the host turn alive until cleanup finishes.
		activeTool.Lock()
		activeTool.Unlock()
	}()

	var ready []chan struct{}
	// leg and calls hold the current model leg; tool executions belong to the
	// leg that requested them. Declared before the registry so the dispatch
	// closure can capture them.
	var leg InvokeResult
	var calls []Item
	var processed int
	registry := lapuatool.NewRegistry()
	allowed := make([]string, 0, len(tools.defs))
	for _, def := range tools.defs {
		var schema map[string]any
		if len(def.Parameters) > 0 {
			if err := json.Unmarshal(def.Parameters, &schema); err != nil {
				return fmt.Errorf("加载工具 %q 参数定义失败: %w", def.Name, err)
			}
		}
		fn := tools.exec[def.Name]
		sdkTool := tools.sdk[def.Name]
		registry.Add(lapuatool.Build(lapuatool.Spec{
			Name: def.Name, Description: def.Description, Schema: schema,
			Run: func(ctx context.Context, input json.RawMessage, tc *lapuatool.ToolContext) (lapuatool.Result, error) {
				index, err := strconv.Atoi(tc.ToolUseID)
				if err != nil || index < 0 || index >= len(ready) {
					return lapuatool.Result{}, fmt.Errorf("无法定位工具调用 %q", tc.ToolUseID)
				}
				select {
				case <-ready[index]:
				case <-ctx.Done():
					return lapuatool.Result{}, ctx.Err()
				}
				activeTool.Lock()
				defer activeTool.Unlock()
				if err := ctx.Err(); err != nil {
					return lapuatool.Result{}, err
				}
				var result toolResult
				if sdkTool != nil {
					// Lapua core tool: execute in-process via the SDK and
					// keep its content blocks (media-capable) intact.
					sdkResult, err := sdkTool.Call(ctx, input, tc)
					if err != nil {
						result = toolResult{Content: err.Error(), IsError: true}
					} else {
						result = toolResult{Content: sdkResultText(sdkResult), Blocks: sdkResult.Content, IsError: sdkResult.IsError}
					}
				} else {
					result = recordToolExecution(ctx, run, leg.InvocationID, calls[index], string(input), func() toolResult {
						return fn(ctx, toolRequest{Name: def.Name, Arguments: string(input)})
					})
				}
				if len(result.Blocks) > 0 {
					return lapuatool.Result{Content: result.Blocks, IsError: result.IsError}, nil
				}
				return lapuatool.Result{
					Content: []llm.ContentBlock{llm.TextBlock(result.Content)},
					IsError: result.IsError,
				}, nil
			},
		}))
		allowed = append(allowed, def.Name)
	}

	deps := harness.QueryDeps{CallModel: func(ctx context.Context, _ llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			var err error
			leg, calls, err = nextLeg(ctx)
			if err != nil {
				yield(llm.StreamEvent{}, err)
				return
			}
			// The ModelExecutor already streamed text and reasoning to the host.
			// Lapua needs only complete tool calls to decide whether to continue.
			processed = 0
			ready = make([]chan struct{}, len(calls))
			for i, call := range calls {
				ready[i] = make(chan struct{})
				if i == 0 {
					close(ready[i])
				}
				// Positional IDs are private to Lapua. Keep provider IDs
				// intact in A1O's history, even if a provider repeats them.
				if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: strconv.Itoa(i), ToolName: call.Name, ToolIndex: i}, nil) ||
					!yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: call.Arguments, ToolIndex: i, HasToolIndex: true}, nil) {
					return
				}
			}
			yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
		}
	}}
	options := harness.QueryInput{
		Tools: registry, Allowed: allowed, PermissionMode: permission.ModeDontAsk,
		MaxTurns: maxLegs, MaxConcurrency: 1,
		Todos: tools.todos,
		// A1O's tool handlers already own their output limits.
		MaxToolOutputChars: math.MaxInt,
	}
	for event, err := range harness.Query(ctx, options, deps) {
		if err != nil {
			return err
		}
		switch event.Kind {
		case harness.KindToolResult:
			block := event.ToolResult
			// All registered tools are exclusive, so results arrive in call order.
			if processed >= len(calls) || strconv.Itoa(processed) != block.ToolUseID {
				return fmt.Errorf("工具结果与调用不匹配: %q", block.ToolUseID)
			}
			call := calls[processed]
			var content strings.Builder
			for _, part := range block.Content {
				content.WriteString(part.Text)
			}
			if err := onToolResult(leg, call, toolResult{Content: content.String(), Blocks: block.Content, IsError: block.IsError}); err != nil {
				return err
			}
			processed++
			if processed < len(ready) {
				close(ready[processed])
			}
		case harness.KindResult:
			return event.Terminal.CompletionError()
		}
	}
	return fmt.Errorf("Lapua 未返回运行结果")
}

func isLapuaTurnLimit(err error) bool {
	var incomplete *harness.IncompleteError
	return errors.As(err, &incomplete) && incomplete.Reason == harness.ReasonMaxTurns
}

// recordToolExecution wraps one tool dispatch with the persisted intent of
// the runtime reconstruction plan §4: the row lands before the tool runs, so
// a crash mid-execution leaves a pending row naming what may already have
// acted on the outside world — the reconciliation resume flow surfaces it. A
// cancelled context after the call marks the outcome unknown instead of
// trusting whatever the interrupted handler returned. Recording failures log
// and never block the tool itself, mirroring emitEvent's tolerance. A nil
// run (direct tests) skips persistence entirely.
func recordToolExecution(ctx context.Context, run *model.AgentRun, invocationID int,
	call Item, arguments string, exec func() toolResult) toolResult {
	if run == nil {
		return exec()
	}
	execution := &model.ToolExecution{
		RunId: run.Id, InvocationId: invocationID,
		CallId: call.CallID, Name: call.Name, Arguments: arguments,
	}
	if err := model.CreateToolExecution(execution); err != nil {
		logger.SysError("failed to persist tool execution intent: " + err.Error())
	}
	start := time.Now()
	result := exec()
	status := model.ToolExecutionStatusCompleted
	if result.IsError {
		status = model.ToolExecutionStatusFailed
	}
	if ctx.Err() != nil {
		status = model.ToolExecutionStatusUnknown
	}
	if err := model.CompleteToolExecution(execution.Id, status, result.IsError, time.Since(start).Milliseconds()); err != nil {
		logger.SysError("failed to complete tool execution record: " + err.Error())
	}
	return result
}
