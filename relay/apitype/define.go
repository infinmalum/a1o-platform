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
	Proxy
	Replicate
	// SystemOne marks TypeSafe channels that only serve the native
	// /v1/systemone route and have no legacy adapter.
	SystemOne
	// OpenAIResponses marks the Responses API wire; conversion to it is
	// handled by the shared lifecycle, not a legacy adapter.
	OpenAIResponses

	Dummy // this one is only for count, do not add any channel after this
)
