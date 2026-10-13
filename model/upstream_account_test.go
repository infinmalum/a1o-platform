package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/common"
)

func setupUpstreamAccountDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UpstreamAccount{}, &UpstreamAccountCooldown{}, &Channel{}, &Ability{}))
	previousDB, previousRedis := DB, common.RedisEnabled
	DB, common.RedisEnabled = db, false
	t.Cleanup(func() { DB, common.RedisEnabled = previousDB, previousRedis })
}

func TestUpstreamAccountDTONeverCarriesCredential(t *testing.T) {
	setupUpstreamAccountDB(t)
	account := UpstreamAccount{
		Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public",
		CredentialKind: "oauth", EncryptedCredential: "sealed-document",
		CredentialVersion: 1, CredentialExpiresAt: 1234,
		Status: UpstreamAccountStatusEnabled, ConcurrencyLimit: 3,
	}
	require.NoError(t, account.Insert())

	loaded, err := GetUpstreamAccountById(account.Id, true)
	require.NoError(t, err)
	dto := loaded.DTO()
	require.NotEmpty(t, loaded.EncryptedCredential)
	// The DTO struct has no credential field at all; the round trip must keep
	// every other administrative attribute.
	require.Equal(t, account.Name, dto.Name)
	require.Equal(t, "openai", dto.Provider)
	require.Equal(t, "chatgpt_plan_public", dto.Profile)
	require.Equal(t, int64(1), dto.CredentialVersion)
	require.Equal(t, int64(1234), dto.CredentialExpiresAt)
	require.Equal(t, UpstreamAccountStatusEnabled, dto.Status)
	require.Equal(t, 3, dto.ConcurrencyLimit)

	// The list path omits the sealed column entirely.
	listed, err := GetAllUpstreamAccounts(0, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
}

func TestUpstreamAccountCredentialVersionCheck(t *testing.T) {
	setupUpstreamAccountDB(t)
	account := UpstreamAccount{
		Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public",
		CredentialKind: "oauth", EncryptedCredential: "v1", CredentialVersion: 1,
		Status: UpstreamAccountStatusEnabled,
	}
	require.NoError(t, account.Insert())

	version, err := UpdateUpstreamAccountCredential(account.Id, 1, "v2", 2000)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	// A stale writer loses the compare-and-swap.
	_, err = UpdateUpstreamAccountCredential(account.Id, 1, "v3", 3000)
	require.ErrorIs(t, err, ErrCredentialVersionConflict)

	// The winner's document is intact.
	loaded, err := GetUpstreamAccountById(account.Id, true)
	require.NoError(t, err)
	require.Equal(t, "v2", loaded.EncryptedCredential)
	require.Equal(t, int64(2), loaded.CredentialVersion)
	require.Equal(t, int64(2000), loaded.CredentialExpiresAt)

	// An unknown account is a not-found, not a conflict.
	_, err = UpdateUpstreamAccountCredential(999, 1, "v2", 0)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDeleteUpstreamAccountGuardedByChannels(t *testing.T) {
	setupUpstreamAccountDB(t)
	account := UpstreamAccount{Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public", CredentialKind: "oauth", Status: UpstreamAccountStatusEnabled}
	require.NoError(t, account.Insert())

	require.NoError(t, DeleteUpstreamAccountById(account.Id))

	second := UpstreamAccount{Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public", CredentialKind: "oauth", Status: UpstreamAccountStatusEnabled}
	require.NoError(t, second.Insert())
	binding := second.Id
	channel := Channel{Name: "sub", Type: 1, Key: "k", Status: ChannelStatusEnabled, Group: "default", Models: "gpt-5", UpstreamAccountID: &binding}
	require.NoError(t, channel.Insert())

	err := DeleteUpstreamAccountById(second.Id)
	require.ErrorContains(t, err, "channels still reference")

	require.NoError(t, channel.Delete())
	require.NoError(t, DeleteUpstreamAccountById(second.Id))
}

func TestUpstreamAccountMetadataAndStatus(t *testing.T) {
	setupUpstreamAccountDB(t)
	account := UpstreamAccount{Name: "chatgpt", Provider: "openai", Profile: "chatgpt_plan_public", CredentialKind: "oauth", Status: UpstreamAccountStatusEnabled, ConcurrencyLimit: 0}
	require.NoError(t, account.Insert())
	require.Equal(t, UpstreamAccountStatusEnabled, account.Status) // Insert defaults 0 to enabled

	require.NoError(t, UpdateUpstreamAccountMetadata(account.Id, "renamed", 5, UpstreamAccountStatusManuallyDisabled))
	loaded, err := GetUpstreamAccountById(account.Id, false)
	require.NoError(t, err)
	require.Equal(t, "renamed", loaded.Name)
	require.Equal(t, 5, loaded.ConcurrencyLimit)
	require.Equal(t, UpstreamAccountStatusManuallyDisabled, loaded.Status)

	require.NoError(t, UpdateUpstreamAccountStatus(account.Id, UpstreamAccountStatusReauthRequired))
	loaded, err = GetUpstreamAccountById(account.Id, false)
	require.NoError(t, err)
	require.Equal(t, UpstreamAccountStatusReauthRequired, loaded.Status)

	require.NoError(t, InsertUpstreamAccountCooldown(account.Id, "gpt-5", "429", 1700000000))
	cooldowns := []UpstreamAccountCooldown{}
	require.NoError(t, DB.Find(&cooldowns).Error)
	require.Len(t, cooldowns, 1)
	require.Equal(t, account.Id, cooldowns[0].AccountId)
	require.Equal(t, "gpt-5", cooldowns[0].Model)
	require.Equal(t, "429", cooldowns[0].Reason)
	require.Equal(t, int64(1700000000), cooldowns[0].UntilTime)
}

func TestGetUpstreamAccountByIdRejectsInvalidID(t *testing.T) {
	setupUpstreamAccountDB(t)
	_, err := GetUpstreamAccountById(0, false)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = GetUpstreamAccountById(-1, true)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
