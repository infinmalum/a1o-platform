package controller

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/credential"
	"github.com/infinmalum/a1o-platform/relay/upstream/openai"
)

// wizardToken derives test values at runtime so the source never carries
// literal secret material.
func wizardToken(t *testing.T, seed string) string {
	t.Helper()
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return seed + "-" + hex.EncodeToString(buf)
}

func setupOAuthWizardDB(t *testing.T) *gorm.DB {
	t.Helper()
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open("file:oauth-wizard-"+hex.EncodeToString(buf)+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UpstreamAccount{}, &model.Option{}, &model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemoryCache, previousSQLite := common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = previousRedis, previousMemoryCache, previousSQLite
		_ = sqlDB.Close()
	})
	return db
}

// oauthWizardFixture runs the connection wizard handlers against a local
// authorization server: a token endpoint that records exchange requests and a
// JWKS endpoint, both under one issuer.
type oauthWizardFixture struct {
	t      *testing.T
	db     *gorm.DB
	store  *credential.Store
	server *httptest.Server
	key    *rsa.PrivateKey
	issuer string

	mu            sync.Mutex
	tokenRequests []url.Values
	accessValue   string
	refreshValue  string
	idToken       string
	scope         string
	failCode      string
}

func newOAuthWizardFixture(t *testing.T) *oauthWizardFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setupOAuthWizardDB(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	buf := make([]byte, 32)
	_, err = rand.Read(buf)
	require.NoError(t, err)
	keys, err := credential.ParseKeyVersionList("1=" + hex.EncodeToString(buf))
	require.NoError(t, err)
	store, err := credential.NewStore(keys, 1)
	require.NoError(t, err)
	fixture := &oauthWizardFixture{
		t: t, store: store, key: key,
		accessValue: wizardToken(t, "access"),
		scope:       openai.RequiredScope,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fixture.mu.Lock()
		fixture.tokenRequests = append(fixture.tokenRequests, r.PostForm)
		access, refresh, idToken, scope, failCode := fixture.accessValue, fixture.refreshValue, fixture.idToken, fixture.scope, fixture.failCode
		fixture.mu.Unlock()
		if failCode != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": failCode})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": refresh,
			"id_token":      idToken,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         scope,
		})
	})
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "alg": "RS256", "kid": "test-key",
				"n": base64.RawURLEncoding.EncodeToString(fixture.key.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(fixture.key.PublicKey.E)).Bytes()),
			}},
		})
	})
	fixture.server = httptest.NewServer(mux)
	fixture.issuer = fixture.server.URL
	t.Cleanup(fixture.server.Close)

	previousClient := oauthClientFactory
	oauthClientFactory = func(hostID string) *openai.Client {
		return &openai.Client{
			Endpoints: openai.Endpoints{
				Authorize: fixture.server.URL + "/authorize",
				Token:     fixture.server.URL + "/token",
				JWKS:      fixture.server.URL + "/jwks.json",
				Issuer:    fixture.issuer,
			},
			HTTP:        fixture.server.Client(),
			AgentName:   "A1O Platform Test",
			HostID:      hostID,
			RedirectURI: openai.DefaultRedirectURI,
		}
	}
	previousStore := credentialStoreForWrite
	credentialStoreForWrite = func() (*credential.Store, error) { return fixture.store, nil }
	t.Cleanup(func() {
		oauthClientFactory = previousClient
		credentialStoreForWrite = previousStore
	})
	return fixture
}

func (f *oauthWizardFixture) mintIDToken(nonce, audience string) string {
	f.t.Helper()
	claims := jwt.MapClaims{
		"iss": f.issuer, "aud": audience, "sub": "subject-1",
		"email": "user@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
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

func (f *oauthWizardFixture) setMintedIDToken(nonce, audience string) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idToken = f.mintIDToken(nonce, audience)
}

func (f *oauthWizardFixture) exchangeRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.tokenRequests...)
}

type oauthWizardResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (f *oauthWizardFixture) call(handler gin.HandlerFunc, body any) oauthWizardResponse {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(f.t, err)
		reader = bytes.NewReader(payload)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/upstream_account/oauth", reader)
	if body != nil {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Set(ctxkey.Id, 1)
	handler(c)
	var response oauthWizardResponse
	require.NoError(f.t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

type wizardSession struct {
	SessionID    string `json:"session_id"`
	AuthorizeURL string `json:"authorize_url"`
	RedirectURI  string `json:"redirect_uri"`
	ExpiresIn    int    `json:"expires_in"`
}

func (f *oauthWizardFixture) start(payload any) wizardSession {
	f.t.Helper()
	response := f.call(StartUpstreamAccountOAuth, payload)
	require.True(f.t, response.Success, response.Message)
	var session wizardSession
	require.NoError(f.t, json.Unmarshal(response.Data, &session))
	return session
}

// authorizeQuery parses the browser URL handed to the administrator.
func (f *oauthWizardFixture) authorizeQuery(session wizardSession) url.Values {
	f.t.Helper()
	parsed, err := url.Parse(session.AuthorizeURL)
	require.NoError(f.t, err)
	return parsed.Query()
}

func wizardCallback(query url.Values, code, clientID string) string {
	values := url.Values{"code": {code}, "state": {query.Get("state")}}
	if clientID != "" {
		values.Set("client_id", clientID)
	}
	return openai.DefaultRedirectURI + "?" + values.Encode()
}

func (f *oauthWizardFixture) complete(payload any) oauthWizardResponse {
	f.t.Helper()
	return f.call(CompleteUpstreamAccountOAuth, payload)
}

// connect runs the whole first sign-in: start, paste back a callback that
// carries the issued client ID, and complete. It also returns the authorize
// URL query for assertions about what the browser round carried.
func (f *oauthWizardFixture) connect(name string) (model.UpstreamAccountDTO, url.Values) {
	f.t.Helper()
	session := f.start(map[string]any{})
	query := f.authorizeQuery(session)
	issued := "oaiapp-" + wizardToken(f.t, "issued")
	f.setMintedIDToken(query.Get("nonce"), issued)
	payload := map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(f.t, "code"), issued),
	}
	if name != "" {
		payload["name"] = name
	}
	response := f.complete(payload)
	require.True(f.t, response.Success, response.Message)
	var dto model.UpstreamAccountDTO
	require.NoError(f.t, json.Unmarshal(response.Data, &dto))
	return dto, query
}

func (f *oauthWizardFixture) manageLogs() []model.Log {
	f.t.Helper()
	var logs []model.Log
	require.NoError(f.t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
	return logs
}

func (f *oauthWizardFixture) sealedDocument(accountID int) credential.Document {
	f.t.Helper()
	account, err := model.GetUpstreamAccountById(accountID, true)
	require.NoError(f.t, err)
	plaintext, err := f.store.Unseal(account.EncryptedCredential)
	require.NoError(f.t, err)
	document, err := credential.ParseDocument(plaintext)
	require.NoError(f.t, err)
	return document
}

func TestOAuthWizardCreatesAccount(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	dto, query := fixture.connect("My ChatGPT plan")

	require.Equal(t, "My ChatGPT plan", dto.Name)
	require.Equal(t, "openai", dto.Provider)
	require.Equal(t, openai.ProfileID, dto.Profile)
	require.Equal(t, 1, dto.AdminUserId)
	require.Equal(t, "subject-1", dto.ExternalId)
	require.NotEmpty(t, dto.RegistrationId)
	require.Equal(t, credential.KindOAuth, dto.CredentialKind)
	require.Equal(t, int64(1), dto.CredentialVersion)
	require.Greater(t, dto.CredentialExpiresAt, time.Now().Add(50*time.Minute).Unix())
	require.Equal(t, model.UpstreamAccountStatusEnabled, dto.Status)

	// The token exchange used the issued client ID, the loopback redirect,
	// the provider audience, and the PKCE verifier of the authorize URL.
	forms := fixture.exchangeRequests()
	require.Len(t, forms, 1)
	form := forms[0]
	require.Equal(t, "authorization_code", form.Get("grant_type"))
	require.Equal(t, dto.RegistrationId, form.Get("client_id"))
	require.Equal(t, openai.DefaultRedirectURI, form.Get("redirect_uri"))
	require.Equal(t, openai.Audience, form.Get("resource"))
	digest := sha256.Sum256([]byte(form.Get("code_verifier")))
	require.Equal(t, query.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(digest[:]))

	document := fixture.sealedDocument(dto.Id)
	require.Equal(t, fixture.currentAccess(), document.OAuth.AccessToken)
	require.Equal(t, dto.RegistrationId, document.OAuth.RegistrationID)
	require.Equal(t, "subject-1", document.OAuth.ExternalAccountID)
	require.Equal(t, "user@example.com", document.OAuth.Email)
	require.True(t, openai.GrantsEntitlement(document.OAuth.Scopes))

	logs := fixture.manageLogs()
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "My ChatGPT plan")
}

func (f *oauthWizardFixture) currentAccess() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accessValue
}

