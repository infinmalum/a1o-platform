package aws

import (
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/anthropic"
	"github.com/infinmalum/a1o-platform/relay/adaptor/aws/utils"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/pkg/errors"
)

var _ utils.AwsAdapter = new(Adaptor)

type Adaptor struct {
}

func (a *Adaptor) ConvertRequest(in *model.ConversionInput) (any, error) {
	request := in.Request
	if request == nil {
		return nil, errors.New("request is nil")
	}

	claudeReq := anthropic.ConvertRequest(*request)
	in.Set(ctxkey.RequestModel, request.Model)
	in.Set(ctxkey.ConvertedRequest, claudeReq)
	return claudeReq, nil
}

func (a *Adaptor) DoResponse(c *adaptor.Context, awsCli *bedrockruntime.Client, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	if meta.IsStream {
		err, usage = StreamHandler(c, awsCli)
	} else {
		err, usage = Handler(c, awsCli, meta.ActualModelName)
	}
	return
}
