package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Stream converts Anthropic Messages events into Responses events. Thinking
// blocks map onto reasoning items carrying their signature, redacted thinking
// onto the encrypted_content slot; unknown block kinds fail the stream instead
// of being dropped, and unmapped envelope fields ride the terminal event's
// provider_metadata. The terminal status maps from message_delta's stop reason.
func (responsesAnthropic) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &anthropicToResponsesStream{emitter: *newResponsesStreamEmitter(dst, requestedModel), blockKinds: map[int]string{}}
	reader := newSSEReader(src)
	fail := func(err error) (native.Usage, error) {
		_ = state.emitter.fail(err.Error())
		return state.emitter.usage, err
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
		if state.emitter.usage.Complete {
			break
		}
	}
	if !state.emitter.usage.Complete {
		return fail(errors.New("Anthropic stream ended before message_stop"))
	}
	return state.emitter.usage, nil
}

type anthropicToResponsesStream struct {
	emitter        responsesStreamEmitter
	stopSeen       bool
	terminalReason string
	blockKinds     map[int]string
}

func (s *anthropicToResponsesStream) event(data []byte) error {
	if s.emitter.usage.Complete {
		return errors.New("Anthropic stream sent an event after message_stop")
	}
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &header) != nil || header.Type == "" {
		return errors.New("invalid Anthropic event")
	}
	switch header.Type {
	case "message_start":
		return s.messageStart(data)
	case "content_block_start":
		return s.blockStart(data)
	case "content_block_delta":
		return s.blockDelta(data)
	case "content_block_stop":
		return s.blockStop(data)
	case "ping":
		return nil
	case "message_delta":
		return s.messageDelta(data)
	case "message_stop":
		if _, extra, err := rawObject(data, "type"); err != nil {
			return err
		} else {
			s.emitter.metadata.absorb(extra)
		}
		if !s.emitter.started || !s.stopSeen {
			return errors.New("Anthropic message_stop arrived before a stop reason")
		}
		if err := s.emitter.closeItems(); err != nil {
			return err
		}
		status, reason, err := responsesStreamStatus(s.terminalReason == "", s.terminalReason)
		if err != nil {
			return err
		}
		return s.emitter.terminal(status, reason)
	case "error":
		fields, extra, err := rawObject(data, "type", "error")
		if err != nil {
			return fmt.Errorf("invalid Anthropic error event: %w", err)
		}
		s.emitter.metadata.absorb(extra)
		failure, _, err := rawObject(fields["error"], "type", "message")
		if err != nil {
			return fmt.Errorf("invalid Anthropic error event: %w", err)
		}
		if message := rawString(failure["message"]); message != "" {
			return errors.New(message)
		}
		return errors.New("upstream Anthropic stream failed")
	default:
		return fmt.Errorf("Anthropic event %q cannot be converted", header.Type)
	}
}

func (s *anthropicToResponsesStream) messageStart(data []byte) error {
	if s.emitter.started {
		return errors.New("Anthropic stream sent message_start twice")
	}
	fields, extra, err := rawObject(data, "type", "message")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_start: %w", err)
	}
	s.emitter.metadata.absorb(extra)
	message, messageExtra, err := rawObject(fields["message"], "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_start: %w", err)
	}
	s.emitter.metadata.absorb(messageExtra)
	s.emitter.responseID = rawString(message["id"])
	if s.emitter.responseID == "" {
		return errors.New("Anthropic message_start requires an id")
	}
	if role := rawString(message["role"]); role != "" && role != "assistant" {
		return errors.New("Anthropic response role must be assistant")
	}
	if nonNull(message["stop_reason"]) || nonNull(message["stop_sequence"]) {
		return errors.New("Anthropic message_start cannot contain a stop reason")
	}
	if nonNull(message["content"]) {
		var initialBlocks []json.RawMessage
		if json.Unmarshal(message["content"], &initialBlocks) != nil || len(initialBlocks) != 0 {
			return errors.New("Anthropic message_start content must be empty")
		}
	}
	if nonNull(message["usage"]) {
		if err := applyAnthropicUsage(message["usage"], &s.emitter.usage, true, &s.emitter.metadata); err != nil {
			return err
		}
	}
	return s.emitter.ensureCreated()
}

