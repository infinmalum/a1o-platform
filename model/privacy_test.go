package model

import (
	"archive/zip"
	"bytes"
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

	start := time.Now().Unix()
	require.NoError(t, DeleteChatSession(retained.Id, owner.Id))
	require.Error(t, DeleteChatSession(stranger.Id, owner.Id))
	chats, err := ListRetainedChats(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	require.GreaterOrEqual(t, chats[0].PurgeAt, start+int64(privacyRetention/time.Second))
	require.NoError(t, prunePrivacyData(t.Context(), chats[0].PurgeAt-1))
	_, err = GetChatSessionById(retained.Id, owner.Id)
	require.Error(t, err)
	_, err = GetAgentRun(run.Id, owner.Id)
	require.Error(t, err)
	recent, err := GetRecentAgentRuns(owner.Id, 10)
	require.NoError(t, err)
	require.Empty(t, recent)
	messages, err := ReviewRetainedChat(t.Context(), 777, retained.Id, 0, "case 12")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "retained title body", messages[0].Content)
	_, err = ReviewRetainedChat(t.Context(), 777, active.Id, 0, "case 12")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	audit, err := ListPrivacyReviewAccess(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, audit, 1)

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
	require.NoError(t, prunePrivacyData(t.Context(), deleted.PrivacyPurgeAt))
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
