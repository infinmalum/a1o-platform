package bridge

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/infinmalum/a1o-platform/relay/native"
)

// responsesStreamEmitter writes the Responses event sequence shared by every
// converter whose client speaks the Responses wire: response.created on first
// content, reasoning/message/function_call output items with their content
// parts, and the terminal response.completed/incomplete. Callers own the
// source-side state machine — which upstream event feeds which emitter method
// — and own the usage accounting. Unmapped upstream envelope fields ride the
// emitter's provider metadata into the terminal event.
type responsesStreamEmitter struct {
	dst        StreamWriter
	model      string
	responseID string
	createdAt  int64
	seq        int
	started    bool
	finished   bool
	usage      native.Usage
	metadata   providerMetadata

	items             []responsesOutputItem
	messageIndex      int
	messageOpen       bool
	textPartOpen      bool
	refusalPartOpen   bool
	refusalIndex      int
	messageText       string
	messageRefusal    string
	reasoningIndex    int
	reasoningPartOpen bool
}

// responsesOutputItem tracks one output item between its added and done
// events. function_call items remember the upstream tool index that feeds
// them, so callers key their deltas by their own indexing scheme. reasoning
// items accumulate visible thinking text, the vendor signature, and the
// opaque encrypted blob so all three round-trip.
type responsesOutputItem struct {
	kind      string // "message", "function_call", or "reasoning"
	index     int
	id        string
	callID    string
	name      string
	arguments string
	toolIndex int
	text      string
	signature string
	encrypted string
}

func newResponsesStreamEmitter(dst StreamWriter, model string) *responsesStreamEmitter {
	return &responsesStreamEmitter{dst: dst, model: model, createdAt: streamTimestamp(), reasoningIndex: -1}
}

func (e *responsesStreamEmitter) nextSeq() int {
	e.seq++
	return e.seq
}

// fail reports a mid-stream conversion failure as a terminal error event so
// the stream never ends by silent truncation.
func (e *responsesStreamEmitter) fail(message string) error {
	payload, err := json.Marshal(map[string]any{"type": "error", "code": "conversion_failed", "message": message})
	if err != nil {
		return err
	}
	return writeStreamEvent(e.dst, payload)
}

func (e *responsesStreamEmitter) emit(eventType string, payload map[string]any) error {
	payload["type"] = eventType
	payload["sequence_number"] = e.nextSeq()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeStreamEvent(e.dst, encoded)
}

func (e *responsesStreamEmitter) ensureCreated() error {
	if e.started {
		return nil
	}
	if e.responseID == "" {
		generated, err := newStreamID("resp_")
		if err != nil {
			return err
		}
		e.responseID = generated
	}
	e.started = true
	return e.emit("response.created", map[string]any{
		"response": map[string]any{
			"id": e.responseID, "object": "response", "created_at": e.createdAt,
			"status": "in_progress", "model": e.model, "output": []any{},
		},
	})
}

// openReasoningItem starts a reasoning output item, closing any open one
// first so the item sequence stays well formed.
func (e *responsesStreamEmitter) openReasoningItem() error {
	if e.reasoningIndex >= 0 {
		if err := e.closeReasoningItem(); err != nil {
			return err
		}
	}
	if err := e.ensureCreated(); err != nil {
		return err
	}
	e.reasoningIndex = len(e.items)
	e.items = append(e.items, responsesOutputItem{kind: "reasoning", index: e.reasoningIndex, id: newResponsesItemID("rsn_")})
	return e.emit("response.output_item.added", map[string]any{
		"output_index": e.reasoningIndex,
		"item":         map[string]any{"type": "reasoning", "id": e.items[e.reasoningIndex].id, "summary": []any{}},
	})
}

// addReasoningText streams a visible thinking increment into the open
// reasoning item as a summary entry.
func (e *responsesStreamEmitter) addReasoningText(text string) error {
	if text == "" {
		return nil
	}
	if e.reasoningIndex < 0 {
		if err := e.openReasoningItem(); err != nil {
			return err
		}
	}
	item := &e.items[e.reasoningIndex]
	item.text += text
	if !e.reasoningPartOpen {
		e.reasoningPartOpen = true
		if err := e.emit("response.reasoning_summary_part.added", map[string]any{
			"item_id": item.id, "output_index": item.index, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": ""},
		}); err != nil {
			return err
		}
	}
	return e.emit("response.reasoning_summary_text.delta", map[string]any{
		"item_id": item.id, "output_index": item.index, "summary_index": 0, "delta": text,
	})
}

