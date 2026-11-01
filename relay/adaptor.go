package relay

import (
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/coze"
	"github.com/infinmalum/a1o-platform/relay/adaptor/deepl"
	"github.com/infinmalum/a1o-platform/relay/adaptor/gemini"
	"github.com/infinmalum/a1o-platform/relay/adaptor/replicate"
	"github.com/infinmalum/a1o-platform/relay/apitype"
)

// GetAdaptor returns the per-protocol adapter for an api type. The native
// lifecycle serves the OpenAI, Anthropic, Gemini, and Responses wires
// directly — including the AWS Bedrock, VertexAI, and Cloudflare kinds whose
// transports live in the native layer — so only the providers whose upstream
// wire has no native protocol keep adapters: Coze, DeepL, Replicate, and the
// Gemini embeddings conversion.
func GetAdaptor(apiType int) adaptor.Adaptor {
	switch apiType {
	case apitype.Gemini:
		return &gemini.Adaptor{}
	case apitype.Coze:
		return &coze.Adaptor{}
	case apitype.DeepL:
		return &deepl.Adaptor{}
	case apitype.Replicate:
		return &replicate.Adaptor{}
	}
	return nil
}
