// Package credential seals upstream subscription credentials with an
// externally supplied master key. The database stores only the sealed
// envelope; without the key configured, all credential operations stay
// disabled and API-key channels keep working unchanged.
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// EnvKeys lists versioned master keys as "version=key" pairs separated by
	// commas, for example "1=<hex>,2=<hex>". Each key must decode to 32 bytes
	// (hex or standard base64).
	EnvKeys = "UPSTREAM_CREDENTIAL_KEYS"
	// EnvActiveVersion selects which key version seals new documents. It
	// defaults to the highest configured version.
	EnvActiveVersion = "UPSTREAM_CREDENTIAL_ACTIVE_VERSION"

	sealContext = "a1o-platform/upstream-credential/v1"
	keyBytes    = 32
)

// Store holds versioned master keys. Sealing always uses the active version;
// unsealing accepts any configured version so rotation keeps old documents
// readable while new writes move to the new key.
type Store struct {
	keys   map[int][keyBytes]byte
	active int
}

func NewStore(keys map[int][keyBytes]byte, activeVersion int) (*Store, error) {
	if len(keys) == 0 {
		return nil, errors.New("credential store requires at least one key")
	}
	if _, ok := keys[activeVersion]; !ok {
		return nil, fmt.Errorf("active key version %d has no key", activeVersion)
	}
	return &Store{keys: keys, active: activeVersion}, nil
}

type sealedEnvelope struct {
	Format     int    `json:"format"`
	KeyVersion int    `json:"key_version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func (s *Store) Seal(plaintext []byte) (string, error) {
	key := s.keys[s.active]
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(sealContext))
	envelope, err := json.Marshal(sealedEnvelope{
		Format:     1,
		KeyVersion: s.active,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return "", err
	}
	return string(envelope), nil
}

func (s *Store) Unseal(sealed string) ([]byte, error) {
	var envelope sealedEnvelope
	if err := json.Unmarshal([]byte(sealed), &envelope); err != nil {
		return nil, errors.New("sealed credential is malformed")
	}
	if envelope.Format != 1 {
		return nil, fmt.Errorf("unsupported sealed credential format %d", envelope.Format)
	}
	key, ok := s.keys[envelope.KeyVersion]
	if !ok {
		return nil, fmt.Errorf("credential was sealed with unavailable key version %d", envelope.KeyVersion)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, errors.New("sealed credential nonce is malformed")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, errors.New("sealed credential ciphertext is malformed")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(sealContext))
	if err != nil {
		return nil, errors.New("sealed credential failed authentication")
	}
	return plaintext, nil
}

func parseKeyMaterial(value string) ([keyBytes]byte, error) {
	var key [keyBytes]byte
	if len(value) == keyBytes*2 {
		if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == keyBytes {
			copy(key[:], decoded)
			return key, nil
		}
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) == keyBytes {
			copy(key[:], decoded)
			return key, nil
		}
	}
	return key, errors.New("key must be 32 bytes encoded as hex or base64")
}

// ParseKeyVersionList parses "1=<key>,2=<key>" into versioned keys.
func ParseKeyVersionList(value string) (map[int][keyBytes]byte, error) {
	keys := make(map[int][keyBytes]byte)
	for _, pair := range strings.Split(value, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		version, material, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("key entry %q must be version=key", pair)
		}
		versionNumber, err := strconv.Atoi(strings.TrimSpace(version))
		if err != nil || versionNumber < 1 {
			return nil, fmt.Errorf("key entry %q has an invalid version", pair)
		}
		key, err := parseKeyMaterial(strings.TrimSpace(material))
		if err != nil {
			return nil, fmt.Errorf("key version %d: %w", versionNumber, err)
		}
		keys[versionNumber] = key
	}
	if len(keys) == 0 {
		return nil, errors.New("no credential keys configured")
	}
	return keys, nil
}

// NewStoreFromEnv builds the store from EnvKeys and EnvActiveVersion. A nil
// store without error means credentials are simply not configured.
func NewStoreFromEnv() (*Store, error) {
	raw := os.Getenv(EnvKeys)
	if raw == "" {
		return nil, nil
	}
	keys, err := ParseKeyVersionList(raw)
	if err != nil {
		return nil, err
	}
	active := 0
	for version := range keys {
		if version > active {
			active = version
		}
	}
	if raw := os.Getenv(EnvActiveVersion); raw != "" {
		active, err = strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer", EnvActiveVersion)
		}
	}
	return NewStore(keys, active)
}

var (
	// defaultStore is initialized from the environment before any dependent
	// package-level variable, so coordinators never capture a nil store that
	// an init function would fill in later.
	defaultStore, defaultStoreErr = NewStoreFromEnv()
)

// DefaultStore returns the process-wide store. A nil store (with nil error)
// means subscription credentials are not configured; an error means the
// configuration is broken and must be fixed before enabling accounts.
func DefaultStore() (*Store, error) {
	return defaultStore, defaultStoreErr
}
