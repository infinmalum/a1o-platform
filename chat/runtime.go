// Package chat implements the built-in chat runtime: a server-side tool loop
// that runs model calls through the shared relay lifecycle and executes tools
// from a subprocess sandbox and MCP servers. Sessions and messages persist
// through the model package; billing, model permissions, and usage logs are
// the normal /v1 path via the hidden per-user chat token.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/common/logger"
	"github.com/infinmalum/one-gateway/model"
)

// DefaultMaxOutputTokens is the per-leg output bound when a session sets none.
const DefaultMaxOutputTokens = int64(4096)

// Runtime wires tool resolution and the turn loop together; it holds no
// per-request state.
type Runtime struct {
	Sandbox *Sandbox
	MCP     *MCPManager
}

var (
	defaultOnce    sync.Once
	defaultRuntime *Runtime
)

// Default returns the process-wide runtime used by the API handlers.
func Default() *Runtime {
	defaultOnce.Do(func() {
		defaultRuntime = &Runtime{Sandbox: &Sandbox{}, MCP: NewMCPManager()}
	})
	return defaultRuntime
}

// TurnRequest carries everything one turn needs; rows are mutated in place
// and persisted by the runtime.
type TurnRequest struct {
	Session *model.ChatSession
	// History is the working message list, oldest first, including the
	// message that starts this turn.
	History []*model.ChatMessage
	// Files indexes the attachments referenced by history, by file id.
	Files map[int]*model.ChatFile
	// Skills are the session's selected, enabled skills.
	Skills []*model.ChatSkill
	// Leg executes one model call; it carries the billing identity.
	Leg LegRunner
	// ContextBudget overrides the configured budget when positive.
	ContextBudget int
	// MaxToolIterations overrides the configured bound when positive.
	MaxToolIterations int
}

// resolveTools builds the turn's tool set: the sandbox when enabled on both
// levels, every visible MCP tool, then the skill filter.
func (r *Runtime) resolveTools(ctx context.Context, turn TurnRequest) *toolSet {
	set := newToolSet()
	if turn.Session.SandboxEnabled && config.ChatSandboxEnabled && r.Sandbox != nil {
		if languages := r.Sandbox.Languages(); len(languages) > 0 {
			set.add(sandboxToolDef(languages), r.runSandbox)
		}
	}
	if r.MCP != nil {
		for _, tool := range r.MCP.ListTools(ctx, turn.Session.UserId) {
			parameters := marshalInputSchema(tool.Tool.InputSchema)
			set.add(Tool{
				Name:        tool.VirtualName,
				Description: fmt.Sprintf("[MCP %s] %s", tool.Server.Name, tool.Tool.Description),
				Parameters:  parameters,
			}, func(ctx context.Context, req toolRequest) toolResult {
				content, isError, err := r.MCP.CallTool(ctx, tool.Server, tool.Tool.Name, req.Arguments)
				if err != nil {
					return toolResult{Content: "[MCP 错误] " + err.Error(), IsError: true}
				}
				return toolResult{Content: content, IsError: isError}
			})
		}
	}
	set.filterBySkills(turn.Skills)
	return set
}

// marshalInputSchema converts an MCP tool schema to raw JSON for the OpenAI
// function definition, defaulting to an empty object schema.
func marshalInputSchema(schema any) json.RawMessage {
	if schema == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return raw
}

// Run executes one chat turn: tool resolution, compaction, then the tool
// loop. Events stream through emit. Upstream and tool failures surface as
// error events; the turn always ends with done unless the client disappeared.
func (r *Runtime) Run(ctx context.Context, emit Emitter, turn TurnRequest) error {
	if turn.Files == nil {
		files, err := loadHistoryFiles(turn.Session, turn.History)
		if err != nil {
			return r.fail(emit, fmt.Errorf("加载附件失败: %w", err))
		}
		turn.Files = files
	}
	tools := r.resolveTools(ctx, turn)
	history, err := r.compactIfNeeded(ctx, emit, turn, tools)
	if err != nil {
		return r.fail(emit, fmt.Errorf("组装上下文失败: %w", err))
	}
	turn.History = history
	return r.runLoop(ctx, emit, turn, tools)
}

