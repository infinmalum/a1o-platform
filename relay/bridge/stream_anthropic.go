package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Stream converts Anthropic Messages events into Chat Completions chunks.
// Thinking text streams as the Chat wire's reasoning_content slot with the
// signature riding provider_metadata; redacted thinking and citations cannot
// round-trip and fail the stream instead of being dropped.
func (chatAnthropic) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &anthropicToChatStream{dst: dst, model: requestedModel, created: streamTimestamp(), blockTools: map[int]int{}}
	reader := newSSEReader(src)
	for {
		data, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return state.usage, writeStreamError(dst, err.Error())
		}
		if err := state.event(data); err != nil {
			_ = writeStreamError(dst, err.Error())
			return state.usage, err
		}
	}
	if !state.usage.Complete {
		err := errors.New("Anthropic stream ended before message_stop")
		_ = writeStreamError(dst, err.Error())
		return state.usage, err
	}
	return state.usage, writeStreamDone(dst)
}

type anthropicToChatStream struct {
	dst          StreamWriter
	model        string
	created      int64
	id           string
	started      bool
	stopSeen     bool
	toolCount    int
	blockTools   map[int]int
	thinkingOpen bool
	usage        native.Usage
	metadata     providerMetadata
}

func (s *anthropicToChatStream) event(data []byte) error {
	if s.usage.Complete {
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
		fields, extra, err := rawObject(data, "type", "index")
		if err != nil {
			return err
		}
		s.metadata.absorb(extra)
		index, err := parseIntField(fields["index"], "block index")
		if err != nil {
			return err
		}
		if s.thinkingOpen {
			s.thinkingOpen = false
		}
		_ = index
		return nil
	case "message_delta":
		return s.messageDelta(data)
	case "message_stop":
		if _, extra, err := rawObject(data, "type"); err != nil {
			return err
		} else {
			s.metadata.absorb(extra)
		}
		if !s.started || !s.stopSeen {
			return errors.New("Anthropic message_stop arrived before a finish reason")
		}
		s.usage.Complete = true
		return nil
	case "ping":
		return nil
	case "error":
		fields, extra, err := rawObject(data, "type", "error")
		if err != nil {
			return fmt.Errorf("invalid Anthropic error event: %w", err)
		}
		s.metadata.absorb(extra)
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

func (s *anthropicToChatStream) messageStart(data []byte) error {
	if s.started {
		return errors.New("Anthropic stream sent message_start twice")
	}
	fields, extra, err := rawObject(data, "type", "message")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_start: %w", err)
	}
	s.metadata.absorb(extra)
	message, messageExtra, err := rawObject(fields["message"], "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_start: %w", err)
	}
	s.metadata.absorb(messageExtra)
	s.id = rawString(message["id"])
	if s.id == "" {
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
	if len(message["usage"]) != 0 {
		if err := s.observeInputUsage(message["usage"]); err != nil {
			return err
		}
	}
	s.started = true
	return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"role": "assistant", "content": ""}, "", nil)
}

func (s *anthropicToChatStream) observeInputUsage(raw json.RawMessage) error {
	usage, extra, err := rawObject(raw, "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")
	if err != nil {
		return fmt.Errorf("invalid Anthropic usage: %w", err)
	}
	s.metadata.absorb(extra)
	input, ok := decodeStreamNumber(usage["input_tokens"])
	if !ok || !nonNull(usage["input_tokens"]) {
		return errors.New("Anthropic usage requires input_tokens")
	}
	cacheCreation, ok := decodeStreamNumber(usage["cache_creation_input_tokens"])
	if !ok {
		return errors.New("invalid Anthropic usage")
	}
	cacheRead, ok := decodeStreamNumber(usage["cache_read_input_tokens"])
	if !ok {
		return errors.New("invalid Anthropic usage")
	}
	s.usage.CacheCreation = max(s.usage.CacheCreation, cacheCreation)
	s.usage.CacheRead = max(s.usage.CacheRead, cacheRead)
	s.usage.Input = max(s.usage.Input, input+cacheCreation+cacheRead)
	s.usage.Seen = true
	return nil
}

