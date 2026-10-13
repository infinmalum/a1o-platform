package openai

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/infinmalum/one-gateway/relay/credential"
)

func sha256Sum(t *testing.T, value string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// testToken derives credential values at runtime so the source never carries
// literal secret material.
func testToken(t *testing.T, seed string) string {
	t.Helper()
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return seed + "-" + hex.EncodeToString(buf)
}

// fakeAuth imitates the OAuth token and JWKS endpoints of the authorization
// server for offline verification of the Sign in with ChatGPT flow.
type fakeAuth struct {
	t            *testing.T
	server       *httptest.Server
	key          *rsa.PrivateKey
	issuer       string
	clientID     string
	accessValue  string
	refreshValue string

	mu            sync.Mutex
	tokenRequests []url.Values
	refreshStatus int
	refreshCode   string
}

func newFakeAuth(t *testing.T) *fakeAuth {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	fake := &fakeAuth{t: t, key: key}
	fake.accessValue = testToken(t, "access")
	fake.refreshValue = testToken(t, "refresh")
	fake.clientID = "oaiapp-" + testToken(t, "client")
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		fake.mu.Lock()
		fake.tokenRequests = append(fake.tokenRequests, r.PostForm)
		refreshStatus, refreshCode := fake.refreshStatus, fake.refreshCode
		fake.mu.Unlock()
		if r.PostForm.Get("grant_type") == "refresh_token" && refreshStatus != 0 {
			w.WriteHeader(refreshStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": refreshCode, "error_description": "rejected"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fake.accessValue,
			"refresh_token": fake.refreshValue,
			"id_token":      fake.mintIDTokenAt("", time.Now().Add(time.Hour)),
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         RequiredScope,
		})
	})
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "alg": "RS256", "kid": "test-key",
				"n": base64.RawURLEncoding.EncodeToString(fake.key.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(fake.key.PublicKey.E)).Bytes()),
			}},
		})
	})
	fake.server = httptest.NewServer(mux)
	fake.issuer = fake.server.URL
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeAuth) mintIDTokenAt(nonce string, expiry time.Time) string {
	f.t.Helper()
	claims := jwt.MapClaims{
		"iss": f.issuer, "aud": f.clientID, "sub": "subject-1",
		"email": "user@example.com",
		"exp":   expiry.Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(f.key)
	require.NoError(f.t, err)
	return signed
}

func (f *fakeAuth) mintIDToken(nonce string) string {
	return f.mintIDTokenAt(nonce, time.Now().Add(time.Hour))
}

func (f *fakeAuth) client() *Client {
	return &Client{
		Endpoints: Endpoints{
			Authorize: f.server.URL + "/authorize",
			Token:     f.server.URL + "/token",
			JWKS:      f.server.URL + "/jwks.json",
			Issuer:    f.issuer,
		},
		HTTP:        f.server.Client(),
		AgentName:   "One Gateway Test",
		HostID:      testToken(f.t, "host"),
		RedirectURI: DefaultRedirectURI,
	}
}

func (f *fakeAuth) requests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.tokenRequests...)
}

func TestBuildAuthorizationRegistersClient(t *testing.T) {
	client := newFakeAuth(t).client()
	request, err := client.BuildAuthorization("")
	require.NoError(t, err)
	parsed, err := url.Parse(request.URL)
	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, "code", query.Get("response_type"))
	require.Equal(t, RegistrationClient, query.Get("client_id"))
	require.Equal(t, client.AgentName, query.Get("agent_name_hint"))
	require.Equal(t, client.HostID, query.Get("ext_agent_host_id"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.Equal(t, Audience, query.Get("resource"))
	require.Equal(t, RequiredScope, query.Get("scope"))
	require.Equal(t, client.RedirectURI, query.Get("redirect_uri"))
	require.NotEmpty(t, query.Get("state"))
	require.Equal(t, query.Get("state"), request.State)
	require.NotEmpty(t, query.Get("nonce"))
	require.Equal(t, query.Get("nonce"), request.Nonce)
	require.NotEmpty(t, request.Verifier)
	digest := sha256Sum(t, request.Verifier)
	require.Equal(t, digest, query.Get("code_challenge"))
}

func TestBuildAuthorizationReusesRegistration(t *testing.T) {
	client := newFakeAuth(t).client()
	registration := testToken(t, "registration")
	request, err := client.BuildAuthorization(registration)
	require.NoError(t, err)
	parsed, err := url.Parse(request.URL)
	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, registration, query.Get("client_id"))
	require.Empty(t, query.Get("agent_name_hint"))
	require.Empty(t, query.Get("ext_agent_host_id"))
}

