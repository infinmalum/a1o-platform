package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// ModelExecutor drives one billed model call: standard Items in, native
// provider protocol out, through the shared relay lifecycle so channel
// selection, bridging, reservation, settlement, and consume logs stay in one
// place. It intentionally owns no retry policy of its own — the run layer
// decides whether a failed invocation is followed by a new one.
//
// The executor speaks the OpenAI Responses wire as its inbound protocol
// (OpenResponses Item semantics; the bridge layer keeps the two protocol
// identities separate) and negotiates the upstream protocol per channel.
type ModelExecutor struct {
	UserID    int
	TokenID   int
	TokenName string
	// HTTPClient overrides the upstream transport in tests.
	HTTPClient *http.Client
}

// InvokeRequest is one model call over the run's current context.
type InvokeRequest struct {
	// RunID associates the call with a persisted run; required.
	RunID int
	Model string
	// Instructions is the run-level system prompt (agent instructions).
	Instructions string
	// Items is the conversation context to replay, ending with the new turn.
	Items []Item
	Tools []Tool
	// MaxOutputTokens bounds the reserved estimate; 0 uses the gateway default.
	MaxOutputTokens int64
	// Temperature, when set, rides the request for wires that carry it.
	Temperature *float64
	// OnTextDelta and OnReasoningDelta stream increments as they arrive;
	// nil in storage-only runs.
	OnTextDelta      func(string)
	OnReasoningDelta func(string)
	// SkipContextAppend keeps the call's output out of the run's persisted
	// context, for auxiliary legs (summarization) and hosts whose history
	// lives elsewhere (chat sessions persist their own rows).
	SkipContextAppend bool
}

// InvokeResult reports one completed model call.
type InvokeResult struct {
	InvocationID int
	ResponseID   string
	// Status is the protocol terminal status: completed, incomplete, or
	// failed. An empty status means the stream ended without a terminal.
	Status string
	Items  []Item
	Usage  native.Usage
}

// Invoke executes one model call and persists its invocation record and run
// events. The returned error carries the lifecycle failure; a nil error with
// Status "failed" or "" reports a protocol-level failure whose settlement
// already happened inside the lifecycle.
func (e *ModelExecutor) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	if req.RunID <= 0 {
		return InvokeResult{}, errors.New("agent invoke requires a run id")
	}
	if req.Model == "" {
		return InvokeResult{}, errors.New("agent invoke requires a model")
	}
	run, err := model.GetAgentRun(req.RunID, e.UserID)
	if err != nil {
		return InvokeResult{}, fmt.Errorf("load run: %w", err)
	}
	channel, upstream, err := e.selectUpstream(req.Model)
	if err != nil {
		return InvokeResult{}, err
	}
	body, err := buildResponsesRequest(req, upstream)
	if err != nil {
		return InvokeResult{}, err
	}

	sequence, err := model.NextRunInvocationSequence(req.RunID)
	if err != nil {
		return InvokeResult{}, fmt.Errorf("allocate invocation sequence: %w", err)
	}
	invocation := &model.ModelInvocation{
		RunId: req.RunID, Sequence: sequence,
		ChannelId: channel.ID, ChannelType: channel.Type,
		UpstreamAccountId: channel.UpstreamAccountID, UpstreamProtocol: string(upstream),
		ModelName: req.Model, MappedModel: mappedName(channel, req.Model),
	}
	requestItems, err := json.Marshal(req.Items)
	if err != nil {
		return InvokeResult{}, fmt.Errorf("marshal request items: %w", err)
	}
	invocation.RequestItems = ptr(string(requestItems))
	if err := model.CreateModelInvocation(invocation); err != nil {
		return InvokeResult{}, fmt.Errorf("persist invocation intent: %w", err)
	}
	// From here on the request intent exists: every exit records a terminal
	// invocation state so a crash cannot look like a clean failure.
	result, invokeErr := e.forward(ctx, req, invocation, channel, upstream, body)
	e.recordOutcome(req.RunID, invocation)
	if invokeErr != nil {
		return result, invokeErr
	}
	// forward returns nil only for calls that produced replayable output;
	// failures stay visible through events and the invocation record.
	if req.SkipContextAppend {
		return result, nil
	}
	if err := e.appendRunContext(run, result.Items); err != nil {
		return result, fmt.Errorf("persist run context: %w", err)
	}
	return result, nil
}

