package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
)

func setupChatDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := openChatTestDB()
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
}

func TestSystemContentJoinsPromptAndSkills(t *testing.T) {
	session := &model.ChatSession{SystemPrompt: "保持简洁"}
	skills := []*model.ChatSkill{
		{Name: "翻译", Instructions: "把用户输入翻译成英文。"},
		{Name: "空", Instructions: "  "},
	}
	got := systemContent(session, skills)
	want := "保持简洁\n\n## Skill: 翻译\n把用户输入翻译成英文。"
	if got != want {
		t.Fatalf("systemContent = %q, want %q", got, want)
	}
}

func TestHistoryToItemsResolvesAttachments(t *testing.T) {
	dir := t.TempDir()
	textPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(textPath, []byte("hello from file"), 0600); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(dir, "pixel.png")
	// A 1x1 PNG.
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(imagePath, png, 0600); err != nil {
		t.Fatal(err)
	}
	files := map[int]*model.ChatFile{
		7: {Id: 7, Name: "note.txt", Kind: model.ChatFileKindText, Path: textPath},
		9: {Id: 9, Name: "pixel.png", Kind: model.ChatFileKindImage, Path: imagePath},
	}
	partsJSON := `[{"type":"text","file_id":7},{"type":"image_url","file_id":9},{"type":"image_url","file_id":11}]`
	encoded := partsJSON
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: "看看这个", Parts: &encoded},
		{Role: model.ChatRoleAssistant, Content: "收到"},
	}
	entries, err := historyToItems(history, files, "test-model")
	if err != nil {
		t.Fatalf("historyToItems failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	parts := entries[0].items[0].Content
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want 4", len(parts))
	}
	if parts[0].Type != PartInputText || parts[0].Text != "看看这个" {
		t.Fatalf("text part = %+v", parts[0])
	}
	if !strings.HasPrefix(parts[1].Text, "[文件 note.txt]\nhello from file") {
		t.Fatalf("text file part = %q", parts[1].Text)
	}
	if parts[2].Type != PartInputImage || !strings.HasPrefix(parts[2].ImageURL, "data:image/png;base64,") {
		t.Fatalf("image part missing data URL: %+v", parts[2])
	}
	if parts[3].Text == "" {
		t.Fatalf("missing placeholder for deleted file: %+v", parts[3])
	}
	if entries[1].items[0].Content[0].Text != "收到" {
		t.Fatalf("assistant content = %+v", entries[1].items[0])
	}
}

// TestHistoryToItemsReplaysReasoningAndToolSignatures pins that an assistant
// row's opaque state rebuilds as a reasoning item preceding the message and
// its function-call items.
func TestHistoryToItemsReplaysReasoningAndToolSignatures(t *testing.T) {
	reasoningJSON := `{"summary":[{"type":"summary_text","text":"推演"}],"signature":"sig-1","thought_signatures":{"c1":"tm-1"}}`
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: "问"},
		{Role: model.ChatRoleAssistant, Content: "答", Reasoning: &reasoningJSON,
			ToolCalls: ptrString(`[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{}"}}]`)},
		{Role: model.ChatRoleTool, Content: "结果", ToolCallId: "c1", Name: "echo"},
	}
	entries, err := historyToItems(history, nil, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	items := entries[1].items
	if len(items) != 3 {
		t.Fatalf("assistant items = %d, want reasoning + message + function_call", len(items))
	}
	if items[0].Type != ItemReasoning || items[0].Signature != "sig-1" || items[0].Summary[0].Text != "推演" {
		t.Fatalf("reasoning item wrong: %+v", items[0])
	}
	if items[1].Type != ItemMessage || items[1].Role != "assistant" || items[1].Content[0].Text != "答" {
		t.Fatalf("message item wrong: %+v", items[1])
	}
	if items[2].Type != ItemFunctionCall || items[2].CallID != "c1" || items[2].ThoughtSignature != "tm-1" {
		t.Fatalf("function_call item wrong: %+v", items[2])
	}
	if entries[2].items[0].Type != ItemFunctionCallOutput || entries[2].items[0].Output != "结果" {
		t.Fatalf("tool output item wrong: %+v", entries[2].items[0])
	}
}

