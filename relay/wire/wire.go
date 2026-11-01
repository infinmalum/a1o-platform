package wire

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/relay/native"
)

// The client-wire writers shared by the provider adapters: the adapters
// accumulate their private upstream into answer text and a usage report, and
// render it onto whichever wire the client spoke.

// Usage is the token report carried into every rendered response. Cached and
// Reasoning carry the provider's token breakdown for the wires that render
// details; they stay zero for vendors that report totals only.
type Usage struct {
	Input     int64
	Output    int64
	Cached    int64
	Reasoning int64
}

// Session renders one streaming response onto the client wire. Delta emits
// the protocol's increment, opening the envelope on first use; Complete
// writes the terminal frame and the usage report.
type Session struct {
	w        http.ResponseWriter
	inbound  native.Protocol
	id       string
	model    string
	started  bool
	sequence int
	text     strings.Builder
	complete bool
}

// NewSession prepares a streaming response. Callers must have written the
// event-stream headers through common.SetEventStreamHeaders.
func NewSession(w http.ResponseWriter, inbound native.Protocol, id, model string) *Session {
	common.SetEventStreamHeaders(w)
	return &Session{w: w, inbound: inbound, id: id, model: model}
}

// Delta streams one text increment.
func (s *Session) Delta(text string) error {
	if s.complete {
		return errors.New("wire session received a delta after completion")
	}
	if text == "" {
		return nil
	}
	if !s.started {
		s.started = true
		if err := s.open(); err != nil {
			return err
		}
	}
	s.text.WriteString(text)
	switch s.inbound {
	case native.Anthropic:
		return s.anthropicEvent("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text},
		})
	case native.OpenAIResponses:
		return s.frame(map[string]any{
			"type": "response.output_text.delta", "sequence_number": s.seq(),
			"item_id": "msg_" + s.id, "output_index": 0, "content_index": 0, "delta": text,
		})
	default:
		return s.frame(map[string]any{
			"id": s.id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}},
		})
	}
}

// Complete closes the streaming envelope with the usage report. The exact
// flag is unused on these wires (every adapter text model ends cleanly) but
// keeps the writer signature honest about interrupted streams being the
// caller's concern.
func (s *Session) Complete(usage Usage) error {
	if s.complete {
		return nil
	}
	s.complete = true
	if !s.started {
		s.started = true
		if err := s.open(); err != nil {
			return err
		}
	}
	switch s.inbound {
	case native.Anthropic:
		if err := s.anthropicEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}); err != nil {
			return err
		}
		if err := s.anthropicEvent("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": anthropicWireUsage(usage),
		}); err != nil {
			return err
		}
		return s.anthropicEvent("message_stop", map[string]any{"type": "message_stop"})
	case native.OpenAIResponses:
		text := s.text.String()
		if err := s.frame(map[string]any{"type": "response.output_text.done", "sequence_number": s.seq(), "item_id": "msg_" + s.id, "output_index": 0, "content_index": 0, "text": text}); err != nil {
			return err
		}
		if err := s.frame(map[string]any{"type": "response.content_part.done", "sequence_number": s.seq(), "item_id": "msg_" + s.id, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}); err != nil {
			return err
		}
		if err := s.frame(map[string]any{
			"type": "response.output_item.done", "sequence_number": s.seq(), "output_index": 0,
			"item": messageItem("msg_"+s.id, text),
		}); err != nil {
			return err
		}
		return s.frame(map[string]any{
			"type": "response.completed", "sequence_number": s.seq(),
			"response": map[string]any{"id": s.id, "object": "response", "status": "completed", "model": s.model, "output": []any{messageItem("msg_"+s.id, text)}, "usage": responsesUsage(usage)},
		})
	default:
		if err := s.frame(map[string]any{
			"id": s.id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		}); err != nil {
			return err
		}
		if err := s.frame(map[string]any{"id": s.id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": s.model, "choices": []any{}, "usage": map[string]int64{"prompt_tokens": usage.Input, "completion_tokens": usage.Output, "total_tokens": usage.Input + usage.Output}}); err != nil {
			return err
		}
		return s.writeFrame([]byte("data: [DONE]\n\n"))
	}
}

func (s *Session) seq() int {
	s.sequence++
	return s.sequence
}

