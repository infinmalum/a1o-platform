package agent

import (
	"context"
	"encoding/json"
	"iter"
	"strconv"
	"strings"
	"sync"

	"github.com/infinmalum/Lapua/llm"
	"github.com/infinmalum/Lapua/permission"
	"github.com/infinmalum/Lapua/subagent"
	lapuatool "github.com/infinmalum/Lapua/tool"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// relayProvider adapts the host's ModelInvoker to Lapua's llm.Provider so a
// subagent's harness can drive its legs through the same gateway: protocol
// negotiation, upstream channels, billing and invocation persistence all stay
// in A1O. Subagent legs run with SkipContextAppend so they never touch the
// parent run's replayable context; their usage still settles per invocation.
type relayProvider struct {
	executor  ModelInvoker
	runID     int
	modelName string

	mu               sync.Mutex
	emitted          []Item // function_call items across legs, in emission order
	lastInvocationID int
}

// leg converts the harness conversation into Items and runs one model leg.
func (p *relayProvider) leg(ctx context.Context, req llm.CompletionRequest) (InvokeResult, error) {
	result, err := p.executor.Invoke(ctx, InvokeRequest{
		RunID:           p.runID,
		Model:           p.modelName,
		Instructions:    strings.Join(req.System, "\n\n"),
		Items:           p.messagesToItems(req.Messages),
		Tools:           providerTools(req.Tools),
		MaxOutputTokens: int64(req.MaxTokens),
		Temperature:     req.Temperature,
		// Subagent context lives in the harness; only billing and the
		// invocation record belong to the parent run.
		SkipContextAppend: true,
	})
	if err != nil {
		return InvokeResult{}, err
	}
	p.mu.Lock()
	for _, item := range result.Items {
		if item.Type == ItemFunctionCall {
			p.emitted = append(p.emitted, item)
		}
	}
	p.lastInvocationID = result.InvocationID
	p.mu.Unlock()
	return result, nil
}

// messagesToItems maps the harness message list onto the standard Item
// schema. Thinking blocks are dropped: A1O's wire carries reasoning only as
// provider-native summary items, which the harness's replayed blocks cannot
// reconstruct. Tool IDs are matched by emission order — the harness replays
// exactly the assistant messages this provider streamed.
func (p *relayProvider) messagesToItems(messages []llm.Message) []Item {
	p.mu.Lock()
	emitted := make([]Item, len(p.emitted))
	copy(emitted, p.emitted)
	p.mu.Unlock()

	var items []Item
	callIDs := map[string]string{}
	emittedIndex := 0
	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			var parts []ContentPart
			for _, b := range msg.Content {
				switch b.Type {
				case llm.BlockText:
					parts = append(parts, ContentPart{Type: PartInputText, Text: b.Text})
				case llm.BlockImage:
					if b.Source != nil {
						parts = append(parts, ContentPart{
							Type:     PartInputImage,
							ImageURL: "data:" + b.Source.MediaType + ";base64," + b.Source.Data,
						})
					}
				case llm.BlockToolResult:
					callID := b.ToolUseID
					if real, ok := callIDs[b.ToolUseID]; ok {
						callID = real
					}
					var text strings.Builder
					for _, part := range b.Content {
						if part.Type == llm.BlockText {
							text.WriteString(part.Text)
						}
					}
					items = append(items, Item{
						Type: ItemFunctionCallOutput, CallID: callID,
						Output: text.String(), Status: ItemStatusCompleted,
					})
					// Tool-result media rides as a synthetic user message,
					// the same convention the parent loop uses.
					var media []ContentPart
					for _, part := range b.Content {
						if part.Type == llm.BlockImage && part.Source != nil {
							media = append(media, ContentPart{
								Type:     PartInputImage,
								ImageURL: "data:" + part.Source.MediaType + ";base64," + part.Source.Data,
							})
						}
					}
					if len(media) > 0 {
						items = append(items, Item{
							Type: ItemMessage, Role: "user",
							Content: media, Status: ItemStatusCompleted,
						})
					}
				}
			}
			if len(parts) > 0 {
				items = append(items, Item{
					Type: ItemMessage, Role: "user",
					Content: parts, Status: ItemStatusCompleted,
				})
			}
		case llm.RoleAssistant:
			var parts []ContentPart
			for _, b := range msg.Content {
				switch b.Type {
				case llm.BlockText:
					parts = append(parts, ContentPart{Type: PartOutputText, Text: b.Text})
				case llm.BlockToolUse:
					callID := b.ID
					if emittedIndex < len(emitted) {
						callID = emitted[emittedIndex].CallID
						callIDs[b.ID] = callID
					}
					emittedIndex++
					items = append(items, Item{
						Type: ItemFunctionCall, Name: b.Name, CallID: callID,
						Arguments: string(b.Input), Status: ItemStatusCompleted,
					})
				}
			}
			if len(parts) > 0 {
				items = append(items, Item{
					Type: ItemMessage, Role: "assistant",
					Content: parts, Status: ItemStatusCompleted,
				})
			}
		}
	}
	return items
}

