// Package google implements the Gemini CLI connection used by the gemini_cli
// profile: the Google OAuth flow of the public Gemini CLI client, the
// rotating refresh, and the Code Assist project onboarding that binds the
// credential to the cloudaicompanion project every upstream request carries.
package google

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
const ProfileID = "gemini_cli"

const (
	// ClientID and ClientSecret are the installed-app credentials embedded in
	// the public Gemini CLI client; every sign-in of this gateway reuses them.
	ClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	ClientSecret = "GOCSPX-4uHgMPm-1o7Sk-geV6u8yMc0m"

	// RequiredScope requests cloud platform access (Code Assist) and the
	// account identity.
	RequiredScope = "https://www.googleapis.com/auth/cloud-platform" +
		" https://www.googleapis.com/auth/userinfo.email" +
		" https://www.googleapis.com/auth/userinfo.profile"

	// DefaultRedirectURI is a loopback callback. Google redirects the browser
	// there with no listener by design; the administrator pastes the full
	// callback URL back.
	DefaultRedirectURI = "http://localhost:8085/oauth2callback"

	// DefaultTier is the onboarding tier used when the provider does not
	// report a current tier for the account.
	DefaultTier = "free-tier"
)

// Endpoints are the OAuth and Code Assist server addresses. Production uses
// Google's documented defaults; offline tests replace them with a local
// server.
type Endpoints struct {
	Authorize    string
	Token        string
	Userinfo     string
	CodeAssist   string
	LoadCode     string
	OnboardUser  string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Authorize:   "https://accounts.google.com/o/oauth2/v2/auth",
		Token:       "https://oauth2.googleapis.com/token",
		Userinfo:    "https://www.googleapis.com/oauth2/v3/userinfo",
		CodeAssist:  "https://cloudcode-pa.googleapis.com",
		LoadCode:    "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
		OnboardUser: "https://cloudcode-pa.googleapis.com/v1internal:onboardUser",
	}
}

// Client speaks the Gemini CLI OAuth flow and the Code Assist onboarding.
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

// BuildAuthorization composes the browser URL of a fresh sign-in. Consent is
// forced so Google always issues a refresh token, even to accounts that
// granted this client before.
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
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	return &AuthorizationRequest{
		URL: c.Endpoints.Authorize + "?" + query.Encode(),
		Verifier: verifier, State: state,
	}, nil
}

// Callback is the parsed loopback redirect.
type Callback struct {
	Code  string
	State string
}

// ParseCallback reads the pasted redirect URL. A denied authorization
// returns the provider error instead of a code.
func ParseCallback(pasted string) (*Callback, error) {
	pasted = strings.TrimSpace(pasted)
	parsed, err := url.Parse(pasted)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("请粘贴完整的回调 URL（浏览器地址栏内容）")
	}
	query := parsed.Query()
	callback := &Callback{Code: query.Get("code"), State: query.Get("state")}
	if errValue := query.Get("error"); errValue != "" {
		return callback, fmt.Errorf("authorization was denied: %s", errValue)
	}
	if callback.Code == "" {
		return callback, errors.New("回调中没有授权码，请重新发起连接并粘贴完整 URL")
	}
	return callback, nil
}

// TokenSet is the token endpoint response.
type TokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
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
// The redirect URI must match the authorization request byte for byte.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (*TokenSet, error) {
	if redirectURI == "" {
		redirectURI = c.RedirectURI
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return c.postToken(ctx, form)
}

// Userinfo is the identity the provider reports for the connected account.
type Userinfo struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
}

