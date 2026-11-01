package replicate

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/relaymode"
	"github.com/pkg/errors"
)

type Adaptor struct {
	meta *meta.Meta
}

// ConvertImageRequest implements adaptor.Adaptor.
func (*Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if request.ReplicateInput != nil {
		return map[string]any{"input": request.ReplicateInput}, nil
	}
	if request.N > 1 {
		return nil, errors.New("Replicate image conversion supports n=1 only")
	}
	return DrawImageRequest{
		Input: ImageInput{
			Steps:    25,
			Prompt:   request.Prompt,
			Guidance: 3,

			SafetyTolerance: 5,
			NImages:         1, // replicate will always return 1 image
			Width:           1440,
			Height:          1440,
			AspectRatio:     "1:1",
		},
	}, nil
}

func (a *Adaptor) ConvertRequest(in *model.ConversionInput) (any, error) {
	return ConvertChatRequest(in)
}

func (a *Adaptor) Init(meta *meta.Meta) {
	a.meta = meta
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	// The prediction endpoint addresses the mapped model: the channel's model
	// mapping is what turns a client-facing alias into the owner/name ID.
	base := strings.TrimRight(meta.BaseURL, "/")
	if base == "" {
		base = "https://api.replicate.com"
	}
	parts := strings.Split(meta.ActualModelName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(meta.ActualModelName, "?#:%") || parts[0] == ".." || parts[1] == ".." {
		return "", errors.New("Replicate model must be owner/name")
	}
	return fmt.Sprintf("%s/v1/models/%s/predictions", base, meta.ActualModelName), nil
}

func (a *Adaptor) SetupRequestHeader(c *adaptor.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)
	req.Header.Set("Authorization", "Bearer "+meta.APIKey)
	return nil
}

func (a *Adaptor) DoRequest(c *adaptor.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	logger.Info(c.Request.Context(), "send request to replicate")
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
}

func (a *Adaptor) DoResponse(c *adaptor.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	switch meta.Mode {
	case relaymode.ImagesGenerations:
		err, usage = ImageHandler(c, resp, meta)
	case relaymode.ChatCompletions:
		// The chat response renders onto the client wire the request arrived
		// on; the same handler serves Chat, Messages, and Responses clients.
		err, usage = ChatHandler(c, resp, meta)
	default:
		err = openai.ErrorWrapper(errors.New("not implemented"), "not_implemented", http.StatusInternalServerError)
	}

	return
}