// selectUpstream negotiates the upstream protocol: candidates ordered by the
// model family, first channel availability wins. The Vercel OpenResponses
// gateway passes through on the same body the Responses upstream receives.
func (e *ModelExecutor) selectUpstream(modelName string) (lifecycle.Channel, native.Protocol, error) {
	candidates := protocolPreference(modelName)
	var failures []string
	for _, candidate := range candidates {
		input := lifecycle.Request{
			Model:     modelName,
			Principal: lifecycle.Principal{UserID: e.UserID, TokenID: e.TokenID, TokenName: e.TokenName},
		}
		channel, herr := lifecycle.SelectForRequest(input, candidate)
		if herr == nil {
			return channel, candidate, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %s", candidate, herr.Message))
	}
	return lifecycle.Channel{}, "", fmt.Errorf("no compatible channel for model %s (%s)",
		modelName, strings.Join(failures, "; "))
}

// forward runs the lifecycle call. The channel is pinned through
// Principal.SpecificChannel so a mid-call retry can never replay bound native
// state (thinking signatures, encrypted reasoning) onto a different channel;
// cross-channel failover is an explicit new-invocation decision for the run
// layer (contract §4).
func (e *ModelExecutor) forward(ctx context.Context, req InvokeRequest, invocation *model.ModelInvocation,
	channel lifecycle.Channel, upstream native.Protocol, body []byte,
) (InvokeResult, error) {
	collector := newStreamCollector(req.OnTextDelta, req.OnReasoningDelta)
	var consumeLogID int
	input := lifecycle.Request{
		Protocol:         native.OpenAIResponses,
		UpstreamProtocol: upstream,
		Model:            req.Model,
		Stream:           true,
		MaxOutputTokens:  req.MaxOutputTokens,
		Body:             body,
		Headers:          http.Header{},
		Channel:          channel,
		Principal: lifecycle.Principal{
			UserID: e.UserID, TokenID: e.TokenID, TokenName: e.TokenName,
			SpecificChannel: true, SpecificChannelID: channel.ID,
		},
		RetryLimit: config.RetryTimes,
		HTTPClient: e.HTTPClient,
		OnSettlement: func(logID int, usage native.Usage) {
			consumeLogID = logID
		},
	}
	herr := lifecycle.Forward(ctx, collector, input)
	outcome := collector.finalize()
	result := InvokeResult{
		InvocationID: invocation.Id,
		ResponseID:   outcome.ResponseID,
		Status:       outcome.Status,
		Items:        outcome.Items,
		Usage:        outcome.Usage,
	}
	invocation.ConsumeLogId = consumeLogID
	if outcome.Usage.Seen {
		usage, err := json.Marshal(outcome.Usage)
		if err == nil {
			invocation.Usage = ptr(string(usage))
		}
	}
	if len(outcome.Items) > 0 {
		responseItems, err := json.Marshal(outcome.Items)
		if err == nil {
			invocation.ResponseItems = ptr(string(responseItems))
		}
	}
	if herr != nil {
		// The lifecycle failed before output reached the collector; the
		// reservation was refunded inside it.
		invocation.Status = model.InvocationStatusFailed
		invocation.Error = herr.Message
		return result, fmt.Errorf("model call failed: %s", herr.Message)
	}
	switch {
	case outcome.Err != nil:
		invocation.Status = model.InvocationStatusFailed
		invocation.FinishStatus = ItemStatusFailed
		invocation.Error = outcome.Err.Error()
		return result, fmt.Errorf("model stream failed: %w", outcome.Err)
	case outcome.Status == "":
		// The upstream accepted the call (settlement happened, possibly
		// retaining the reservation for a partial stream) but no terminal
		// event arrived. This is never reported as success.
		invocation.Status = model.InvocationStatusUnknown
		invocation.Error = "model stream ended without a terminal event"
		return result, errors.New(invocation.Error)
	case outcome.Status == ItemStatusFailed:
		invocation.Status = model.InvocationStatusFailed
		invocation.FinishStatus = outcome.Status
		invocation.Error = "model reported failure"
		return result, errors.New(invocation.Error)
	default:
		invocation.Status = model.InvocationStatusSucceeded
		invocation.FinishStatus = outcome.Status
		return result, nil
	}
}

// recordOutcome persists the terminal invocation state and its event.
func (e *ModelExecutor) recordOutcome(runID int, invocation *model.ModelInvocation) {
	if err := model.CompleteModelInvocation(invocation); err != nil {
		// The invocation stays pending in the database, which is exactly the
		// state a crash leaves; the caller's result still stands but the
		// record gap is visible instead of papered over.
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"invocation_id":     invocation.Id,
		"status":            invocation.Status,
		"finish_status":     invocation.FinishStatus,
		"model":             invocation.ModelName,
		"channel_id":        invocation.ChannelId,
		"upstream_protocol": invocation.UpstreamProtocol,
		"usage":             json.RawMessage(orNull(invocation.Usage)),
		"error":             invocation.Error,
	})
	eventType := model.RunEventInvocationCompleted
	if invocation.Status != model.InvocationStatusSucceeded {
		eventType = model.RunEventInvocationFailed
	}
	e.appendEvent(runID, eventType, invocation.Id, payload)
}

