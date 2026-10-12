package credential

import (
	"encoding/json"
	"errors"
	"fmt"
)

const KindOAuth = "oauth"

var (
	ErrStoreDisabled = errors.New("subscription credential encryption is not configured")
	// ErrReauthorization marks a refresh that the provider permanently
	// rejected (for example OAuth invalid_grant); the account needs to be
	// connected again by an administrator.
	ErrReauthorization = errors.New("credential requires reauthorization")
	// ErrCredentialModified reports a lost version-checked write: another
	// process rotated the credential first.
	ErrCredentialModified = errors.New("credential was modified concurrently")
)

// Document is the typed credential payload sealed inside an account. Provider
// transport code may carry extra fields: sealing preserves the original JSON
// bytes, and this struct only enforces the shared invariants.
type Document struct {
	Kind  string         `json:"kind"`
	OAuth *OAuthDocument `json:"oauth,omitempty"`
}

type OAuthDocument struct {
	AccessToken       string   `json:"access_token"`
	RefreshToken      string   `json:"refresh_token,omitempty"`
	ExpiresAt         int64    `json:"expires_at,omitempty"` // unix seconds
	Scopes            []string `json:"scopes,omitempty"`
	RegistrationID    string   `json:"registration_id,omitempty"`
	ExternalAccountID string   `json:"external_account_id,omitempty"`
}

// ExpiresAt returns the document expiry in unix seconds, 0 when unknown.
func (d Document) ExpiresAt() int64 {
	if d.OAuth != nil {
		return d.OAuth.ExpiresAt
	}
	return 0
}

// ParseDocument validates the shared invariants of a credential document.
func ParseDocument(payload []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(payload, &doc); err != nil {
		return doc, fmt.Errorf("credential document is not valid JSON: %w", err)
	}
	switch doc.Kind {
	case KindOAuth:
		if doc.OAuth == nil || doc.OAuth.AccessToken == "" {
			return doc, errors.New("oauth credential requires an access token")
		}
	default:
		return doc, fmt.Errorf("unsupported credential kind %q", doc.Kind)
	}
	return doc, nil
}
