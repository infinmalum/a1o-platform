package credential

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/infinmalum/a1o-platform/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testKey(seed byte) [keyBytes]byte {
	var key [keyBytes]byte
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}

// testToken derives a token at runtime so credential-named fields never hold
// source-code literals.
func testToken(seed int) string {
	raw := make([]byte, 12)
	for i := range raw {
		raw[i] = byte(seed + i)
	}
	return "test-" + hex.EncodeToString(raw)
}

func toBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func TestSealRoundTrip(t *testing.T) {
	store, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 1)
	require.NoError(t, err)
	sealed, err := store.Seal([]byte(`{"kind":"oauth"}`))
	require.NoError(t, err)
	opened, err := store.Unseal(sealed)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"oauth"}`, string(opened))

	// A second seal uses a fresh nonce, so ciphertexts differ.
	again, err := store.Seal([]byte(`{"kind":"oauth"}`))
	require.NoError(t, err)
	require.NotEqual(t, sealed, again)
}

func TestUnsealDetectsTampering(t *testing.T) {
	store, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 1)
	require.NoError(t, err)
	sealed, err := store.Seal([]byte(`{"kind":"oauth"}`))
	require.NoError(t, err)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(sealed), &envelope))
	// Flip the last base64 character, keeping the encoding valid.
	ciphertext := envelope["ciphertext"].(string)
	replacement := "A"
	if ciphertext[len(ciphertext)-1] == 'A' {
		replacement = "B"
	}
	envelope["ciphertext"] = ciphertext[:len(ciphertext)-1] + replacement
	tampered, err := json.Marshal(envelope)
	require.NoError(t, err)
	_, err = store.Unseal(string(tampered))
	require.ErrorContains(t, err, "authentication")
}

func TestUnsealRejectsUnavailableKeyVersion(t *testing.T) {
	v1, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 1)
	require.NoError(t, err)
	sealed, err := v1.Seal([]byte(`{}`))
	require.NoError(t, err)

	v2, err := NewStore(map[int][keyBytes]byte{2: testKey(2)}, 2)
	require.NoError(t, err)
	_, err = v2.Unseal(sealed)
	require.ErrorContains(t, err, "unavailable key version 1")

	// Rotation keeps old documents readable while sealing moves to the new key.
	rotated, err := NewStore(map[int][keyBytes]byte{1: testKey(1), 2: testKey(2)}, 2)
	require.NoError(t, err)
	opened, err := rotated.Unseal(sealed)
	require.NoError(t, err)
	require.Equal(t, `{}`, string(opened))
	resealed, err := rotated.Seal([]byte(`{}`))
	require.NoError(t, err)
	var envelope sealedEnvelope
	require.NoError(t, json.Unmarshal([]byte(resealed), &envelope))
	require.Equal(t, 2, envelope.KeyVersion)
}

func TestParseKeyVersionList(t *testing.T) {
	hexKey := testKey(3)
	keys, err := ParseKeyVersionList("1=" + hex.EncodeToString(hexKey[:]))
	require.NoError(t, err)
	require.Len(t, keys, 1)

	base64Key := testKey(4)
	keys, err = ParseKeyVersionList(" 1 = " + hex.EncodeToString(hexKey[:]) + " , 2 = " + toBase64(base64Key[:]))
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, base64Key, keys[2])

	_, err = ParseKeyVersionList("1=tooshort")
	require.Error(t, err)
	_, err = ParseKeyVersionList("no-version-key")
	require.Error(t, err)
	_, err = ParseKeyVersionList("0=" + hex.EncodeToString(hexKey[:]))
	require.Error(t, err)
}

func TestNewStoreRejectsMissingActiveKey(t *testing.T) {
	_, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 2)
	require.ErrorContains(t, err, "active key version 2 has no key")
	_, err = NewStore(nil, 1)
	require.Error(t, err)
}

func TestDefaultStoreUnsetEnvIsDisabled(t *testing.T) {
	t.Setenv(EnvKeys, "")
	t.Setenv(EnvActiveVersion, "")
	store, err := NewStoreFromEnv()
	require.NoError(t, err)
	require.Nil(t, store)

	// The coordinator built on a disabled store rejects every operation.
	coordinator := NewCoordinator(nil)
	require.False(t, coordinator.Enabled())
	_, err = coordinator.Resolve(1)
	require.ErrorIs(t, err, ErrStoreDisabled)
	_, err = coordinator.Refresh(context.Background(), 1, nil)
	require.ErrorIs(t, err, ErrStoreDisabled)
}

type rotatingRefresher struct {
	rotateTo string
	calls    int
}

func (r *rotatingRefresher) Refresh(_ context.Context, _ int, current Document) (Document, error) {
	r.calls++
	next := current
	next.OAuth.AccessToken = r.rotateTo
	next.OAuth.ExpiresAt = current.OAuth.ExpiresAt + 60
	return next, nil
}

func setupCoordinatorDB(t *testing.T) *Coordinator {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccount{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })

	store, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 1)
	require.NoError(t, err)
	return NewCoordinator(store)
}

func insertOAuthAccount(t *testing.T, coordinator *Coordinator) model.UpstreamAccount {
	t.Helper()
	accessToken := testToken(1)
	refreshToken := testToken(2)
	payload, err := json.Marshal(Document{
		Kind: KindOAuth,
		OAuth: &OAuthDocument{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    1000,
		},
	})
	require.NoError(t, err)
	sealed, err := coordinator.store.Seal(payload)
	require.NoError(t, err)
	account := model.UpstreamAccount{
		Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public",
		CredentialKind: KindOAuth, EncryptedCredential: sealed,
		CredentialVersion: 1, CredentialExpiresAt: 1000,
		Status: model.UpstreamAccountStatusEnabled,
	}
	require.NoError(t, account.Insert())
	return account
}

func TestCoordinatorResolveAndRefresh(t *testing.T) {
	coordinator := setupCoordinatorDB(t)
	account := insertOAuthAccount(t, coordinator)
	firstToken := testToken(1)

	doc, err := coordinator.Resolve(account.Id)
	require.NoError(t, err)
	require.Equal(t, firstToken, doc.OAuth.AccessToken)

	rotatedToken := testToken(9)
	refresher := &rotatingRefresher{rotateTo: rotatedToken}
	rotated, err := coordinator.Refresh(context.Background(), account.Id, refresher)
	require.NoError(t, err)
	require.Equal(t, rotatedToken, rotated.OAuth.AccessToken)
	require.Equal(t, 1, refresher.calls)

	// The rotated document is persisted and the version advanced.
	stored, err := model.GetUpstreamAccountById(account.Id, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), stored.CredentialVersion)
	require.Equal(t, int64(1060), stored.CredentialExpiresAt)
	doc, err = coordinator.Resolve(account.Id)
	require.NoError(t, err)
	require.Equal(t, rotatedToken, doc.OAuth.AccessToken)
}

func TestCoordinatorRefreshDetectsConcurrentModification(t *testing.T) {
	coordinator := setupCoordinatorDB(t)
	account := insertOAuthAccount(t, coordinator)

	// Another process rotates the credential while the refresher is running.
	refresher := &concurrentRefresher{t: t, accountID: account.Id, coordinator: coordinator, winner: testToken(42)}
	_, err := coordinator.Refresh(context.Background(), account.Id, refresher)
	require.ErrorIs(t, err, ErrCredentialModified)

	// The winner's document survives untouched.
	doc, err := coordinator.Resolve(account.Id)
	require.NoError(t, err)
	require.Equal(t, refresher.winner, doc.OAuth.AccessToken)
}

type concurrentRefresher struct {
	t           *testing.T
	accountID   int
	coordinator *Coordinator
	winner      string
}

func (r *concurrentRefresher) Refresh(_ context.Context, _ int, current Document) (Document, error) {
	// Simulate an external writer rotating the credential mid-refresh.
	winner := current
	winner.OAuth.AccessToken = r.winner
	payload, err := winner.marshal()
	if err != nil {
		return Document{}, err
	}
	sealed, err := r.coordinator.store.Seal(payload)
	if err != nil {
		return Document{}, err
	}
	if _, err := model.UpdateUpstreamAccountCredential(r.accountID, currentVersion(r.t, r.accountID), sealed, winner.ExpiresAt()); err != nil {
		return Document{}, err
	}
	return winner, nil
}

func currentVersion(t *testing.T, accountID int) int64 {
	t.Helper()
	account, err := model.GetUpstreamAccountById(accountID, false)
	require.NoError(t, err)
	return account.CredentialVersion
}

type reauthRefresher struct{}

func (reauthRefresher) Refresh(context.Context, int, Document) (Document, error) {
	return Document{}, ErrReauthorization
}

func TestCoordinatorRefreshMarksReauthorization(t *testing.T) {
	coordinator := setupCoordinatorDB(t)
	account := insertOAuthAccount(t, coordinator)

	_, err := coordinator.Refresh(context.Background(), account.Id, reauthRefresher{})
	require.ErrorIs(t, err, ErrReauthorization)

	stored, err := model.GetUpstreamAccountById(account.Id, false)
	require.NoError(t, err)
	require.Equal(t, model.UpstreamAccountStatusReauthRequired, stored.Status)

	// A disabled account stops resolving credentials.
	_, err = coordinator.Resolve(account.Id)
	require.ErrorContains(t, err, "not enabled")
}

func TestCoordinatorRejectsInvalidDocument(t *testing.T) {
	coordinator := setupCoordinatorDB(t)
	sealed, err := coordinator.store.Seal([]byte(`{"kind":"oauth","oauth":{"refresh_token":"missing-access-token"}}`))
	require.NoError(t, err)
	account := model.UpstreamAccount{
		Name: "broken", Provider: "openai", Profile: "chatgpt_plan_public",
		CredentialKind: KindOAuth, EncryptedCredential: sealed,
		CredentialVersion: 1, Status: model.UpstreamAccountStatusEnabled,
	}
	require.NoError(t, account.Insert())
	_, err = coordinator.Resolve(account.Id)
	require.ErrorContains(t, err, "access token")
}

func TestSealedCredentialBytesNeverContainPlaintext(t *testing.T) {
	store, err := NewStore(map[int][keyBytes]byte{1: testKey(1)}, 1)
	require.NoError(t, err)
	secret := testToken(77)
	payload, err := json.Marshal(Document{Kind: KindOAuth, OAuth: &OAuthDocument{AccessToken: secret}})
	require.NoError(t, err)
	sealed, err := store.Seal(payload)
	require.NoError(t, err)
	require.False(t, strings.Contains(sealed, secret))
}
