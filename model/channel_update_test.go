package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
)

func setupChannelUpdateTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		DB = previousDB
		common.RedisEnabled = previousRedis
	})
	return db
}

func createUpdateFixtureChannel(t *testing.T, db *gorm.DB) *Channel {
	t.Helper()
	baseURL := "https://api.example.com"
	mapping := `{"gpt-4":"gpt-4o"}`
	channel := &Channel{
		Name: "c1", Type: 1, Key: "sk-orig", Models: "gpt-4", Group: "default",
		BaseURL: &baseURL, ModelMapping: &mapping,
	}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

// TestChannelUpdatePersistsClearedFields pins that editing a channel can
// clear optional fields. Plain struct Updates silently skipped zero values,
// so an emptied model list or config never reached the database.
func TestChannelUpdatePersistsClearedFields(t *testing.T) {
	db := setupChannelUpdateTest(t)
	channel := createUpdateFixtureChannel(t, db)

	row, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	empty := ""
	row.Models = ""
	row.Config = ""
	row.BaseURL = &empty
	row.ModelMapping = nil
	require.NoError(t, row.Update())

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, "", reloaded.Models)
	require.Equal(t, "", reloaded.Config)
	require.NotNil(t, reloaded.BaseURL)
	require.Equal(t, "", *reloaded.BaseURL)
	require.Nil(t, reloaded.ModelMapping)
}

// TestChannelUpdateKeyHandling pins the key rule: an empty key means "keep
// the stored one" (edit payloads round-trip the key omitted by the GET
// handler), while a non-empty key replaces it.
func TestChannelUpdateKeyHandling(t *testing.T) {
	db := setupChannelUpdateTest(t)
	channel := createUpdateFixtureChannel(t, db)

	row, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	row.Key = ""
	require.NoError(t, row.Update())
	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, "sk-orig", reloaded.Key)

	row, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	row.Key = "sk-new"
	require.NoError(t, row.Update())
	reloaded, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, "sk-new", reloaded.Key)
}

// TestChannelUpdatePreservesMaintainedCounters pins that a stale edit payload
// cannot rewind server-maintained fields.
func TestChannelUpdatePreservesMaintainedCounters(t *testing.T) {
	db := setupChannelUpdateTest(t)
	channel := createUpdateFixtureChannel(t, db)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).
		Updates(map[string]any{"used_quota": int64(1234), "balance": 3.5, "test_time": int64(77)}).Error)

	row, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	row.UsedQuota = 0
	row.Balance = 0
	row.TestTime = 0
	row.Name = "renamed"
	require.NoError(t, row.Update())

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.EqualValues(t, 1234, reloaded.UsedQuota)
	require.Equal(t, 3.5, reloaded.Balance)
	require.EqualValues(t, 77, reloaded.TestTime)
	require.Equal(t, "renamed", reloaded.Name)
}