// setReasoningSignature attaches the vendor thought signature to the open
// reasoning item so it round-trips on replay.
func (e *responsesStreamEmitter) setReasoningSignature(signature string) error {
	if signature == "" {
		return nil
	}
	if e.reasoningIndex < 0 {
		if err := e.openReasoningItem(); err != nil {
			return err
		}
	}
	e.items[e.reasoningIndex].signature = signature
	return nil
}

// setReasoningEncrypted carries a sealed reasoning blob as its own reasoning
// item. The gateway and client cannot read it; the issuing vendor validates it
// on replay.
func (e *responsesStreamEmitter) setReasoningEncrypted(encrypted string) error {
	if encrypted == "" {
		return nil
	}
	if e.reasoningIndex >= 0 {
		if err := e.closeReasoningItem(); err != nil {
			return err
		}
	}
	if err := e.ensureCreated(); err != nil {
		return err
	}
	index := len(e.items)
	e.items = append(e.items, responsesOutputItem{kind: "reasoning", index: index, id: newResponsesItemID("rsn_"), encrypted: encrypted})
	if err := e.emit("response.output_item.added", map[string]any{
		"output_index": index,
		"item":         map[string]any{"type": "reasoning", "id": e.items[index].id, "summary": []any{}},
	}); err != nil {
		return err
	}
	return e.closeReasoningItem()
}

// closeReasoningItem emits the done events for the open reasoning item.
func (e *responsesStreamEmitter) closeReasoningItem() error {
	if e.reasoningIndex < 0 {
		return nil
	}
	item := e.items[e.reasoningIndex]
	e.reasoningIndex = -1
	e.reasoningPartOpen = false
	done := map[string]any{"type": "reasoning", "id": item.id, "summary": []any{}}
	if item.text != "" {
		if err := e.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": item.id, "output_index": item.index, "summary_index": 0, "text": item.text,
		}); err != nil {
			return err
		}
		done["summary"] = []any{map[string]string{"type": "summary_text", "text": item.text}}
		if err := e.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": item.id, "output_index": item.index, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": item.text},
		}); err != nil {
			return err
		}
	}
	if item.signature != "" {
		done["signature"] = item.signature
	}
	if item.encrypted != "" {
		done["encrypted_content"] = item.encrypted
	}
	return e.emit("response.output_item.done", map[string]any{
		"output_index": item.index,
		"item":         done,
	})
}

func (e *responsesStreamEmitter) ensureMessageItem() error {
	if e.messageOpen {
		return nil
	}
	if e.reasoningIndex >= 0 {
		if err := e.closeReasoningItem(); err != nil {
			return err
		}
	}
	if err := e.ensureCreated(); err != nil {
		return err
	}
	e.messageOpen = true
	e.messageIndex = len(e.items)
	e.items = append(e.items, responsesOutputItem{kind: "message", index: e.messageIndex, id: newResponsesItemID("msg_")})
	return e.emit("response.output_item.added", map[string]any{
		"output_index": e.messageIndex,
		"item":         map[string]any{"type": "message", "id": e.items[e.messageIndex].id, "status": "in_progress", "role": "assistant", "content": []any{}},
	})
}

func (e *responsesStreamEmitter) ensureTextPart() error {
	if err := e.ensureMessageItem(); err != nil {
		return err
	}
	if e.textPartOpen {
		return nil
	}
	e.textPartOpen = true
	return e.emit("response.content_part.added", map[string]any{
		"item_id": e.items[e.messageIndex].id, "output_index": e.messageIndex, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	})
}

func (e *responsesStreamEmitter) ensureRefusalPart() error {
	if err := e.ensureMessageItem(); err != nil {
		return err
	}
	if e.refusalPartOpen {
		return nil
	}
	e.refusalPartOpen = true
	e.refusalIndex = 0
	if e.textPartOpen {
		e.refusalIndex = 1
	}
	return e.emit("response.content_part.added", map[string]any{
		"item_id": e.items[e.messageIndex].id, "output_index": e.messageIndex, "content_index": e.refusalIndex,
		"part": map[string]any{"type": "refusal", "refusal": ""},
	})
}

