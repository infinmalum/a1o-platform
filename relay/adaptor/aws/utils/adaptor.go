package utils

import (
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/infinmalum/one-gateway/relay/adaptor"
	"github.com/infinmalum/one-gateway/relay/meta"
	"github.com/infinmalum/one-gateway/relay/model"
)

type AwsAdapter interface {
	ConvertRequest(input *model.ConversionInput) (any, error)
	DoResponse(c *adaptor.Context, awsCli *bedrockruntime.Client, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode)
}