func TestOAuthWizardAuthorizeURLFollowsSpec(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	require.Equal(t, openai.DefaultRedirectURI, session.RedirectURI)
	require.Equal(t, 600, session.ExpiresIn)
	query := fixture.authorizeQuery(session)
	require.Equal(t, "code", query.Get("response_type"))
	require.Equal(t, openai.RegistrationClient, query.Get("client_id"))
	require.NotEmpty(t, query.Get("agent_name_hint"))
	require.NotEmpty(t, query.Get("ext_agent_host_id"))
	require.Equal(t, openai.RequiredScope, query.Get("scope"))
	require.Equal(t, openai.Audience, query.Get("resource"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.NotEmpty(t, query.Get("code_challenge"))
	require.NotEmpty(t, query.Get("state"))
	require.NotEmpty(t, query.Get("nonce"))

	// The host identifier persists across sessions.
	second := fixture.start(map[string]any{})
	require.Equal(t, query.Get("ext_agent_host_id"), fixture.authorizeQuery(second).Get("ext_agent_host_id"))
}

func TestOAuthWizardDefaultsNameToEmail(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	dto, _ := fixture.connect("")
	require.Equal(t, "user@example.com", dto.Name)
}

func TestOAuthWizardReconnectsAccount(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	created, _ := fixture.connect("Plan A")
	registration := created.RegistrationId

	require.NoError(t, model.UpdateUpstreamAccountStatus(created.Id, model.UpstreamAccountStatusReauthRequired))
	fixture.mu.Lock()
	fixture.accessValue = wizardToken(t, "rotated-access")
	fixture.mu.Unlock()

	// Reconnect keeps the saved client registration; no registration hints.
	session := fixture.start(map[string]any{"account_id": created.Id})
	query := fixture.authorizeQuery(session)
	require.Equal(t, registration, query.Get("client_id"))
	require.Empty(t, query.Get("agent_name_hint"))
	require.Empty(t, query.Get("ext_agent_host_id"))

	// The callback carries only code and state; the session supplies the
	// registration for the exchange.
	fixture.setMintedIDToken(query.Get("nonce"), registration)
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), ""),
	})
	require.True(t, response.Success, response.Message)
	var dto model.UpstreamAccountDTO
	require.NoError(t, json.Unmarshal(response.Data, &dto))
	require.Equal(t, created.Id, dto.Id)
	require.Equal(t, registration, dto.RegistrationId)
	require.Equal(t, int64(2), dto.CredentialVersion)
	require.Equal(t, model.UpstreamAccountStatusEnabled, dto.Status)

	forms := fixture.exchangeRequests()
	require.Len(t, forms, 2)
	require.Equal(t, registration, forms[1].Get("client_id"))

	document := fixture.sealedDocument(created.Id)
	require.Equal(t, fixture.currentAccess(), document.OAuth.AccessToken)

	logs := fixture.manageLogs()
	require.Len(t, logs, 2)
	require.Contains(t, logs[1].Content, "重新连接")
}

func TestOAuthWizardRejectsDifferentIssuedClient(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	created, _ := fixture.connect("Plan A")

	session := fixture.start(map[string]any{"account_id": created.Id})
	query := fixture.authorizeQuery(session)
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), "oaiapp-"+wizardToken(t, "imposter")),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "不同的 client ID")

	// The original credential is untouched.
	account, err := model.GetUpstreamAccountById(created.Id, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), account.CredentialVersion)
	require.Equal(t, created.RegistrationId, account.RegistrationId)
}

