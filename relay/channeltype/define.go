package channeltype

// Channel kind IDs are persisted database identifiers: the numbers below are
// permanent, and so are the gaps. Retired kinds keep their old numbers
// reserved forever — never renumber a kind and never reuse a vacant slot for
// a new kind, or existing rows silently change meaning.
//
// Vacant retired IDs: 2, 4-13, 15-23, 25-26, 28-32, 35-36, 39-41, 44-45, 47-49, 51.
const (
	Unknown = 0
	OpenAI  = 1
	Azure   = 3

	Anthropic = 14
	Gemini    = 24
	Minimax   = 27
	AwsClaude = 33
	Coze      = 34

	Cloudflare = 37
	DeepL      = 38

	VertextAI = 42
	Replicate = 46
	Proxy     = 43

	OpenAICompatible = 50
	TypeSafe         = 52

	// OpenAIResponses targets upstreams that serve the Responses API
	// (/v1/responses) rather than Chat Completions; it shares the OpenAI
	// family wire but is routed to its own channel kind so admins can pick
	// the right endpoint per upstream.
	OpenAIResponses = 53

	// Dummy is the exclusive upper bound of valid kind IDs, not a kind.
	Dummy = 54
)

// IsValid reports whether kind names a channel kind that actually exists.
// The vacant retired slots inside the ID range are not kinds, so a range
// check alone would accept zombie rows; every validation and switch over
// kinds goes through this or names kinds explicitly.
func IsValid(kind int) bool {
	switch kind {
	case OpenAI, Azure, Anthropic, Gemini, Minimax, AwsClaude, Coze,
		Cloudflare, DeepL, VertextAI, Replicate, Proxy, OpenAICompatible,
		TypeSafe, OpenAIResponses:
		return true
	}
	return false
}
