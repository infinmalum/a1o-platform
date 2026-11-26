package model

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/infinmalum/norma-e/memory"
	"github.com/infinmalum/a1o-platform/common/blacklist"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPrivacyRetentionAndExport(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := gorm.Open(sqlite.Open("privacy.sqlite"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogDB; _ = sqlDB.Close() })
	require.NoError(t, migrateDB())
	owner := User{Username: "export-owner", Email: "owner@example.test", Password: "secret-password", AccessToken: "secret-access", AffCode: "export-owner"}
	other := User{Username: "export-other", AccessToken: "other-access", AffCode: "export-other"}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&other).Error)
	active := ChatSession{UserId: owner.Id, Title: "active title"}
	retained := ChatSession{UserId: owner.Id, Title: "retained title"}
	stranger := ChatSession{UserId: other.Id, Title: "stranger title"}
	for _, chat := range []*ChatSession{&active, &retained, &stranger} {
		require.NoError(t, db.Create(chat).Error)
		require.NoError(t, InsertChatMessage(&ChatMessage{SessionId: chat.Id, Role: ChatRoleUser, Content: chat.Title + " body"}))
	}
	require.NoError(t, InsertChatMessage(&ChatMessage{SessionId: retained.Id, Role: ChatRoleTool, Content: "private raw tool result"}))
	run := AgentRun{UserId: owner.Id, SessionId: retained.Id, Status: RunStatusCompleted}
	require.NoError(t, CreateAgentRun(&run))
	require.NoError(t, db.Create(&RunEvent{RunId: run.Id, Sequence: 1, Type: "privacy-test"}).Error)
	require.NoError(t, db.Create(&Token{UserId: owner.Id, Key: "secret-api-token"}).Error)
	file, err := SaveChatFilePayload(owner.Id, ChatFileKindText, "example.txt", []byte("original attachment"))
	require.NoError(t, err)

	store := UserMemoryStore(owner.Id)
	require.NoError(t, store.Save(memory.Memory{Name: "entry", Content: "note to export"}))
	enabled, entries, err := ListUserMemory(t.Context(), owner.Id)
	require.NoError(t, err)
	require.False(t, enabled)
	require.Len(t, entries, 1)

	var pruner privacyPruner
	start := time.Now().Unix()
	require.NoError(t, DeleteChatSession(retained.Id, owner.Id))
	require.Error(t, DeleteChatSession(stranger.Id, owner.Id))
	require.NoError(t, db.Take(&retained, retained.Id).Error)
	require.GreaterOrEqual(t, retained.PrivacyPurgeAt, start+int64(privacyRetention/time.Second))
	require.NoError(t, pruner.prune(t.Context(), retained.PrivacyPurgeAt-1))
	_, err = GetChatSessionById(retained.Id, owner.Id)
	require.Error(t, err)
	_, err = GetAgentRun(run.Id, owner.Id)
	require.Error(t, err)
	recent, err := GetRecentAgentRuns(owner.Id, 10)
	require.NoError(t, err)
	require.Empty(t, recent)

	var archive bytes.Buffer
	require.NoError(t, WritePrivacyExport(t.Context(), &archive, owner.Id))
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	entriesByName := map[string]string{}
	for _, entry := range reader.File {
		fileReader, err := entry.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(fileReader)
		require.NoError(t, err)
		require.NoError(t, fileReader.Close())
		entriesByName[entry.Name] = string(content)
		require.NotContains(t, string(content), "secret-")
		require.NotContains(t, string(content), "stranger title")
		require.NotContains(t, string(content), "retained title")
	}
	require.Contains(t, entriesByName["messages.jsonl"], "active title body")
	require.Contains(t, entriesByName["memory.json"], "note to export")
	require.Equal(t, "original attachment", entriesByName["attachments/1"])
	require.NoError(t, DeleteUserMemory(t.Context(), owner.Id, entries[0].ID))
	_, entries, err = ListUserMemory(t.Context(), owner.Id)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.NoError(t, store.Save(memory.Memory{Name: "after", Content: "cleanup this"}))

	payment := PaymentOrder{ID: "fixture-order", UserID: owner.Id, Amount: 120, Currency: "usd", Quota: 100}
	require.NoError(t, db.Create(&payment).Error)
	t.Cleanup(func() { blacklist.UnbanUser(owner.Id) })
	require.NoError(t, owner.Delete())
	_, err = os.Stat(store.Dir)
	require.True(t, os.IsNotExist(err))
	require.Nil(t, ValidateAccessToken("secret-access"))
	var deleted User
	require.NoError(t, db.Take(&deleted, owner.Id).Error)
	require.Equal(t, UserStatusDeleted, deleted.Status)
	require.Empty(t, deleted.Email)
	require.NoError(t, markAccountDeleted(t.Context(), owner.Id))
	require.NoError(t, db.Take(&deleted, owner.Id).Error)
	require.NoError(t, pruner.prune(t.Context(), deleted.PrivacyPurgeAt))
	require.ErrorIs(t, db.Take(&ChatSession{}, active.Id).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.Take(&ChatSession{}, retained.Id).Error, gorm.ErrRecordNotFound)
	require.ErrorIs(t, db.Take(&AgentRun{}, run.Id).Error, gorm.ErrRecordNotFound)
	var copies int64
	require.NoError(t, db.Model(&RunEvent{}).Where("run_id = ?", run.Id).Count(&copies).Error)
	require.Zero(t, copies)
	_, err = os.Stat(file.Path)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Clean(store.Dir))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, db.Take(&ChatSession{}, stranger.Id).Error)
	require.NoError(t, db.Take(&payment, "id = ?", payment.ID).Error)
}

func TestPrivacyBatchResumesPastTimedOutItem(t *testing.T) {
	db := openAgentRunTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChatSession{}))
	for id := 1; id <= 3; id++ {
		require.NoError(t, db.Create(&ChatSession{Id: id, PrivacyPurgeAt: 1}).Error)
	}
	var cursor int
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := prunePrivacyBatch(ctx, db.Model(&ChatSession{}), &cursor, 2, 1, "conversation",
		func(_ context.Context, id int, _ int64) error {
			require.Equal(t, 1, id)
			cancel()
			return context.DeadlineExceeded
		})
	require.ErrorContains(t, err, "conversation 1")
	require.Equal(t, 1, cursor)

	var visited []int
	require.NoError(t, prunePrivacyBatch(t.Context(), db.Model(&ChatSession{}), &cursor, 2, 1, "conversation",
		func(_ context.Context, id int, _ int64) error {
			visited = append(visited, id)
			return db.Delete(&ChatSession{}, id).Error
		}))
	require.Equal(t, []int{2, 3}, visited)
	require.Equal(t, 3, cursor)
	require.NoError(t, prunePrivacyBatch(t.Context(), db.Model(&ChatSession{}), &cursor, 2, 1, "conversation",
		func(_ context.Context, _ int, _ int64) error {
			t.Fatal("scan should wrap after reaching the end")
			return nil
		}))
	require.Zero(t, cursor)
}
