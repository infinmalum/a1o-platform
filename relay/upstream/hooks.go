package upstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/infinmalum/a1o-platform/relay/credential"
)

// RequestValidator enforces a profile's capability matrix on one upstream
// request body — the converted body when a bridge produced it — and returns
// the normalized body to forward. An error fails the attempt explicitly; the
// lifecycle decides whether another channel may serve.
type RequestValidator func(body []byte) ([]byte, error)

var (
	hooksMu    sync.RWMutex
	refreshers = map[string]credential.Refresher{}
	validators = map[string]RequestValidator{}
)

// RegisterRefresher wires the credential rotator of one profile, for example
// the OAuth refresh of chatgpt_plan_public.
func RegisterRefresher(profileID string, refresher credential.Refresher) error {
	if profileID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	if refresher == nil {
		return fmt.Errorf("refresher must not be nil")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	refreshers[profileID] = refresher
	return nil
}

// RefresherFor returns the registered credential rotator of a profile.
func RefresherFor(profileID string) (credential.Refresher, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	refresher, ok := refreshers[profileID]
	return refresher, ok
}

// RegisterRequestValidator wires the capability validator of one profile.
func RegisterRequestValidator(profileID string, validator RequestValidator) error {
	if profileID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	if validator == nil {
		return fmt.Errorf("validator must not be nil")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	validators[profileID] = validator
	return nil
}

// RequestValidatorFor returns the registered capability validator of a profile.
func RequestValidatorFor(profileID string) (RequestValidator, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	validator, ok := validators[profileID]
	return validator, ok
}

// ConnectorSession carries one pending authorization across the browser
// round trip: the connector-owned PKCE verifier, anti-forgery state, optional
// nonce, and the redirect URI embedded in the authorization request.
type ConnectorSession struct {
	Verifier    string
	State       string
	Nonce       string
	RedirectURI string
	// RegistrationID carries the reconnect target's saved client registration
	// into Complete; providers with dynamic registration use it for the token
	// exchange and to reject mismatched re-registrations.
	RegistrationID string
}

// ConnectorResult is the outcome of a completed sign-in: the credential
// document to seal, the provider identity for display, and the client
// registration the credential belongs to.
type ConnectorResult struct {
	Document       credential.Document
	ExternalID     string
	Email          string
	DisplayName    string
	RegistrationID string
}

// Connector runs one provider's OAuth connection flow: preparing a pending
// browser authorization and completing it with the URL (or code) the
// administrator pastes back.
type Connector interface {
	// Begin builds the authorization URL for a fresh browser sign-in. An
	// empty registrationID starts a first sign-in; a saved one reconnects
	// an existing registration when the provider supports it.
	Begin(registrationID string) (authorizeURL string, session *ConnectorSession, err error)
	// Complete consumes the pasted callback URL or code. It validates the
	// state, exchanges the credential, and resolves the provider identity.
	Complete(ctx context.Context, session *ConnectorSession, callbackURL string) (*ConnectorResult, error)
}

var connectors = map[string]Connector{}

// RegisterConnector wires the OAuth connection flow of one profile.
func RegisterConnector(profileID string, connector Connector) error {
	if profileID == "" {
		return fmt.Errorf("profile ID must not be empty")
	}
	if connector == nil {
		return fmt.Errorf("connector must not be nil")
	}
	hooksMu.Lock()
	defer hooksMu.Unlock()
	connectors[profileID] = connector
	return nil
}

// ConnectorFor returns the registered OAuth connection flow of a profile.
func ConnectorFor(profileID string) (Connector, bool) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()
	connector, ok := connectors[profileID]
	return connector, ok
}