func (s *anthropicToChatStream) blockStart(data []byte) error {
	if !s.started {
		return errors.New("Anthropic stream sent content before message_start")
	}
	fields, extra, err := rawObject(data, "type", "index", "content_block")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content_block_start: %w", err)
	}
	s.metadata.absorb(extra)
	index, err := parseIntField(fields["index"], "block index")
	if err != nil {
		return err
	}
	block, blockExtra, err := rawObject(fields["content_block"], "type", "text", "id", "name", "input", "thinking", "signature", "data", "citations")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content block: %w", err)
	}
	s.metadata.absorb(blockExtra)
	kind := rawString(block["type"])
	switch kind {
	case "text":
		if len(block) > 2 {
			return errors.New("invalid Anthropic text block")
		}
		var initial string
		if json.Unmarshal(block["text"], &initial) != nil {
			return errors.New("Anthropic text block requires text")
		}
		if initial != "" {
			return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"content": initial}, "", nil)
		}
		return nil
	case "thinking":
		s.thinkingOpen = true
		if initial := rawString(block["thinking"]); initial != "" {
			return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"reasoning_content": initial}, "", nil)
		}
		return nil
	case "redacted_thinking":
		return errors.New("Anthropic redacted thinking cannot round-trip through the Chat wire; use the Anthropic or Responses endpoint")
	case "tool_use":
		if len(block) != 4 {
			return errors.New("invalid Anthropic tool_use block")
		}
		id, name := rawString(block["id"]), rawString(block["name"])
		if id == "" || name == "" {
			return errors.New("Anthropic tool_use requires an id and name")
		}
		toolIndex := s.toolCount
		s.toolCount++
		s.blockTools[index] = toolIndex
		delta := map[string]any{"tool_calls": []any{map[string]any{
			"index": toolIndex, "id": id, "type": "function",
			"function": map[string]string{"name": name, "arguments": ""},
		}}}
		return emitChatChunk(s.dst, s.id, s.created, s.model, delta, "", nil)
	default:
		return fmt.Errorf("Anthropic content block %q cannot be converted", kind)
	}
}

func (s *anthropicToChatStream) blockDelta(data []byte) error {
	if !s.started {
		return errors.New("Anthropic stream sent content before message_start")
	}
	fields, extra, err := rawObject(data, "type", "index", "delta")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content_block_delta: %w", err)
	}
	s.metadata.absorb(extra)
	index, err := parseIntField(fields["index"], "block index")
	if err != nil {
		return err
	}
	delta, deltaExtra, err := rawObject(fields["delta"], "type", "text", "partial_json", "thinking", "signature")
	if err != nil {
		return fmt.Errorf("invalid Anthropic content delta: %w", err)
	}
	s.metadata.absorb(deltaExtra)
	switch rawString(delta["type"]) {
	case "text_delta":
		if len(delta) != 2 {
			return errors.New("invalid Anthropic text delta")
		}
		return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"content": rawString(delta["text"])}, "", nil)
	case "thinking_delta":
		if !s.thinkingOpen {
			return errors.New("Anthropic thinking delta arrived outside a thinking block")
		}
		return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"reasoning_content": rawString(delta["thinking"])}, "", nil)
	case "signature_delta":
		if !s.thinkingOpen {
			return errors.New("Anthropic signature delta arrived outside a thinking block")
		}
		// The Chat wire has no per-message signature slot; the signature
		// rides provider_metadata so nothing is lost.
		s.metadata.set("thinking_signature", rawString(delta["signature"]))
		return nil
	case "input_json_delta":
		if len(delta) != 2 {
			return errors.New("invalid Anthropic tool delta")
		}
		toolIndex, ok := s.blockTools[index]
		if !ok {
			return errors.New("Anthropic tool delta arrived before its tool_use block")
		}
		toolDelta := map[string]any{"tool_calls": []any{map[string]any{
			"index":    toolIndex,
			"function": map[string]string{"arguments": rawString(delta["partial_json"])},
		}}}
		return emitChatChunk(s.dst, s.id, s.created, s.model, toolDelta, "", nil)
	default:
		return fmt.Errorf("Anthropic delta %q cannot be converted", rawString(delta["type"]))
	}
}

