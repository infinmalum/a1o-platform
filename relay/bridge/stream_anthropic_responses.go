package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Stream converts Responses events into Anthropic Messages events. Reasoning
// items map onto thinking blocks carrying their signature, and encrypted
// reasoning onto redacted_thinking, so sealed blobs round-trip to the vendor
// that issued them; refusal deltas stream as text because a refusal is display
// text to the recipient; unmapped envelope fields ride message_stop's
// provider_metadata. Function-call arguments recovered from a done event keep
// providers that skip delta events intact, and every open block closes before
// the terminal pair. The terminal events are written only after the upstream
// terminal event so an interrupted stream never looks complete.
func (anthropicResponses) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &responsesToAnthropicStream{writer: anthropicStreamWriter{dst: dst, model: requestedModel}, blocks: map[string]int{}}
	reader := newSSEReader(src)
	fail := func(err error) (native.Usage, error) {
		_ = writeAnthropicStreamError(dst, err.Error())
		return state.writer.usage, err
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
		if state.writer.usage.Complete {
			break
		}
	}
	if !state.writer.usage.Complete {
		return fail(errors.New("Responses stream ended before response.completed"))
	}
	return state.writer.usage, nil
}

// responsesToAnthropicStream maps Responses output items onto Messages
// content blocks. Blocks are keyed by the upstream item id, so argument deltas
// reach the tool_use block that opened for their function_call item.
type responsesToAnthropicStream struct {
	writer             anthropicStreamWriter
	blocks             map[string]int
	argumentsSeenItems map[string]bool
	textOpen           bool
	textItemID         string
	stopReason         string
	sawToolCall        bool
	finished           bool
	metadata           providerMetadata
}

// closeOpenBlock closes whichever block type is streaming for the item.
func (s *responsesToAnthropicStream) closeItemBlock(itemID string) error {
	if s.textOpen && s.textItemID == itemID {
		return s.closeTextBlock()
	}
	if index, seen := s.blocks[itemID]; seen {
		return s.writer.closeBlock(index)
	}
	return nil
}

func (s *responsesToAnthropicStream) event(data []byte) error {
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
		fields, extra, err := rawObject(data, "type", "sequence_number", "response")
		if err != nil {
			return fmt.Errorf("invalid Responses response.created: %w", err)
		}
		s.metadata.absorb(extra)
		created, createdExtra, err := rawObject(fields["response"], "id", "status")
		if err != nil {
			return fmt.Errorf("invalid Responses response.created: %w", err)
		}
		s.metadata.absorb(createdExtra)
		if id := rawString(created["id"]); id != "" {
			s.writer.id = id
		}
		return nil
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
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return s.reasoningDelta(data)
	case "response.output_text.done", "response.refusal.done", "response.content_part.added",
		"response.content_part.done", "response.function_call_arguments.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
		"response.reasoning_summary_text.done", "response.reasoning_text.done":
		// Completed-item and part echoes restate state the deltas already
		// applied; nothing is dropped.
		return nil
	case "response.completed", "response.incomplete":
		return s.finishedEvent(data)
	case "response.failed":
		fields, _, err := rawObject(data, "type", "sequence_number", "response")
		if err != nil {
			return fmt.Errorf("invalid Responses failure event: %w", err)
		}
		if message := responsesFailureMessage(fields["response"]); message != "" {
			return errors.New(message)
		}
		return errors.New("upstream Responses stream failed")
	case "error":
		fields, _, err := rawObject(data, "type", "sequence_number", "code", "message", "param")
		if err != nil {
			return fmt.Errorf("invalid Responses error event: %w", err)
		}
		if message := rawString(fields["message"]); message != "" {
			return errors.New(message)
		}
		return errors.New("upstream Responses stream failed")
	default:
		return fmt.Errorf("Responses event %q cannot be converted", header.Type)
	}
}