// appendRunContext appends the call's output items to the run's persisted
// context so the next invocation replays them.
func (e *ModelExecutor) appendRunContext(run *model.AgentRun, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	contextItems := []Item{}
	if run.ContextItems != nil && *run.ContextItems != "" {
		if err := json.Unmarshal([]byte(*run.ContextItems), &contextItems); err != nil {
			return fmt.Errorf("run context is corrupt: %w", err)
		}
	}
	contextItems = append(contextItems, items...)
	encoded, err := json.Marshal(contextItems)
	if err != nil {
		return err
	}
	return model.UpdateAgentRunContext(run.Id, string(encoded))
}

func (e *ModelExecutor) appendEvent(runID int, eventType string, invocationID int, payload []byte) {
	sequence, err := model.NextRunEventSequence(runID)
	if err != nil {
		// Event persistence failures surface as needs_reconciliation material;
		// they never fail the model call itself after settlement.
		return
	}
	_ = model.CreateRunEvent(&model.RunEvent{
		RunId: runID, Sequence: sequence,
		EventId:       fmt.Sprintf("evt_%d_%d", runID, sequence),
		Type:          eventType,
		SchemaVersion: "1",
		InvocationId:  invocationID,
		Payload:       ptr(string(payload)),
	})
}

// protocolPreference orders the upstream protocol candidates for a model.
// The family prefix leads so VertexAI-style publishers align on the first
// candidate; the Responses family precedes bridged wires for everyone else.
func protocolPreference(modelName string) []native.Protocol {
	switch {
	case strings.HasPrefix(modelName, "claude"):
		return []native.Protocol{native.Anthropic, native.OpenAIResponses, native.Gemini, native.OpenAIChat}
	case strings.HasPrefix(modelName, "gemini"):
		return []native.Protocol{native.Gemini, native.OpenAIResponses, native.Anthropic, native.OpenAIChat}
	default:
		return []native.Protocol{native.OpenAIResponses, native.OpenResponses, native.Anthropic, native.Gemini, native.OpenAIChat}
	}
}

func mappedName(channel lifecycle.Channel, modelName string) string {
	if replacement := channel.ModelMapping[modelName]; replacement != "" {
		return replacement
	}
	return modelName
}

