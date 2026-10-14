package chat

import "encoding/json"

// Event types streamed to the browser client during a chat turn.
const (
	EventMessageStart = "message_start"
	EventDelta        = "delta"
	EventToolCall     = "tool_call"
	EventToolResult   = "tool_result"
	EventUsage        = "usage"
	EventCompact      = "compact"
	EventError        = "error"
	EventDone         = "done"
)

// Event is one SSE payload of the chat turn protocol. Fields beyond Type are
// populated per event kind.
type Event struct {
	Type             string `json:"type"`
	MessageId        int    `json:"message_id,omitempty"`
	Delta            string `json:"delta,omitempty"`
	ToolCallID       string `json:"tool_call_id,omitempty"`
	Name             string `json:"name,omitempty"`
	Arguments        string `json:"arguments,omitempty"`
	Content          string `json:"content,omitempty"`
	IsError          bool   `json:"is_error,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Emitter delivers one event to the connected client. An error return means
// the client stopped consuming; the turn aborts through request cancellation.
type Emitter func(Event) error

// EncodeEvent renders the SSE wire form; the caller writes and flushes it.
func EncodeEvent(event Event) []byte {
	payload, err := json.Marshal(event)
	if err != nil {
		payload = []byte(`{"type":"error","error":"event encoding failed"}`)
	}
	out := make([]byte, 0, len(payload)+8)
	out = append(out, "data: "...)
	out = append(out, payload...)
	return append(out, '\n', '\n')
}
