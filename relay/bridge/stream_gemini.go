package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/one-gateway/relay/native"
)

// geminiStreamEvent holds the converted view of one streaming
// GenerateContentResponse. Unrepresentable parts and unknown candidate fields
// are rejected by the parser so neither stream direction can drop them.
type geminiStreamEvent struct {
	parts        []geminiPart
	finishReason string
	responseID   string
	metadata     map[string]json.RawMessage
	prompt       int64
	output       int64
	cached       int64
	hasUsage     bool
}

func parseGeminiStreamEvent(data []byte) (*geminiStreamEvent, error) {
	fields, extra, err := rawObject(data, geminiResponseFields...)
	if err != nil {
		return nil, fmt.Errorf("invalid Gemini event: %w", err)
	}
	event := &geminiStreamEvent{metadata: map[string]json.RawMessage{}}
	for name, value := range extra {
		event.metadata[name] = value
	}
	event.responseID = rawString(fields["responseId"])
	for _, name := range []string{"promptFeedback", "modelVersion", "modelStatus"} {
		if len(fields[name]) != 0 {
			event.metadata[name] = fields[name]
		}
	}
	if len(fields["usageMetadata"]) != 0 {
		prompt, output, cached, err := geminiUsage(fields["usageMetadata"], false)
		if err != nil {
			return nil, err
		}
		event.prompt, event.output, event.cached = prompt, output, cached
		event.hasUsage = true
		event.metadata["usageMetadata"] = fields["usageMetadata"]
	}
	if len(fields["candidates"]) == 0 || string(fields["candidates"]) == "null" {
		return event, nil
	}
	var rawCandidates []json.RawMessage
	if json.Unmarshal(fields["candidates"], &rawCandidates) != nil || len(rawCandidates) > 1 {
		return nil, errors.New("Gemini event must contain at most one candidate")
	}
	if len(rawCandidates) == 0 {
		return event, nil
	}
	candidate, extra, err := rawObject(rawCandidates[0], geminiCandidateFields...)
	if err != nil {
		return nil, fmt.Errorf("invalid Gemini candidate: %w", err)
	}
	for key, value := range extra {
		event.metadata[key] = value
	}
	for _, name := range []string{"safetyRatings", "citationMetadata", "tokenCount", "groundingAttributions", "groundingMetadata", "avgLogprobs", "logprobsResult", "urlContextMetadata", "finishMessage"} {
		if len(candidate[name]) != 0 {
			event.metadata[name] = candidate[name]
		}
	}
	if err := geminiCandidateIndex(candidate["index"]); err != nil {
		return nil, err
	}
	if reason := rawString(candidate["finishReason"]); reason != "" {
		if _, err := mapGeminiFinishReason(reason, false); err != nil {
			return nil, err
		}
		event.finishReason = reason
	}
	if len(candidate["content"]) == 0 || string(candidate["content"]) == "null" {
		return event, nil
	}
	content, extra, err := rawObject(candidate["content"], "role", "parts")
	if err != nil {
		return nil, fmt.Errorf("invalid Gemini content: %w", err)
	}
	ignoreExtras(extra)
	if role := rawString(content["role"]); role != "" && role != "model" {
		return nil, errors.New("Gemini response content role must be model")
	}
	var rawParts []json.RawMessage
	if json.Unmarshal(content["parts"], &rawParts) != nil {
		return nil, errors.New("Gemini content parts must be an array")
	}
	for _, rawPart := range rawParts {
		part, err := parseGeminiResponsePart(rawPart)
		if err != nil {
			return nil, err
		}
		event.parts = append(event.parts, part)
	}
	return event, nil
}

// Stream converts Chat Completions chunks into Gemini GenerateContent events.
// Text and reasoning deltas emit immediately; function call deltas accumulate
// because Gemini carries complete functionCall parts. The terminal events are
// written only after the upstream [DONE] marker so an interrupted upstream
// stream never looks complete to the client.
func (geminiChat) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &chatToGeminiStream{dst: dst, model: requestedModel, tools: map[int]*chatToolAccumulator{}}
	reader := newSSEReader(src)
	fail := func(err error) (native.Usage, error) {
		_ = writeGeminiStreamError(dst, err.Error())
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
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			if err := state.finish(); err != nil {
				return fail(err)
			}
			return state.usage, nil
		}
		if err := state.chunk(data); err != nil {
			return fail(err)
		}
	}
	if state.finished {
		return state.usage, nil
	}
	return fail(errors.New("Chat stream ended before [DONE]"))
}