// addText streams a display-text increment into the open message item.
func (e *responsesStreamEmitter) addText(text string) error {
	if text == "" {
		return nil
	}
	if err := e.ensureTextPart(); err != nil {
		return err
	}
	e.messageText += text
	return e.emit("response.output_text.delta", map[string]any{
		"item_id": e.items[e.messageIndex].id, "output_index": e.items[e.messageIndex].index,
		"content_index": 0, "delta": text,
	})
}

// addRefusal streams a refusal increment into the open message item.
func (e *responsesStreamEmitter) addRefusal(refusal string) error {
	if refusal == "" {
		return nil
	}
	if err := e.ensureRefusalPart(); err != nil {
		return err
	}
	e.messageRefusal += refusal
	return e.emit("response.refusal.delta", map[string]any{
		"item_id": e.items[e.messageIndex].id, "output_index": e.items[e.messageIndex].index,
		"content_index": e.refusalIndex, "delta": refusal,
	})
}

// openToolItem starts a function_call output item and returns its tracked
// record. toolIndex is the caller's own tool counter, used to route later
// argument deltas back to this item.
func (e *responsesStreamEmitter) openToolItem(callID, name string, toolIndex int) (*responsesOutputItem, error) {
	if callID == "" || name == "" {
		return nil, errors.New("function calls require a call id and name")
	}
	if e.reasoningIndex >= 0 {
		if err := e.closeReasoningItem(); err != nil {
			return nil, err
		}
	}
	if err := e.ensureCreated(); err != nil {
		return nil, err
	}
	e.items = append(e.items, responsesOutputItem{kind: "function_call", index: len(e.items),
		id: newResponsesItemID("fc_"), callID: callID, name: name, toolIndex: toolIndex})
	item := &e.items[len(e.items)-1]
	if err := e.emit("response.output_item.added", map[string]any{
		"output_index": item.index,
		"item": map[string]any{"type": "function_call", "id": item.id, "status": "in_progress",
			"call_id": item.callID, "name": item.name, "arguments": ""},
	}); err != nil {
		return nil, err
	}
	return item, nil
}

func (e *responsesStreamEmitter) toolItemByIndex(toolIndex int) *responsesOutputItem {
	for i := range e.items {
		if e.items[i].kind == "function_call" && e.items[i].toolIndex == toolIndex {
			return &e.items[i]
		}
	}
	return nil
}

// addToolArguments appends one function-call argument increment.
func (e *responsesStreamEmitter) addToolArguments(toolIndex int, arguments string) error {
	if arguments == "" {
		return nil
	}
	item := e.toolItemByIndex(toolIndex)
	if item == nil {
		return errors.New("tool arguments arrived before their function_call item")
	}
	item.arguments += arguments
	return e.emit("response.function_call_arguments.delta", map[string]any{
		"item_id": item.id, "output_index": item.index, "delta": arguments,
	})
}

// setToolSignature attaches a vendor thought signature to the tool-call item
// with the given tool index, keeping Gemini's function-call signature
// round-trip intact.
func (e *responsesStreamEmitter) setToolSignature(toolIndex int, signature string) error {
	if signature == "" {
		return nil
	}
	item := e.toolItemByIndex(toolIndex)
	if item == nil {
		return errors.New("tool signature arrived before its function_call item")
	}
	item.signature = signature
	return nil
}