func (s *anthropicToChatStream) messageDelta(data []byte) error {
	if !s.started {
		return errors.New("Anthropic stream sent message_delta before message_start")
	}
	fields, extra, err := rawObject(data, "type", "delta", "usage")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message_delta: %w", err)
	}
	s.metadata.absorb(extra)
	delta, deltaExtra, err := rawObject(fields["delta"], "type", "stop_reason", "stop_sequence")
	if err != nil {
		return fmt.Errorf("invalid Anthropic message delta: %w", err)
	}
	s.metadata.absorb(deltaExtra)
	if matched := rawString(delta["stop_sequence"]); matched != "" {
		s.metadata.set("matched_stop_sequence", matched)
	}
	if len(fields["usage"]) != 0 {
		if err := s.observeOutputUsage(fields["usage"]); err != nil {
			return err
		}
	}
	finish := ""
	if reason := rawString(delta["stop_reason"]); reason != "" {
		mapped, err := mapAnthropicStopReason(reason)
		if err != nil {
			return err
		}
		finish = mapped
		s.stopSeen = true
	}
	if err := emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{}, finish, nil); err != nil {
		return err
	}
	// The usage chunk closes the stream with the provider's counts and the
	// cache breakdown, matching what include_usage produces on native Chat.
	if s.usage.Seen {
		envelope := map[string]any{
			"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{},
			"usage":   chatUsageEnvelope(s.usage),
		}
		if m := s.metadata.value(); m != nil {
			envelope["provider_metadata"] = m
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		return writeStreamEvent(s.dst, encoded)
	}
	return nil
}

func (s *anthropicToChatStream) observeOutputUsage(raw json.RawMessage) error {
	usage, extra, err := rawObject(raw, "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")
	if err != nil {
		return fmt.Errorf("invalid Anthropic usage: %w", err)
	}
	s.metadata.absorb(extra)
	if nonNull(usage["input_tokens"]) {
		input, ok := decodeStreamNumber(usage["input_tokens"])
		if !ok {
			return errors.New("invalid Anthropic usage")
		}
		cacheCreation, ok := decodeStreamNumber(usage["cache_creation_input_tokens"])
		if !ok {
			return errors.New("invalid Anthropic usage")
		}
		cacheRead, ok := decodeStreamNumber(usage["cache_read_input_tokens"])
		if !ok {
			return errors.New("invalid Anthropic usage")
		}
		s.usage.CacheCreation = max(s.usage.CacheCreation, cacheCreation)
		s.usage.CacheRead = max(s.usage.CacheRead, cacheRead)
		s.usage.Input = max(s.usage.Input, input+cacheCreation+cacheRead)
		s.usage.Seen = true
	}
	if nonNull(usage["output_tokens"]) {
		output, ok := decodeStreamNumber(usage["output_tokens"])
		if !ok {
			return errors.New("invalid Anthropic usage")
		}
		s.usage.Output = max(s.usage.Output, output)
		s.usage.Seen = true
	}
	return nil
}

// Stream converts Chat Completions chunks into Anthropic Messages events. The
// terminal events are written only after the upstream [DONE] marker so an
// interrupted upstream stream never looks complete to the client.
func (anthropicChat) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &chatToAnthropicStream{writer: anthropicStreamWriter{dst: dst, model: requestedModel}, toolBlocks: map[int]int{}}
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
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			if err := state.finish(); err != nil {
				return fail(err)
			}
			return state.writer.usage, nil
		}
		if err := state.chunk(data); err != nil {
			return fail(err)
		}
	}
	if state.finished {
		return state.writer.usage, nil
	}
	return fail(errors.New("Chat stream ended before [DONE]"))
}

type chatToAnthropicStream struct {
	writer        anthropicStreamWriter
	textOpen      bool
	textBlock     int
	thinkingOpen  bool
	thinkingBlock int
	toolBlocks    map[int]int
	toolOrder     []int
	stopReason    string
	inputSeen     bool
	outputSeen    bool
	metadata      providerMetadata
	finished      bool
}

func (s *chatToAnthropicStream) chunk(data []byte) error {
	fields, extra, err := rawObject(data, "id", "object", "created", "model", "choices", "usage")
	if err != nil {
		return fmt.Errorf("invalid Chat chunk: %w", err)
	}
	s.metadata.absorb(extra)
	if id := rawString(fields["id"]); id != "" {
		s.writer.id = id
	}
	var choices []json.RawMessage
	if json.Unmarshal(fields["choices"], &choices) != nil {
		return errors.New("Chat chunk choices must be an array")
	}
	if len(choices) > 1 {
		return errors.New("Chat chunk must contain one choice")
	}
	if nonNull(fields["usage"]) {
		if err := s.observeUsage(fields["usage"]); err != nil {
			return err
		}
	}
	if len(choices) == 0 {
		return nil
	}
	choice, choiceExtra, err := rawObject(choices[0], "index", "delta", "finish_reason")
	if err != nil {
		return fmt.Errorf("invalid Chat choice: %w", err)
	}
	s.metadata.absorb(choiceExtra)
	if index, err := parseIntField(choice["index"], "choice index"); err != nil || index != 0 {
		return errors.New("Chat response choice index must be zero")
	}
	if finish := rawString(choice["finish_reason"]); finish != "" {
		mapped, err := mapChatFinishReason(finish)
		if err != nil {
			return err
		}
		s.stopReason = mapped
	}
	if !nonNull(choice["delta"]) {
		return nil
	}
	return s.delta(choice["delta"])
}