// runLoop drives the model legs and tool executions for one turn over the
// given (already resolved) tool set.
func (r *Runtime) runLoop(ctx context.Context, emit Emitter, turn TurnRequest, tools *toolSet) error {
	maxIterations := turn.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = config.ChatMaxToolIterations
	}
	if maxIterations <= 0 {
		maxIterations = 8
	}

	legMessages := make([]openAIMessage, 0, len(turn.History)+8)
	if system := systemContent(turn.Session, turn.Skills); system != "" {
		legMessages = append(legMessages, openAIMessage{Role: "system", Content: system})
	}
	wire, err := historyToWire(turn.History, turn.Files, turn.Session.Model)
	if err != nil {
		return r.fail(emit, err)
	}
	for _, item := range wire {
		legMessages = append(legMessages, item.msg)
	}

	// The persisted history must never end with an assistant tool_calls row
	// missing its tool messages: the next turn's upstream rejects that
	// sequence outright. pendingCalls tracks the calls of the latest leg
	// whose results have not been stored yet; the deferred flush fabricates
	// placeholder tool rows for whatever remains on any exit path.
	var pendingCalls []LegToolCall
	defer func() {
		for _, call := range pendingCalls {
			row := &model.ChatMessage{
				SessionId:  turn.Session.Id,
				Role:       model.ChatRoleTool,
				Content:    "[工具执行被中断，未取得结果]",
				ToolCallId: call.ID,
				Name:       call.Name,
			}
			if err := model.InsertChatMessage(row); err != nil {
				logger.SysError("failed to persist interrupted tool placeholder: " + err.Error())
			}
		}
	}()

	totalPrompt, totalCompletion := 0, 0
	for iteration := 0; ; iteration++ {
		if iteration > maxIterations {
			return r.fail(emit, fmt.Errorf("已达工具调用轮数上限（%d）", maxIterations))
		}
		result, persistedCalls, legErr := r.runLeg(ctx, emit, turn, legMessages, tools)
		totalPrompt += result.PromptTokens
		totalCompletion += result.CompletionTokens
		emit(Event{Type: EventUsage, PromptTokens: result.PromptTokens, CompletionTokens: result.CompletionTokens})

		pendingCalls = persistedCalls
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if legErr != nil {
			return r.fail(emit, legErr)
		}
		if len(result.ToolCalls) == 0 || len(tools.defs) == 0 {
			break
		}
		legMessages = append(legMessages, openAIMessage{
			Role:      model.ChatRoleAssistant,
			Content:   result.Content,
			ToolCalls: toWireToolCalls(result.ToolCalls),
		})
		for _, call := range result.ToolCalls {
			callResult := r.executeTool(ctx, tools, call)
			emit(Event{Type: EventToolCall, ToolCallID: call.ID, Name: call.Name, Arguments: call.Arguments})
			emit(Event{Type: EventToolResult, ToolCallID: call.ID, Name: call.Name,
				Content: callResult.Content, IsError: callResult.IsError})
			row := &model.ChatMessage{
				SessionId:  turn.Session.Id,
				Role:       model.ChatRoleTool,
				Content:    callResult.Content,
				ToolCallId: call.ID,
				Name:       call.Name,
			}
			if err := model.InsertChatMessage(row); err != nil {
				return r.fail(emit, fmt.Errorf("保存工具结果失败: %w", err))
			}
			for i := range pendingCalls {
				if pendingCalls[i].ID == call.ID {
					pendingCalls = append(pendingCalls[:i], pendingCalls[i+1:]...)
					break
				}
			}
			legMessages = append(legMessages, openAIMessage{
				Role: model.ChatRoleTool, Content: callResult.Content,
				ToolCallID: call.ID, Name: call.Name,
			})
		}
	}
	emit(Event{Type: EventDone, PromptTokens: totalPrompt, CompletionTokens: totalCompletion})
	return nil
}

