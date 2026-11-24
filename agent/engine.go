package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	normatool "github.com/infinmalum/norma-e/tool"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

// DefaultMaxOutputTokens is the per-leg output bound when a session sets none.
const DefaultMaxOutputTokens = int64(4096)

// Engine drives turns over the shared model leg: tool resolution, context
// building, the tool loop, and run-event emission. It holds no per-request
// state; Chat and Work differ only in the request they hand it.
type Engine struct {
	Sandbox *Sandbox
	MCP     *MCPManager
	// extraTools merges additional tools after the sandbox and MCP sets,
	// before the skill filter. Tests use it to script tool round trips
	// without standing up an MCP server; production code leaves it nil.
	extraTools *toolSet
}

var (
	defaultOnce   sync.Once
	defaultEngine *Engine
)

// Default returns the process-wide engine used by the API handlers.
func Default() *Engine {
	defaultOnce.Do(func() {
		defaultEngine = &Engine{Sandbox: &Sandbox{}, MCP: NewMCPManager()}
	})
	return defaultEngine
}

// ModelInvoker executes one billed model call. *ModelExecutor is the
// production implementation; tests substitute fakes.
type ModelInvoker interface {
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error)
}

// ChatTurnRequest carries everything one chat turn needs; rows are mutated
// in place and persisted by the engine.
type ChatTurnRequest struct {
	// Run is the persisted run record for this turn; invocation records and
	// structural events associate with it.
	Run *model.AgentRun
	// Session is the owning conversation.
	Session *model.ChatSession
	// History is the working message list, oldest first, including the
	// message that starts this turn.
	History []*model.ChatMessage
	// Files indexes the attachments referenced by history, by file id.
	Files map[int]*model.ChatFile
	// Skills are the session's selected, enabled skills.
	Skills []*model.ChatSkill
	// AgentSpec is the frozen configuration of the agent version this run
	// pinned, or nil for plain sessions. Its instructions prepend the run's
	// system content; its sandbox flag caps the session's.
	AgentSpec *model.AgentVersionSpec
	// ReferenceFiles are the agent version's reference-file attachments;
	// they ride every leg as a preamble item, exactly like attachments do,
	// regardless of the current user (built-in agents carry their admin
	// author's published material).
	ReferenceFiles []*model.ChatFile
	Knowledge      []model.KnowledgeMatch
	// Executor performs the model legs; it carries the billing identity.
	Executor ModelInvoker
	// ContextBudget overrides the configured budget when positive.
	ContextBudget int
	// MaxToolIterations overrides the configured bound when positive.
	MaxToolIterations int
}

