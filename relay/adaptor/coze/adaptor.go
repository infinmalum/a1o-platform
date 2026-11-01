package coze

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/pkg/errors"
)

type Adaptor struct {
	meta *meta.Meta
}

func (a *Adaptor) Init(meta *meta.Meta) {
	a.meta = meta
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	return fmt.Sprintf("%s/v3/chat", strings.TrimRight(meta.BaseURL, "/")), nil
}

func (a *Adaptor) SetupRequestHeader(c *adaptor.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)
	req.Header.Set("Authorization", "Bearer "+meta.APIKey)
	return nil
}

func (a *Adaptor) ConvertRequest(in *model.ConversionInput) (any, error) {
	return ConvertRequest(in, strings.TrimPrefix(a.meta.ActualModelName, "bot-"), a.meta.Config.UserID)
}

func (a *Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *adaptor.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
}

func (a *Adaptor) DoResponse(c *adaptor.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	promptTokens, completionTokens, handleErr := HandleResponse(c.Writer, resp, meta)
	if handleErr != nil {
		return &model.Usage{PromptTokens: int(promptTokens), CompletionTokens: int(completionTokens)}, openai.ErrorWrapper(handleErr, "coze_response_failed", http.StatusBadGateway)
	}
	return &model.Usage{
		PromptTokens:     int(promptTokens),
		CompletionTokens: int(completionTokens),
		TotalTokens:      int(promptTokens + completionTokens),
	}, nil
}