func TestBuildAuthorizationRequiresRegistrationIdentity(t *testing.T) {
	client := &Client{Endpoints: DefaultEndpoints(), RedirectURI: DefaultRedirectURI}
	_, err := client.BuildAuthorization("")
	require.Error(t, err)
}

func TestParseCallback(t *testing.T) {
	code := testToken(t, "code")
	issued := testToken(t, "issued")
	callback, err := ParseCallback(url.Values{"code": {code}, "state": {"state-value"}, "client_id": {issued}})
	require.NoError(t, err)
	require.Equal(t, code, callback.Code)
	require.Equal(t, "state-value", callback.State)
	require.Equal(t, issued, callback.IssuedClientID)

	_, err = ParseCallback(url.Values{"error": {"access_denied"}})
	require.Error(t, err)

	_, err = ParseCallback(url.Values{"state": {"state-value"}})
	require.Error(t, err)
}

func TestExchangeSendsAuthorizationGrant(t *testing.T) {
	fake := newFakeAuth(t)
	client := fake.client()
	code := testToken(t, "code")
	verifier := testToken(t, "verifier")
	set, err := client.Exchange(context.Background(), fake.clientID, code, verifier, DefaultRedirectURI)
	require.NoError(t, err)
	require.Equal(t, fake.accessValue, set.AccessToken)
	require.Equal(t, fake.refreshValue, set.RefreshToken)
	require.Equal(t, int64(3600), set.ExpiresIn)
	require.NotEmpty(t, set.IDToken)
	require.Equal(t, RequiredScope, set.Scope)
	require.Len(t, fake.requests(), 1)
	form := fake.requests()[0]
	require.Equal(t, "authorization_code", form.Get("grant_type"))
	require.Equal(t, fake.clientID, form.Get("client_id"))
	require.Equal(t, code, form.Get("code"))
	require.Equal(t, verifier, form.Get("code_verifier"))
	require.Equal(t, DefaultRedirectURI, form.Get("redirect_uri"))
	require.Equal(t, Audience, form.Get("resource"))
}

func TestRefresherRotatesCredential(t *testing.T) {
	fake := newFakeAuth(t)
	refresher := Refresher{Client: fake.client()}
	current := credential.Document{Kind: credential.KindOAuth, OAuth: &credential.OAuthDocument{
		AccessToken:       testToken(t, "old-access"),
		RefreshToken:      testToken(t, "old-refresh"),
		RegistrationID:    fake.clientID,
		ExternalAccountID: "subject-1",
		ExpiresAt:         time.Now().Add(time.Minute).Unix(),
	}}
	next, err := refresher.Refresh(context.Background(), 7, current)
	require.NoError(t, err)
	require.Equal(t, fake.accessValue, next.OAuth.AccessToken)
	require.Equal(t, fake.refreshValue, next.OAuth.RefreshToken)
	require.Greater(t, next.OAuth.ExpiresAt, time.Now().Add(50*time.Minute).Unix())
	require.NotEmpty(t, next.OAuth.IDToken)
	require.Equal(t, current.OAuth.RegistrationID, next.OAuth.RegistrationID)
	require.Equal(t, current.OAuth.ExternalAccountID, next.OAuth.ExternalAccountID)
	require.True(t, GrantsEntitlement(next.OAuth.Scopes))
	require.Len(t, fake.requests(), 1)
	form := fake.requests()[0]
	require.Equal(t, "refresh_token", form.Get("grant_type"))
	require.Equal(t, fake.clientID, form.Get("client_id"))
	require.Equal(t, current.OAuth.RefreshToken, form.Get("refresh_token"))
}

