package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/one-gateway/relay/native"
)

// Stream converts Responses events into Chat Completions chunks. Reasoning
// items and summaries carry no Chat representation and are skipped; server
// tools cannot appear because the request conversion only forwards function
// tools.
func (r *chatResponses) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &responsesToChatStream{
		dst: dst, model: requestedModel, created: streamTimestamp(),
		includeUsage: r.includeUsage, toolIndexes: map[int]int{}, toolDeltaSeen: map[int]bool{},
	}
	reader := newSSEReader(src)
	fail := func(err error) (native.Usage, error) {
		_ = writeStreamError(dst, err.Error())
		return state.usage, err
	}
	for {
		data, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(err)
		}
		if err := state.event(data); err != nil {
			return fail(err)
		}
		if state.usage.Complete {
			break
		}
	}
	if !state.usage.Complete {
		return fail(errors.New("Responses stream ended before response.completed"))
	}
	return state.usage, writeStreamDone(dst)
}

type responsesToChatStream struct {
	dst           StreamWriter
	model         string
	created       int64
	id            string
	started       bool
	includeUsage  bool
	toolCount     int
	toolIndexes   map[int]int
	toolDeltaSeen map[int]bool
	usage         native.Usage
}

func (s *responsesToChatStream) event(data []byte) error {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &header) != nil || header.Type == "" {
		return errors.New("invalid Responses event")
	}
	switch header.Type {
	case "response.created", "response.in_progress", "response.queued":
		if header.Type != "response.created" {
			return nil
		}
		return s.responseCreated(data)
	case "response.output_item.added":
		return s.itemAdded(data)
	case "response.output_item.done":
		return s.itemDone(data)
	case "response.output_text.delta":
		return s.textDelta(data)
	case "response.refusal.delta":
		return s.refusalDelta(data)
	case "response.function_call_arguments.delta":
		return s.argumentsDelta(data)
	case "response.completed", "response.incomplete":
		return s.finished(data)
	case "response.failed":
		return s.failed(data)
	case "error":
		fields, _, err := rawObject(data, "type", "sequence_number", "code", "message", "param")
		if err != nil {
			return fmt.Errorf("invalid Responses error event: %w", err)
		}
		if message := rawString(fields["message"]); message != "" {
			return errors.New(message)
		}
		return errors.New("upstream Responses stream failed")
	// Reasoning summaries and completed-item echoes carry no Chat state.
	case "response.output_text.done", "response.content_part.added", "response.content_part.done",
		"response.function_call_arguments.done", "response.reasoning_summary_part.added",
		"response.reasoning_summary_part.done", "response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done", "response.reasoning_text.delta", "response.reasoning_text.done":
		return nil
	default:
		return fmt.Errorf("Responses event %q cannot be converted", header.Type)
	}
}

func (s *responsesToChatStream) responseCreated(data []byte) error {
	if s.started {
		return errors.New("Responses stream sent response.created twice")
	}
	fields, _, err := rawObject(data, "type", "sequence_number", "response")
	if err != nil {
		return fmt.Errorf("invalid Responses response.created: %w", err)
	}
	created, _, err := rawObject(fields["response"], "id", "status")
	if err != nil {
		return fmt.Errorf("invalid Responses response.created: %w", err)
	}
	if id := rawString(created["id"]); id != "" {
		s.id = id
	}
	return s.ensureStart()
}

// ensureStart emits the leading role chunk, generating a response ID when the
// upstream never sent response.created.
func (s *responsesToChatStream) ensureStart() error {
	if s.started {
		return nil
	}
	if s.id == "" {
		generated, err := newStreamID(chatCompletionIDPrefix)
		if err != nil {
			return err
		}
		s.id = generated
	}
	if err := emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"role": "assistant", "content": ""}, "", nil); err != nil {
		return err
	}
	s.started = true
	return nil
}