// FetchUserinfo resolves the account identity of a fresh access token.
func (c *Client) FetchUserinfo(ctx context.Context, accessToken string) (*Userinfo, error) {
	data, err := c.getJSON(ctx, c.Endpoints.Userinfo, accessToken)
	if err != nil {
		return nil, err
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

func (c *Client) getJSON(ctx context.Context, endpoint, accessToken string) ([]byte, error) {
	if parsed, err := url.Parse(endpoint); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("endpoint must be an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		return nil, fmt.Errorf("endpoint returned %d", res.StatusCode)
	}
	return data, readErr
}

// codeAssistMetadata is the client description every Code Assist call
// carries; the server rejects requests without it.
func codeAssistMetadata() map[string]any {
	return map[string]any{
		"ideType":             "IDE_UNSPECIFIED",
		"platform":            "PLATFORM_UNSPECIFIED",
		"pluginType":          "GEMINI",
		"dupeProjectAllowed":  true,
	}
}

// ResolveProject binds the account to its cloudaicompanion project. Google
// reports the project on loadCodeAssist for accounts that onboarded before;
// fresh accounts go through onboardUser until the project is assigned.
func (c *Client) ResolveProject(ctx context.Context, accessToken string) (string, error) {
	if parsed, err := url.Parse(c.Endpoints.CodeAssist); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("code assist endpoint must be an http(s) URL")
	}
	loadPayload, err := json.Marshal(map[string]any{"metadata": codeAssistMetadata()})
	if err != nil {
		return "", err
	}
	body, err := c.postCodeAssist(ctx, c.Endpoints.LoadCode, accessToken, loadPayload)
	if err != nil {
		return "", err
	}
	var loaded struct {
		CurrentTier *struct {
			ID string `json:"id"`
		} `json:"currentTier"`
		Project json.RawMessage `json:"cloudaicompanionProject"`
	}
	if err := json.Unmarshal(body, &loaded); err != nil {
		return "", fmt.Errorf("loadCodeAssist response is not valid JSON: %w", err)
	}
	if project := decodeProject(loaded.Project); project != "" {
		return project, nil
	}
	tier := DefaultTier
	if loaded.CurrentTier != nil && loaded.CurrentTier.ID != "" {
		tier = loaded.CurrentTier.ID
	}
	onboardPayload, err := json.Marshal(map[string]any{
		"tierId":   tier,
		"metadata": codeAssistMetadata(),
	})
	if err != nil {
		return "", err
	}
	// Onboarding completes asynchronously on the provider side; poll briefly.
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		body, err := c.postCodeAssist(ctx, c.Endpoints.OnboardUser, accessToken, onboardPayload)
		if err != nil {
			return "", err
		}
		var onboarded struct {
			Done     bool            `json:"done"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(body, &onboarded); err != nil {
			return "", fmt.Errorf("onboardUser response is not valid JSON: %w", err)
		}
		var project json.RawMessage
		if len(onboarded.Response) > 0 {
			var nested struct {
				Project json.RawMessage `json:"cloudaicompanionProject"`
			}
			if json.Unmarshal(onboarded.Response, &nested) == nil {
				project = nested.Project
			}
		}
		if resolved := decodeProject(project); resolved != "" {
			return resolved, nil
		}
	}
	return "", errors.New("未能获取 Code Assist 项目，请稍后重试")
}

// decodeProject accepts both the string and the {id: ...} project shapes the
// API has used across versions.
func decodeProject(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var object struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.ID
	}
	return ""
}

func (c *Client) postCodeAssist(ctx context.Context, endpoint, accessToken string, payload []byte) ([]byte, error) {
	if parsed, err := url.Parse(endpoint); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("code assist endpoint must be an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GeminiCLI/v25.11.0 (linux; x64)")
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("code assist endpoint returned %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, readErr
}

// Refresher rotates the OAuth credential of a gemini_cli account by
// implementing credential.Refresher for the coordinator. Google keeps the
// refresh token stable, so the project binding survives every rotation.
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
	set, err := client.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.OAuth.RefreshToken},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
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
	// Google rarely rotates the refresh token; a response without one keeps
	// the current token rather than dropping it.
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
func DocumentFromTokenSet(set *TokenSet, identity *Userinfo, project string) credential.Document {
	expiresAt := int64(0)
	if set.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(set.ExpiresIn) * time.Second).Unix()
	}
	scopes := strings.Fields(set.Scope)
	if len(scopes) == 0 {
		scopes = strings.Fields(RequiredScope)
	}
	return credential.Document{
		Kind: credential.KindOAuth,
		OAuth: &credential.OAuthDocument{
			AccessToken:       set.AccessToken,
			RefreshToken:      set.RefreshToken,
			ExpiresAt:         expiresAt,
			Scopes:            scopes,
			RegistrationID:    ClientID,
			ExternalAccountID: identity.Subject,
			Email:             identity.Email,
			Project:           project,
		},
	}
}

// Connect implements the upstream.Connector flow of gemini_cli.
type Connect struct{}

// Begin builds the Google authorization URL.
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

// Complete validates the pasted loopback callback, exchanges the credential,
// resolves the account identity, and binds the Code Assist project.
func (Connect) Complete(ctx context.Context, session *upstream.ConnectorSession, callbackURL string) (*upstream.ConnectorResult, error) {
	callback, err := ParseCallback(callbackURL)
	if err != nil {
		return nil, err
	}
	if session.State != "" && callback.State != session.State {
		return nil, errors.New("state 校验失败，请重新发起连接")
	}
	client := ClientFactory()
	tokenSet, err := client.Exchange(ctx, callback.Code, session.Verifier, session.RedirectURI)
	if err != nil {
		return nil, err
	}
	if tokenSet.AccessToken == "" {
		return nil, errors.New("token 响应缺少 access token")
	}
	if tokenSet.RefreshToken == "" {
		return nil, errors.New("Google 未返回 refresh token，请在授权页确认勾选同意后重试")
	}
	identity, err := client.FetchUserinfo(ctx, tokenSet.AccessToken)
	if err != nil {
		return nil, err
	}
	project, err := client.ResolveProject(ctx, tokenSet.AccessToken)
	if err != nil {
		return nil, err
	}
	return &upstream.ConnectorResult{
		Document:       DocumentFromTokenSet(tokenSet, identity, project),
		ExternalID:     identity.Subject,
		Email:          identity.Email,
		DisplayName:    identity.Email,
		RegistrationID: ClientID,
	}, nil
}
