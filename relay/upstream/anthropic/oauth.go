// Package anthropic implements the Claude Code connection used by the
// anthropic_claude_code profile: the OAuth authorization flow of the public
// Claude Code client, the console token endpoint, and the rotating refresh.
// The credential serves the Anthropic Messages wire as a bearer token.
package anthropic

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

	"github.com/infinmalum/a1o-platform/relay/credential"
	"github.com/infinmalum/a1o-platform/relay/upstream"
)

// ProfileID is the upstream profile served by this package.
const ProfileID = "anthropic_claude_code"

const (
	// ClientID is the OAuth client identifier of the public Claude Code
	// client; every sign-in of this gateway reuses it.
	ClientID = "9d1c250a-e61b-44d9-88ed-5944d9642fb5"

	// RequiredScope requests identity and inference on the connected
	// subscription plan.
	RequiredScope = "org:create_api_key user:profile user:inference"

	// DefaultRedirectURI is Claude Code's console callback. The hosted page
	// has no listener by provider design; the administrator copies the code
	// it displays and pastes it back.
	DefaultRedirectURI = "https://console.anthropic.com/oauth/code/callback"
)

// Endpoints are the OAuth server addresses. Production uses Anthropic's
// documented defaults; offline tests replace them with a local server.
type Endpoints struct {
	Authorize string
	Token     string
	Userinfo  string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Authorize: "https://claude.ai/oauth/authorize",
		Token:     "https://console.anthropic.com/v1/oauth/token",
		Userinfo:  "https://api.anthropic.com/v1/oauth/userinfo",
	}
}

// Client speaks the Claude Code OAuth flow.
type Client struct {
	Endpoints   Endpoints
	HTTP        *http.Client
	RedirectURI string
}

const oauthHTTPTimeout = 30 * time.Second

func NewClient() *Client {
	return &Client{
		Endpoints:   DefaultEndpoints(),
		HTTP:        &http.Client{Timeout: oauthHTTPTimeout},
		RedirectURI: DefaultRedirectURI,
	}
}

// ClientFactory builds the OAuth client for connection flows; tests replace
// it to point at a local server.
var ClientFactory = NewClient

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
}

// BuildAuthorization composes the browser URL of a fresh sign-in.
func (c *Client) BuildAuthorization() (*AuthorizationRequest, error) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {c.RedirectURI},
		"scope":                 {RequiredScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return &AuthorizationRequest{
		URL: c.Endpoints.Authorize + "?" + query.Encode(),
		Verifier: verifier, State: state,
	}, nil
}

// Callback is the pasted authorization result. The console callback page
// shows the pair as `code#state`; a full redirect URL with query parameters
// is accepted as well.
type Callback struct {
	Code  string
	State string
}

// ParseCallback reads the pasted code pair or redirect URL. A denied
// authorization returns the provider error instead of a code.
func ParseCallback(pasted string) (*Callback, error) {
	pasted = strings.TrimSpace(pasted)
	callback := &Callback{}
	if parsed, err := url.Parse(pasted); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		query := parsed.Query()
		callback.Code = query.Get("code")
		callback.State = query.Get("state")
		if errValue := query.Get("error"); errValue != "" {
			return callback, fmt.Errorf("authorization was denied: %s", errValue)
		}
	} else {
		// The console page displays the raw `code#state` pair.
		if index := strings.Index(pasted, "#"); index >= 0 {
			callback.Code, callback.State = pasted[:index], pasted[index+1:]
		} else {
			callback.Code = pasted
		}
	}
	if callback.Code == "" {
		return callback, errors.New("回调中没有授权码，请粘贴完整内容")
	}
	return callback, nil
}

// TokenSet is the token endpoint response.
type TokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

// TokenError reports a non-200 token endpoint response.
type TokenError struct {
	Status      int    `json:"-"`
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

func (c *Client) postToken(ctx context.Context, payload map[string]any) (*TokenSet, error) {
	if parsed, err := url.Parse(c.Endpoints.Token); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("oauth token endpoint must be an http(s) URL")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.Token, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var tokenErr TokenError
		_ = json.Unmarshal(data, &tokenErr)
		tokenErr.Status = res.StatusCode
		return nil, &tokenErr
	}
	if readErr != nil {
		return nil, readErr
	}
	var set TokenSet
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("token response is not valid JSON: %w", err)
	}
	return &set, nil
}