func TestOAuthWizardRejectsStateMismatchAndConsumesSession(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	query := fixture.authorizeQuery(session)
	fixture.setMintedIDToken(query.Get("nonce"), "oaiapp-"+wizardToken(t, "issued"))

	forged := url.Values{"code": {wizardToken(t, "code")}, "state": {wizardToken(t, "forged-state")}, "client_id": {"oaiapp-issued"}}
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": openai.DefaultRedirectURI + "?" + forged.Encode(),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "state")

	// The failed attempt consumed the session; replaying with the correct
	// callback cannot succeed.
	response = fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), "oaiapp-"+wizardToken(t, "issued")),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "过期")

	var count int64
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestOAuthWizardRejectsMissingEntitlement(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	query := fixture.authorizeQuery(session)
	fixture.mu.Lock()
	fixture.scope = "openid profile email offline_access"
	fixture.mu.Unlock()
	fixture.setMintedIDToken(query.Get("nonce"), "oaiapp-"+wizardToken(t, "issued"))
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), "oaiapp-"+wizardToken(t, "issued")),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, openai.EntitlementScope)

	var count int64
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestOAuthWizardRejectsDeniedCallback(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	denied := url.Values{"error": {"access_denied"}, "state": {fixture.authorizeQuery(session).Get("state")}}
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": openai.DefaultRedirectURI + "?" + denied.Encode(),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "access_denied")
}

func TestOAuthWizardExpiresStaleSession(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	upstreamOAuthSessions.mu.Lock()
	for _, candidate := range upstreamOAuthSessions.sessions {
		if candidate.ID == session.SessionID {
			candidate.ExpiresAt = time.Now().Add(-time.Second)
		}
	}
	upstreamOAuthSessions.mu.Unlock()

	query := fixture.authorizeQuery(session)
	fixture.setMintedIDToken(query.Get("nonce"), "oaiapp-"+wizardToken(t, "issued"))
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), "oaiapp-"+wizardToken(t, "issued")),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "过期")
}

func TestOAuthWizardRejectsCallbackWithoutClientID(t *testing.T) {
	fixture := newOAuthWizardFixture(t)
	session := fixture.start(map[string]any{})
	query := fixture.authorizeQuery(session)
	response := fixture.complete(map[string]any{
		"session_id":   session.SessionID,
		"callback_url": wizardCallback(query, wizardToken(t, "code"), ""),
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "client ID")
}

func TestStartOAuthValidatesInput(t *testing.T) {
	fixture := newOAuthWizardFixture(t)

	response := fixture.call(StartUpstreamAccountOAuth, map[string]any{"profile": "not-a-profile"})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "未知的 Profile")

	// An account without a saved registration cannot be reconnected.
	account := model.UpstreamAccount{
		Name: "legacy", Provider: "openai", Profile: openai.ProfileID,
		CredentialKind: credential.KindOAuth, CredentialVersion: 1,
		Status: model.UpstreamAccountStatusEnabled,
	}
	require.NoError(t, account.Insert())
	response = fixture.call(StartUpstreamAccountOAuth, map[string]any{"account_id": account.Id})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "客户端注册 ID")

	// A profile mismatch is rejected.
	mismatch := model.UpstreamAccount{
		Name: "other", Provider: "other", Profile: "other_profile",
		RegistrationId: wizardToken(t, "reg"), CredentialVersion: 1,
		Status: model.UpstreamAccountStatusEnabled,
	}
	require.NoError(t, mismatch.Insert())
	response = fixture.call(StartUpstreamAccountOAuth, map[string]any{"account_id": mismatch.Id})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "不匹配")

	// A nonexistent account surfaces a load error.
	response = fixture.call(StartUpstreamAccountOAuth, map[string]any{"account_id": 999})
	require.False(t, response.Success)

	// A missing body defaults to the ChatGPT plan flow.
	session := fixture.start(nil)
	require.NotEmpty(t, session.SessionID)
	require.Contains(t, session.AuthorizeURL, "/authorize")
}

func TestCompleteOAuthValidatesInput(t *testing.T) {
	fixture := newOAuthWizardFixture(t)

	response := fixture.complete(map[string]any{"callback_url": openai.DefaultRedirectURI})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "session_id")

	response = fixture.complete(map[string]any{"session_id": "missing", "callback_url": openai.DefaultRedirectURI})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "过期")
}
