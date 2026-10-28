package vertexai

import (
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/adaptor"
	claude "github.com/infinmalum/a1o-platform/relay/adaptor/vertexai/claude"
	gemini "github.com/infinmalum/a1o-platform/relay/adaptor/vertexai/gemini"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
)

// VertexAI serves exactly two wire families. Models come from the admin's
// channel config, so the family is resolved from the model name prefix
// instead of a hardcoded roster that goes stale.
type VertexAIModelType int

const (
	VerterAIClaude VertexAIModelType = iota + 1
	VerterAIGemini
)

func resolveModelType(model string) VertexAIModelType {
	if strings.HasPrefix(model, "gemini") {
		return VerterAIGemini
	}
	return VerterAIClaude
}

type innerAIAdapter interface {
	ConvertRequest(input *model.ConversionInput) (any, error)
	DoResponse(c *adaptor.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode)
}

func GetAdaptor(model string) innerAIAdapter {
	switch resolveModelType(model) {
	case VerterAIClaude:
		return &claude.Adaptor{}
	case VerterAIGemini:
		return &gemini.Adaptor{}
	default:
		return nil
	}
}
