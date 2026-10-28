package openai

import (
	"fmt"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/infinmalum/a1o-platform/relay/model"
)

func ResponseText2Usage(responseText string, modelName string, promptTokens int) *model.Usage {
	usage := &model.Usage{}
	usage.PromptTokens = promptTokens
	usage.CompletionTokens = CountTokenText(responseText, modelName)
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage
}

func GetFullRequestURL(baseURL string, requestURL string, channelType int) string {
	// An admin-supplied base URL is the vendor's documented full prefix
	// (e.g. https://open.bigmodel.cn/api/paas/v4); appending the versioned
	// request path as-is would double the version segment, so the path's
	// /v1 is dropped. A base equal to the kind's official default means the
	// field was left empty and the versioned official path appends untouched.
	// Azure keeps its deployment-shaped path rule below.
	if channelType != channeltype.Azure && baseURL != channeltype.BaseURL(channelType) {
		return fmt.Sprintf("%s%s", strings.TrimSuffix(baseURL, "/"), strings.TrimPrefix(requestURL, "/v1"))
	}
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") && channelType == channeltype.Azure {
		fullRequestURL = fmt.Sprintf("%s%s", baseURL, strings.TrimPrefix(requestURL, "/openai/deployments"))
	}
	return fullRequestURL
}
