package utils

import (
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
)

type AwsAdapter interface {
	ConvertRequest(input *model.ConversionInput) (any, error)
	DoResponse(c *adaptor.Context, awsCli *bedrockruntime.Client, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode)
}
