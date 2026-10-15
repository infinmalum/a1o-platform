// Package openai implements the Sign in with ChatGPT connection used by the
// chatgpt_plan_public profile: the OAuth authorization flow with dynamic
// client registration, the rotating refresh, ID-token validation against the
// server's JWKS, and the subscription capability matrix of the public
// Responses endpoint.
package openai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/infinmalum/one-gateway/relay/credential"
)

// ProfileID is the upstream profile served by this package.
const ProfileID = "chatgpt_plan_public"

const (
	// RegistrationClient starts the first sign-in. The authorization server
	// issues the real client ID during registration; that ID must be
	// persisted and reused for every later exchange.
	RegistrationClient = "dynamic_agent_client"

	// Audience is the resource the tokens are issued for: the public
	// Responses endpoint.
	Audience = "https://api.openai.com/v1"

	// RequiredScope requests identity, offline access, and the ChatGPT plan
	// entitlement.
	RequiredScope = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

	// EntitlementScope must be granted before plan-backed inference is
	// allowed; a valid identity alone does not authorize plan usage.
	EntitlementScope = "chatgpt.tokens.use.direct"

	// DefaultRedirectURI is the documented loopback callback. Only the port
	// may vary between sign-ins of the same registration.
	DefaultRedirectURI = "http://127.0.0.1:1455/auth/callback"
)

// Endpoints are the OAuth server addresses. Production uses OpenAI's
// documented defaults; offline tests replace them with a local server.
type Endpoints struct {
	Authorize string
	Token     string
	JWKS      string
	Issuer    string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Authorize: "https://auth.openai.com/api/accounts/authorize",
		Token:     "https://auth.openai.com/api/accounts/oauth/token",
		JWKS:      "https://auth.openai.com/.well-known/jwks.json",
		Issuer:    "https://auth.openai.com",
	}
}

// Client speaks the Sign in with ChatGPT OAuth flow.
type Client struct {
	Endpoints   Endpoints
	HTTP        *http.Client
	AgentName   string // agent_name_hint, sent only on initial registration
	HostID      string // ext_agent_host_id, stable per host
	RedirectURI string
}

// oauthHTTPTimeout bounds every OAuth endpoint call. The refresh path runs
// under the coordinator's per-account flight, so a hung token endpoint must
// not pin the account indefinitely.
const oauthHTTPTimeout = 30 * time.Second

// NewClient builds a client with the production endpoints.
func NewClient(agentName, hostID string) *Client {
	return &Client{
		Endpoints:   DefaultEndpoints(),
		HTTP:        &http.Client{Timeout: oauthHTTPTimeout},
		AgentName:   agentName,
		HostID:      hostID,
		RedirectURI: DefaultRedirectURI,
	}
}

func randomToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewPKCE returns a verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	verifier, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// AuthorizationRequest describes one browser authorization attempt.
type AuthorizationRequest struct {
	URL      string
	Verifier string
	State    string
	Nonce    string
}

// BuildAuthorization composes the browser URL. An empty registrationID starts
// the first sign-in, which performs dynamic client registration and must
// persist the client ID issued on the callback.
func (c *Client) BuildAuthorization(registrationID string) (*AuthorizationRequest, error) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"response_type":         {"code"},
		"redirect_uri":          {c.RedirectURI},
		"scope":                 {RequiredScope},
		"resource":              {Audience},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if registrationID == "" {
		if c.AgentName == "" || c.HostID == "" {
			return nil, errors.New("agent name and host ID are required for client registration")
		}
		query.Set("client_id", RegistrationClient)
		query.Set("agent_name_hint", c.AgentName)
		query.Set("ext_agent_host_id", c.HostID)
	} else {
		query.Set("client_id", registrationID)
	}
	return &AuthorizationRequest{
		URL:      c.Endpoints.Authorize + "?" + query.Encode(),
		Verifier: verifier, State: state, Nonce: nonce,
	}, nil
}

// Callback is the parsed loopback redirect.
type Callback struct {
	Code  string
	State string
	// IssuedClientID is the client ID issued during registration. OpenAI
	// returns it on the first callback; later sign-ins reuse the saved one.
	IssuedClientID string
	Err            string
}

