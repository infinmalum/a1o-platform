package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
)

func TestBusinessStatsAggregation(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open("file:business-stats?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Log{}, &Channel{}, &User{}))
	DB, LOG_DB = db, db
	common.UsingSQLite = true
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite = previousSQLite
		_ = sqlDB.Close()
	})

	logs := []*Log{
		{UserId: 1, CreatedAt: 1000, Type: LogTypeConsume, ModelName: "gpt-4o", Quota: 100},
		{UserId: 1, CreatedAt: 1100, Type: LogTypeConsume, ModelName: "gpt-4o", Quota: 50},
		{UserId: 2, CreatedAt: 1200, Type: LogTypeConsume, ModelName: "claude-3-5-sonnet", Quota: 25},
		{UserId: 2, CreatedAt: 1300, Type: LogTypeTopup, Quota: 500},
		{UserId: 3, CreatedAt: 5000, Type: LogTypeConsume, ModelName: "gpt-4o", Quota: 999},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	stats, err := GetBusinessLogStats(900, 2000)
	require.NoError(t, err)
	require.EqualValues(t, 175, stats.ConsumedQuota)
	require.EqualValues(t, 500, stats.TopupQuota)
	require.EqualValues(t, 3, stats.Requests)
	// The user who only topped up still counts as active.
	require.EqualValues(t, 2, stats.ActiveUsers)

	top, err := TopModelsByQuota(900, 2000, 8)
	require.NoError(t, err)
	require.Len(t, top, 2)
	require.Equal(t, "gpt-4o", top[0].ModelName)
	require.EqualValues(t, 150, top[0].Quota)
	require.EqualValues(t, 2, top[0].Requests)
	require.Equal(t, "claude-3-5-sonnet", top[1].ModelName)

	channels := []*Channel{
		{Name: "low", Status: ChannelStatusEnabled, Balance: 1, BalanceUpdatedTime: 100},
		{Name: "auto", Status: ChannelStatusAutoDisabled, Balance: 50, BalanceUpdatedTime: 100},
		{Name: "manual", Status: ChannelStatusManuallyDisabled, Balance: 0, BalanceUpdatedTime: 0},
		{Name: "negative", Status: ChannelStatusEnabled, Balance: -1, BalanceUpdatedTime: 100},
	}
	require.NoError(t, DB.Create(&channels).Error)

	health, err := GetChannelHealthSummary(5)
	require.NoError(t, err)
	require.EqualValues(t, 4, health.Total)
	require.EqualValues(t, 2, health.Enabled)
	require.EqualValues(t, 1, health.AutoDisabled)
	require.EqualValues(t, 1, health.ManuallyDisabled)
	require.EqualValues(t, 1, health.LowBalance)
	// A never-fetched balance and a negative one both mean "unknown", not empty.
	require.EqualValues(t, 2, health.BalanceUnavailable)

	users := []*User{
		{Username: "one", Status: UserStatusEnabled, AccessToken: "token-one", AffCode: "aff-one"},
		{Username: "two", Status: UserStatusEnabled, AccessToken: "token-two", AffCode: "aff-two"},
		{Username: "gone", Status: UserStatusDeleted, AccessToken: "token-gone", AffCode: "aff-gone"},
	}
	require.NoError(t, DB.Create(&users).Error)
	count, err := CountUsers()
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
