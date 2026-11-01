package agent

import "encoding/json"

// EventEnvelope is the versioned run event wire form streamed to clients and
// persisted for replay (agent-runtime-contract.md §2). Deltas stream
// transiently: they carry no sequence or event id and never reach the
// RunEvent table.
type EventEnvelope struct {
	RunID         int             `json:"run_id"`
	EventID       string          `json:"event_id,omitempty"`
	Sequence      int64           `json:"sequence,omitempty"`
	Type          string          `json:"type"`
	SchemaVersion string          `json:"schema_version,omitempty"`
	InvocationID  int             `json:"invocation_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// Emitter delivers one envelope to the connected client. An error return
// means the client stopped consuming; the turn aborts through request
// cancellation.
type Emitter func(EventEnvelope) error

// EncodeEvent renders the SSE wire form; the caller writes and flushes it.
func EncodeEvent(event EventEnvelope) []byte {
	payload, err := json.Marshal(event)
	if err != nil {
		payload = []byte(`{"type":"run.failed","payload":{"error":"event encoding failed"}}`)
	}
	out := make([]byte, 0, len(payload)+8)
	out = append(out, "data: "...)
	out = append(out, payload...)
	return append(out, '\n', '\n')
}
