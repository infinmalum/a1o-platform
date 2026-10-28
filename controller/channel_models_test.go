package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	require.Equal(t, "/v1/models", gotPath)
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

	// The custom OpenAI-compatible kind carries the version prefix in its
	// base URL, so only /models is appended.
	_, err := fetchUpstreamModels(server.Client(), channeltype.OpenAICompatible, server.URL+"/v1", "")
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

func TestFetchUpstreamModelsRefusesTypeSafeAndBadURLs(t *testing.T) {
	_, err := fetchUpstreamModels(http.DefaultClient, channeltype.TypeSafe, "https://api.typesafe.ai", "k")
	require.Error(t, err)

	_, err = fetchUpstreamModels(http.DefaultClient, channeltype.OpenAI, "ftp://example.com", "k")
	require.Error(t, err)

	_, err = fetchUpstreamModels(http.DefaultClient, channeltype.OpenAICompatible, "", "k")
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
