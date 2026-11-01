package deepl

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/infinmalum/a1o-platform/common/helper"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/wire"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
)

type Adaptor struct {
	meta       *meta.Meta
	promptText string
}

func (a *Adaptor) Init(meta *meta.Meta) {
	a.meta = meta
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	return fmt.Sprintf("%s/v2/translate", strings.TrimRight(meta.BaseURL, "/")), nil
}

func (a *Adaptor) SetupRequestHeader(c *adaptor.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)
	req.Header.Set("Authorization", "DeepL-Auth-Key "+meta.APIKey)
	return nil
}

func (a *Adaptor) ConvertRequest(in *model.ConversionInput) (any, error) {
	request := in.Request
	if request == nil {
		return nil, errors.New("request is nil")
	}
	convertedRequest, text, err := ConvertRequest(*request)
	if err != nil {
		return nil, err
	}
	a.promptText = text
	return convertedRequest, nil
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
	defer resp.Body.Close()
	var result Response
	if e := json.NewDecoder(resp.Body).Decode(&result); e != nil {
		return nil, openai.ErrorWrapper(e, "deepl_invalid_response", http.StatusBadGateway)
	}
	if result.Message != "" || len(result.Translations) != 1 {
		message := result.Message
		if message == "" {
			message = "DeepL returned an invalid translations count"
		}
		return nil, openai.ErrorWrapper(errors.New(message), "deepl_error", http.StatusBadGateway)
	}
	// DeepL bills source characters: billed_characters is the authoritative
	// prompt count, and the translation itself is the completion output.
	promptCount := utf8.RuneCountInString(a.promptText)
	if result.Translations[0].BilledCharacters != nil {
		if *result.Translations[0].BilledCharacters < 0 {
			return nil, openai.ErrorWrapper(errors.New("negative DeepL billed_characters"), "deepl_invalid_usage", http.StatusBadGateway)
		}
		promptCount = *result.Translations[0].BilledCharacters
	}
	completionCount := utf8.RuneCountInString(result.Translations[0].Text)
	usage = &model.Usage{PromptTokens: promptCount, CompletionTokens: completionCount, TotalTokens: promptCount + completionCount}
	if e := wire.Write(c.Writer, meta.Inbound, helper.GetResponseID(c.GetString(helper.RequestIdKey)), meta.ActualModelName, result.Translations[0].Text, wire.Usage{Input: int64(promptCount), Output: int64(completionCount)}, meta.IsStream); e != nil {
		return usage, openai.ErrorWrapper(e, "write_response_failed", http.StatusBadGateway)
	}
	return usage, nil
}
