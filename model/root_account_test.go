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

// TestCreateRootAccountNeverUsesDefaultPassword pins the first-boot repair:
// without INITIAL_ROOT_PASSWORD the bootstrap root gets a random secret, so
// the publicly known 123456 must not authenticate.
func TestCreateRootAccountNeverUsesDefaultPassword(t *testing.T) {
	setupRootAccountTest(t)
	previous := config.InitialRootPassword
	config.InitialRootPassword = ""
	t.Cleanup(func() { config.InitialRootPassword = previous })

	require.NoError(t, CreateRootAccountIfNeed())
	var user User
	require.NoError(t, DB.First(&user, "username = ?", "root").Error)
	require.False(t, common.ValidatePasswordAndHash("123456", user.Password))

	// A second run must not duplicate the root account.
	require.NoError(t, CreateRootAccountIfNeed())
	var count int64
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "root").Count(&count).Error)
	require.EqualValues(t, 1, count)
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
