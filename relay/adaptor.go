package relay

import (
	"github.com/infinmalum/one-gateway/relay/adaptor"
	"github.com/infinmalum/one-gateway/relay/adaptor/anthropic"
	"github.com/infinmalum/one-gateway/relay/adaptor/aws"
	"github.com/infinmalum/one-gateway/relay/adaptor/cloudflare"
	"github.com/infinmalum/one-gateway/relay/adaptor/coze"
	"github.com/infinmalum/one-gateway/relay/adaptor/deepl"
	"github.com/infinmalum/one-gateway/relay/adaptor/gemini"
	"github.com/infinmalum/one-gateway/relay/adaptor/openai"
	"github.com/infinmalum/one-gateway/relay/adaptor/replicate"
	"github.com/infinmalum/one-gateway/relay/adaptor/vertexai"
	"github.com/infinmalum/one-gateway/relay/apitype"
)

// GetAdaptor returns the legacy per-vendor adapter for an api type. Vendors
// with official OpenAI-compatible endpoints (Zhipu, Tencent, Cohere, Ollama,
// and the retired PaLM/Baidu/Ali/Xunfei/AIProxyLibrary natives) are served by
// the shared OpenAI adapter instead and fall through to nil here.
func GetAdaptor(apiType int) adaptor.Adaptor {
	switch apiType {
	case apitype.Anthropic:
		return &anthropic.Adaptor{}
	case apitype.AwsClaude:
		return &aws.Adaptor{}
	case apitype.Gemini:
		return &gemini.Adaptor{}
	case apitype.OpenAI:
		return &openai.Adaptor{}
	case apitype.Coze:
		return &coze.Adaptor{}
	case apitype.Cloudflare:
		return &cloudflare.Adaptor{}
	case apitype.DeepL:
		return &deepl.Adaptor{}
	case apitype.VertexAI:
		return &vertexai.Adaptor{}
	case apitype.Replicate:
		return &replicate.Adaptor{}
	}
	return nil
}
