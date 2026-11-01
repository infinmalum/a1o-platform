package apitype

const (
	OpenAI = iota
	Anthropic
	Gemini
	AwsClaude
	Coze
	Cloudflare
	DeepL
	VertexAI
	Replicate
	// SystemOne marks TypeSafe channels that only serve the native
	// /v1/systemone route and have no legacy adapter.
	SystemOne
	// OpenAIResponses marks the Responses API wire; conversion to it is
	// handled by the shared lifecycle, not a legacy adapter.
	OpenAIResponses
	// OpenResponses marks the openresponses.org wire served by the Vercel AI
	// Gateway. It retains a distinct native protocol identity. Common item/event
	// conversion is shared with Responses; providerOptions and extensions
	// remain raw on native requests. No legacy adapter exists for it either.
	OpenResponses

	Dummy // this one is only for count, do not add any channel after this
)