// runLeg streams one model leg: it pre-creates the assistant row, streams
// deltas, then persists content, tool calls, and token usage. It returns the
// tool calls that are actually stored on the assistant row — nil when the
// row write failed or the model hallucinated calls while no tools were
// offered, because either way the stored history has no tool_calls to answer.
func (r *Runtime) runLeg(ctx context.Context, emit Emitter, turn TurnRequest, messages []openAIMessage, tools *toolSet) (LegResult, []LegToolCall, error) {
	maxTokens := turn.Session.MaxTokens
	if maxTokens <= 0 {
		maxTokens = int(DefaultMaxOutputTokens)
	}
	body, err := composeBody(turn.Session, messages, tools.defs, int64(maxTokens))
	if err != nil {
		return LegResult{}, nil, err
	}
	row := &model.ChatMessage{SessionId: turn.Session.Id, Role: model.ChatRoleAssistant}
	if err := model.InsertChatMessage(row); err != nil {
		return LegResult{}, nil, fmt.Errorf("保存消息失败: %w", err)
	}
	emit(Event{Type: EventMessageStart, MessageId: row.Id})

	result, legErr := turn.Leg.RunLeg(ctx, LegRequest{Body: body, MaxOutputTokens: int64(maxTokens)},
		func(delta string) {
			emit(Event{Type: EventDelta, MessageId: row.Id, Delta: delta})
		})

	row.Content = result.Content
	row.Tokens = result.CompletionTokens
	var persistedCalls []LegToolCall
	if len(result.ToolCalls) > 0 && len(tools.defs) > 0 {
		raw, _ := json.Marshal(toWireToolCalls(result.ToolCalls))
		encoded := string(raw)
		row.ToolCalls = &encoded
		persistedCalls = result.ToolCalls
	}
	if err := model.UpdateChatMessage(row); err != nil {
		return result, nil, fmt.Errorf("保存消息失败: %w", err)
	}
	if ctx.Err() != nil {
		return result, persistedCalls, ctx.Err()
	}
	return result, persistedCalls, legErr
}

// executeTool dispatches one tool call with its own timeout.
func (r *Runtime) executeTool(ctx context.Context, tools *toolSet, call LegToolCall) toolResult {
	fn, ok := tools.exec[call.Name]
	if !ok {
		return toolResult{Content: fmt.Sprintf("未知工具 %q", call.Name), IsError: true}
	}
	return fn(ctx, toolRequest{Name: call.Name, Arguments: call.Arguments})
}

// fail reports a turn failure; the client still receives done.
func (r *Runtime) fail(emit Emitter, err error) error {
	emit(Event{Type: EventError, Error: err.Error()})
	emit(Event{Type: EventDone})
	return err
}

// composeBody builds the chat completions body for one leg.
func composeBody(session *model.ChatSession, messages []openAIMessage, tools []Tool, maxTokens int64) ([]byte, error) {
	body := map[string]any{
		"model":          session.Model,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"max_tokens":     maxTokens,
	}
	if session.Temperature != nil {
		body["temperature"] = *session.Temperature
	}
	if session.TopP != nil {
		body["top_p"] = *session.TopP
	}
	if len(tools) > 0 {
		defs := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			parameters := tool.Parameters
			if parameters == nil {
				parameters = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			defs = append(defs, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  parameters,
				},
			})
		}
		body["tools"] = defs
		body["tool_choice"] = "auto"
	}
	return json.Marshal(body)
}

// toWireToolCalls converts leg tool calls into the persisted wire form.
func toWireToolCalls(calls []LegToolCall) []openAIToolCall {
	out := make([]openAIToolCall, 0, len(calls))
	for _, call := range calls {
		wire := openAIToolCall{ID: call.ID, Type: "function"}
		wire.Function.Name = call.Name
		wire.Function.Arguments = call.Arguments
		out = append(out, wire)
	}
	return out
}
