package model

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMCPSealTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChatMCPServer{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
	return db
}

// fixtureSecret derives test values at runtime so the source never carries
// literal secret material.
func fixtureSecret(t *testing.T, prefix string) string {
	t.Helper()
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return prefix + "-" + hexEncode(buf)
}

func hexEncode(buf []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(buf)*2)
	for _, b := range buf {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

// mcpFixtureEnv builds an env JSON payload with a runtime secret.
func mcpFixtureEnv(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`{"API_KEY":%q}`, fixtureSecret(t, "secret"))
}

// fakeSealer mimics the relay/credential envelope shape (format 1, base64
// payload) without real cryptography — the model layer only routes values
// through the interface; envelope compatibility with the real store is
// pinned in the credential package tests.
type fakeSealer struct {
	opens bool
}

func (s *fakeSealer) Seal(plaintext []byte) (string, error) {
	// Same field names and types as the relay/credential envelope.
	raw, err := json.Marshal(struct {
		Format     int    `json:"format"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}{Format: 1, Nonce: "bnNl", Ciphertext: base64.StdEncoding.EncodeToString(plaintext)})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *fakeSealer) Unseal(sealed string) ([]byte, error) {
	if !s.opens {
		return nil, fmt.Errorf("sealed credential failed authentication")
	}
	var envelope struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal([]byte(sealed), &envelope); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(envelope.Ciphertext)
}

func wireSealer(t *testing.T, opens bool) {
	t.Helper()
	previous := CredentialSealerFactory
	CredentialSealerFactory = func() (CredentialSealer, error) {
		return &fakeSealer{opens: opens}, nil
	}
	t.Cleanup(func() { CredentialSealerFactory = previous })
}

// TestUpsertChatMCPServerSealsCredentials pins that user-supplied MCP
// env/headers land in the database only as sealed envelopes and read back as
// plaintext — they used to sit in the DB in the clear while upstream account
// credentials were force-encrypted.
func TestUpsertChatMCPServerSealsCredentials(t *testing.T) {
	db := setupMCPSealTest(t)
	wireSealer(t, true)

	env := mcpFixtureEnv(t)
	headers := fmt.Sprintf(`{"Authorization":"Bearer %s"}`, fixtureSecret(t, "bearer"))
	server := &ChatMCPServer{
		OwnerId: ChatSkillOwnerGlobal, Name: "tools", Transport: ChatMCPServerHTTP,
		URL: "https://mcp.example.com", Env: &env, Headers: &headers,
	}
	require.NoError(t, UpsertChatMCPServer(server))

	storedEnv := loadedColumn(t, db, server.Id, "env")
	storedHeaders := loadedColumn(t, db, server.Id, "headers")
	require.NotEqual(t, env, *storedEnv)
	require.NotEqual(t, headers, *storedHeaders)
	require.True(t, mcpIsSealed(*storedEnv), "env should be sealed: %s", *storedEnv)
	require.True(t, mcpIsSealed(*storedHeaders), "headers should be sealed: %s", *storedHeaders)

	loaded, err := GetMCPServerForOwner(server.Id, ChatSkillOwnerGlobal)
	require.NoError(t, err)
	require.Equal(t, env, *loaded.Env)
	require.Equal(t, headers, *loaded.Headers)

	listed, err := ListEnabledMCPServers(ChatSkillOwnerGlobal)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, env, *listed[0].Env)
	require.Equal(t, headers, *listed[0].Headers)
}

// loadedColumn reads one raw column so the assertion sees exactly what is
// stored, not what the read helpers decrypted.
func loadedColumn(t *testing.T, db *gorm.DB, id int, column string) *string {
	t.Helper()
	var value *string
	require.NoError(t, db.Raw(
		fmt.Sprintf("select %s from chat_mcp_servers where id = ?", column), id).
		Scan(&value).Error)
	return value
}

// TestUpsertChatMCPServerFailsClosedWithoutSealer pins that saving a new
// secret without credential sealing configured is rejected instead of
// writing cleartext.
func TestUpsertChatMCPServerFailsClosedWithoutSealer(t *testing.T) {
	db := setupMCPSealTest(t)
	previous := CredentialSealerFactory
	CredentialSealerFactory = nil
	t.Cleanup(func() { CredentialSealerFactory = previous })

	env := mcpFixtureEnv(t)
	server := &ChatMCPServer{
		OwnerId: ChatSkillOwnerGlobal, Name: "tools", Transport: ChatMCPServerHTTP,
		URL: "https://mcp.example.com", Env: &env,
	}
	require.Error(t, UpsertChatMCPServer(server))
	var count int64
	require.NoError(t, db.Model(&ChatMCPServer{}).Count(&count).Error)
	require.Zero(t, count)

	// No secrets on the row: saving proceeds without a sealer.
	bare := &ChatMCPServer{
		OwnerId: ChatSkillOwnerGlobal, Name: "bare", Transport: ChatMCPServerHTTP,
		URL: "https://mcp.example.com",
	}
	require.NoError(t, UpsertChatMCPServer(bare))
}

// TestListMCPServersPassesLegacyPlaintextThrough pins that rows written
// before sealing keep loading with their plaintext intact.
func TestListMCPServersPassesLegacyPlaintextThrough(t *testing.T) {
	db := setupMCPSealTest(t)
	wireSealer(t, true)

	env := mcpFixtureEnv(t)
	legacy := &ChatMCPServer{
		OwnerId: ChatSkillOwnerGlobal, Name: "legacy", Transport: ChatMCPServerHTTP,
		URL: "https://mcp.example.com", Env: &env,
	}
	require.NoError(t, db.Create(legacy).Error)

	listed, err := ListMCPServers(ChatSkillOwnerGlobal)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, env, *listed[0].Env)
}

// TestListMCPServersToleratesUnsealableRow pins that a row sealed with a key
// no longer configured degrades to a failing MCP call instead of erroring
// the whole list.
func TestListMCPServersToleratesUnsealableRow(t *testing.T) {
	setupMCPSealTest(t)
	wireSealer(t, false)

	env := mcpFixtureEnv(t)
	server := &ChatMCPServer{
		OwnerId: ChatSkillOwnerGlobal, Name: "locked", Transport: ChatMCPServerHTTP,
		URL: "https://mcp.example.com", Env: &env,
	}
	// Sealed by the configured sealer...
	require.NoError(t, UpsertChatMCPServer(server))

	// ...and later unreadable because the key rotated out.
	previous := CredentialSealerFactory
	CredentialSealerFactory = func() (CredentialSealer, error) {
		return &fakeSealer{opens: false}, nil
	}
	t.Cleanup(func() { CredentialSealerFactory = previous })

	listed, err := ListMCPServers(ChatSkillOwnerGlobal)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, mcpIsSealed(*listed[0].Env))
}