type chatToolAccumulator struct {
	id        string
	name      string
	arguments string
}

type chatToGeminiStream struct {
	dst        StreamWriter
	model      string
	id         string
	stopReason string
	usage      native.Usage
	cached     int64
	tools      map[int]*chatToolAccumulator
	toolList   []int
	events     int
	finished   bool
}

func (s *chatToGeminiStream) chunk(data []byte) error {
	fields, extra, err := rawObject(data, "id", "object", "created", "model", "choices", "usage")
	if err != nil {
		return fmt.Errorf("invalid Chat chunk: %w", err)
	}
	ignoreExtras(extra)
	if id := rawString(fields["id"]); id != "" {
		s.id = id
	}
	if nonNull(fields["usage"]) {
		if err := s.observeUsage(fields["usage"]); err != nil {
			return err
		}
	}
	var choices []json.RawMessage
	if json.Unmarshal(fields["choices"], &choices) != nil {
		return errors.New("Chat chunk choices must be an array")
	}
	if len(choices) > 1 {
		return errors.New("Chat chunk must contain one choice")
	}
	if len(choices) == 0 {
		return nil
	}
	choice, choiceExtra, err := rawObject(choices[0], "index", "delta", "finish_reason")
	if err != nil {
		return fmt.Errorf("invalid Chat choice: %w", err)
	}
	ignoreExtras(choiceExtra)
	if index, err := parseIntField(choice["index"], "choice index"); err != nil || index != 0 {
		return errors.New("Chat response choice index must be zero")
	}
	if finish := rawString(choice["finish_reason"]); finish != "" {
		mapped, err := chatFinishReasonToGemini(finish)
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

func (s *chatToGeminiStream) observeUsage(raw json.RawMessage) error {
	// DeepSeek-compatible providers add dedicated cache-hit fields; they map
	// onto cachedContentTokenCount in the terminal event.
	usage, extra, err := rawObject(raw, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens")
	if err != nil {
		return fmt.Errorf("invalid Chat usage: %w", err)
	}
	ignoreExtras(extra)
	var fields struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		PromptDetails    struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens"`
	}
	if json.Unmarshal(raw, &fields) != nil || fields.PromptTokens < 0 || fields.CompletionTokens < 0 {
		return errors.New("invalid Chat usage")
	}
	if nonNull(usage["prompt_tokens"]) {
		s.usage.Input = max(s.usage.Input, fields.PromptTokens)
		s.usage.Seen = true
	}
	if nonNull(usage["completion_tokens"]) {
		s.usage.Output = max(s.usage.Output, fields.CompletionTokens)
		s.usage.Seen = true
	}
	s.cached = max(s.cached, fields.PromptDetails.CachedTokens, fields.PromptCacheHitTokens)
	return nil
}

func (s *chatToGeminiStream) delta(raw json.RawMessage) error {
	delta, extra, err := rawObject(raw, "role", "content", "tool_calls", "refusal", "reasoning_content")
	if err != nil {
		return fmt.Errorf("invalid Chat delta: %w", err)
	}
	ignoreExtras(extra)
	if role := rawString(delta["role"]); role != "" && role != "assistant" {
		return errors.New("Chat response role must be assistant")
	}
	if nonNull(delta["refusal"]) {
		return errors.New("Chat refusal cannot be converted")
	}
	if nonNull(delta["content"]) {
		var text string
		if json.Unmarshal(delta["content"], &text) != nil {
			return errors.New("Chat content must be text")
		}
		if text != "" {
			if err := s.emitParts([]map[string]any{{"text": text}}); err != nil {
				return err
			}
		}
	}
	if reasoning := rawString(delta["reasoning_content"]); reasoning != "" {
		if err := s.emitParts([]map[string]any{{"text": reasoning, "thought": true}}); err != nil {
			return err
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

func (s *chatToGeminiStream) toolCall(callRaw json.RawMessage) error {
	call, extra, err := rawObject(callRaw, "index", "id", "type", "function")
	if err != nil {
		return fmt.Errorf("invalid Chat tool call: %w", err)
	}
	ignoreExtras(extra)
	index, err := parseIntField(call["index"], "tool call index")
	if err != nil {
		return err
	}
	accumulator, seen := s.tools[index]
	function, _, err := rawObject(call["function"], "name", "arguments")
	if err != nil {
		return fmt.Errorf("invalid Chat tool call: %w", err)
	}
	if !seen {
		id, kind, name := rawString(call["id"]), rawString(call["type"]), rawString(function["name"])
		if id == "" || kind != "function" || name == "" {
			return errors.New("only named function tool calls can be converted")
		}
		accumulator = &chatToolAccumulator{id: id, name: name}
		s.tools[index] = accumulator
		s.toolList = append(s.toolList, index)
	}
	accumulator.arguments += rawString(function["arguments"])
	return nil
}

// emitParts writes one GenerateContent event carrying the given parts.
func (s *chatToGeminiStream) emitParts(parts []map[string]any) error {
	s.events++
	candidate := map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}}
	event := map[string]any{"candidates": []any{candidate}, "modelVersion": s.model}
	if s.id != "" {
		event["responseId"] = s.id
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return writeStreamEvent(s.dst, payload)
}

func (s *chatToGeminiStream) finish() error {
	if s.finished {
		return nil
	}
	if s.events == 0 {
		return errors.New("Chat stream ended without content")
	}
	if s.stopReason == "" {
		return errors.New("Chat stream ended without a finish reason")
	}
	if len(s.toolList) > 0 {
		var parts []map[string]any
		for _, index := range s.toolList {
			accumulator := s.tools[index]
			args, err := chatToolArgsObject(accumulator.arguments)
			if err != nil {
				return err
			}
			functionCall := map[string]any{"name": accumulator.name, "args": args}
			if accumulator.id != "" {
				functionCall["id"] = accumulator.id
			}
			parts = append(parts, map[string]any{"functionCall": functionCall})
		}
		if err := s.emitParts(parts); err != nil {
			return err
		}
	}
	candidate := map[string]any{"index": 0, "finishReason": s.stopReason}
	event := map[string]any{"candidates": []any{candidate}, "modelVersion": s.model}
	if s.id != "" {
		event["responseId"] = s.id
	}
	if s.usage.Seen {
		usageMetadata := map[string]any{"promptTokenCount": s.usage.Input, "candidatesTokenCount": s.usage.Output, "totalTokenCount": s.usage.Input + s.usage.Output}
		if s.cached > 0 {
			usageMetadata["cachedContentTokenCount"] = s.cached
		}
		event["usageMetadata"] = usageMetadata
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := writeStreamEvent(s.dst, payload); err != nil {
		return err
	}
	s.usage.Complete = true
	s.finished = true
	return nil
}

// writeGeminiStreamError reports a mid-stream conversion failure to a Gemini
// client in the GenerateContent error envelope, so the stream never ends by
// silent truncation.
func writeGeminiStreamError(dst StreamWriter, message string) error {
	payload, err := json.Marshal(map[string]any{"error": map[string]any{"code": 500, "message": message, "status": "INTERNAL"}})
	if err != nil {
		return err
	}
	return writeStreamEvent(dst, payload)
}

// Stream converts Gemini GenerateContent events into Chat Completions chunks.
// Function calls become tool call deltas; thought parts have no Chat
// representation and fail the stream. The stream ends when the upstream
// connection closes; Gemini has no terminal event, so a missing finish reason
// fails the conversion.
func (chatGemini) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &geminiToChatStream{dst: dst, model: requestedModel, created: streamTimestamp()}
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
	if err := state.complete(); err != nil {
		_ = writeStreamError(dst, err.Error())
		return state.usage, err
	}
	return state.usage, nil
}

type geminiToChatStream struct {
	dst          StreamWriter
	model        string
	created      int64
	id           string
	finishReason string
	usage        native.Usage
	metadata     map[string]json.RawMessage
	events       int
	toolCount    int
}

func (s *geminiToChatStream) event(data []byte) error {
	parsed, err := parseGeminiStreamEvent(data)
	if err != nil {
		return err
	}
	s.events++
	if s.id == "" && parsed.responseID != "" {
		s.id = parsed.responseID
	}
	if parsed.hasUsage {
		s.usage.Input = max(s.usage.Input, parsed.prompt)
		s.usage.Output = max(s.usage.Output, parsed.output)
		s.usage.Seen = true
	}
	for key, value := range parsed.metadata {
		if s.metadata == nil {
			s.metadata = make(map[string]json.RawMessage)
		}
		s.metadata[key] = value
	}
	if parsed.finishReason != "" {
		s.finishReason = parsed.finishReason
	}
	var text string
	for _, part := range parsed.parts {
		switch part.kind {
		case geminiPartText:
			text += part.text
		case geminiPartFunctionCall:
			if err := s.emitText(text); err != nil {
				return err
			}
			text = ""
			if err := s.emitToolCall(part); err != nil {
				return err
			}
		default:
			return errors.New("Gemini thought parts cannot be represented in Chat")
		}
	}
	return s.emitText(text)
}

func (s *geminiToChatStream) emitText(text string) error {
	if text == "" {
		return nil
	}
	if s.id == "" {
		generated, err := newStreamID(chatCompletionIDPrefix)
		if err != nil {
			return err
		}
		s.id = generated
	}
	return emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{"role": "assistant", "content": text}, "", nil)
}

func (s *geminiToChatStream) emitToolCall(part geminiPart) error {
	if s.id == "" {
		generated, err := newStreamID(chatCompletionIDPrefix)
		if err != nil {
			return err
		}
		s.id = generated
	}
	callID := part.callID
	if callID == "" {
		generated, err := newStreamID(chatToolCallIDPrefix)
		if err != nil {
			return err
		}
		callID = generated
	}
	toolIndex := s.toolCount
	s.toolCount++
	delta := map[string]any{"tool_calls": []any{map[string]any{
		"index": toolIndex, "id": callID, "type": "function",
		"function": map[string]string{"name": part.name, "arguments": string(part.args)},
	}}}
	return emitChatChunk(s.dst, s.id, s.created, s.model, delta, "", nil)
}

func (s *geminiToChatStream) complete() error {
	if s.events == 0 {
		return errors.New("Gemini stream ended without events")
	}
	if s.finishReason == "" {
		return errors.New("Gemini stream ended without a finish reason")
	}
	mapped, err := mapGeminiFinishReason(s.finishReason, s.toolCount > 0)
	if err != nil {
		return err
	}
	if s.id == "" {
		generated, err := newStreamID(chatCompletionIDPrefix)
		if err != nil {
			return err
		}
		s.id = generated
	}
	extra := map[string]any{}
	if s.usage.Seen {
		usage := map[string]any{"prompt_tokens": s.usage.Input, "completion_tokens": s.usage.Output, "total_tokens": s.usage.Input + s.usage.Output}
		if s.metadata != nil {
			if cached, ok := s.metadata["usageMetadata"]; ok {
				var fields struct {
					Cached int64 `json:"cachedContentTokenCount"`
				}
				if json.Unmarshal(cached, &fields) == nil && fields.Cached > 0 {
					usage["prompt_tokens_details"] = map[string]int64{"cached_tokens": fields.Cached}
				}
			}
		}
		extra["usage"] = usage
	}
	if len(s.metadata) > 0 {
		extra["provider_metadata"] = map[string]any{"gemini": s.metadata}
	}
	if err := emitChatChunk(s.dst, s.id, s.created, s.model, map[string]any{}, mapped, extra); err != nil {
		return err
	}
	s.usage.Complete = true
	return writeStreamDone(s.dst)
}

// Stream converts Gemini GenerateContent events into Anthropic Messages
// events. Function calls become tool_use blocks and thought parts become
// thinking deltas carrying the thought signature.
func (messagesGemini) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &geminiToAnthropicStream{writer: anthropicStreamWriter{dst: dst, model: requestedModel}}
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
	}
	if err := state.complete(); err != nil {
		return fail(err)
	}
	return state.writer.usage, nil
}

type geminiToAnthropicStream struct {
	writer        anthropicStreamWriter
	textOpen      bool
	thinkingOpen  bool
	textBlock     int
	thinkingBlock int
	finishReason  string
	metadata      map[string]json.RawMessage
	events        int
	toolCalls     int
}

func (s *geminiToAnthropicStream) event(data []byte) error {
	parsed, err := parseGeminiStreamEvent(data)
	if err != nil {
		return err
	}
	s.events++
	if s.writer.id == "" && parsed.responseID != "" {
		s.writer.id = parsed.responseID
	}
	if parsed.hasUsage {
		s.writer.usage.Input = max(s.writer.usage.Input, parsed.prompt)
		s.writer.usage.Output = max(s.writer.usage.Output, parsed.output)
		s.writer.usage.Seen = true
	}
	for key, value := range parsed.metadata {
		if s.metadata == nil {
			s.metadata = make(map[string]json.RawMessage)
		}
		s.metadata[key] = value
	}
	if parsed.finishReason != "" {
		s.finishReason = parsed.finishReason
	}
	for _, part := range parsed.parts {
		switch part.kind {
		case geminiPartText:
			if err := s.closeThinking(); err != nil {
				return err
			}
			if err := s.openText(); err != nil {
				return err
			}
			if err := s.emitTextDelta(part.text); err != nil {
				return err
			}
		case geminiPartThought:
			if s.textOpen {
				if err := s.closeText(); err != nil {
					return err
				}
			}
			if !s.thinkingOpen {
				if err := s.openThinking(); err != nil {
					return err
				}
			}
			if part.text != "" {
				if err := s.emitThinkingDelta(part.text); err != nil {
					return err
				}
			}
			if part.signature != "" {
				if err := s.emitSignatureDelta(part.signature); err != nil {
					return err
				}
				s.thinkingOpen = false
			}
		case geminiPartSignature:
			if !s.thinkingOpen {
				return errors.New("Gemini thought signature has no open thinking block")
			}
			if err := s.emitSignatureDelta(part.signature); err != nil {
				return err
			}
			s.thinkingOpen = false
		case geminiPartFunctionCall:
			if s.textOpen {
				if err := s.closeText(); err != nil {
					return err
				}
			}
			if err := s.closeThinking(); err != nil {
				return err
			}
			if err := s.emitToolUse(part); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *geminiToAnthropicStream) openText() error {
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

func (s *geminiToAnthropicStream) openThinking() error {
	if s.thinkingOpen {
		return nil
	}
	index, err := s.writer.openBlock(map[string]any{"type": "thinking", "thinking": ""})
	if err != nil {
		return err
	}
	s.thinkingBlock = index
	s.thinkingOpen = true
	return nil
}

func (s *geminiToAnthropicStream) closeText() error {
	if !s.textOpen {
		return nil
	}
	if err := s.writer.closeBlock(s.textBlock); err != nil {
		return err
	}
	s.textOpen = false
	return nil
}

func (s *geminiToAnthropicStream) closeThinking() error {
	if !s.thinkingOpen {
		return nil
	}
	if err := s.writer.closeBlock(s.thinkingBlock); err != nil {
		return err
	}
	s.thinkingOpen = false
	return nil
}

func (s *geminiToAnthropicStream) emitTextDelta(text string) error {
	return s.writer.delta(s.textBlock, map[string]any{"type": "text_delta", "text": text})
}

func (s *geminiToAnthropicStream) emitThinkingDelta(text string) error {
	return s.writer.delta(s.thinkingBlock, map[string]any{"type": "thinking_delta", "thinking": text})
}

func (s *geminiToAnthropicStream) emitSignatureDelta(signature string) error {
	return s.writer.delta(s.thinkingBlock, map[string]any{"type": "signature_delta", "signature": signature})
}

func (s *geminiToAnthropicStream) emitToolUse(part geminiPart) error {
	blockID := part.callID
	if blockID == "" {
		generated, err := newStreamID(anthropicToolUseIDPrefix)
		if err != nil {
			return err
		}
		blockID = generated
	}
	index, err := s.writer.openBlock(map[string]any{"type": "tool_use", "id": blockID, "name": part.name, "input": map[string]any{}})
	if err != nil {
		return err
	}
	if err := s.writer.delta(index, map[string]any{"type": "input_json_delta", "partial_json": string(part.args)}); err != nil {
		return err
	}
	if err := s.writer.closeBlock(index); err != nil {
		return err
	}
	s.toolCalls++
	return nil
}

func (s *geminiToAnthropicStream) complete() error {
	if s.events == 0 {
		return errors.New("Gemini stream ended without events")
	}
	if s.finishReason == "" {
		return errors.New("Gemini stream ended without a finish reason")
	}
	if !s.writer.usage.Seen {
		return errors.New("Gemini stream ended without provider usage")
	}
	mapped, err := mapGeminiStopReason(s.finishReason, s.toolCalls > 0)
	if err != nil {
		return err
	}
	if err := s.writer.ensureStart(); err != nil {
		return err
	}
	if err := s.closeThinking(); err != nil {
		return err
	}
	if err := s.closeText(); err != nil {
		return err
	}
	if err := s.writer.finish(mapped, "gemini", s.metadata); err != nil {
		return err
	}
	return nil
}
