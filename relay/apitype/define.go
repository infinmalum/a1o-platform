package apitype

const (
	OpenAI = iota
	Anthropic
	PaLM
	Baidu
	Zhipu
	Ali
	Xunfei
	AIProxyLibrary
	Tencent
	Gemini
	Ollama
	AwsClaude
	Coze
	Cohere
	Cloudflare
	DeepL
	VertexAI
	Proxy
	Replicate
	// SystemOne marks TypeSafe channels that only serve the native
	// /v1/systemone route and have no legacy adapter.
	SystemOne

	Dummy // this one is only for count, do not add any channel after this
)