func (s *responsesToAnthropicStream) itemAdded(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "output_index", "item")
	if err != nil {
		return fmt.Errorf("invalid Responses output_item event: %w", err)
	}
	s.metadata.absorb(extra)
	item, itemExtra, err := rawObject(fields["item"], "type", "id", "call_id", "name", "arguments", "status", "role", "content", "summary", "encrypted_content", "signature", "thought_signature")
	if err != nil {
		return fmt.Errorf("invalid Responses output item: %w", err)
	}
	s.metadata.absorb(itemExtra)
	itemID := rawString(item["id"])
	switch rawString(item["type"]) {
	case "message":
		return nil
	case "reasoning":
		if blob := rawString(item["encrypted_content"]); blob != "" {
			// A sealed blob is complete as it arrives; the issuing vendor is
			// the only party that can read it, and it round-trips opaquely.
			index, err := s.writer.openBlock(map[string]any{"type": "redacted_thinking", "data": blob})
			if err != nil {
				return err
			}
			if itemID != "" {
				s.blocks[itemID] = index
			}
			return s.writer.closeBlock(index)
		}
		if itemID == "" {
			return nil
		}
		index, err := s.writer.openBlock(map[string]any{"type": "thinking", "thinking": ""})
		if err != nil {
			return err
		}
		s.blocks[itemID] = index
		return nil
	case "function_call":
		callID, name := rawString(item["call_id"]), rawString(item["name"])
		if callID == "" || name == "" {
			return errors.New("Responses function_call requires a call_id and name")
		}
		if itemID != "" {
			if _, seen := s.blocks[itemID]; seen {
				return errors.New("Responses stream sent a function_call item twice")
			}
			index, err := s.writer.openBlock(map[string]any{"type": "tool_use", "id": callID, "name": name, "input": map[string]any{}})
			if err != nil {
				return err
			}
			s.blocks[itemID] = index
		}
		s.sawToolCall = true
		return nil
	default:
		return fmt.Errorf("Responses output item %q cannot be converted", rawString(item["type"]))
	}
}

func (s *responsesToAnthropicStream) itemDone(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "output_index", "item")
	if err != nil {
		return fmt.Errorf("invalid Responses output_item event: %w", err)
	}
	s.metadata.absorb(extra)
	item, itemExtra, err := rawObject(fields["item"], "type", "id", "call_id", "name", "arguments", "status", "role", "content", "summary", "encrypted_content", "signature", "thought_signature")
	if err != nil {
		return fmt.Errorf("invalid Responses output item: %w", err)
	}
	s.metadata.absorb(itemExtra)
	itemID := rawString(item["id"])
	switch rawString(item["type"]) {
	case "reasoning":
		if itemID == "" {
			return nil
		}
		index, seen := s.blocks[itemID]
		if !seen {
			return nil
		}
		// Signature deltas must precede the block stop so the client can
		// pair them with the thinking block.
		if signature := rawString(item["signature"]); signature != "" {
			if err := s.writer.delta(index, map[string]any{"type": "signature_delta", "signature": signature}); err != nil {
				return err
			}
		}
		delete(s.blocks, itemID)
		return s.writer.closeBlock(index)
	case "function_call":
		// A provider that streams complete arguments without deltas still
		// produces a valid tool_use block, and the block must close.
		if itemID == "" {
			return nil
		}
		index, seen := s.blocks[itemID]
		if !seen {
			return nil
		}
		if arguments := rawString(item["arguments"]); arguments != "" && !s.argumentsSeen(itemID) {
			if err := s.writer.delta(index, map[string]any{"type": "input_json_delta", "partial_json": arguments}); err != nil {
				return err
			}
		}
		delete(s.blocks, itemID)
		return s.writer.closeBlock(index)
	case "message":
		return nil
	default:
		return nil
	}
}

// argumentsSeen reports whether any argument delta already streamed for the
// item. The map records item ids that received at least one delta.
func (s *responsesToAnthropicStream) argumentsSeen(itemID string) bool {
	_, ok := s.argumentsSeenItems[itemID]
	return ok
}

func (s *responsesToAnthropicStream) textDelta(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "item_id", "output_index", "content_index", "delta", "obfuscation", "logprobs")
	if err != nil {
		return fmt.Errorf("invalid Responses delta event: %w", err)
	}
	s.metadata.absorb(extra)
	if nonNull(fields["logprobs"]) {
		return errors.New("Responses logprobs cannot be converted")
	}
	itemID := rawString(fields["item_id"])
	if text := rawString(fields["delta"]); text != "" {
		index, err := s.openTextBlock(itemID)
		if err != nil {
			return err
		}
		return s.writer.delta(index, map[string]any{"type": "text_delta", "text": text})
	}
	return nil
}

func (s *responsesToAnthropicStream) refusalDelta(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "item_id", "output_index", "content_index", "delta", "obfuscation")
	if err != nil {
		return fmt.Errorf("invalid Responses delta event: %w", err)
	}
	s.metadata.absorb(extra)
	itemID := rawString(fields["item_id"])
	if refusal := rawString(fields["delta"]); refusal != "" {
		index, err := s.openTextBlock(itemID)
		if err != nil {
			return err
		}
		return s.writer.delta(index, map[string]any{"type": "text_delta", "text": refusal})
	}
	return nil
}

func (s *responsesToAnthropicStream) argumentsDelta(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "item_id", "output_index", "delta", "obfuscation")
	if err != nil {
		return fmt.Errorf("invalid Responses delta event: %w", err)
	}
	s.metadata.absorb(extra)
	itemID := rawString(fields["item_id"])
	index, seen := s.blocks[itemID]
	if !seen {
		return errors.New("Responses tool arguments arrived before their function_call item")
	}
	if s.argumentsSeenItems == nil {
		s.argumentsSeenItems = map[string]bool{}
	}
	s.argumentsSeenItems[itemID] = true
	if arguments := rawString(fields["delta"]); arguments != "" {
		return s.writer.delta(index, map[string]any{"type": "input_json_delta", "partial_json": arguments})
	}
	return nil
}