// Exchange redeems the authorization code with the retained PKCE verifier.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*TokenSet, error) {
	return c.postToken(ctx, map[string]any{
		"grant_type":    "authorization_code",
		"client_id":     ClientID,
		"code":          code,
		"redirect_uri":  c.RedirectURI,
		"code_verifier": verifier,
		"state":         "",
	})
}

// Userinfo is the identity the provider reports for the connected account.
type Userinfo struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

// FetchUserinfo resolves the account identity of a fresh access token.
func (c *Client) FetchUserinfo(ctx context.Context, accessToken string) (*Userinfo, error) {
	if parsed, err := url.Parse(c.Endpoints.Userinfo); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("oauth userinfo endpoint must be an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoints.Userinfo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth userinfo endpoint returned %d", res.StatusCode)
	}
	if readErr != nil {
		return nil, readErr
	}
	var identity Userinfo
	if err := json.Unmarshal(data, &identity); err != nil {
		return nil, fmt.Errorf("userinfo response is not valid JSON: %w", err)
	}
	if identity.Subject == "" {
		return nil, errors.New("userinfo response is missing a subject")
	}
	return &identity, nil
}

// Refresher rotates the OAuth credential of an anthropic_claude_code account
// by implementing credential.Refresher for the coordinator.
type Refresher struct {
	Client *Client
}

func (r Refresher) Refresh(ctx context.Context, accountID int, current credential.Document) (credential.Document, error) {
	if current.OAuth == nil {
		return credential.Document{}, errors.New("oauth credential is missing")
	}
	if current.OAuth.RefreshToken == "" {
		return credential.Document{}, credential.ErrReauthorization
	}
	client := r.Client
	if client == nil {
		client = NewClient()
	}
	set, err := client.postToken(ctx, map[string]any{
		"grant_type":    "refresh_token",
		"client_id":     ClientID,
		"refresh_token": current.OAuth.RefreshToken,
	})
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
	// The refresh token rotates on every successful refresh; a response
	// without one keeps the current token rather than dropping it.
	if set.RefreshToken != "" {
		next.RefreshToken = set.RefreshToken
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
func DocumentFromTokenSet(set *TokenSet, identity *Userinfo) credential.Document {
	expiresAt := int64(0)
	if set.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(set.ExpiresIn) * time.Second).Unix()
	}
	return credential.Document{
		Kind: credential.KindOAuth,
		OAuth: &credential.OAuthDocument{
			AccessToken:       set.AccessToken,
			RefreshToken:      set.RefreshToken,
			ExpiresAt:         expiresAt,
			Scopes:            strings.Fields(set.Scope),
			RegistrationID:    ClientID,
			ExternalAccountID: identity.Subject,
			Email:             identity.Email,
		},
	}
}

// Connect implements the upstream.Connector flow of anthropic_claude_code.
type Connect struct{}

// Begin builds the Claude Code authorization URL.
func (Connect) Begin(registrationID string) (string, *upstream.ConnectorSession, error) {
	client := ClientFactory()
	authorization, err := client.BuildAuthorization()
	if err != nil {
		return "", nil, err
	}
	return authorization.URL, &upstream.ConnectorSession{
		Verifier: authorization.Verifier, State: authorization.State,
		RedirectURI: client.RedirectURI, RegistrationID: ClientID,
	}, nil
}

// Complete validates the pasted code pair, exchanges the credential, and
// resolves the account identity.
func (Connect) Complete(ctx context.Context, session *upstream.ConnectorSession, callbackURL string) (*upstream.ConnectorResult, error) {
	callback, err := ParseCallback(callbackURL)
	if err != nil {
		return nil, err
	}
	if session.State != "" && callback.State != session.State {
		return nil, errors.New("state 校验失败，请重新发起连接")
	}
	client := ClientFactory()
	tokenSet, err := client.Exchange(ctx, callback.Code, session.Verifier)
	if err != nil {
		return nil, err
	}
	if tokenSet.AccessToken == "" {
		return nil, errors.New("token 响应缺少 access token")
	}
	identity, err := client.FetchUserinfo(ctx, tokenSet.AccessToken)
	if err != nil {
		return nil, err
	}
	displayName := identity.Email
	if displayName == "" {
		displayName = identity.Name
	}
	return &upstream.ConnectorResult{
		Document:       DocumentFromTokenSet(tokenSet, identity),
		ExternalID:     identity.Subject,
		Email:          identity.Email,
		DisplayName:    displayName,
		RegistrationID: ClientID,
	}, nil
}
