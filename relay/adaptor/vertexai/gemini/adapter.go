package vertexai

import (
	"net/http"

	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/gemini"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/relaymode"
	"github.com/pkg/errors"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertRequest(in *model.ConversionInput) (any, error) {
	request := in.Request
	if request == nil {
		return nil, errors.New("request is nil")
	}

	geminiRequest := gemini.ConvertRequest(*request)
	in.Set(ctxkey.RequestModel, request.Model)
	in.Set(ctxkey.ConvertedRequest, geminiRequest)
	return geminiRequest, nil
}

func (a *Adaptor) DoResponse(c *adaptor.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	if meta.IsStream {
		var responseText string
		err, responseText = gemini.StreamHandler(c, resp)
		usage = openai.ResponseText2Usage(responseText, meta.ActualModelName, meta.PromptTokens)
	} else {
		switch meta.Mode {
		case relaymode.Embeddings:
			err, usage = gemini.EmbeddingHandler(c, resp)
		default:
			err, usage = gemini.Handler(c, resp, meta.PromptTokens, meta.ActualModelName)
		}
	}
	return
}
