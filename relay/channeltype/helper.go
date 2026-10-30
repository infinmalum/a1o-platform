package channeltype

import "github.com/infinmalum/a1o-platform/relay/apitype"

// ToAPIType maps a channel kind onto its wire protocol. Kinds without a case
// fall through to OpenAI: OpenAI, Azure, and OpenRouter speak the OpenAI wire
// directly, and the shared OpenAI adapter plus the per-kind request-URL rules
// in relay/adaptor/openai cover their request shapes.
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
	case TypeSafe:
		apiType = apitype.SystemOne
	case OpenAIResponses:
		apiType = apitype.OpenAIResponses
	}

	return apiType
}
