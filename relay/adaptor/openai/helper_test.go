package openai

import (
	"testing"

	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// TestGetFullRequestURL pins the base URL joining rule: an admin-supplied
// base URL is the vendor's documented full version-prefixed prefix and the
// request path's /v1 is dropped before appending; a base equal to the kind's
// official default means the field was left empty and the versioned official
// path appends untouched.
func TestGetFullRequestURL(t *testing.T) {
	cases := []struct {
		name       string
		baseURL    string
		requestURL string
		kind       int
		want       string
	}{
		{
			name:       "official openai default keeps versioned path",
			baseURL:    channeltype.BaseURL(channeltype.OpenAI),
			requestURL: "/v1/chat/completions",
			kind:       channeltype.OpenAI,
			want:       "https://api.openai.com/v1/chat/completions",
		},
		{
			name:       "vendor completions prefix replaces /v1",
			baseURL:    "https://open.bigmodel.cn/api/paas/v4",
			requestURL: "/v1/chat/completions",
			kind:       channeltype.OpenAI,
			want:       "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		},
		{
			name:       "official responses default keeps versioned path",
			baseURL:    channeltype.BaseURL(channeltype.OpenAIResponses),
			requestURL: "/v1/responses",
			kind:       channeltype.OpenAIResponses,
			want:       "https://api.openai.com/v1/responses",
		},
		{
			name:       "vendor responses prefix replaces /v1",
			baseURL:    "https://open.bigmodel.cn/api/v1",
			requestURL: "/v1/responses",
			kind:       channeltype.OpenAIResponses,
			want:       "https://open.bigmodel.cn/api/v1/responses",
		},
		{
			name:       "compatible kind always strips /v1",
			baseURL:    "https://api.deepseek.com/v1",
			requestURL: "/v1/chat/completions",
			kind:       channeltype.OpenAICompatible,
			want:       "https://api.deepseek.com/v1/chat/completions",
		},
		{
			name:       "azure deployment path appends untouched",
			baseURL:    "https://docs-test-001.openai.azure.com",
			requestURL: "/openai/deployments/gpt4o/chat/completions",
			kind:       channeltype.Azure,
			want:       "https://docs-test-001.openai.azure.com/openai/deployments/gpt4o/chat/completions",
		},
		{
			name:       "cloudflare azure gateway strips deployments prefix",
			baseURL:    "https://gateway.ai.cloudflare.com/v1/acc/gw/openai",
			requestURL: "/openai/deployments/gpt4o/chat/completions",
			kind:       channeltype.Azure,
			want:       "https://gateway.ai.cloudflare.com/v1/acc/gw/openai/gpt4o/chat/completions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetFullRequestURL(tc.baseURL, tc.requestURL, tc.kind)
			if got != tc.want {
				t.Fatalf("GetFullRequestURL(%q, %q, %d) = %q, want %q", tc.baseURL, tc.requestURL, tc.kind, got, tc.want)
			}
		})
	}
}
