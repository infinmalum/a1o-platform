package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupLogSearchTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	DB, LOG_DB = db, db
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
	})
	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeConsume, Content: "模型倍率 0.25", ModelName: "gpt-4", TokenName: "tok-a"}).Error)
	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeSystem, Content: "系统维护完成", ModelName: "claude-2", TokenName: "tok-b"}).Error)
	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeTopup, Content: "兑换码充值", ModelName: "", TokenName: ""}).Error)
	require.NoError(t, db.Create(&Log{UserId: 2, Type: LogTypeSystem, Content: "系统维护完成", ModelName: "claude-2", TokenName: "tok-c"}).Error)
	return db
}

// TestSearchUserLogsMatchesTextColumns pins that the keyword is matched
// against the log's text columns. It used to be compared against the numeric
// type column, so every non-numeric keyword returned nothing.
func TestSearchUserLogsMatchesTextColumns(t *testing.T) {
	setupLogSearchTest(t)

	logs, err := SearchUserLogs(1, "gpt-4")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "gpt-4", logs[0].ModelName)

	logs, err = SearchUserLogs(1, "系统维护")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "tok-b", logs[0].TokenName)

	logs, err = SearchUserLogs(1, "tok-a")
	require.NoError(t, err)
	require.Len(t, logs, 1)

	// Numeric keywords must not leak through the type column: user 1 has a
	// type=1 top-up log, but no text column containing "1". The old code
	// matched the keyword against the numeric type and returned that row.
	logs, err = SearchUserLogs(1, "1")
	require.NoError(t, err)
	require.Empty(t, logs)

	// Search stays within the user's own logs.
	logs, err = SearchUserLogs(2, "系统维护")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, 2, logs[0].UserId)
}

// TestSearchAllLogsMatchesTextColumns covers the admin variant, which had the
// same keyword-as-type defect.
func TestSearchAllLogsMatchesTextColumns(t *testing.T) {
	setupLogSearchTest(t)

	logs, err := SearchAllLogs("充值")
	require.NoError(t, err)
	require.Len(t, logs, 1)

	logs, err = SearchAllLogs("claude-2")
	require.NoError(t, err)
	require.Len(t, logs, 2)

	logs, err = SearchAllLogs("tok-c")
	require.NoError(t, err)
	require.Len(t, logs, 1)

	logs, err = SearchAllLogs("不存在的关键字")
	require.NoError(t, err)
	require.Empty(t, logs)
}
