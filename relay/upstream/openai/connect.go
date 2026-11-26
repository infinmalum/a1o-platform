package openai

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/upstream"
)

// AgentName is the agent_name_hint sent only on the initial dynamic client
// registration.
const AgentName = "A1O Platform"

// ClientFactory builds the OAuth client for one connection flow; tests
// replace it to point at a local authorization server.
var ClientFactory = func(agentName, hostID string) *Client {
	return NewClient(agentName, hostID)
}

// Connect implements the upstream.Connector flow of chatgpt_plan_public: the
// entitlement and identity checks live here so the connection wizard stays
// provider-agnostic.
type Connect struct{}

// Begin builds the Sign in with ChatGPT authorization URL. The first sign-in
// performs dynamic client registration and the issued client ID arrives on
// the callback; a saved registration reconnects with its own client ID.
func (Connect) Begin(registrationID string) (string, *upstream.ConnectorSession, error) {
	hostID, err := model.GetOrCreateUpstreamOAuthHostID()
	if err != nil {
		return "", nil, err
	}
	client := ClientFactory(AgentName, hostID)
	authorization, err := client.BuildAuthorization(registrationID)
	if err != nil {
		return "", nil, err
	}
	return authorization.URL, &upstream.ConnectorSession{
		Verifier: authorization.Verifier, State: authorization.State,
		Nonce: authorization.Nonce, RedirectURI: client.RedirectURI,
		RegistrationID: registrationID,
	}, nil
}

// Complete validates the pasted loopback callback, exchanges the code,
// verifies the plan entitlement and the ID token, and returns the sealed
// credential input.
func (Connect) Complete(ctx context.Context, session *upstream.ConnectorSession, callbackURL string) (*upstream.ConnectorResult, error) {
	parsed, err := url.Parse(callbackURL)
	if err != nil {
		return nil, errors.New("回调 URL 无法解析")
	}
	callback, err := ParseCallback(parsed.Query())
	if err != nil {
		return nil, err
	}
	if callback.State != session.State {
		return nil, errors.New("state 校验失败，请重新发起连接")
	}
	clientID := callback.IssuedClientID
	if clientID == "" {
		clientID = session.RegistrationID
	}
	if clientID == "" {
		return nil, errors.New("回调中没有签发的 client ID，动态注册未完成，请重新发起连接")
	}
	if session.RegistrationID != "" && callback.IssuedClientID != "" && callback.IssuedClientID != session.RegistrationID {
		return nil, errors.New("授权服务器返回了不同的 client ID，已拒绝重连以保护原注册")
	}
	client := ClientFactory("", "")
	tokenSet, err := client.Exchange(ctx, clientID, callback.Code, session.Verifier, session.RedirectURI)
	if err != nil {
		return nil, err
	}
	if !GrantsEntitlement(strings.Fields(tokenSet.Scope)) {
		return nil, errors.New("授权未包含 ChatGPT 计划使用权限（" + EntitlementScope + "）")
	}
	identity, err := client.ValidateIDToken(ctx, tokenSet.IDToken, clientID, session.Nonce)
	if err != nil {
		return nil, err
	}
	document := DocumentFromTokenSet(tokenSet, clientID, identity.Subject)
	document.OAuth.Email = identity.Email
	return &upstream.ConnectorResult{
		Document:       document,
		ExternalID:     identity.Subject,
		Email:          identity.Email,
		DisplayName:    identity.Email,
		RegistrationID: clientID,
	}, nil
}
