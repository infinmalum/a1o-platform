package controller

import (
	"net/http"

	"github.com/infinmalum/a1o-platform/relay/lifecycle"
	"github.com/infinmalum/a1o-platform/relay/model"
)

func RelayErrorHandler(response *http.Response) *model.ErrorWithStatusCode {
	upstream := lifecycle.ReadUpstreamError(response)
	return &model.ErrorWithStatusCode{
		StatusCode: upstream.Status,
		Error: model.Error{
			Message: upstream.Message, Type: upstream.Type,
			Code: upstream.Code, Param: upstream.Param,
		},
	}
}
