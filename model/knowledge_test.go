package model

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKnowledgeLibraryOwnershipRetrievalAndDeletion(t *testing.T) {
	previous := DB
	DB = openAgentRunTestDB(t)
	t.Cleanup(func() { DB = previous })
	require.NoError(t, DB.AutoMigrate(&ChatSession{}, &KnowledgeLibrary{}, &KnowledgeDocument{}, &KnowledgeChunk{}))

	ctx := context.Background()
	library, err := CreateKnowledgeLibrary(ctx, 1, "手册")
	require.NoError(t, err)
	document, err := ImportKnowledgeDocument(ctx, 1, library.Id, "手册.md", []byte("# 退款流程\n退款须在七天内申请，出错时记录 ERR-042。\n# 登录\n使用账号登录。"))
	require.NoError(t, err)
	_, err = ImportKnowledgeDocument(ctx, 2, library.Id, "private.txt", []byte("不可写入"))
	require.ErrorIs(t, err, ErrKnowledgeNotFound)

	for _, query := range []string{"退款", "如何申请退款？", "err-042"} {
		matches, err := SearchKnowledge(ctx, 1, library.Id, query)
		require.NoError(t, err)
		require.NotEmpty(t, matches, query)
		require.Equal(t, document.Id, matches[0].DocumentId)
		require.Equal(t, "退款流程", matches[0].Heading)
		require.Equal(t, 1, matches[0].StartLine)
		require.Equal(t, 2, matches[0].EndLine)
	}
	hidden, err := SearchKnowledge(ctx, 2, library.Id, "退款")
	require.NoError(t, err)
	require.Empty(t, hidden)
	_, err = GetKnowledgeDocument(ctx, 2, document.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	_, err = ListKnowledgeDocuments(ctx, 2, library.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	preview, err := GetKnowledgeDocument(ctx, 1, document.Id)
	require.NoError(t, err)
	require.Equal(t, document.Body, preview.Body)
	require.ErrorIs(t, DeleteKnowledgeDocument(ctx, 2, library.Id, document.Id), ErrKnowledgeNotFound)
	require.ErrorIs(t, DeleteKnowledgeLibrary(ctx, 2, library.Id), ErrKnowledgeNotFound)

	for range 2 {
		session := ChatSession{UserId: 1, Model: "test-model"}
		require.NoError(t, CreateChatSession(&session))
		require.ErrorIs(t, SetChatKnowledgeLibrary(ctx, 2, session.Id, library.Id), ErrKnowledgeNotFound)
		require.NoError(t, SetChatKnowledgeLibrary(ctx, 1, session.Id, library.Id))
		actual, err := GetChatSessionById(session.Id, 1)
		require.NoError(t, err)
		require.Equal(t, library.Id, actual.KnowledgeLibraryId)
	}

	require.NoError(t, DeleteKnowledgeDocument(ctx, 1, library.Id, document.Id))
	matches, err := SearchKnowledge(ctx, 1, library.Id, "退款")
	require.NoError(t, err)
	require.Empty(t, matches)
	_, err = GetKnowledgeDocument(ctx, 1, document.Id)
	require.ErrorIs(t, err, ErrKnowledgeNotFound)
	libraries, err := ListKnowledgeLibraries(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, libraries[0].Size)
	require.Zero(t, libraries[0].DocumentCount)

	require.NoError(t, DeleteKnowledgeLibrary(ctx, 1, library.Id))
	sessions, err := GetUserChatSessions(1)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	for _, session := range sessions {
		require.Zero(t, session.KnowledgeLibraryId)
	}
}

func TestKnowledgeImportLimitsAndSearchBound(t *testing.T) {
	previous := DB
	DB = openAgentRunTestDB(t)
	t.Cleanup(func() { DB = previous })
	require.NoError(t, DB.AutoMigrate(&KnowledgeLibrary{}, &KnowledgeDocument{}, &KnowledgeChunk{}))
	ctx := context.Background()
	library, err := CreateKnowledgeLibrary(ctx, 1, "容量验证")
	require.NoError(t, err)

	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "invalid.txt", []byte{0xff})
	require.Error(t, err)
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "invalid.pdf", []byte("text"))
	require.Error(t, err)
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "large.txt", make([]byte, KnowledgeDocumentLimit+1))
	require.Error(t, err)
	body := []byte("ERR-042 " + strings.Repeat("x", KnowledgeDocumentLimit-len("ERR-042 ")))
	for range KnowledgeLibraryLimit / KnowledgeDocumentLimit {
		_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "bounded.txt", body)
		require.NoError(t, err)
	}
	_, err = ImportKnowledgeDocument(ctx, 1, library.Id, "overflow.txt", []byte("ERR-042"))
	require.ErrorIs(t, err, ErrKnowledgeLimit)
	matches, err := SearchKnowledge(ctx, 1, library.Id, "ERR-042")
	require.NoError(t, err)
	require.Len(t, matches, knowledgeMaxResults)
}