func (s *anthropicToResponsesStream) blockStart(data []byte) error {
	if !s.emitter.started {
		return errors.New("Anthropic stream sent content before message_start")
	}
	fields, extra, err := rawObject(data, "type", "index", "content_block")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content_block_start: %w", err)
	}
	s.emitter.metadata.absorb(extra)
	index, err := parseIntField(fields["index"], "block index")
	if err != nil {
		return err
	}
	block, blockExtra, err := rawObject(fields["content_block"], "type", "text", "id", "name", "input", "thinking", "signature", "data")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content block: %w", err)
	}
	s.emitter.metadata.absorb(blockExtra)
	kind := rawString(block["type"])
	s.blockKinds[index] = kind
	switch kind {
	case "text":
		if len(block) > 2 {
			return errors.New("invalid Anthropic text block")
		}
		if initial := rawString(block["text"]); initial != "" {
			return s.emitter.addText(initial)
		}
		return nil
	case "thinking":
		return s.emitter.openReasoningItem()
	case "redacted_thinking":
		// The sealed blob is complete at block start; the issuing vendor is
		// the only party that can read it, and it round-trips opaquely.
		return s.emitter.setReasoningEncrypted(rawString(block["data"]))
	case "tool_use":
		if len(block) != 4 {
			return errors.New("invalid Anthropic tool_use block")
		}
		// The block index keys both the start and the input_json_delta events,
		// so it doubles as the emitter's tool index.
		_, err := s.emitter.openToolItem(rawString(block["id"]), rawString(block["name"]), index)
		return err
	default:
		return fmt.Errorf("Anthropic content block %q cannot be converted", kind)
	}
}

func (s *anthropicToResponsesStream) blockDelta(data []byte) error {
	if !s.emitter.started {
		return errors.New("Anthropic stream sent content before message_start")
	}
	fields, extra, err := rawObject(data, "type", "index", "delta")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content_block_delta: %w", err)
	}
	s.emitter.metadata.absorb(extra)
	index, err := parseIntField(fields["index"], "block index")
	if err != nil {
		return err
	}
	delta, deltaExtra, err := rawObject(fields["delta"], "type", "text", "partial_json", "thinking", "signature")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content delta: %w", err)
	}
	s.emitter.metadata.absorb(deltaExtra)
	switch rawString(delta["type"]) {
	case "text_delta":
		if len(delta) != 2 {
			return errors.New("invalid Anthropic text delta")
		}
		return s.emitter.addText(rawString(delta["text"]))
	case "thinking_delta":
		if s.blockKinds[index] != "thinking" {
			return errors.New("Anthropic thinking delta arrived outside a thinking block")
		}
		return s.emitter.addReasoningText(rawString(delta["thinking"]))
	case "signature_delta":
		if s.blockKinds[index] != "thinking" {
			return errors.New("Anthropic signature delta arrived outside a thinking block")
		}
		return s.emitter.setReasoningSignature(rawString(delta["signature"]))
	case "input_json_delta":
		if len(delta) != 2 {
			return errors.New("invalid Anthropic tool delta")
		}
		if s.emitter.toolItemByIndex(index) == nil {
			return errors.New("Anthropic tool delta arrived before its tool_use block")
		}
		return s.emitter.addToolArguments(index, rawString(delta["partial_json"]))
	default:
		return fmt.Errorf("Anthropic delta %q cannot be converted", rawString(delta["type"]))
	}
}

func (s *anthropicToResponsesStream) blockStop(data []byte) error {
	fields, extra, err := rawObject(data, "type", "index")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content_block_stop: %w", err)
	}
	s.emitter.metadata.absorb(extra)
	index, err := parseIntField(fields["index"], "block index")
	if err != nil {
		return err
	}
	if s.blockKinds[index] == "thinking" {
		return s.emitter.closeReasoningItem()
	}
	return nil
}

func (s *anthropicToResponsesStream) messageDelta(data []byte) error {
	if !s.emitter.started {
		return errors.New("Anthropic stream sent message_delta before message_start")
	}
	fields, extra, err := rawObject(data, "type", "delta", "usage")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_delta: %w", err)
	}
	s.emitter.metadata.absorb(extra)
	delta, deltaExtra, err := rawObject(fields["delta"], "type", "stop_reason", "stop_sequence")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message delta: %w", err)
	}
	s.emitter.metadata.absorb(deltaExtra)
	if nonNull(fields["usage"]) {
		if err := applyAnthropicUsage(fields["usage"], &s.emitter.usage, false, &s.emitter.metadata); err != nil {
			return err
		}
	}
	if matched := rawString(delta["stop_sequence"]); matched != "" {
		s.emitter.metadata.set("matched_stop_sequence", matched)
	}
	if reason := rawString(delta["stop_reason"]); reason != "" {
		switch reason {
		case "end_turn", "stop_sequence", "tool_use", "refusal":
			s.stopSeen, s.terminalReason = true, ""
		case "max_tokens", "model_context_window_exceeded":
			s.stopSeen, s.terminalReason = true, "max_output_tokens"
		default:
			return fmt.Errorf("Anthropic stop reason %q cannot be converted", reason)
		}
	}
	return nil
}
