package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBranchChatSessionCopiesHistoryWithoutCopyingLatestReply(t *testing.T) {
	previous := DB
	DB = openAgentRunTestDB(t)
	t.Cleanup(func() { DB = previous })
	require.NoError(t, DB.AutoMigrate(&ChatSession{}, &ChatMessage{}))

	session := ChatSession{UserId: 1, Title: "original", Model: "test", KnowledgeLibraryId: 42, AgentVersionId: 21, SkillIds: []int{7}}
	require.NoError(t, CreateChatSession(&session))
	var firstUser ChatMessage
	// More than one database batch verifies that pagination follows the source
	// IDs even while the branch inserts rows with newer IDs into the same table.
	for index := range 42 {
		role := ChatRoleUser
		if index%2 == 1 {
			role = ChatRoleAssistant
		}
		message := ChatMessage{SessionId: session.Id, Role: role, Content: "previous"}
		require.NoError(t, InsertChatMessage(&message))
		if index == 0 {
			firstUser = message
		}
	}
	saved, err := SaveChatCompaction(&session, firstUser.Id, "previous summary")
	require.NoError(t, err)
	require.True(t, saved)

	parts := `[{"type":"text","file_id":55}]`
	prompt := ChatMessage{SessionId: session.Id, Role: ChatRoleUser, Content: "retry this", Parts: &parts}
	reply := ChatMessage{SessionId: session.Id, Role: ChatRoleAssistant, Content: "old answer"}
	require.NoError(t, InsertChatMessage(&prompt))
	require.NoError(t, InsertChatMessage(&reply))

	_, _, err = BranchChatSession(context.Background(), &session, firstUser.Id, "stale")
	require.ErrorIs(t, err, ErrChatBranchUnavailable)
	branch, messageID, err := BranchChatSession(context.Background(), &session, prompt.Id, "retry")
	require.NoError(t, err)
	require.NotEqual(t, session.Id, branch.Id)
	require.Equal(t, session.UserId, branch.UserId)
	require.Equal(t, session.KnowledgeLibraryId, branch.KnowledgeLibraryId)
	require.Equal(t, session.AgentVersionId, branch.AgentVersionId)
	require.Equal(t, session.SkillIds, branch.SkillIds)
	require.Equal(t, session.Summary, branch.Summary)
	stored, err := GetChatSessionById(branch.Id, session.UserId)
	require.NoError(t, err)
	require.Equal(t, session.SkillIds, stored.SkillIds)

	rows, err := GetSessionMessages(branch.Id)
	require.NoError(t, err)
	require.Len(t, rows, 43)
	require.Equal(t, rows[0].Id, branch.CompactedThrough)
	require.Equal(t, messageID, rows[42].Id)
	require.Equal(t, prompt.Content, rows[42].Content)
	require.Equal(t, prompt.Parts, rows[42].Parts)
	require.NotEqual(t, prompt.Id, messageID)

	original, err := GetSessionMessages(session.Id)
	require.NoError(t, err)
	require.Len(t, original, 44)
	require.Equal(t, reply.Id, original[43].Id)
	require.Equal(t, reply.Content, original[43].Content)
}