func providerTools(schemas []llm.ToolSchema) []Tool {
	if len(schemas) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(schemas))
	for _, schema := range schemas {
		raw, err := json.Marshal(schema.InputSchema)
		if err != nil {
			raw = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, Tool{Name: schema.Name, Description: schema.Description, Parameters: raw})
	}
	return out
}

func llmUsage(u native.Usage) llm.Usage {
	input := u.Input - u.CacheRead - u.CacheCreation
	if input < 0 {
		input = 0
	}
	return llm.Usage{
		InputTokens:      int(input),
		OutputTokens:     int(u.Output),
		CacheReadTokens:  int(u.CacheRead),
		CacheWriteTokens: int(u.CacheCreation),
	}
}

// Stream implements llm.Provider. Legs are non-streaming internally (the
// subagent has no live consumer), so each leg yields one complete event
// burst; tool IDs are globally unique positionals the result converter maps
// back to provider call IDs on replay.
func (p *relayProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		result, err := p.leg(ctx, req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		var text strings.Builder
		callIndex := 0
		for _, item := range result.Items {
			switch item.Type {
			case ItemMessage:
				for _, part := range item.Content {
					if part.Type == PartOutputText {
						text.WriteString(part.Text)
					}
				}
			case ItemFunctionCall:
				if text.Len() > 0 {
					if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: text.String()}, nil) {
						return
					}
					text.Reset()
				}
				id := strconv.Itoa(callIndex)
				if !yield(llm.StreamEvent{Type: llm.SEToolUseStart, ToolID: id, ToolName: item.Name, ToolIndex: callIndex}, nil) ||
					!yield(llm.StreamEvent{Type: llm.SEToolInputJSON, Text: item.Arguments, ToolIndex: callIndex, HasToolIndex: true}, nil) {
					return
				}
				callIndex++
			}
		}
		if text.Len() > 0 {
			if !yield(llm.StreamEvent{Type: llm.SETextDelta, Text: text.String()}, nil) {
				return
			}
		}
		stopReason := "end_turn"
		if result.Status == ItemStatusIncomplete {
			stopReason = "max_tokens"
		}
		yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stopReason, Usage: llmUsage(result.Usage)}, nil)
		yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	}
}

// Complete implements the non-streaming half of llm.Provider (used by
// compaction summarizers and NonStreaming loops). The assistant message
// mirrors Stream's events with the same positional tool IDs.
func (p *relayProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	result, err := p.leg(ctx, req)
	if err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	var blocks []llm.ContentBlock
	callIndex := 0
	for _, item := range result.Items {
		switch item.Type {
		case ItemMessage:
			for _, part := range item.Content {
				if part.Type == PartOutputText {
					blocks = append(blocks, llm.TextBlock(part.Text))
				}
			}
		case ItemFunctionCall:
			blocks = append(blocks, llm.ContentBlock{
				Type: llm.BlockToolUse, ID: strconv.Itoa(callIndex),
				Name: item.Name, Input: json.RawMessage(item.Arguments),
			})
			callIndex++
		}
	}
	stopReason := "end_turn"
	if result.Status == ItemStatusIncomplete {
		stopReason = "max_tokens"
	}
	return llm.Message{Role: llm.RoleAssistant, Content: blocks}, stopReason, llmUsage(result.Usage), nil
}

