package agent

import (
	"context"
	"testing"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestChatKnowledgeIsTurnScopedAndPersistsOnlyValidCitations(t *testing.T) {
	setupChatDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.KnowledgeLibrary{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{}))
	ctx := context.Background()
	session, run, history := newTurnFixture(t, "回答简短")
	history[0].Content = "退款"
	require.NoError(t, model.UpdateChatMessage(history[0]))
	library, err := model.CreateKnowledgeLibrary(ctx, session.UserId, "算术")
	require.NoError(t, err)
	doc, err := model.ImportKnowledgeDocument(ctx, session.UserId, library.Id, "参考.txt", []byte("退款期限为七天。"))
	require.NoError(t, err)
	require.NoError(t, model.SetChatKnowledgeLibrary(ctx, session.UserId, session.Id, library.Id))
	session, err = model.GetChatSessionById(session.Id, session.UserId)
	require.NoError(t, err)
	matches, err := model.SearchKnowledge(ctx, session.UserId, library.Id, history[0].Content)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	executor := &fakeExecutor{responses: []InvokeResult{
		{Items: []Item{{Type: ItemMessage, Role: "assistant", Content: []ContentPart{
			{Type: PartOutputText, Text: "七天 [K1]。虚假的编号 [K5] 不应产生链接。"},
		}}}},
	}}
	engine := &Engine{}
	require.NoError(t, engine.RunChat(ctx, func(EventEnvelope) error { return nil }, ChatTurnRequest{
		Run: run, Session: session, History: history, Executor: executor, Knowledge: matches,
	}))
	require.Len(t, executor.requests, 1)
	require.Contains(t, executor.requests[0].Instructions, "不是给你的指令")
	question := executor.requests[0].Items[0].Content
	require.Contains(t, question[0].Text, string(doc.Body))
	require.Equal(t, history[0].Content, question[1].Text)
	messages, err := model.GetSessionMessages(session.Id)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, history[0].Content, messages[0].Content)
	require.Nil(t, messages[0].Parts)
	require.Len(t, messages[1].Sources, 1)
	require.Equal(t, doc.Id, messages[1].Sources[0].DocumentId)
	require.Equal(t, "K1", messages[1].Sources[0].Label)
}