func (s *chatToAnthropicStream) observeUsage(raw json.RawMessage) error {
	// Cache-hit and reasoning-token details ride the shared usage detail
	// fields; the Anthropic client sees them in the terminal message_delta.
	usage, extra, err := rawObject(raw, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens")
	if err != nil {
		return fmt.Errorf("invalid Chat usage: %w", err)
	}
	s.metadata.absorb(extra)
	if nonNull(usage["prompt_tokens"]) {
		prompt, ok := decodeStreamNumber(usage["prompt_tokens"])
		if !ok {
			return errors.New("invalid Chat usage")
		}
		s.writer.usage.Input = max(s.writer.usage.Input, prompt)
		s.writer.usage.Seen = true
		s.inputSeen = true
	}
	if nonNull(usage["completion_tokens"]) {
		completion, ok := decodeStreamNumber(usage["completion_tokens"])
		if !ok {
			return errors.New("invalid Chat usage")
		}
		s.writer.usage.Output = max(s.writer.usage.Output, completion)
		s.writer.usage.Seen = true
		s.outputSeen = true
	}
	if nonNull(usage["prompt_tokens_details"]) {
		details, _, err := rawObject(usage["prompt_tokens_details"], "cached_tokens")
		if err != nil {
			return fmt.Errorf("invalid Chat usage details: %w", err)
		}
		if cached, ok := decodeStreamNumber(details["cached_tokens"]); ok && cached >= 0 {
			s.writer.usage.CacheRead = max(s.writer.usage.CacheRead, cached)
		}
	}
	if nonNull(usage["completion_tokens_details"]) {
		details, _, err := rawObject(usage["completion_tokens_details"], "reasoning_tokens")
		if err != nil {
			return fmt.Errorf("invalid Chat usage details: %w", err)
		}
		if reasoning, ok := decodeStreamNumber(details["reasoning_tokens"]); ok && reasoning >= 0 {
			s.writer.usage.Reasoning = max(s.writer.usage.Reasoning, reasoning)
		}
	}
	if hit, ok := decodeStreamNumber(usage["prompt_cache_hit_tokens"]); ok && hit > 0 {
		s.writer.usage.CacheRead = max(s.writer.usage.CacheRead, hit)
	}
	return nil
}

func (s *chatToAnthropicStream) delta(raw json.RawMessage) error {
	// Reasoning_content deltas (DeepSeek reasoner) stream as thinking blocks;
	// the Chat wire has no signature to carry, so the thinking block carries
	// text only.
	delta, extra, err := rawObject(raw, "role", "content", "tool_calls", "refusal", "reasoning_content")
	if err != nil {
		return fmt.Errorf("invalid Chat delta: %w", err)
	}
	s.metadata.absorb(extra)
	if role := rawString(delta["role"]); role != "" && role != "assistant" {
		return errors.New("Chat response role must be assistant")
	}
	if nonNull(delta["refusal"]) {
		// A refusal is display text to the recipient, so it streams as an
		// ordinary text block.
		var refusal string
		if json.Unmarshal(delta["refusal"], &refusal) != nil {
			return errors.New("Chat refusal must be text")
		}
		if refusal != "" {
			if err := s.openText(); err != nil {
				return err
			}
			if err := s.emitTextDelta(refusal); err != nil {
				return err
			}
		}
	}
	if reasoning := rawString(delta["reasoning_content"]); reasoning != "" {
		if !s.thinkingOpen {
			if s.textOpen {
				if err := s.closeBlock(s.textBlock); err != nil {
					return err
				}
				s.textOpen = false
			}
			index, err := s.writer.openBlock(map[string]any{"type": "thinking", "thinking": ""})
			if err != nil {
				return err
			}
			s.thinkingBlock = index
			s.thinkingOpen = true
		}
		if err := s.writer.delta(s.thinkingBlock, map[string]any{"type": "thinking_delta", "thinking": reasoning}); err != nil {
			return err
		}
	}
	if nonNull(delta["content"]) {
		var text string
		if json.Unmarshal(delta["content"], &text) != nil {
			return errors.New("Chat content must be text")
		}
		if text != "" {
			if s.thinkingOpen {
				if err := s.closeBlock(s.thinkingBlock); err != nil {
					return err
				}
				s.thinkingOpen = false
			}
			if err := s.openText(); err != nil {
				return err
			}
			if err := s.emitTextDelta(text); err != nil {
				return err
			}
		}
	}
	if nonNull(delta["tool_calls"]) {
		var calls []json.RawMessage
		if json.Unmarshal(delta["tool_calls"], &calls) != nil {
			return errors.New("Chat tool_calls must be an array")
		}
		for _, callRaw := range calls {
			if err := s.toolCall(callRaw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *chatToAnthropicStream) toolCall(callRaw json.RawMessage) error {
	call, extra, err := rawObject(callRaw, "index", "id", "type", "function")
	if err != nil {
		return fmt.Errorf("invalid Chat tool call: %w", err)
	}
	s.metadata.absorb(extra)
	index, err := parseIntField(call["index"], "tool call index")
	if err != nil {
		return err
	}
	blockIndex, seen := s.toolBlocks[index]
	function, _, err := rawObject(call["function"], "name", "arguments")
	if err != nil {
		return fmt.Errorf("invalid Chat tool call: %w", err)
	}
	if !seen {
		id, kind, name := rawString(call["id"]), rawString(call["type"]), rawString(function["name"])
		if id == "" || kind != "function" || name == "" {
			return errors.New("only named function tool calls can be converted")
		}
		if s.textOpen {
			if err := s.closeBlock(s.textBlock); err != nil {
				return err
			}
			s.textOpen = false
		}
		if s.thinkingOpen {
			if err := s.closeBlock(s.thinkingBlock); err != nil {
				return err
			}
			s.thinkingOpen = false
		}
		blockIndex, err = s.writer.openBlock(map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}})
		if err != nil {
			return err
		}
		s.toolBlocks[index] = blockIndex
		s.toolOrder = append(s.toolOrder, blockIndex)
	}
	if arguments := rawString(function["arguments"]); arguments != "" {
		if err := s.emitToolDelta(blockIndex, arguments); err != nil {
			return err
		}
	}
	return nil
}

func (s *chatToAnthropicStream) openText() error {
	if s.textOpen {
		return nil
	}
	index, err := s.writer.openBlock(map[string]any{"type": "text", "text": ""})
	if err != nil {
		return err
	}
	s.textBlock = index
	s.textOpen = true
	return nil
}

func (s *chatToAnthropicStream) closeBlock(index int) error {
	return s.writer.closeBlock(index)
}

func (s *chatToAnthropicStream) emitTextDelta(text string) error {
	return s.writer.delta(s.textBlock, map[string]any{"type": "text_delta", "text": text})
}

func (s *chatToAnthropicStream) emitToolDelta(blockIndex int, arguments string) error {
	return s.writer.delta(blockIndex, map[string]any{"type": "input_json_delta", "partial_json": arguments})
}

func (s *chatToAnthropicStream) finish() error {
	if s.finished {
		return nil
	}
	if !s.writer.started {
		return errors.New("Chat stream ended without content")
	}
	if s.stopReason == "" {
		return errors.New("Chat stream ended without a finish reason")
	}
	if !s.inputSeen || !s.outputSeen {
		return errors.New("Chat stream ended without complete provider usage")
	}
	if s.textOpen {
		if err := s.closeBlock(s.textBlock); err != nil {
			return err
		}
		s.textOpen = false
	}
	if s.thinkingOpen {
		if err := s.closeBlock(s.thinkingBlock); err != nil {
			return err
		}
		s.thinkingOpen = false
	}
	for _, blockIndex := range s.toolOrder {
		if err := s.closeBlock(blockIndex); err != nil {
			return err
		}
	}
	// The Messages wire has no reasoning-token slot; the detail rides
	// provider_metadata on message_stop.
	if s.writer.usage.Reasoning > 0 {
		if encoded, err := json.Marshal(s.writer.usage.Reasoning); err == nil {
			s.metadata.set("reasoning_tokens", json.RawMessage(encoded))
		}
	}
	if err := s.writer.finish(s.stopReason, "openai", s.metadata.fields); err != nil {
		return err
	}
	s.finished = true
	return nil
}

func firstKey(values map[string]json.RawMessage) string {
	for key := range values {
		return key
	}
	return ""
}