func insertChatFixture(t *testing.T, session *model.ChatSession, messages ...*model.ChatMessage) {
	t.Helper()
	if err := model.CreateChatSession(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, message := range messages {
		message.SessionId = session.Id
		if err := model.InsertChatMessage(message); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
}

// insertMessages persists history rows for an already-created session.
func insertMessages(t *testing.T, session *model.ChatSession, messages ...*model.ChatMessage) {
	t.Helper()
	for _, message := range messages {
		message.SessionId = session.Id
		if err := model.InsertChatMessage(message); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
}

func newCompactFixture(t *testing.T) (*model.ChatSession, *model.AgentRun) {
	t.Helper()
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	if err := model.CreateChatSession(session); err != nil {
		t.Fatal(err)
	}
	run := &model.AgentRun{UserId: session.UserId, SessionId: session.Id, Kind: "chat",
		Status: model.RunStatusRunning, Model: session.Model}
	if err := model.CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func TestCompactIfNeededSkipsWithinBudget(t *testing.T) {
	setupChatDB(t)
	session, run := newCompactFixture(t)
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: "你好"},
		{Role: model.ChatRoleAssistant, Content: "你好！"},
	}
	insertMessages(t, session, history...)
	executor := &fakeExecutor{responses: []InvokeResult{{
		Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: "不应调用"}}},
		},
	}}}
	engine := &Engine{}
	turn := ChatTurnRequest{Run: run, Session: session, History: history, Executor: executor, ContextBudget: 100000}

	working, err := engine.compactIfNeeded(context.Background(), func(EventEnvelope) error { return nil }, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded failed: %v", err)
	}
	if len(executor.requests) != 0 {
		t.Fatalf("summarizer called %d times within budget", len(executor.requests))
	}
	if len(working) != len(history) {
		t.Fatalf("working set = %d, want %d", len(working), len(history))
	}
	if session.Summary != "" || session.CompactedThrough != 0 {
		t.Fatalf("session mutated without compaction: %q %d", session.Summary, session.CompactedThrough)
	}
}

func TestCompactIfNeededSummarizesOldHistory(t *testing.T) {
	setupChatDB(t)
	session, run := newCompactFixture(t)
	var history []*model.ChatMessage
	// Long filler messages drive the estimate over the tiny budget.
	for i := 0; i < 6; i++ {
		history = append(history, &model.ChatMessage{
			Role: model.ChatRoleUser, Content: strings.Repeat("讨论长上下文压缩策略。", 60),
		})
		history = append(history, &model.ChatMessage{
			Role: model.ChatRoleAssistant, Content: strings.Repeat("好的，我们继续分析。", 60),
		})
	}
	insertMessages(t, session, history...)
	executor := &fakeExecutor{responses: []InvokeResult{{
		Items: []Item{
			{Type: ItemMessage, Role: "assistant", Content: []ContentPart{{Type: PartOutputText, Text: " 用户在讨论上下文压缩。"}}},
		},
	}}}
	engine := &Engine{}
	turn := ChatTurnRequest{Run: run, Session: session, History: history, Executor: executor, ContextBudget: 900}

	var compacted EventEnvelope
	working, err := engine.compactIfNeeded(context.Background(), func(event EventEnvelope) error {
		compacted = event
		return nil
	}, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded failed: %v", err)
	}
	if len(executor.requests) != 1 {
		t.Fatalf("summarizer called %d times, want 1", len(executor.requests))
	}
	if session.Summary == "" || session.CompactedThrough == 0 {
		t.Fatalf("summary not persisted: %q %d", session.Summary, session.CompactedThrough)
	}
	if compacted.Type != model.RunEventContextCompacted {
		t.Fatalf("compact event = %+v", compacted)
	}
	if len(working) >= len(history) {
		t.Fatalf("working set not reduced: %d of %d", len(working), len(history))
	}
	// The kept side must include the newest message.
	if working[len(working)-1].Id != history[len(history)-1].Id {
		t.Fatalf("newest message not kept")
	}
	// The persisted session row carries the summary.
	stored, err := model.GetChatSessionById(session.Id, 1)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if stored.Summary != session.Summary || stored.CompactedThrough != session.CompactedThrough {
		t.Fatalf("stored summary mismatch: %q %d", stored.Summary, stored.CompactedThrough)
	}
	// The summarizer leg never feeds the run context.
	if len(executor.requests) == 1 && !executor.requests[0].SkipContextAppend {
		t.Fatalf("summarizer leg must skip context append")
	}
}

func TestCompactIfNeededSurvivesSummarizerFailure(t *testing.T) {
	setupChatDB(t)
	previousBudget := config.ChatContextBudgetTokens
	config.ChatContextBudgetTokens = 100
	t.Cleanup(func() { config.ChatContextBudgetTokens = previousBudget })

	session, run := newCompactFixture(t)
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: strings.Repeat("这是一个很长的历史消息。", 200)},
		{Role: model.ChatRoleAssistant, Content: strings.Repeat("很长很长的回复内容。", 200)},
	}
	insertMessages(t, session, history...)
	executor := &fakeExecutor{errs: []error{context.DeadlineExceeded}}
	engine := &Engine{}
	turn := ChatTurnRequest{Run: run, Session: session, History: history, Executor: executor, ContextBudget: 100}

	working, err := engine.compactIfNeeded(context.Background(), func(EventEnvelope) error { return nil }, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded should survive summarizer failure: %v", err)
	}
	if len(working) != len(history) {
		t.Fatalf("working set = %d, want unchanged %d", len(working), len(history))
	}
}
