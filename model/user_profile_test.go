package model

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUserProfileTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
	return db
}

// fixtureUserSecret derives test values at runtime so the source never
// carries literal secret material.
func fixtureUserSecret(t *testing.T, prefix string) string {
	t.Helper()
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return prefix + "-" + hex.EncodeToString(buf)
}

func createProfileFixtureUser(t *testing.T, db *gorm.DB) *User {
	t.Helper()
	user := &User{
		Username: "alice", Password: fixtureUserSecret(t, "hash"), DisplayName: "Alice",
		Role: RoleCommonUser, Status: UserStatusEnabled, Email: "a@example.com",
		AccessToken: fixtureUserSecret(t, "token"), Quota: 100, UsedQuota: 42, Group: "default",
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

// TestUpdateProfilePersistsZeroValues pins the user-edit repair: struct
// Updates silently skipped zero values, so an admin zeroing a quota recorded
// a log line but changed nothing.
func TestUpdateProfilePersistsZeroValues(t *testing.T) {
	db := setupUserProfileTest(t)
	created := createProfileFixtureUser(t, db)

	row, err := GetUserById(created.Id, false)
	require.NoError(t, err)
	row.Quota = 0
	row.Email = ""
	row.DisplayName = ""
	require.NoError(t, row.UpdateProfile(false))

	reloaded, err := GetUserById(created.Id, true)
	require.NoError(t, err)
	require.EqualValues(t, 0, reloaded.Quota)
	require.Equal(t, "", reloaded.Email)
	require.Equal(t, "", reloaded.DisplayName)
	// Fields outside the profile list keep their stored values even though
	// the in-memory row never carried them.
	require.Equal(t, created.Password, reloaded.Password)
	require.Equal(t, created.AccessToken, reloaded.AccessToken)
	require.EqualValues(t, 42, reloaded.UsedQuota)
	require.EqualValues(t, RoleCommonUser, reloaded.Role)
	require.EqualValues(t, UserStatusEnabled, reloaded.Status)
}

// TestUpdateProfilePasswordOnlyWhenRequested pins that the stored password
// hash is only rewritten when a new password arrives.
func TestUpdateProfilePasswordOnlyWhenRequested(t *testing.T) {
	db := setupUserProfileTest(t)
	created := createProfileFixtureUser(t, db)

	row, err := GetUserById(created.Id, false)
	require.NoError(t, err)
	row.Password = fixtureUserSecret(t, "rejected")
	require.NoError(t, row.UpdateProfile(false))
	reloaded, err := GetUserById(created.Id, true)
	require.NoError(t, err)
	require.Equal(t, created.Password, reloaded.Password)

	row, err = GetUserById(created.Id, false)
	require.NoError(t, err)
	newHash := fixtureUserSecret(t, "fresh")
	row.Password = newHash
	require.NoError(t, row.UpdateProfile(true))
	reloaded, err = GetUserById(created.Id, true)
	require.NoError(t, err)
	require.Equal(t, newHash, reloaded.Password)
}