// buildResponsesRequest renders the inbound Responses body. Bridged upstreams
// receive the minimal convertible subset (their converters strict-reject
// unknown fields); the Responses-family upstream additionally carries
// store=false and the encrypted-reasoning include so sealed thinking
// round-trips with OpenAI.
func buildResponsesRequest(req InvokeRequest, upstream native.Protocol) ([]byte, error) {
	request := map[string]any{
		"model":  req.Model,
		"stream": true,
	}
	if len(req.Items) == 0 {
		return nil, errors.New("agent invoke requires at least one input item")
	}
	input := make([]map[string]any, 0, len(req.Items))
	for i, item := range req.Items {
		wire, err := itemToWire(item)
		if err != nil {
			return nil, fmt.Errorf("input item %d: %w", i, err)
		}
		input = append(input, wire)
	}
	request["input"] = input
	if req.Instructions != "" {
		request["instructions"] = req.Instructions
	}
	if req.Temperature != nil {
		request["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if tool.Name == "" {
				return nil, errors.New("tool name is required")
			}
			parameters := tool.Parameters
			if len(parameters) == 0 {
				parameters = json.RawMessage(`{}`)
			}
			entry := map[string]any{
				"type": "function", "name": tool.Name,
				"parameters": json.RawMessage(parameters),
			}
			if tool.Description != "" {
				entry["description"] = tool.Description
			}
			tools = append(tools, entry)
		}
		request["tools"] = tools
	}
	if req.MaxOutputTokens > 0 {
		request["max_output_tokens"] = req.MaxOutputTokens
	}
	if upstream == native.OpenAIResponses || upstream == native.OpenResponses {
		request["store"] = false
		request["include"] = []string{"reasoning.encrypted_content"}
	}
	return json.Marshal(request)
}

// itemToWire renders one standard Item onto the Responses wire shape the
// bridge converters parse. Unrepresentable items fail loudly.
func itemToWire(item Item) (map[string]any, error) {
	wire := map[string]any{"type": item.Type}
	switch item.Type {
	case ItemMessage:
		switch item.Role {
		case "user", "assistant":
		default:
			return nil, fmt.Errorf("message role %q cannot be represented", item.Role)
		}
		wire["role"] = item.Role
		parts := make([]map[string]any, 0, len(item.Content))
		for _, part := range item.Content {
			switch part.Type {
			case PartInputText, PartOutputText, PartReasoningText:
				entry := map[string]any{"type": part.Type, "text": part.Text}
				if len(part.Annotations) > 0 {
					entry["annotations"] = part.Annotations
				}
				parts = append(parts, entry)
			case PartInputImage:
				if item.Role != "user" {
					return nil, errors.New("input_image parts belong on user messages")
				}
				if part.ImageURL == "" {
					return nil, errors.New("input_image part requires an image_url")
				}
				parts = append(parts, map[string]any{"type": PartInputImage, "image_url": part.ImageURL})
			case PartRefusal:
				parts = append(parts, map[string]any{"type": PartRefusal, "refusal": part.Refusal})
			default:
				return nil, fmt.Errorf("content part %q cannot be represented", part.Type)
			}
		}
		if len(parts) == 0 {
			return nil, errors.New("message item carries no content parts")
		}
		wire["content"] = parts
	case ItemReasoning:
		if len(item.Summary) > 0 {
			summary := make([]map[string]any, 0, len(item.Summary))
			for _, entry := range item.Summary {
				summary = append(summary, map[string]any{"type": PartSummaryText, "text": entry.Text})
			}
			wire["summary"] = summary
		}
		if len(item.Content) > 0 {
			content := make([]map[string]any, 0, len(item.Content))
			for _, part := range item.Content {
				content = append(content, map[string]any{"type": PartReasoningText, "text": part.Text})
			}
			wire["content"] = content
		}
		if item.EncryptedContent != "" {
			wire["encrypted_content"] = item.EncryptedContent
		}
		if item.Signature != "" {
			wire["signature"] = item.Signature
		}
		if len(item.ProviderState) > 0 {
			return nil, errors.New("reasoning item carries opaque provider state no wire can replay")
		}
	case ItemFunctionCall:
		if item.CallID == "" || item.Name == "" {
			return nil, errors.New("function_call item requires call_id and name")
		}
		wire["call_id"] = item.CallID
		wire["name"] = item.Name
		wire["arguments"] = item.Arguments
		if item.ThoughtSignature != "" {
			wire["thought_signature"] = item.ThoughtSignature
		}
	case ItemFunctionCallOutput:
		if item.CallID == "" || item.Output == "" {
			return nil, errors.New("function_call_output item requires call_id and output")
		}
		wire["call_id"] = item.CallID
		wire["output"] = item.Output
	default:
		return nil, fmt.Errorf("item type %q cannot be sent upstream", item.Type)
	}
	return wire, nil
}

func ptr[T any](value T) *T {
	return &value
}

func orNull(value *string) string {
	if value == nil {
		return "null"
	}
	return *value
}