func (s *Session) open() error {
	switch s.inbound {
	case native.Anthropic:
		if err := s.anthropicEvent("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": s.id, "type": "message", "role": "assistant", "model": s.model,
				"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]int64{"input_tokens": 0, "output_tokens": 0},
			},
		}); err != nil {
			return err
		}
		return s.anthropicEvent("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
	case native.OpenAIResponses:
		if err := s.frame(map[string]any{
			"type": "response.created", "sequence_number": s.seq(),
			"response": map[string]any{"id": s.id, "object": "response", "status": "in_progress", "model": s.model, "output": []any{}},
		}); err != nil {
			return err
		}
		if err := s.frame(map[string]any{
			"type": "response.output_item.added", "sequence_number": s.seq(), "output_index": 0,
			"item": map[string]any{"type": "message", "id": "msg_" + s.id, "status": "in_progress", "role": "assistant", "content": []any{}},
		}); err != nil {
			return err
		}
		return s.frame(map[string]any{"type": "response.content_part.added", "sequence_number": s.seq(), "item_id": "msg_" + s.id, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
	default:
		return s.frame(map[string]any{
			"id": s.id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}},
		})
	}
}

func (s *Session) anthropicEvent(name string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	buffer := make([]byte, 0, len(name)+len(encoded)+16)
	buffer = append(buffer, "event: "...)
	buffer = append(buffer, name...)
	buffer = append(buffer, '\n')
	buffer = append(buffer, "data: "...)
	buffer = append(buffer, encoded...)
	buffer = append(buffer, '\n', '\n')
	return s.writeFrame(buffer)
}

func (s *Session) writeFrame(buffer []byte) error {
	_, err := s.w.Write(buffer)
	if err == nil {
		if f, ok := s.w.(http.Flusher); ok {
			f.Flush()
		}
	}
	return err
}

func (s *Session) frame(payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	buffer := make([]byte, 0, len(encoded)+80)
	if s.inbound == native.OpenAIResponses {
		if kind, ok := payload["type"].(string); ok {
			buffer = append(buffer, []byte("event: "+kind+"\n")...)
		}
	}
	buffer = append(buffer, "data: "...)
	buffer = append(buffer, encoded...)
	buffer = append(buffer, '\n', '\n')
	return s.writeFrame(buffer)
}

// Write renders the complete answer in one shot: a streaming session with a
// single delta, or one JSON body for synchronous clients.
func Write(w http.ResponseWriter, inbound native.Protocol, id, model, text string, usage Usage, stream bool) error {
	if !stream {
		switch inbound {
		case native.Anthropic:
			return writeJSON(w, map[string]any{
				"id": orDefault(id, "msg_provider"), "type": "message", "role": "assistant", "model": model,
				"content":     []any{map[string]string{"type": "text", "text": text}},
				"stop_reason": "end_turn", "stop_sequence": nil,
				"usage": anthropicWireUsage(usage),
			})
		case native.OpenAIResponses:
			return writeJSON(w, map[string]any{
				"id": orDefault(id, "resp_provider"), "object": "response", "status": "completed", "model": model,
				"output": []any{messageItem("msg_"+orDefault(id, "provider"), text)},
				"usage":  responsesUsage(usage),
			})
		default:
			return writeJSON(w, map[string]any{
				"id": orDefault(id, "chatcmpl-provider"), "object": "chat.completion", "created": time.Now().Unix(), "model": model,
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
				"usage":   chatWireUsage(usage),
			})
		}
	}
	session := NewSession(w, inbound, id, model)
	if err := session.Delta(text); err != nil {
		return err
	}
	return session.Complete(usage)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func messageItem(id, text string) map[string]any {
	return map[string]any{
		"type": "message", "id": id, "status": "completed", "role": "assistant",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
	}
}

func responsesUsage(usage Usage) map[string]any {
	result := map[string]any{"input_tokens": usage.Input, "output_tokens": usage.Output, "total_tokens": usage.Input + usage.Output}
	if usage.Cached > 0 {
		result["input_tokens_details"] = map[string]int64{"cached_tokens": usage.Cached}
	}
	if usage.Reasoning > 0 {
		result["output_tokens_details"] = map[string]int64{"reasoning_tokens": usage.Reasoning}
	}
	return result
}

// anthropicWireUsage renders the Messages usage object; the cached read count
// rides its own field when the provider reported one.
func anthropicWireUsage(usage Usage) map[string]any {
	result := map[string]any{"input_tokens": usage.Input, "output_tokens": usage.Output}
	if usage.Cached > 0 {
		result["cache_read_input_tokens"] = usage.Cached
	}
	return result
}

// chatWireUsage renders the Chat usage object with the detail objects when
// the provider reported them.
func chatWireUsage(usage Usage) map[string]any {
	result := map[string]any{"prompt_tokens": usage.Input, "completion_tokens": usage.Output, "total_tokens": usage.Input + usage.Output}
	if usage.Cached > 0 {
		result["prompt_tokens_details"] = map[string]int64{"cached_tokens": usage.Cached}
	}
	if usage.Reasoning > 0 {
		result["completion_tokens_details"] = map[string]int64{"reasoning_tokens": usage.Reasoning}
	}
	return result
}

func writeJSON(w http.ResponseWriter, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(payload)
	return err
}
