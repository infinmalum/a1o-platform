package bridge

import (
	"errors"
	"io"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// Stream converts Gemini GenerateContent events into Responses events. Thought
// parts carry the model's visible reasoning onto reasoning items so the next
// turn replays them, with the thought signature riding the same item; the
// function-call signature rides the function_call item. Gemini has no terminal
// event, so the stream ends when the upstream connection closes and a missing
// finish reason or usage report fails the conversion.
func (responsesGemini) Stream(dst StreamWriter, src io.Reader, requestedModel string) (native.Usage, error) {
	state := &geminiToResponsesStream{emitter: *newResponsesStreamEmitter(dst, requestedModel)}
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
	}
	if err := state.complete(); err != nil {
		return fail(err)
	}
	return state.emitter.usage, nil
}

type geminiToResponsesStream struct {
	emitter      responsesStreamEmitter
	events       int
	finishReason string
	toolCount    int
}

func (s *geminiToResponsesStream) event(data []byte) error {
	parsed, err := parseGeminiStreamEvent(data)
	if err != nil {
		return err
	}
	s.events++
	if s.emitter.responseID == "" && parsed.responseID != "" {
		s.emitter.responseID = parsed.responseID
	}
	if parsed.hasUsage {
		s.emitter.usage.Input = max(s.emitter.usage.Input, parsed.prompt)
		s.emitter.usage.Output = max(s.emitter.usage.Output, parsed.output)
		s.emitter.usage.Seen = true
	}
	if parsed.finishReason != "" {
		s.finishReason = parsed.finishReason
	}
	for _, part := range parsed.parts {
		switch part.kind {
		case geminiPartText:
			if part.signature != "" {
				if err := s.emitter.setReasoningSignature(part.signature); err != nil {
					return err
				}
			}
			if err := s.emitter.addText(part.text); err != nil {
				return err
			}
		case geminiPartThought:
			if err := s.emitter.addReasoningText(part.text); err != nil {
				return err
			}
			if err := s.emitter.setReasoningSignature(part.signature); err != nil {
				return err
			}
		case geminiPartSignature:
			if err := s.emitter.setReasoningSignature(part.signature); err != nil {
				return err
			}
		case geminiPartFunctionCall:
			callID := part.callID
			if callID == "" {
				generated, err := newStreamID("call_")
				if err != nil {
					return err
				}
				callID = generated
			}
			if _, err := s.emitter.openToolItem(callID, part.name, s.toolCount); err != nil {
				return err
			}
			if err := s.emitter.addToolArguments(s.toolCount, string(part.args)); err != nil {
				return err
			}
			if err := s.emitter.setToolSignature(s.toolCount, part.signature); err != nil {
				return err
			}
			s.toolCount++
		}
	}
	return nil
}

func (s *geminiToResponsesStream) complete() error {
	if s.events == 0 {
		return errors.New("Gemini stream ended without events")
	}
	if s.finishReason == "" {
		return errors.New("Gemini stream ended without a finish reason")
	}
	if !s.emitter.usage.Seen {
		return errors.New("Gemini stream ended without provider usage")
	}
	completed := s.finishReason == "STOP"
	if !completed && s.finishReason != "MAX_TOKENS" {
		return errors.New("Gemini finish reason " + s.finishReason + " cannot be converted")
	}
	if err := s.emitter.closeItems(); err != nil {
		return err
	}
	status, reason, err := responsesStreamStatus(completed, "max_output_tokens")
	if err != nil {
		return err
	}
	return s.emitter.terminal(status, reason)
}