// ParseCallback reads the loopback redirect. A denied authorization returns
// the provider error instead of a code.
func ParseCallback(query url.Values) (*Callback, error) {
	callback := &Callback{
		Code:           query.Get("code"),
		State:          query.Get("state"),
		IssuedClientID: firstNonEmpty(query.Get("client_id"), query.Get("issued_client_id")),
		Err:            query.Get("error"),
	}
	if callback.Err != "" {
		return callback, fmt.Errorf("authorization was denied: %s", callback.Err)
	}
	if callback.Code == "" {
		return callback, errors.New("callback is missing an authorization code")
	}
	return callback, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// TokenSet is the token endpoint response.
type TokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// TokenError reports a non-200 token endpoint response.
type TokenError struct {
	Status      int
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *TokenError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("oauth token endpoint returned %d (%s): %s", e.Status, e.Code, e.Description)
	}
	return fmt.Sprintf("oauth token endpoint returned %d (%s)", e.Status, e.Code)
}

// IsReauthorization reports errors that permanently invalidate the current
// credential: a rejected grant means the account must be connected again.
func (e *TokenError) IsReauthorization() bool {
	switch e.Code {
	case "invalid_grant", "invalid_client", "unauthorized_client":
		return true
	}
	return false
}

func (c *Client) postToken(ctx context.Context, form url.Values) (*TokenSet, error) {
	if parsed, err := url.Parse(c.Endpoints.Token); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("oauth token endpoint must be an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var tokenErr TokenError
		_ = json.Unmarshal(body, &tokenErr)
		tokenErr.Status = res.StatusCode
		return nil, &tokenErr
	}
	if readErr != nil {
		return nil, readErr
	}
	var set TokenSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("token response is not valid JSON: %w", err)
	}
	return &set, nil
}

// Exchange redeems an authorization code. The client ID, verifier, and
// redirect URI must match the authorization request.
func (c *Client) Exchange(ctx context.Context, clientID, code, verifier, redirectURI string) (*TokenSet, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
		"resource":      {Audience},
	}
	return c.postToken(ctx, form)
}

// Refresher rotates the OAuth credential of a chatgpt_plan_public account by
// implementing credential.Refresher for the coordinator.
type Refresher struct {
	Client *Client
}

func (r Refresher) Refresh(ctx context.Context, accountID int, current credential.Document) (credential.Document, error) {
	if current.OAuth == nil {
		return credential.Document{}, errors.New("oauth credential is missing")
	}
	if current.OAuth.RegistrationID == "" || current.OAuth.RefreshToken == "" {
		return credential.Document{}, credential.ErrReauthorization
	}
	client := r.Client
	if client == nil {
		client = NewClient("", "")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {current.OAuth.RegistrationID},
		"refresh_token": {current.OAuth.RefreshToken},
		"resource":      {Audience},
	}
	set, err := client.postToken(ctx, form)
	if err != nil {
		var tokenErr *TokenError
		if errors.As(err, &tokenErr) && tokenErr.IsReauthorization() {
			return credential.Document{}, credential.ErrReauthorization
		}
		return credential.Document{}, err
	}
	if set.AccessToken == "" {
		return credential.Document{}, errors.New("refresh response is missing an access token")
	}
	next := *current.OAuth
	next.AccessToken = set.AccessToken
	// The refresh token rotates with every successful refresh; a response
	// without one keeps the current token rather than dropping it.
	if set.RefreshToken != "" {
		next.RefreshToken = set.RefreshToken
	}
	if set.IDToken != "" {
		next.IDToken = set.IDToken
	}
	if set.ExpiresIn > 0 {
		next.ExpiresAt = time.Now().Add(time.Duration(set.ExpiresIn) * time.Second).Unix()
	}
	if set.Scope != "" {
		next.Scopes = strings.Fields(set.Scope)
	}
	return credential.Document{Kind: current.Kind, OAuth: &next}, nil
}

// DocumentFromTokenSet builds the credential document of a fresh sign-in.
// Sealing preserves the original JSON, so the id token and email survive
// round trips for reconnect hints and administrative display.
func DocumentFromTokenSet(set *TokenSet, registrationID, externalID string) credential.Document {
	return credential.Document{
		Kind: credential.KindOAuth,
		OAuth: &credential.OAuthDocument{
			AccessToken:       set.AccessToken,
			RefreshToken:      set.RefreshToken,
			IDToken:           set.IDToken,
			ExpiresAt:         time.Now().Add(time.Duration(set.ExpiresIn) * time.Second).Unix(),
			Scopes:            strings.Fields(set.Scope),
			RegistrationID:    registrationID,
			ExternalAccountID: externalID,
		},
	}
}

// GrantsEntitlement reports whether the granted scopes include plan usage.
func GrantsEntitlement(scopes []string) bool {
	for _, scope := range scopes {
		if scope == EntitlementScope {
			return true
		}
	}
	return false
}
