package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Stream converts Chat Completions chunks into Responses events. The terminal
// response.completed carries the accumulated output items and usage; it is
// written only once the stream ends (on [DONE] or EOF), because Chat upstreams
// may report usage after the finish-reason chunk. Reasoning deltas map onto
// reasoning summary items, and unmapped provider fields ride the terminal
// event's provider_metadata.
func (responsesChat) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &chatToResponsesStream{emitter: *newResponsesStreamEmitter(dst, requestedModel)}
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
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			break
		}
		if err := state.chunk(data); err != nil {
			return fail(err)
		}
	}
	if !state.finishSeen {
		return fail(errors.New("Chat stream ended before a finish reason"))
	}
	status, reason, err := responsesStreamStatus(state.terminalReason != "length" && state.terminalReason != "content_filter", state.terminalReason)
	if err != nil {
		return fail(err)
	}
	if err := state.finish(status, reason); err != nil {
		return fail(err)
	}
	return state.emitter.usage, nil
}

// chatToResponsesStream accumulates Chat chunks into the Responses event
// sequence. toolCount starts at -1 so openToolItem receives zero-based indexes
// on the first tool call.
type chatToResponsesStream struct {
	emitter        responsesStreamEmitter
	finishSeen     bool
	terminalReason string
}

func (s *chatToResponsesStream) chunk(data []byte) error {
	fields, extras, err := rawObject(data, "id", "object", "created", "model", "choices", "usage")
	if err != nil {
		return fmt.Errorf("invalid Chat chunk: %w", err)
	}
	s.emitter.metadata.absorb(extras)
	if id := rawString(fields["id"]); id != "" && s.emitter.responseID == "" {
		s.emitter.responseID = id
	}
	if created := responseCreatedAt(fields["created"]); created > 0 {
		s.emitter.createdAt = created
	}
	if nonNull(fields["usage"]) {
		details, err := chatUsageDetails(fields["usage"])
		if err != nil {
			return err
		}
		s.emitter.usage.Input = max(s.emitter.usage.Input, details.Input)
		s.emitter.usage.Output = max(s.emitter.usage.Output, details.Output)
		s.emitter.usage.CacheRead = max(s.emitter.usage.CacheRead, details.CacheRead)
		s.emitter.usage.Reasoning = max(s.emitter.usage.Reasoning, details.Reasoning)
		s.emitter.usage.Seen = true
	}
	var choices []json.RawMessage
	if json.Unmarshal(fields["choices"], &choices) != nil {
		return errors.New("Chat chunk choices must be an array")
	}
	if len(choices) > 1 {
		return errors.New("Chat chunks must carry one choice")
	}
	if len(choices) == 0 {
		return nil
	}
	choice, choiceExtras, err := rawObject(choices[0], "index", "delta", "finish_reason")
	if err != nil {
		return err
	}
	s.emitter.metadata.absorb(choiceExtras)
	if finish := rawString(choice["finish_reason"]); finish != "" {
		switch finish {
		case "stop", "tool_calls", "length", "content_filter":
			s.finishSeen, s.terminalReason = true, finish
		default:
			return fmt.Errorf("Chat finish_reason %q cannot be converted", finish)
		}
	}
	deltaFields, deltaExtras, err := rawObject(choice["delta"], "role", "content", "refusal", "tool_calls", "reasoning_content")
	if err != nil {
		return err
	}
	s.emitter.metadata.absorb(deltaExtras)
	if err := s.emitter.ensureCreated(); err != nil {
		return err
	}
	if reasoning := rawString(deltaFields["reasoning_content"]); reasoning != "" {
		if err := s.emitter.addReasoningText(reasoning); err != nil {
			return err
		}
	}
	content, err := chatDeltaContent(deltaFields["content"])
	if err != nil {
		return err
	}
	if content != "" {
		if err := s.emitter.addText(content); err != nil {
			return err
		}
	}
	if refusal := rawString(deltaFields["refusal"]); refusal != "" {
		if err := s.emitter.addRefusal(refusal); err != nil {
			return err
		}
	}
	if len(deltaFields["tool_calls"]) != 0 {
		if err := s.toolDeltas(deltaFields["tool_calls"]); err != nil {
			return err
		}
	}
	return nil
}

// chatDeltaContent reads a delta content field: a string, or the array-of-parts
// shape some providers stream. Non-text parts fail instead of vanishing.
func chatDeltaContent(raw json.RawMessage) (string, error) {
	if !nonNull(raw) {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return "", errors.New("Chat delta content must be a string or a part array")
	}
	var result string
	for _, partRaw := range parts {
		part, err := object(partRaw, "type", "text")
		if err != nil {
			return "", err
		}
		if rawString(part["type"]) != "text" {
			return "", fmt.Errorf("Chat delta part %q cannot be converted", rawString(part["type"]))
		}
		result += rawString(part["text"])
	}
	return result, nil
}

// toolDeltas applies the tool_calls array of one chunk. The first delta for a
// Chat tool index opens the function_call item; later deltas append arguments.
func (s *chatToResponsesStream) toolDeltas(raw json.RawMessage) error {
	var calls []json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return errors.New("Chat tool_calls must be an array")
	}
	for _, callRaw := range calls {
		call, err := object(callRaw, "index", "id", "type", "function")
		if err != nil {
			return err
		}
		index, err := parseIntField(call["index"], "tool call index")
		if err != nil {
			return err
		}
		if s.emitter.toolItemByIndex(index) == nil {
			callID := rawString(call["id"])
			function, err := object(call["function"], "name", "arguments")
			if err != nil {
				return err
			}
			if _, err := s.emitter.openToolItem(callID, rawString(function["name"]), index); err != nil {
				return err
			}
		}
		function, err := object(call["function"], "name", "arguments")
		if err != nil {
			return err
		}
		if err := s.emitter.addToolArguments(index, rawString(function["arguments"])); err != nil {
			return err
		}
	}
	return nil
}

// finish closes every open item and writes the terminal event. The terminal
// reason observed on the finish-reason chunk maps onto the Responses status
// only here, because Chat upstreams may still stream usage afterwards.
func (s *chatToResponsesStream) finish(status, reason string) error {
	if err := s.emitter.closeItems(); err != nil {
		return err
	}
	return s.emitter.terminal(status, reason)
}