// closeItems emits the done events for open items, with done events for any
// open function_call items first so output ordering matches Chat upstreams
// that interleave text and tool calls.
func (e *responsesStreamEmitter) closeItems() error {
	if e.reasoningIndex >= 0 {
		if err := e.closeReasoningItem(); err != nil {
			return err
		}
	}
	for _, item := range e.items {
		switch item.kind {
		case "message":
			content := []any{}
			if e.messageText != "" {
				if err := e.emit("response.output_text.done", map[string]any{
					"item_id": item.id, "output_index": item.index, "content_index": 0, "text": e.messageText,
				}); err != nil {
					return err
				}
				content = append(content, map[string]any{"type": "output_text", "text": e.messageText, "annotations": []any{}})
				if err := e.emit("response.content_part.done", map[string]any{
					"item_id": item.id, "output_index": item.index, "content_index": 0,
					"part": map[string]any{"type": "output_text", "text": e.messageText, "annotations": []any{}},
				}); err != nil {
					return err
				}
			}
			if e.messageRefusal != "" {
				if err := e.emit("response.refusal.done", map[string]any{
					"item_id": item.id, "output_index": item.index, "content_index": e.refusalIndex, "refusal": e.messageRefusal,
				}); err != nil {
					return err
				}
				content = append(content, map[string]any{"type": "refusal", "refusal": e.messageRefusal})
				if err := e.emit("response.content_part.done", map[string]any{
					"item_id": item.id, "output_index": item.index, "content_index": e.refusalIndex,
					"part": map[string]any{"type": "refusal", "refusal": e.messageRefusal},
				}); err != nil {
					return err
				}
			}
			if err := e.emit("response.output_item.done", map[string]any{
				"output_index": item.index,
				"item": map[string]any{"type": "message", "id": item.id, "status": "completed",
					"role": "assistant", "content": content},
			}); err != nil {
				return err
			}
		case "function_call":
			if err := e.emit("response.function_call_arguments.done", map[string]any{
				"item_id": item.id, "output_index": item.index, "arguments": item.arguments,
			}); err != nil {
				return err
			}
			done := map[string]any{"type": "function_call", "id": item.id, "status": "completed",
				"call_id": item.callID, "name": item.name, "arguments": item.arguments}
			if item.signature != "" {
				done["thought_signature"] = item.signature
			}
			if err := e.emit("response.output_item.done", map[string]any{
				"output_index": item.index,
				"item":         done,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// terminal writes the response.completed or response.incomplete event. usage
// is included only when the provider reported it, with the cache and
// reasoning details the provider supplied.
func (e *responsesStreamEmitter) terminal(status, incompleteReason string) error {
	if e.finished {
		return nil
	}
	if !e.started {
		return errors.New("upstream stream ended before any content")
	}
	e.finished = true
	completed := map[string]any{
		"id": e.responseID, "object": "response", "created_at": e.createdAt,
		"status": status, "model": e.model, "output": e.completedOutput(),
	}
	if status == "incomplete" {
		completed["incomplete_details"] = map[string]string{"reason": incompleteReason}
	}
	if e.usage.Seen {
		completed["usage"] = responsesUsageEnvelope(e.usage)
	}
	if m := e.metadata.value(); m != nil {
		completed["provider_metadata"] = m
	}
	if err := e.emit("response."+status, map[string]any{"response": completed}); err != nil {
		return err
	}
	e.usage.Complete = true
	return nil
}

// completedOutput snapshots the finished items in output order for the
// terminal response object.
func (e *responsesStreamEmitter) completedOutput() []any {
	output := []any{}
	for _, item := range e.items {
		switch item.kind {
		case "reasoning":
			done := map[string]any{"type": "reasoning", "id": item.id, "summary": []any{}}
			if item.text != "" {
				done["summary"] = []any{map[string]string{"type": "summary_text", "text": item.text}}
			}
			if item.signature != "" {
				done["signature"] = item.signature
			}
			if item.encrypted != "" {
				done["encrypted_content"] = item.encrypted
			}
			output = append(output, done)
		case "message":
			content := []any{}
			if e.messageText != "" {
				content = append(content, map[string]any{"type": "output_text", "text": e.messageText, "annotations": []any{}})
			}
			if e.messageRefusal != "" {
				content = append(content, map[string]any{"type": "refusal", "refusal": e.messageRefusal})
			}
			output = append(output, map[string]any{"type": "message", "id": item.id, "status": "completed",
				"role": "assistant", "content": content})
		case "function_call":
			done := map[string]any{"type": "function_call", "id": item.id, "status": "completed",
				"call_id": item.callID, "name": item.name, "arguments": item.arguments}
			if item.signature != "" {
				done["thought_signature"] = item.signature
			}
			output = append(output, done)
		}
	}
	return output
}

// responsesStreamStatus maps a terminal condition onto the Responses status
// pair, rejecting reasons no converter can represent.
func responsesStreamStatus(completed bool, reason string) (string, string, error) {
	if completed {
		return "completed", "", nil
	}
	switch reason {
	case "max_output_tokens":
		return "incomplete", "max_output_tokens", nil
	case "content_filter":
		return "incomplete", "content_filter", nil
	default:
		return "", "", fmt.Errorf("stop reason %q cannot be converted", reason)
	}
}
