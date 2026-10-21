package model

import (
	"testing"

	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRootAccountTest(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
}

// TestCreateRootAccountSkippedWithoutConfiguredPassword pins the first-boot
// behavior: without INITIAL_ROOT_PASSWORD no account is created at all, so
// no guessable or logged secret can exist — the operator chooses the root
// password through the /setup wizard instead.
func TestCreateRootAccountSkippedWithoutConfiguredPassword(t *testing.T) {
	setupRootAccountTest(t)
	previous := config.InitialRootPassword
	config.InitialRootPassword = ""
	t.Cleanup(func() { config.InitialRootPassword = previous })

	require.NoError(t, CreateRootAccountIfNeed())
	var count int64
	require.NoError(t, DB.Model(&User{}).Count(&count).Error)
	require.EqualValues(t, 0, count, "a fresh database must stay empty for the /setup wizard")
}

// TestCreateRootAccountUsesConfiguredPassword pins the INITIAL_ROOT_PASSWORD
// seeding path.
func TestCreateRootAccountUsesConfiguredPassword(t *testing.T) {
	setupRootAccountTest(t)
	previous := config.InitialRootPassword
	config.InitialRootPassword = fixtureUserSecret(t, "seed")
	t.Cleanup(func() { config.InitialRootPassword = previous })

	require.NoError(t, CreateRootAccountIfNeed())
	var user User
	require.NoError(t, DB.First(&user, "username = ?", "root").Error)
	require.True(t, common.ValidatePasswordAndHash(config.InitialRootPassword, user.Password))
}
