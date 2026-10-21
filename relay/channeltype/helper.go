package channeltype

import "github.com/infinmalum/one-gateway/relay/apitype"

// ToAPIType maps a channel type onto its wire protocol. Retired native
// providers (PaLM, Baidu, Zhipu, Ali, Xunfei, Tencent, Cohere, Ollama,
// AIProxyLibrary) fall through to OpenAI: their vendors all publish official
// OpenAI-compatible endpoints now, so the shared OpenAI wire plus the
// per-type request-URL rules in relay/adaptor/openai replace the old
// per-vendor adapters.
func ToAPIType(channelType int) int {
	apiType := apitype.OpenAI
	switch channelType {
	case Anthropic:
		apiType = apitype.Anthropic
	case Gemini:
		apiType = apitype.Gemini
	case AwsClaude:
		apiType = apitype.AwsClaude
	case Coze:
		apiType = apitype.Coze
	case Cloudflare:
		apiType = apitype.Cloudflare
	case DeepL:
		apiType = apitype.DeepL
	case VertextAI:
		apiType = apitype.VertexAI
	case Replicate:
		apiType = apitype.Replicate
	case Proxy:
		apiType = apitype.Proxy
	case TypeSafe:
		apiType = apitype.SystemOne
	case OpenAIResponses:
		apiType = apitype.OpenAIResponses
	}

	return apiType
}