func (s *responsesToChatStream) itemAdded(data []byte) error {
	if err := s.ensureStart(); err != nil {
		return err
	}
	item, index, err := s.outputItem(data)
	if err != nil {
		return err
	}
	switch rawString(item["type"]) {
	case "message", "reasoning":
		return nil
	case "function_call":
		callID, name := rawString(item["call_id"]), rawString(item["name"])
		if callID == "" || name == "" {
			return errors.New("Responses function_call requires a call_id and name")
		}
		toolIndex := s.toolCount
		s.toolCount++
		s.toolIndexes[index] = toolIndex
		delta := map[string]any{"tool_calls": []any{map[string]any{
			"index": toolIndex, "id": callID, "type": "function",
			"function": map[string]string{"name": name, "arguments": ""},
		}}}
		return emitChatChunk(s.dst, s.id, s.created, s.model, delta, "", nil)
	default:
		return fmt.Errorf("Responses output item %q cannot be converted", rawString(item["type"]))
	}
}

func (s *responsesToChatStream) itemDone(data []byte) error {
	if err := s.ensureStart(); err != nil {
		return err
	}
	item, index, err := s.outputItem(data)
	if err != nil {
		return err
	}
	switch rawString(item["type"]) {
	case "message", "reasoning":
		return nil
	case "function_call":
		// A provider that streams complete arguments without deltas still
		// produces a valid Chat tool call.
		if s.toolDeltaSeen[index] {
			return nil
		}
		toolIndex, ok := s.toolIndexes[index]
		if !ok {
			return errors.New("Responses function_call ended without a start event")
		}
		if arguments := rawString(item["arguments"]); arguments != "" {
			return s.emitArguments(toolIndex, arguments)
		}
		return nil
	default:
		return fmt.Errorf("Responses output item %q cannot be converted", rawString(item["type"]))
	}
}

// outputItem parses an output_item event and returns the item object with its
// output index. Item fields beyond tool identity are envelope state and
// ignored.
func (s *responsesToChatStream) outputItem(data []byte) (map[string]json.RawMessage, int, error) {
	fields, extra, err := rawObject(data, "type", "sequence_number", "output_index", "item")
	if err != nil {
		return nil, 0, fmt.Errorf("invalid Responses output_item event: %w", err)
	}
	if len(extra) > 0 {
		return nil, 0, fmt.Errorf("Responses output_item field %q cannot be converted", firstKey(extra))
	}
	index, err := parseIntField(fields["output_index"], "output index")
	if err != nil {
		return nil, 0, err
	}
	item, extra, err := rawObject(fields["item"], "type", "id", "call_id", "name", "arguments", "status", "role", "content", "summary", "encrypted_content")
	if err != nil {
		return nil, 0, fmt.Errorf("invalid Responses output item: %w", err)
	}
	if len(extra) > 0 {
		return nil, 0, fmt.Errorf("Responses output item field %q cannot be converted", firstKey(extra))
	}
	return item, index, nil
}

func (s *responsesToChatStream) textDelta(data []byte) error {
	delta, err := s.contentDelta(data, "type", "sequence_number", "item_id", "output_index", "content_index", "delta", "obfuscation", "logprobs")
	if err != nil {
		return err
	}
	if nonNull(delta["logprobs"]) {
		return errors.New("Responses logprobs cannot be converted")
	}
	if text := rawString(delta["delta"]); text != "" {
		return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"content": text}, "", nil)
	}
	return nil
}

func (s *responsesToChatStream) refusalDelta(data []byte) error {
	delta, err := s.contentDelta(data, "type", "sequence_number", "item_id", "output_index", "content_index", "delta", "obfuscation")
	if err != nil {
		return err
	}
	if refusal := rawString(delta["delta"]); refusal != "" {
		return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"refusal": refusal}, "", nil)
	}
	return nil
}