// resolveTools builds the turn's tool set: the sandbox when enabled on both
// levels (the agent version's flag caps the session's), every visible MCP
// tool, then the skill filter.
func (e *Engine) resolveTools(ctx context.Context, turn ChatTurnRequest) *toolSet {
	set := newToolSet()
	sandboxAllowed := turn.Session.SandboxEnabled &&
		(turn.AgentSpec == nil || turn.AgentSpec.SandboxEnabled)
	if sandboxAllowed && config.ChatSandboxEnabled && e.Sandbox != nil {
		if languages := e.Sandbox.Languages(); len(languages) > 0 {
			set.add(sandboxToolDef(languages), e.runSandbox)
		}
	}
	if e.MCP != nil {
		for _, tool := range e.MCP.ListTools(ctx, turn.Session.UserId) {
			parameters := marshalInputSchema(tool.Tool.InputSchema)
			set.add(Tool{
				Name:        tool.VirtualName,
				Description: fmt.Sprintf("[MCP %s] %s", tool.Server.Name, tool.Tool.Description),
				Parameters:  parameters,
			}, func(ctx context.Context, req toolRequest) toolResult {
				content, isError, err := e.MCP.CallTool(ctx, tool.Server, tool.Tool.Name, req.Arguments)
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
	addNormaTools(ctx, set, turn.Session.UserId)
	set.filterBySkills(turn.Skills)
	return set
}

// addNormaTools registers Norma core tools that run inside the gateway
// process. Web tools are read-only in the permission pipeline, so they work
// under the host's dontAsk mode; WebFetch additionally passes the SSRF guard
// below. Search backend misconfiguration degrades to a tool-level error,
// never a run error. Image generation reuses the studio pipeline; the tool
// is offered only when the user's group has an image-capable model.
func addNormaTools(ctx context.Context, set *toolSet, userId int) {
	if ws, err := normatool.NewWebSearch(normatool.WebSearchConfig{
		Backend:      config.ChatWebSearchBackend,
		BraveAPIKey:  config.ChatWebSearchAPIKey,
		TavilyAPIKey: config.ChatWebSearchAPIKey,
		Proxy:        config.ChatWebSearchProxy,
	}); err == nil {
		if config.ChatWebToolsEnabled {
			set.addSDK(ws)
		}
	} else {
		logger.SysError("failed to build web search tool: " + err.Error())
	}
	if config.ChatWebToolsEnabled {
		set.addSDK(newGuardedWebFetch(normatool.NewWebFetch(normatool.WebFetchConfig{
			Proxy: config.ChatWebSearchProxy,
		})))
	}
	if hasImageModel(ctx, userId) {
		set.add(generateImageToolDef(), imageGenExec(userId))
	}
	addMemoryTools(ctx, set, userId)
}

// hasImageModel probes the user's group for an image-capable model. A cache
// or database outage must not kill the chat turn for an optional tool, so
// any panic or error degrades to "no image tool".
func hasImageModel(ctx context.Context, userId int) (ok bool) {
	defer func() { recover() }()
	_, err := defaultImageModel(ctx, userId)
	return err == nil
}

// marshalInputSchema converts an MCP tool schema to raw JSON for the function
// definition, defaulting to an empty object schema.
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

// RunChat executes one chat turn: tool resolution, compaction, then the tool
// loop. Structural events persist to the run's event log before streaming;
// text and reasoning deltas stream transiently. Upstream and tool failures
// surface as run.failed; the turn always ends with run.completed unless the
// client disappeared.
func (e *Engine) RunChat(ctx context.Context, emit Emitter, turn ChatTurnRequest) error {
	if turn.Files == nil {
		files, err := loadHistoryFiles(turn.Session, turn.History)
		if err != nil {
			return e.fail(turn, emit, fmt.Errorf("加载附件失败: %w", err))
		}
		turn.Files = files
	}
	if err := model.UpdateAgentRunStatus(turn.Run.Id, model.RunStatusRunning, ""); err != nil {
		logger.SysError("failed to mark run running: " + err.Error())
	}
	_ = e.emitEvent(turn.Run, 0, model.RunEventRunStarted,
		map[string]any{"session_id": turn.Session.Id}, true, emit)
	tools := e.resolveTools(ctx, turn)
	history, err := e.compactIfNeeded(ctx, emit, turn, tools)
	if err != nil {
		return e.fail(turn, emit, fmt.Errorf("组装上下文失败: %w", err))
	}
	turn.History = history
	return e.turnLoop(ctx, emit, turn, tools)
}

// turnLoop drives the model legs and tool executions for one turn over the
// given (already resolved) tool set.
func (e *Engine) turnLoop(ctx context.Context, emit Emitter, turn ChatTurnRequest, tools *toolSet) error {
	maxIterations := turn.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = config.ChatMaxToolIterations
	}
	if maxIterations <= 0 {
		maxIterations = 8
	}

	working, err := historyToItems(turn.History, turn.Files, turn.Session.Model)
	if err != nil {
		return e.fail(turn, emit, err)
	}
	// The agent version's reference files ride ahead of the conversation.
	preamble, err := referenceFileItems(turn.ReferenceFiles)
	if err != nil {
		return e.fail(turn, emit, err)
	}
	knowledge, sources := knowledgePreamble(turn)
	items := make([]Item, 0, len(working)*2+len(preamble))
	items = append(items, preamble...)
	for i, entry := range working {
		if knowledge != "" && i == len(working)-1 && entry.row.Role == model.ChatRoleUser {
			// Keep this turn's sources adjacent to its question. The original
			// history and the durable user message contain no source text.
			entry.items[0].Content = append([]ContentPart{{Type: PartInputText, Text: knowledge}}, entry.items[0].Content...)
		}
		items = append(items, entry.items...)
	}

	// The persisted history must never end with an assistant tool_calls row
	// missing its tool messages: the next turn's upstream rejects that
	// sequence outright. pendingCalls tracks the calls of the latest leg
	// whose results have not been stored yet; the deferred flush fabricates
	// placeholder tool rows for whatever remains on any exit path.
	var pendingCalls []Item
	defer func() {
		for _, call := range pendingCalls {
			row := &model.ChatMessage{
				SessionId:  turn.Session.Id,
				Role:       model.ChatRoleTool,
				Content:    "[工具执行被中断，未取得结果]",
				ToolCallId: call.CallID,
				Name:       call.Name,
			}
			if err := model.InsertChatMessage(row); err != nil {
				logger.SysError("failed to persist interrupted tool placeholder: " + err.Error())
			}
		}
	}()

	totalPrompt, totalCompletion := int64(0), int64(0)
	err = normaLoop(ctx, turn.Run, tools, maxIterations+1, func(ctx context.Context) (InvokeResult, []Item, error) {
		result, pending, legErr := e.runLeg(ctx, emit, turn, items, tools, sources)
		pendingCalls = append([]Item(nil), pending...)
		totalPrompt += result.Usage.Input
		totalCompletion += result.Usage.Output
		return result, pending, legErr
	}, func(result InvokeResult, call Item, callResult toolResult) error {
		emitToolResult(e, turn.Run, result.InvocationID, call, callResult, emit)
		row := &model.ChatMessage{
			SessionId:  turn.Session.Id,
			Role:       model.ChatRoleTool,
			Content:    callResult.Content,
			ToolCallId: call.CallID,
			Name:       call.Name,
		}
		if err := model.InsertChatMessage(row); err != nil {
			return fmt.Errorf("保存工具结果失败: %w", err)
		}
		pendingCalls = pendingCalls[1:]
		items = append(items, call, Item{Type: ItemFunctionCallOutput,
			CallID: call.CallID, Output: callResult.Content})
		items = append(items, mediaResultItems(callResult)...)
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isNormaTurnLimit(err) {
		err = fmt.Errorf("已达工具调用轮数上限（%d）", maxIterations)
	}
	if err != nil {
		return e.fail(turn, emit, err)
	}
	if err := model.UpdateAgentRunStatus(turn.Run.Id, model.RunStatusCompleted, ""); err != nil {
		logger.SysError("failed to mark run completed: " + err.Error())
	}
	_ = e.emitEvent(turn.Run, 0, model.RunEventRunCompleted,
		map[string]any{"prompt_tokens": totalPrompt, "completion_tokens": totalCompletion}, true, emit)
	return nil
}

// runLeg streams one model leg: it pre-creates the assistant row, streams
// deltas, then persists content, tool calls, opaque reasoning state, and
// token usage. It returns the function-call items that were actually stored
// on the assistant row — nil when the row write failed or the model
// hallucinated calls while no tools were offered, because either way the
// stored history has no tool_calls to answer.
func (e *Engine) runLeg(ctx context.Context, emit Emitter, turn ChatTurnRequest, items []Item, tools *toolSet, sources []model.KnowledgeCitation) (InvokeResult, []Item, error) {
	maxTokens := turn.Session.MaxTokens
	if maxTokens <= 0 {
		maxTokens = int(DefaultMaxOutputTokens)
	}
	row := &model.ChatMessage{SessionId: turn.Session.Id, Role: model.ChatRoleAssistant}
	if err := model.InsertChatMessage(row); err != nil {
		return InvokeResult{}, nil, fmt.Errorf("保存消息失败: %w", err)
	}
	_ = e.emitEvent(turn.Run, 0, model.RunEventMessageStarted,
		map[string]any{"message_id": row.Id}, true, emit)
	// The callbacks append the row id so the client can anchor deltas; they
	// run inside Invoke before the result is complete.
	instructions := systemContent(turn.Session, turn.Skills, turn.AgentSpec)
	if turn.Session.KnowledgeLibraryId != 0 {
		instructions = strings.TrimSpace(instructions + "\n\n" + knowledgeInstructions)
	}
	result, legErr := turn.Executor.Invoke(ctx, InvokeRequest{
		RunID:           turn.Run.Id,
		Model:           turn.Session.Model,
		Instructions:    instructions,
		Items:           items,
		Tools:           tools.defs,
		MaxOutputTokens: int64(maxTokens),
		Temperature:     turn.Session.Temperature,
		OnTextDelta: func(delta string) {
			_ = e.emitEvent(turn.Run, 0, "response.output_text.delta",
				map[string]any{"delta": delta, "message_id": row.Id}, false, emit)
		},
		OnReasoningDelta: func(delta string) {
			_ = e.emitEvent(turn.Run, 0, "response.reasoning_summary_text.delta",
				map[string]any{"delta": delta}, false, emit)
		},
	})

	text := messageText(result.Items)
	calls := resultToolCalls(result.Items)
	row.Content = text
	row.Sources = usedKnowledgeSources(text, sources)
	row.Tokens = int(result.Usage.Output)
	if len(calls) > 0 && len(tools.defs) > 0 {
		raw, _ := json.Marshal(toStoredToolCalls(calls))
		encoded := string(raw)
		row.ToolCalls = &encoded
	} else {
		calls = nil
	}
	if reasoning := toStoredReasoning(result.Items); reasoning != nil {
		raw, _ := json.Marshal(reasoning)
		encoded := string(raw)
		row.Reasoning = &encoded
	}
	if err := model.UpdateChatMessage(row); err != nil {
		return result, nil, fmt.Errorf("保存消息失败: %w", err)
	}
	_ = e.emitEvent(turn.Run, result.InvocationID, model.RunEventInvocationUsage,
		map[string]any{"prompt_tokens": result.Usage.Input, "completion_tokens": result.Usage.Output},
		true, emit)
	if ctx.Err() != nil {
		return result, calls, ctx.Err()
	}
	return result, calls, legErr
}

func emitToolResult(e *Engine, run *model.AgentRun, invocationID int, call Item, result toolResult, emit Emitter) {
	_ = e.emitEvent(run, invocationID, model.RunEventToolCalled,
		map[string]any{"tool_call_id": call.CallID, "name": call.Name, "arguments": call.Arguments}, true, emit)
	_ = e.emitEvent(run, invocationID, model.RunEventToolResult,
		map[string]any{"tool_call_id": call.CallID, "name": call.Name,
			"content": result.Content, "is_error": result.IsError}, true, emit)
}

// fail reports a turn failure; the client still receives run.completed.
func (e *Engine) fail(turn ChatTurnRequest, emit Emitter, err error) error {
	_ = e.emitEvent(turn.Run, 0, model.RunEventRunFailed,
		map[string]any{"error": err.Error()}, true, emit)
	if updateErr := model.UpdateAgentRunStatus(turn.Run.Id, model.RunStatusFailed, err.Error()); updateErr != nil {
		logger.SysError("failed to mark run failed: " + updateErr.Error())
	}
	_ = e.emitEvent(turn.Run, 0, model.RunEventRunCompleted,
		map[string]any{"status": model.RunStatusFailed}, true, emit)
	return err
}

// emitEvent persists a structural event (persist=true) with its monotonic
// sequence, then streams the envelope. Deltas (persist=false) stream only;
// their persistence lives in the invocation's response items.
func (e *Engine) emitEvent(run *model.AgentRun, invocationID int, eventType string, payload any, persist bool, emit Emitter) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"event payload encoding failed"}`)
	}
	envelope := EventEnvelope{
		RunID: run.Id, Type: eventType, SchemaVersion: "1",
		InvocationID: invocationID, Payload: raw,
	}
	if persist {
		sequence, seqErr := model.NextRunEventSequence(run.Id)
		if seqErr != nil {
			// A gap in the event log never justifies fabricating failure of
			// the work itself; the invocation record still carries the truth.
			logger.SysError("failed to allocate run event sequence: " + seqErr.Error())
		} else {
			envelope.Sequence = sequence
			envelope.EventID = fmt.Sprintf("evt_%d_%d", run.Id, sequence)
			event := &model.RunEvent{
				RunId: run.Id, Sequence: sequence, EventId: envelope.EventID,
				Type: eventType, SchemaVersion: "1", InvocationId: invocationID,
				Payload: ptr(string(raw)),
			}
			if createErr := model.CreateRunEvent(event); createErr != nil {
				logger.SysError("failed to persist run event: " + createErr.Error())
			}
		}
	}
	return emit(envelope)
}

// resultToolCalls extracts the function-call items from one leg's output.
func resultToolCalls(items []Item) []Item {
	var calls []Item
	for _, item := range items {
		if item.Type == ItemFunctionCall {
			calls = append(calls, item)
		}
	}
	return calls
}

// toStoredToolCalls renders leg tool calls in the persisted wire form.
func toStoredToolCalls(calls []Item) []storedToolCall {
	out := make([]storedToolCall, 0, len(calls))
	for _, call := range calls {
		stored := storedToolCall{ID: call.CallID, Type: "function"}
		stored.Function.Name = call.Name
		stored.Function.Arguments = call.Arguments
		out = append(out, stored)
	}
	return out
}

// toStoredReasoning collects one leg's opaque state: the reasoning item's
// slots plus any function-call thought signatures.
func toStoredReasoning(items []Item) *storedReasoning {
	var stored *storedReasoning
	for _, item := range items {
		switch item.Type {
		case ItemReasoning:
			if item.Summary == nil && item.Content == nil && item.Signature == "" && item.EncryptedContent == "" {
				continue
			}
			stored = &storedReasoning{Summary: item.Summary, Content: item.Content,
				Signature: item.Signature, EncryptedContent: item.EncryptedContent,
				ThoughtSignatures: map[string]string{}}
		case ItemFunctionCall:
			if item.ThoughtSignature == "" {
				continue
			}
			if stored == nil {
				stored = &storedReasoning{ThoughtSignatures: map[string]string{}}
			}
			if stored.ThoughtSignatures == nil {
				stored.ThoughtSignatures = map[string]string{}
			}
			stored.ThoughtSignatures[item.CallID] = item.ThoughtSignature
		}
	}
	return stored
}
