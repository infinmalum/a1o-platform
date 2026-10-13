package openai

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

const maxJWKSPayload = 1 << 20

type jwk struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

func (c *Client) publicKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoints.JWKS, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxJWKSPayload))
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned %d", res.StatusCode)
	}
	if readErr != nil {
		return nil, readErr
	}
	var parsed jwksResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("jwks response is not valid JSON: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(parsed.Keys))
	for _, key := range parsed.Keys {
		if key.Kty != "RSA" || key.Alg != "RS256" || key.Kid == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil || len(modulus) == 0 {
			continue
		}
		exponentBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
			continue
		}
		exponent := 0
		for _, b := range exponentBytes {
			exponent = exponent<<8 | int(b)
		}
		if exponent < 3 || exponent%2 == 0 {
			continue
		}
		keys[key.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks response contains no usable RSA keys")
	}
	return keys, nil
}

// Identity is the validated ID-token subject.
type Identity struct {
	Subject string
	Email   string
}

// ValidateIDToken verifies the ID-token signature against the server's JWKS
// and checks the issuer, audience, expiry, and nonce. The validated subject
// identifies the connected account.
func (c *Client) ValidateIDToken(ctx context.Context, rawToken, expectedClientID, expectedNonce string) (Identity, error) {
	if expectedClientID == "" {
		return Identity{}, errors.New("ID-token validation requires the issued client ID")
	}
	keys, err := c.publicKeys(ctx)
	if err != nil {
		return Identity{}, err
	}
	parsed, err := jwt.Parse(rawToken, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown signing key %q", kid)
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(c.Endpoints.Issuer),
		jwt.WithAudience(expectedClientID), jwt.WithExpirationRequired())
	if err != nil {
		return Identity{}, fmt.Errorf("ID-token validation failed: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return Identity{}, errors.New("ID-token validation failed")
	}
	if expectedNonce != "" {
		nonce, _ := claims["nonce"].(string)
		if nonce != expectedNonce {
			return Identity{}, errors.New("ID-token nonce mismatch")
		}
	}
	identity := Identity{}
	identity.Subject, _ = claims["sub"].(string)
	identity.Email, _ = claims["email"].(string)
	if identity.Subject == "" {
		return Identity{}, errors.New("ID token has no subject")
	}
	return identity, nil
}

// UnverifiedIDTokenAudience reads the audience of an unsigned-checked ID token.
// It resolves the issued client ID on registration callbacks that do not carry
// it as a query parameter; the value is only trusted after a subsequent
// ValidateIDToken confirms the signature against that audience.
func UnverifiedIDTokenAudience(rawToken string) string {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation(), jwt.WithValidMethods([]string{"RS256"}))
	token, _, err := parser.ParseUnverified(rawToken, jwt.MapClaims{})
	if err != nil {
		return ""
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	switch audience := claims["aud"].(type) {
	case string:
		return audience
	case []any:
		if len(audience) > 0 {
			value, _ := audience[0].(string)
			return value
		}
	}
	return ""
}