func TestRefresherReauthorizationOnInvalidGrant(t *testing.T) {
	fake := newFakeAuth(t)
	fake.mu.Lock()
	fake.refreshStatus, fake.refreshCode = http.StatusBadRequest, "invalid_grant"
	fake.mu.Unlock()
	refresher := Refresher{Client: fake.client()}
	current := credential.Document{Kind: credential.KindOAuth, OAuth: &credential.OAuthDocument{
		AccessToken: testToken(t, "access"), RefreshToken: testToken(t, "refresh"),
		RegistrationID: fake.clientID,
	}}
	_, err := refresher.Refresh(context.Background(), 7, current)
	require.ErrorIs(t, err, credential.ErrReauthorization)
}

func TestRefresherTransientErrorStaysRetryable(t *testing.T) {
	fake := newFakeAuth(t)
	fake.mu.Lock()
	fake.refreshStatus, fake.refreshCode = http.StatusServiceUnavailable, "temporarily_unavailable"
	fake.mu.Unlock()
	refresher := Refresher{Client: fake.client()}
	current := credential.Document{Kind: credential.KindOAuth, OAuth: &credential.OAuthDocument{
		AccessToken: testToken(t, "access"), RefreshToken: testToken(t, "refresh"),
		RegistrationID: fake.clientID,
	}}
	_, err := refresher.Refresh(context.Background(), 7, current)
	require.Error(t, err)
	require.NotErrorIs(t, err, credential.ErrReauthorization)
}

func TestRefresherWithoutRegistrationNeedsReauth(t *testing.T) {
	refresher := Refresher{}
	current := credential.Document{Kind: credential.KindOAuth, OAuth: &credential.OAuthDocument{
		AccessToken: testToken(t, "access"), RefreshToken: testToken(t, "refresh"),
	}}
	_, err := refresher.Refresh(context.Background(), 7, current)
	require.ErrorIs(t, err, credential.ErrReauthorization)
}

func TestValidateIDToken(t *testing.T) {
	fake := newFakeAuth(t)
	client := fake.client()
	nonce := testToken(t, "nonce")
	token := fake.mintIDToken(nonce)
	identity, err := client.ValidateIDToken(context.Background(), token, fake.clientID, nonce)
	require.NoError(t, err)
	require.Equal(t, "subject-1", identity.Subject)
	require.Equal(t, "user@example.com", identity.Email)

	_, err = client.ValidateIDToken(context.Background(), token, fake.clientID, testToken(t, "other-nonce"))
	require.Error(t, err)

	_, err = client.ValidateIDToken(context.Background(), token, "other-client", nonce)
	require.Error(t, err)

	expired := fake.mintIDTokenAt(nonce, time.Now().Add(-time.Minute))
	_, err = client.ValidateIDToken(context.Background(), expired, fake.clientID, nonce)
	require.Error(t, err)
}

func TestValidateIDTokenRejectsForeignSignature(t *testing.T) {
	fake := newFakeAuth(t)
	client := fake.client()
	foreignKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	claims := jwt.MapClaims{
		"iss": fake.issuer, "aud": fake.clientID, "sub": "subject-1",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(foreignKey)
	require.NoError(t, err)
	_, err = client.ValidateIDToken(context.Background(), signed, fake.clientID, "")
	require.Error(t, err)
}

func TestUnverifiedIDTokenAudience(t *testing.T) {
	fake := newFakeAuth(t)
	token := fake.mintIDToken("")
	require.Equal(t, fake.clientID, UnverifiedIDTokenAudience(token))
}

func TestDocumentFromTokenSetAndEntitlement(t *testing.T) {
	set := &TokenSet{
		AccessToken: testToken(t, "access"), RefreshToken: testToken(t, "refresh"),
		IDToken: testToken(t, "id"), ExpiresIn: 3600, Scope: RequiredScope,
	}
	doc := DocumentFromTokenSet(set, "client-1", "subject-1")
	require.Equal(t, credential.KindOAuth, doc.Kind)
	require.Equal(t, set.AccessToken, doc.OAuth.AccessToken)
	require.Equal(t, set.RefreshToken, doc.OAuth.RefreshToken)
	require.Equal(t, "client-1", doc.OAuth.RegistrationID)
	require.Equal(t, "subject-1", doc.OAuth.ExternalAccountID)
	require.Greater(t, doc.ExpiresAt(), time.Now().Add(50*time.Minute).Unix())
	require.True(t, GrantsEntitlement(doc.OAuth.Scopes))
	require.False(t, GrantsEntitlement([]string{"openid", "profile"}))
}
