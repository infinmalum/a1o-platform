package channeltype

// Channel kind IDs are persisted database identifiers: the numbers below
// address rows in the channels table and must never be renumbered.
const (
	Unknown = 0
	OpenAI  = 1
	Azure   = 3

	Anthropic = 14
	Gemini    = 24
	AwsClaude = 33
	Coze      = 34

	Cloudflare = 37
	DeepL      = 38

	VertextAI = 42
	Replicate = 46

	TypeSafe = 52

	// OpenAIResponses targets upstreams that serve the Responses API
	// (/v1/responses) rather than Chat Completions; it shares the OpenAI
	// family wire but is routed to its own channel kind so admins can pick
	// the right endpoint per upstream.
	OpenAIResponses = 53

	// OpenRouter is the aggregator's unified API: one key and one
	// version-bearing base (https://openrouter.ai/api/v1) serve the OpenAI
	// chat, OpenAI Responses, Anthropic Messages, and SystemOne wires, so
	// the kind forwards all four and needs no operator-supplied base URL.
	OpenRouter = 54

	// Dummy is the exclusive upper bound of valid kind IDs, not a kind.
	Dummy = 55
)

// IsValid reports whether kind names a channel kind that actually exists.
// The ID space has gaps, so a range check alone would accept unassigned
// IDs; every validation and switch over kinds goes through this or names
// kinds explicitly.
func IsValid(kind int) bool {
	switch kind {
	case OpenAI, Azure, Anthropic, Gemini, AwsClaude, Coze,
		Cloudflare, DeepL, VertextAI, Replicate,
		TypeSafe, OpenAIResponses, OpenRouter:
		return true
	}
	return false
}