// reasoningDelta streams visible thinking text into the item's thinking block.
func (s *responsesToAnthropicStream) reasoningDelta(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "item_id", "output_index", "summary_index", "content_index", "delta", "obfuscation")
	if err != nil {
		return fmt.Errorf("invalid Responses delta event: %w", err)
	}
	s.metadata.absorb(extra)
	itemID := rawString(fields["item_id"])
	if text := rawString(fields["delta"]); text != "" {
		index, err := s.openThinkingBlock(itemID)
		if err != nil {
			return err
		}
		return s.writer.delta(index, map[string]any{"type": "thinking_delta", "thinking": text})
	}
	return nil
}

// openThinkingBlock opens the thinking block a reasoning item streams into,
// falling back to a synthetic block when the upstream never sent item.added.
func (s *responsesToAnthropicStream) openThinkingBlock(itemID string) (int, error) {
	if index, seen := s.blocks[itemID]; seen {
		return index, nil
	}
	if itemID == "" {
		return 0, errors.New("Responses reasoning delta arrived without an item id")
	}
	index, err := s.writer.openBlock(map[string]any{"type": "thinking", "thinking": ""})
	if err != nil {
		return 0, err
	}
	s.blocks[itemID] = index
	return index, nil
}

// openTextBlock opens the text block a message item streams into, closing the
// previous message's block first so each message item is its own block.
func (s *responsesToAnthropicStream) openTextBlock(itemID string) (int, error) {
	if s.textOpen && s.textItemID == itemID {
		return s.blocks[s.textItemID], nil
	}
	if s.textOpen {
		if err := s.closeTextBlock(); err != nil {
			return 0, err
		}
	}
	if itemID == "" {
		return 0, errors.New("Responses delta arrived without an item id")
	}
	if _, seen := s.blocks[itemID]; seen {
		return 0, errors.New("Responses stream sent a message item twice")
	}
	index, err := s.writer.openBlock(map[string]any{"type": "text", "text": ""})
	if err != nil {
		return 0, err
	}
	s.blocks[itemID] = index
	s.textItemID = itemID
	s.textOpen = true
	return index, nil
}

func (s *responsesToAnthropicStream) closeTextBlock() error {
	if !s.textOpen {
		return nil
	}
	index := s.blocks[s.textItemID]
	if err := s.writer.closeBlock(index); err != nil {
		return err
	}
	s.textOpen = false
	s.textItemID = ""
	return nil
}

func (s *responsesToAnthropicStream) finishedEvent(data []byte) error {
	fields, extra, err := rawObject(data, "type", "sequence_number", "response")
	if err != nil {
		return fmt.Errorf("invalid Responses terminal event: %w", err)
	}
	s.metadata.absorb(extra)
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
			InputDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(fields["response"], &payload) != nil {
		return errors.New("invalid Responses terminal event")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return fmt.Errorf("upstream Responses request failed: %s", payload.Error.Message)
	}
	switch payload.Status {
	case "completed":
		s.stopReason = "end_turn"
		if s.sawToolCall {
			s.stopReason = "tool_use"
		}
	case "incomplete":
		reason := ""
		if payload.IncompleteDetails != nil {
			reason = payload.IncompleteDetails.Reason
		}
		switch reason {
		case "max_output_tokens":
			s.stopReason = "max_tokens"
		case "content_filter":
			s.stopReason = "refusal"
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
		s.writer.usage.Input = max(s.writer.usage.Input, payload.Usage.InputTokens)
		s.writer.usage.Output = max(s.writer.usage.Output, payload.Usage.OutputTokens)
		if payload.Usage.InputDetails != nil {
			s.writer.usage.CacheRead = max(s.writer.usage.CacheRead, payload.Usage.InputDetails.CachedTokens)
		}
		if payload.Usage.OutputDetails != nil {
			s.writer.usage.Reasoning = max(s.writer.usage.Reasoning, payload.Usage.OutputDetails.ReasoningTokens)
		}
		s.writer.usage.Seen = true
	}
	if err := s.closeTextBlock(); err != nil {
		return err
	}
	// Close any block still open (thinking or tool_use) so the event sequence
	// is well formed before the terminal pair.
	for itemID, index := range s.blocks {
		if err := s.writer.closeBlock(index); err != nil {
			return err
		}
		delete(s.blocks, itemID)
	}
	if err := s.writer.ensureStart(); err != nil {
		return err
	}
	return s.writer.finish(s.stopReason, "responses", s.metadata.fields)
}
