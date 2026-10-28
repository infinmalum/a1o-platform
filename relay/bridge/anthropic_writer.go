package bridge

import (
	"encoding/json"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// anthropicStreamWriter writes the Anthropic Messages event envelope shared by
// both outbound stream directions: message_start on first content,
// content_block bookkeeping, and the terminal message_delta/message_stop pair.
// Callers own the state machine around it — which blocks may open, and when
// the stream is allowed to finish.
type anthropicStreamWriter struct {
	dst       StreamWriter
	model     string
	id        string
	usage     native.Usage
	started   bool
	nextBlock int
}

// ensureStart emits message_start once, generating a message ID when the
// upstream never provided one.
func (w *anthropicStreamWriter) ensureStart() error {
	if w.started {
		return nil
	}
	if w.id == "" {
		generated, err := newStreamID(anthropicMessageIDPrefix)
		if err != nil {
			return err
		}
		w.id = generated
	}
	start, err := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": w.id, "type": "message", "role": "assistant", "model": w.model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]int64{"input_tokens": w.usage.Input, "output_tokens": 0},
		},
	})
	if err != nil {
		return err
	}
	if err := writeAnthropicEvent(w.dst, "message_start", start); err != nil {
		return err
	}
	w.started = true
	return nil
}

// openBlock emits content_block_start and returns the new block index.
func (w *anthropicStreamWriter) openBlock(contentBlock map[string]any) (int, error) {
	if err := w.ensureStart(); err != nil {
		return 0, err
	}
	index := w.nextBlock
	w.nextBlock++
	start, err := json.Marshal(map[string]any{"type": "content_block_start", "index": index, "content_block": contentBlock})
	if err != nil {
		return 0, err
	}
	if err := writeAnthropicEvent(w.dst, "content_block_start", start); err != nil {
		return 0, err
	}
	return index, nil
}

func (w *anthropicStreamWriter) closeBlock(index int) error {
	stop, err := json.Marshal(map[string]any{"type": "content_block_stop", "index": index})
	if err != nil {
		return err
	}
	return writeAnthropicEvent(w.dst, "content_block_stop", stop)
}

// delta emits a content_block_delta for the block with the given index.
func (w *anthropicStreamWriter) delta(index int, payload map[string]any) error {
	body, err := json.Marshal(map[string]any{"type": "content_block_delta", "index": index, "delta": payload})
	if err != nil {
		return err
	}
	return writeAnthropicEvent(w.dst, "content_block_delta", body)
}

// finish writes message_delta with the stop reason and accumulated usage,
// then message_stop carrying provider metadata. Complete is set on the usage
// once the terminal events are on the wire.
func (w *anthropicStreamWriter) finish(stopReason, providerKey string, metadata map[string]json.RawMessage) error {
	delta, err := json.Marshal(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]int64{"input_tokens": w.usage.Input, "output_tokens": w.usage.Output},
	})
	if err != nil {
		return err
	}
	if err := writeAnthropicEvent(w.dst, "message_delta", delta); err != nil {
		return err
	}
	stopEvent := map[string]any{"type": "message_stop"}
	if len(metadata) > 0 {
		stopEvent["provider_metadata"] = map[string]any{providerKey: metadata}
	}
	payload, err := json.Marshal(stopEvent)
	if err != nil {
		return err
	}
	if err := writeAnthropicEvent(w.dst, "message_stop", payload); err != nil {
		return err
	}
	w.usage.Complete = true
	return nil
}
