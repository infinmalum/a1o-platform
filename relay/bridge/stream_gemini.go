package bridge

import (
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
	if len(extra) > 0 {
		return nil, fmt.Errorf("Gemini content field %q cannot be converted", firstKey(extra))
	}
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
