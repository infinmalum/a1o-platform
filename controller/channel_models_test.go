package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"github.com/stretchr/testify/require"
)

func TestFetchUpstreamModelsOpenAIShape(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m2"},{"id":"m1"},{"id":"m1"},{"id":""}]}`))
	}))
	defer server.Close()

	models, err := fetchUpstreamModels(server.Client(), channeltype.OpenAI, server.URL, "sk-test")
	require.NoError(t, err)
	// A non-default base is a vendor prefix carrying its own version segment,
	// so only /models is appended.
	require.Equal(t, "/models", gotPath)
	require.Equal(t, "Bearer sk-test", gotAuth)
	require.Equal(t, []string{"m1", "m2"}, models)
}

func TestFetchUpstreamModelsCustomBaseKeepsPrefix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer server.Close()

	// A vendor base URL on the OpenAI kind carries the version prefix in the
	// base URL, so only /models is appended.
	_, err := fetchUpstreamModels(server.Client(), channeltype.OpenAI, server.URL+"/v1", "")
	require.NoError(t, err)
	require.Equal(t, "/v1/models", gotPath)
}

func TestFetchUpstreamModelsAnthropicHeaders(t *testing.T) {
	var gotAPIKey, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-5"}]}`))
	}))
	defer server.Close()

	models, err := fetchUpstreamModels(server.Client(), channeltype.Anthropic, server.URL, "ak-test")
	require.NoError(t, err)
	require.Equal(t, "ak-test", gotAPIKey)
	require.Equal(t, "2023-06-01", gotVersion)
	require.Equal(t, []string{"claude-sonnet-4-5"}, models)
}

func TestFetchUpstreamModelsGeminiFiltersAndStripsPrefix(t *testing.T) {
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-goog-api-key")
		_, _ = w.Write([]byte(`{"models":[
			{"name":"models/gemini-2.0-flash","supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/gemini-2.0-flash","supportedGenerationMethods":["generateContent"]},
			{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]},
			{"name":"models/aqa","supportedGenerationMethods":[]}
		]}`))
	}))
	defer server.Close()

	models, err := fetchUpstreamModels(server.Client(), channeltype.Gemini, server.URL, "g-key")
	require.NoError(t, err)
	require.Equal(t, "g-key", gotAPIKey)
	require.Equal(t, []string{"aqa", "gemini-2.0-flash"}, models)
}

func TestFetchUpstreamModelsOpenRouterAndDeepL(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.RawQuery != "" {
			gotPath = r.URL.Path + "?" + r.URL.RawQuery
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if gotAuth == "DeepL-Auth-Key dl-key" {
			_, _ = w.Write([]byte(`[{"language":"ZH","name":"Chinese"},{"language":"EN-US","name":"English (American)"},{"language":"ZH"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer server.Close()

	// The fixed OpenRouter base carries /api/v1, so only /models appends.
	_, err := fetchUpstreamModels(server.Client(), channeltype.OpenRouter, server.URL+"/api/v1", "or-key")
	require.NoError(t, err)
	require.Equal(t, "/api/v1/models", gotPath)
	require.Equal(t, "Bearer or-key", gotAuth)

	// DeepL lists its translatable target languages as the channel's models.
	models, err := fetchUpstreamModels(server.Client(), channeltype.DeepL, server.URL, "dl-key")
	require.NoError(t, err)
	require.Equal(t, "/v2/languages?type=target", gotPath)
	require.Equal(t, "DeepL-Auth-Key dl-key", gotAuth)
	require.Equal(t, []string{"EN-US", "ZH"}, models)
}

func TestFetchUpstreamModelsRefusesTypeSafeAndBadURLs(t *testing.T) {
	_, err := fetchUpstreamModels(http.DefaultClient, channeltype.TypeSafe, "https://api.typesafe.ai", "k")
	require.Error(t, err)

	_, err = fetchUpstreamModels(http.DefaultClient, channeltype.OpenAI, "ftp://example.com", "k")
	require.Error(t, err)

	_, err = fetchUpstreamModels(http.DefaultClient, channeltype.VertextAI, "", "k")
	require.Error(t, err)
}

func TestFetchUpstreamModelsSurfacesUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer server.Close()

	_, err := fetchUpstreamModels(server.Client(), channeltype.OpenAI, server.URL, "sk-bad")
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "bad key")
}

// TestParseOpenRouterCatalog pins the live-catalog conversion: OpenRouter
// publishes USD per token, the ratio scale is $2 per million tokens, and the
// completion ratio is output over input.
func TestParseOpenRouterCatalog(t *testing.T) {
	body := []byte(`{"data":[
		{"id":"vendor/paid","pricing":{"prompt":"0.000003","completion":"0.000015"}},
		{"id":"vendor/free","pricing":{"prompt":"0","completion":"0"}},
		{"id":"vendor/backwards","pricing":{"prompt":"0","completion":"0.00001"}},
		{"id":"vendor/broken","pricing":{"prompt":"not-a-number","completion":"0.00001"}},
		{"id":"vendor/paid","pricing":{"prompt":"0.000003","completion":"0.000015"}}
	]}`)
	models, prices, err := parseOpenRouterCatalog(body)
	require.NoError(t, err)
	require.Equal(t, []string{"vendor/backwards", "vendor/broken", "vendor/free", "vendor/paid"}, models)
	require.Len(t, prices, 2)
	// $3 per million input tokens -> ratio 1.5; output is 5x input.
	require.Equal(t, billingratio.FetchedModelPrice{Input: 1.5, Completion: 5}, prices["vendor/paid"])
	// A free model bills exactly nothing.
	require.Equal(t, billingratio.FetchedModelPrice{Input: 0, Completion: 0}, prices["vendor/free"])
	// Free input with paid output cannot be expressed in the charge formula
	// and stays unpriced instead of being guessed at.
	_, ok := prices["vendor/backwards"]
	require.False(t, ok)
	_, ok = prices["vendor/broken"]
	require.False(t, ok)
}
