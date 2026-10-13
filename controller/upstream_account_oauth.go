package controller

// Sign in with ChatGPT connection wizard. The administrator starts a pending
// authorization session from the management UI, completes sign-in in the
// browser — whose loopback redirect has no listener by provider design when
// the gateway runs remotely — and pastes the full callback URL back. The
// gateway then validates state, exchanges the code with the retained PKCE
// verifier, verifies the ID token against the provider's JWKS, and stores the
// sealed credential. All OAuth parameters follow the official
// Sign in with ChatGPT specification; refreshes stay server-owned.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/one-gateway/common/ctxkey"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/credential"
	"github.com/infinmalum/one-gateway/relay/upstream"
	"github.com/infinmalum/one-gateway/relay/upstream/openai"
)

const (
	upstreamOAuthSessionTTL  = 10 * time.Minute
	upstreamOAuthMaxSessions = 64
	upstreamOAuthAgentName   = "One Gateway"
)

// oauthClientFactory builds the Sign in with ChatGPT client; tests replace it
// to point at a local authorization server.
var oauthClientFactory = func(hostID string) *openai.Client {
	return openai.NewClient(upstreamOAuthAgentName, hostID)
}

type upstreamOAuthSession struct {
	ID             string
	State          string
	Nonce          string
	Verifier       string
	Profile        string
	AccountID      int // reconnect target; 0 creates a new account
	RegistrationID string
	RedirectURI    string
	ExpiresAt      time.Time
}

// upstreamOAuthSessionStore keeps pending authorization sessions in memory.
// The first deployment runs one gateway replica; multi-replica coordination
// is a later reliability stage.
type upstreamOAuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*upstreamOAuthSession
}

var upstreamOAuthSessions = &upstreamOAuthSessionStore{
	sessions: map[string]*upstreamOAuthSession{},
}

func (s *upstreamOAuthSessionStore) put(session *upstreamOAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, candidate := range s.sessions {
		if now.After(candidate.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	if len(s.sessions) >= upstreamOAuthMaxSessions {
		var oldestID string
		var oldest time.Time
		for id, candidate := range s.sessions {
			if oldestID == "" || candidate.ExpiresAt.Before(oldest) {
				oldestID, oldest = id, candidate.ExpiresAt
			}
		}
		delete(s.sessions, oldestID)
	}
	s.sessions[session.ID] = session
}

// take removes a session on first use; a session completes at most one
// authorization regardless of the outcome.
func (s *upstreamOAuthSessionStore) take(id string) (*upstreamOAuthSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	return session, ok
}

type upstreamOAuthSessionPayload struct {
	Profile   string `json:"profile"`
	AccountID int    `json:"account_id"`
}

type upstreamOAuthSessionResponse struct {
	SessionID    string `json:"session_id"`
	AuthorizeURL string `json:"authorize_url"`
	RedirectURI  string `json:"redirect_uri"`
	ExpiresIn    int    `json:"expires_in"`
}

// StartUpstreamAccountOAuth creates a pending authorization session and
// returns the browser URL. AccountID > 0 reconnects an existing account with
// its saved client registration; otherwise the flow performs the provider's
// dynamic client registration.
func StartUpstreamAccountOAuth(c *gin.Context) {
	var payload upstreamOAuthSessionPayload
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.Profile == "" {
		payload.Profile = openai.ProfileID
	}
	profile, ok := upstream.Lookup(payload.Profile)
	if !ok || !profile.Subscription {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "未知的 Profile：" + payload.Profile})
		return
	}
	if _, ok := upstream.RefresherFor(payload.Profile); !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Profile " + payload.Profile + " 未注册凭据刷新器"})
		return
	}
	registrationID := ""
	if payload.AccountID > 0 {
		account, err := model.GetUpstreamAccountById(payload.AccountID, false)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "上游账号不存在：" + err.Error()})
			return
		}
		if account.Profile != payload.Profile {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("账号 %d 的 Profile 是 %s，与此连接流程不匹配", account.Id, account.Profile)})
			return
		}
		registrationID = account.RegistrationId
		if registrationID == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "该账号没有已保存的客户端注册 ID，请改用新建连接"})
			return
		}
	}
	hostID, err := model.GetOrCreateUpstreamOAuthHostID()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "生成主机标识失败：" + err.Error()})
		return
	}
	client := oauthClientFactory(hostID)
	authorization, err := client.BuildAuthorization(registrationID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "构建授权链接失败：" + err.Error()})
		return
	}
	sessionID, err := randomSessionID()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "生成会话失败：" + err.Error()})
		return
	}
	upstreamOAuthSessions.put(&upstreamOAuthSession{
		ID: sessionID, State: authorization.State, Nonce: authorization.Nonce,
		Verifier: authorization.Verifier, Profile: payload.Profile,
		AccountID: payload.AccountID, RegistrationID: registrationID,
		RedirectURI: client.RedirectURI, ExpiresAt: time.Now().Add(upstreamOAuthSessionTTL),
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "",
		"data": upstreamOAuthSessionResponse{
			SessionID: sessionID, AuthorizeURL: authorization.URL,
			RedirectURI: client.RedirectURI, ExpiresIn: int(upstreamOAuthSessionTTL.Seconds()),
		}})
}

func randomSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type upstreamOAuthCompletePayload struct {
	SessionID   string `json:"session_id"`
	CallbackURL string `json:"callback_url"`
	Name        string `json:"name"`
}