// subagentRegistry builds a fresh registry for child agents. The parent
// loop's registry binds Run closures to the parent's positional call state,
// so children dispatch through direct execution instead: SDK tools call in
// process, host tools keep the persisted execution intent with a synthetic
// call id.
func subagentRegistry(set *toolSet, run *model.AgentRun, p *relayProvider) *lapuatool.Registry {
	registry := lapuatool.NewRegistry()
	for _, def := range set.defs {
		if def.Name == CompleteTaskToolName || def.Name == subagentToolName {
			continue
		}
		var schema map[string]any
		if len(def.Parameters) > 0 {
			if err := json.Unmarshal(def.Parameters, &schema); err != nil {
				logger.SysError("subagent registry: skip tool " + def.Name + ": " + err.Error())
				continue
			}
		}
		fn := set.exec[def.Name]
		sdkTool := set.sdk[def.Name]
		registry.Add(lapuatool.Build(lapuatool.Spec{
			Name: def.Name, Description: def.Description, Schema: schema,
			Run: func(ctx context.Context, input json.RawMessage, tc *lapuatool.ToolContext) (lapuatool.Result, error) {
				if sdkTool != nil {
					result, err := sdkTool.Call(ctx, input, tc)
					if err != nil {
						return lapuatool.Result{Content: []llm.ContentBlock{llm.TextBlock(err.Error())}, IsError: true}, nil
					}
					if len(result.Content) == 0 {
						result.Content = []llm.ContentBlock{llm.TextBlock(sdkResultText(result))}
					}
					return lapuatool.Result{Content: result.Content, IsError: result.IsError}, nil
				}
				if fn == nil {
					return lapuatool.Result{Content: []llm.ContentBlock{llm.TextBlock("工具不可用")}, IsError: true}, nil
				}
				result := recordToolExecution(ctx, run, p.lastInvocationID,
					Item{CallID: tc.ToolUseID, Name: def.Name}, string(input), func() toolResult {
						return fn(ctx, toolRequest{Name: def.Name, Arguments: string(input)})
					})
				if len(result.Blocks) > 0 {
					return lapuatool.Result{Content: result.Blocks, IsError: result.IsError}, nil
				}
				return lapuatool.Result{Content: []llm.ContentBlock{llm.TextBlock(result.Content)}, IsError: result.IsError}, nil
			},
		}))
	}
	return registry
}

const subagentToolName = "Agent"

// addSubagentTool registers the Lapua Agent tool for work runs. Chat stays
// single-agent by design. Definitions are derived from the resolved tool
// set: a researcher gets the web pair only, a generalist inherits every
// parent tool except completion and delegation. Recursion is capped two
// levels below the parent.
func (e *Engine) addSubagentTool(ctx context.Context, set *toolSet, req WorkTurnRequest) {
	if !config.WorkSubagentEnabled {
		return
	}
	p := &relayProvider{executor: req.Executor, runID: req.Run.Id, modelName: req.Run.Model}
	registry := subagentRegistry(set, req.Run, p)
	var names []string
	var webNames []string
	for _, def := range set.defs {
		if def.Name == CompleteTaskToolName || def.Name == subagentToolName {
			continue
		}
		names = append(names, def.Name)
		if def.Name == "web_search" || def.Name == "WebFetch" {
			webNames = append(webNames, def.Name)
		}
	}
	if len(names) == 0 {
		return
	}
	agents := map[string]subagent.Definition{
		"generalist": {
			Description:    "拥有与主代理相同工具集的通用子代理，适合并行处理独立子任务或隔离大体量中间输出。",
			Prompt:         []string{"你是工作运行中的子代理。专注完成交给你的子任务，自主使用可用工具，最终返回完整、自足的结果文本，不要调用 complete_task。"},
			Tools:          names,
			PermissionMode: permission.ModeDontAsk,
			MaxTurns:       config.WorkSubagentMaxTurns,
		},
	}
	if len(webNames) > 0 {
		agents["researcher"] = subagent.Definition{
			Description:    "只读网络研究子代理（web_search / WebFetch），适合检索资料并汇总来源。",
			Prompt:         []string{"你是网络研究子代理。用 web_search 和 WebFetch 检索并交叉验证，返回带来源的结论。"},
			Tools:          webNames,
			PermissionMode: permission.ModeDontAsk,
			MaxTurns:       config.WorkSubagentMaxTurns,
		}
	}
	set.addSDK(subagent.NewTool(subagent.Config{
		Provider:    p,
		ParentTools: registry,
		Agents:      agents,
		MaxDepth:    3,
	}))
}
