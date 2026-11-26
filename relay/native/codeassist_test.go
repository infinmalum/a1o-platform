package native

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareCodeAssistBodyMovesModelAndProject(t *testing.T) {
	body, err := prepareCodeAssistBody([]byte(`{"model":"gemini-3-pro","contents":[]}`), "gemini-3-pro", "proj-1")
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gemini-3-pro","project":"proj-1","request":{"contents":[]}}`, string(body))
}

func TestUnwrapCodeAssistReturnsInnerResponse(t *testing.T) {
	inner, err := unwrapCodeAssist([]byte(`{"response":{"candidates":[]}}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"candidates":[]}`, string(inner))
}

func TestUnwrapCodeAssistPassesThroughAndReportsErrors(t *testing.T) {
	inner, err := unwrapCodeAssist([]byte(`{"candidates":[]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"candidates":[]}`, string(inner))

	_, err = unwrapCodeAssist([]byte(`{"error":{"message":"quota exceeded"}}`))
	require.ErrorContains(t, err, "quota exceeded")
}

func TestNormalizeCodeAssistResponseUnwrapsJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"response":{"usageMetadata":{"promptTokenCount":7}}}`))),
	}
	normalized := NormalizeCodeAssistResponse(resp, Request{ChannelType: 57, Protocol: Gemini})
	payload, err := io.ReadAll(normalized.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"usageMetadata":{"promptTokenCount":7}}`, string(payload))
	require.Equal(t, int64(len(payload)), normalized.ContentLength)
}