func (s *responsesToChatStream) argumentsDelta(data []byte) error {
	delta, err := s.contentDelta(data, "type", "sequence_number", "item_id", "output_index", "delta", "obfuscation")
	if err != nil {
		return err
	}
	index, err := parseIntField(delta["output_index"], "output index")
	if err != nil {
		return err
	}
	toolIndex, ok := s.toolIndexes[index]
	if !ok {
		return errors.New("Responses tool arguments arrived before their function_call item")
	}
	s.toolDeltaSeen[index] = true
	if arguments := rawString(delta["delta"]); arguments != "" {
		return s.emitArguments(toolIndex, arguments)
	}
	return nil
}

// contentDelta applies the shared event shape of delta events: strict
// top-level fields plus the delta payload.
func (s *responsesToChatStream) contentDelta(data []byte, permitted ...string) (map[string]json.RawMessage, error) {
	if err := s.ensureStart(); err != nil {
		return nil, err
	}
	fields, extra, err := rawObject(data, permitted...)
	if err != nil {
		return nil, fmt.Errorf("invalid Responses delta event: %w", err)
	}
	if len(extra) > 0 {
		return nil, fmt.Errorf("Responses delta field %q cannot be converted", firstKey(extra))
	}
	return fields, nil
}

func (s *responsesToChatStream) emitArguments(toolIndex int, arguments string) error {
	delta := map[string]any{"tool_calls": []any{map[string]any{
		"index":    toolIndex,
		"function": map[string]string{"arguments": arguments},
	}}}
	return emitChatChunk(s.dst, s.id, s.created, s.model, delta, "", nil)
}

func (s *responsesToChatStream) finished(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "response")
	if err != nil {
		return fmt.Errorf("invalid Responses terminal event: %w", err)
	}
	if len(extra) > 0 {
		return fmt.Errorf("Responses terminal event field %q cannot be converted", firstKey(extra))
	}
	var payload struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(fields["response"], &payload) != nil {
		return errors.New("invalid Responses terminal event")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return fmt.Errorf("upstream Responses request failed: %s", payload.Error.Message)
	}
	var finish string
	switch payload.Status {
	case "completed":
		// Chat clients dispatch on the tool_calls finish reason.
		if s.toolCount > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	case "incomplete":
		reason := ""
		if payload.IncompleteDetails != nil {
			reason = payload.IncompleteDetails.Reason
		}
		switch reason {
		case "max_output_tokens":
			finish = "length"
		case "content_filter":
			finish = "content_filter"
		default:
			return fmt.Errorf("Responses incomplete reason %q cannot be converted", reason)
		}
	default:
		return fmt.Errorf("Responses status %q cannot be converted", payload.Status)
	}
	if payload.Usage != nil {
		if payload.Usage.InputTokens < 0 || payload.Usage.OutputTokens < 0 {
			return errors.New("invalid Responses usage")
		}
		s.usage.Input = max(s.usage.Input, payload.Usage.InputTokens)
		s.usage.Output = max(s.usage.Output, payload.Usage.OutputTokens)
		s.usage.Seen = true
	}
	if err := emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{}, finish, nil); err != nil {
		return err
	}
	if s.includeUsage && s.usage.Seen {
		envelope := map[string]any{
			"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{},
			"usage":   map[string]int64{"prompt_tokens": s.usage.Input, "completion_tokens": s.usage.Output, "total_tokens": s.usage.Input + s.usage.Output},
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		if err := writeStreamEvent(s.dst, encoded); err != nil {
			return err
		}
	}
	s.usage.Complete = true
	return nil
}

func (s *responsesToChatStream) failed(data []byte) error {
	fields, _, err := rawObject(data, "type", "sequence_number", "response")
	if err != nil {
		return fmt.Errorf("invalid Responses failure event: %w", err)
	}
	if message := responsesFailureMessage(fields["response"]); message != "" {
		return errors.New(message)
	}
	return errors.New("upstream Responses stream failed")
}
