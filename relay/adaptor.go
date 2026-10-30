package relay

import (
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/anthropic"
	"github.com/infinmalum/a1o-platform/relay/adaptor/aws"
	"github.com/infinmalum/a1o-platform/relay/adaptor/cloudflare"
	"github.com/infinmalum/a1o-platform/relay/adaptor/coze"
	"github.com/infinmalum/a1o-platform/relay/adaptor/deepl"
	"github.com/infinmalum/a1o-platform/relay/adaptor/gemini"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/adaptor/replicate"
	"github.com/infinmalum/a1o-platform/relay/adaptor/vertexai"
	"github.com/infinmalum/a1o-platform/relay/apitype"
)

// GetAdaptor returns the per-protocol adapter for an api type. Channel kinds
// that speak the OpenAI wire directly (OpenAI, Azure) map onto apitype.OpenAI
// via ToAPIType and are served by the shared OpenAI adapter, so this switch
// never sees them.
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
