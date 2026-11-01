// Package agent hosts the agent runtime's model leg: the standard Item
// schema, the run event envelope, and the ModelExecutor that maps Items onto
// native provider protocols through the shared relay lifecycle. Chat/Work
// orchestration (Engine, Scheduler) lands in later stages of the runtime
// reconstruction plan; this package only owns the model-call vertical.
package agent

import "encoding/json"

// Item type discriminators, aligned with the pinned OpenResponses
// 2026-04-24 spec (docs/agent-runtime-contract.md §1).
const (
	ItemMessage            = "message"
	ItemFunctionCall       = "function_call"
	ItemFunctionCallOutput = "function_call_output"
	ItemReasoning          = "reasoning"
)

// Item lifecycle statuses from the same spec.
const (
	ItemStatusInProgress = "in_progress"
	ItemStatusCompleted  = "completed"
	ItemStatusIncomplete = "incomplete"
	ItemStatusFailed     = "failed"
)

// Content part types. input_text/output_text/refusal and summary_text are
// spec names; reasoning_text carries raw visible reasoning inside a
// reasoning item's content.
const (
	PartInputText     = "input_text"
	PartOutputText    = "output_text"
	PartRefusal       = "refusal"
	PartSummaryText   = "summary_text"
	PartReasoningText = "reasoning_text"
)

// ContentPart is one message/reasoning content part.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Refusal holds the refusal text for type "refusal" parts.
	Refusal string `json:"refusal,omitempty"`
	// Annotations ride on output_text parts (citations) and round-trip to
	// providers that understand them.
	Annotations []map[string]any `json:"annotations,omitempty"`
}

// SummaryPart is one reasoning summary entry (type "summary_text").
type SummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// OpaqueBlock carries a native payload that no standard Item field can
// represent (server-side tool results, vendor extensions beyond the slots
// below). Each block records who issued it so replay can verify the
// destination before attaching it. R1 state uses the dedicated slots on Item
// and the invocation-level attribution; this type is the R2+ extension point.
type OpaqueBlock struct {
	Kind              string `json:"kind"`
	Vendor            string `json:"vendor"`
	Protocol          string `json:"protocol"`
	ChannelID         int    `json:"channel_id"`
	UpstreamAccountID int    `json:"upstream_account_id"`
	Version           string `json:"version"`
	Payload           string `json:"payload"`
}

// Item is the standard model-leg unit. JSON field names follow the Responses
// wire shapes the bridges produce and consume, so persistence and replay need
// no second vocabulary; a1o extensions (signature, thought_signature,
// provider_state) are documented in the contract.
type Item struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`

	// message
	Role    string        `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`

	// function_call
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// ThoughtSignature carries Gemini's function-call thought signature
	// (a1o extension matching the bridge's request-side key).
	ThoughtSignature string `json:"thought_signature,omitempty"`

	// function_call_output
	Output string `json:"output,omitempty"`

	// reasoning
	Summary          []SummaryPart `json:"summary,omitempty"`
	EncryptedContent string        `json:"encrypted_content,omitempty"`
	// Signature carries the vendor thinking signature on reasoning items
	// (a1o extension matching the bridge's request-side key).
	Signature string `json:"signature,omitempty"`

	ProviderState []OpaqueBlock `json:"provider_state,omitempty"`
}

// Tool is one function tool exposed to the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}