// CompleteUpstreamAccountOAuth finishes a pending session with the loopback
// callback URL pasted back by the administrator: it validates state, resolves
// the issued client registration, exchanges the code, verifies identity and
// entitlement, and creates or reconnects the sealed account.
func CompleteUpstreamAccountOAuth(c *gin.Context) {
	var payload upstreamOAuthCompletePayload
	if err := c.ShouldBindJSON(&payload); err != nil || payload.SessionID == "" || payload.CallbackURL == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "缺少 session_id 或 callback_url"})
		return
	}
	session, ok := upstreamOAuthSessions.take(payload.SessionID)
	if !ok || time.Now().After(session.ExpiresAt) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权会话不存在或已过期，请重新发起连接"})
		return
	}
	parsed, err := url.Parse(payload.CallbackURL)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "回调 URL 无法解析"})
		return
	}
	callback, err := openai.ParseCallback(parsed.Query())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if callback.State != session.State {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "state 校验失败，请重新发起连接"})
		return
	}
	clientID := callback.IssuedClientID
	if clientID == "" {
		clientID = session.RegistrationID
	}
	if clientID == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "回调中没有签发的 client ID，动态注册未完成，请重新发起连接"})
		return
	}
	if session.RegistrationID != "" && callback.IssuedClientID != "" && callback.IssuedClientID != session.RegistrationID {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权服务器返回了不同的 client ID，已拒绝重连以保护原注册"})
		return
	}
	ctx := c.Request.Context()
	hostID, err := model.GetOrCreateUpstreamOAuthHostID()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "读取主机标识失败：" + err.Error()})
		return
	}
	client := oauthClientFactory(hostID)
	tokenSet, err := client.Exchange(ctx, clientID, callback.Code, session.Verifier, session.RedirectURI)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权码交换失败：" + err.Error()})
		return
	}
	if !openai.GrantsEntitlement(strings.Fields(tokenSet.Scope)) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "授权未包含 ChatGPT 计划使用权限（" + openai.EntitlementScope + "）"})
		return
	}
	identity, err := client.ValidateIDToken(ctx, tokenSet.IDToken, clientID, session.Nonce)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "ID token 校验失败：" + err.Error()})
		return
	}
	document := openai.DocumentFromTokenSet(tokenSet, clientID, identity.Subject)
	document.OAuth.Email = identity.Email
	docPayload, err := json.Marshal(document)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	store, ok := storeForWrite(c)
	if !ok {
		return
	}
	sealed, err := store.Seal(docPayload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	adminId := c.GetInt(ctxkey.Id)
	if session.AccountID > 0 {
		dto, message := reconnectUpstreamAccount(ctx, session, docPayload, sealed)
		if dto == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": message})
			return
		}
		model.RecordLog(ctx, adminId, model.LogTypeManage,
			fmt.Sprintf("管理员通过 OAuth 重新连接了上游账号 %s（id: %d）", dto.Name, dto.Id))
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": dto})
		return
	}
	name := payload.Name
	if name == "" {
		name = identity.Email
	}
	if name == "" {
		name = identity.Subject
	}
	account := model.UpstreamAccount{
		Name: name, Provider: profileProvider(session.Profile), Profile: session.Profile,
		AdminUserId: adminId, ExternalId: identity.Subject, RegistrationId: clientID,
		CredentialKind: document.Kind, EncryptedCredential: sealed,
		CredentialVersion: 1, CredentialExpiresAt: document.ExpiresAt(),
	}
	if err := account.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	model.RecordLog(ctx, adminId, model.LogTypeManage,
		fmt.Sprintf("管理员通过 OAuth 连接了上游账号 %s（id: %d, provider: %s, profile: %s）", account.Name, account.Id, account.Provider, account.Profile))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": account.DTO()})
}

func profileProvider(profileID string) string {
	if profile, ok := upstream.Lookup(profileID); ok {
		return profile.Provider
	}
	return ""
}

// reconnectUpstreamAccount replaces the credential of an existing account
// through the version-checked write and re-enables a reauthorization-marked
// account. It returns nil with a message on failure.
func reconnectUpstreamAccount(ctx context.Context, session *upstreamOAuthSession, docPayload []byte, sealed string) (*model.UpstreamAccountDTO, string) {
	account, err := model.GetUpstreamAccountById(session.AccountID, true)
	if err != nil {
		return nil, "上游账号不存在：" + err.Error()
	}
	document, err := credential.ParseDocument(docPayload)
	if err != nil {
		return nil, err.Error()
	}
	if document.Kind != account.CredentialKind {
		return nil, fmt.Sprintf("凭据类型必须为 %s", account.CredentialKind)
	}
	if account.RegistrationId != "" && account.RegistrationId != document.OAuth.RegistrationID {
		return nil, "新凭据的 client 注册与账号不一致，已拒绝写入"
	}
	if _, err := model.UpdateUpstreamAccountCredential(account.Id, account.CredentialVersion, sealed, document.ExpiresAt()); err != nil {
		if err == model.ErrCredentialVersionConflict {
			return nil, "凭据刚刚被其他进程更新，请重新发起连接"
		}
		return nil, err.Error()
	}
	if account.Status == model.UpstreamAccountStatusReauthRequired {
		// A fresh authorization replaces the rejected one; the account is
		// usable again until the provider says otherwise.
		if err := model.UpdateUpstreamAccountStatus(account.Id, model.UpstreamAccountStatusEnabled); err != nil {
			return nil, err.Error()
		}
	}
	updated, err := model.GetUpstreamAccountById(account.Id, false)
	if err != nil {
		return nil, err.Error()
	}
	dto := updated.DTO()
	return &dto, ""
}
