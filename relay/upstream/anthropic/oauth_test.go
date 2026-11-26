package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCallbackAcceptsCodePair(t *testing.T) {
	callback, err := ParseCallback("abcd1234#state-6789")
	require.NoError(t, err)
	require.Equal(t, "abcd1234", callback.Code)
	require.Equal(t, "state-6789", callback.State)
}

func TestParseCallbackAcceptsRedirectURL(t *testing.T) {
	callback, err := ParseCallback(DefaultRedirectURI + "?code=url-code&state=url-state")
	require.NoError(t, err)
	require.Equal(t, "url-code", callback.Code)
	require.Equal(t, "url-state", callback.State)
}

func TestParseCallbackRejectsDeniedAndEmpty(t *testing.T) {
	_, err := ParseCallback(DefaultRedirectURI + "?error=access_denied")
	require.ErrorContains(t, err, "access_denied")

	_, err = ParseCallback("")
	require.Error(t, err)
}

func TestBuildAuthorizationFollowsClaudeCodeSpec(t *testing.T) {
	client := NewClient()
	request, err := client.BuildAuthorization()
	require.NoError(t, err)
	require.Contains(t, request.URL, DefaultEndpoints().Authorize+"?")
	require.Contains(t, request.URL, "client_id="+ClientID)
	require.Contains(t, request.URL, "code_challenge_method=S256")
	require.Contains(t, request.URL, "state="+request.State)
	require.NotEmpty(t, request.Verifier)
	require.NotEqual(t, request.Verifier, request.State)
}
