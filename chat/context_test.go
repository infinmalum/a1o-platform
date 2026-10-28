package chat

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

func TestHistoryToWireResolvesAttachments(t *testing.T) {
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
	items, err := historyToWire(history, files, "test-model")
	if err != nil {
		t.Fatalf("historyToWire failed: %v", err)
	}
	parts, ok := items[0].msg.Content.([]contentPart)
	if !ok {
		t.Fatalf("expected content parts, got %T", items[0].msg.Content)
	}
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want 4", len(parts))
	}
	if parts[0].Text != "看看这个" {
		t.Fatalf("text part = %q", parts[0].Text)
	}
	if parts[1].Text == "" || filepath.Base(parts[1].Text[:11]) == "" {
		t.Fatalf("text file part missing content: %q", parts[1].Text)
	}
	if parts[2].ImageURL == nil || len(parts[2].ImageURL.URL) < len("data:image/png;base64,") {
		t.Fatalf("image part missing data URL: %+v", parts[2])
	}
	if parts[3].Text == "" {
		t.Fatalf("missing placeholder for deleted file: %+v", parts[3])
	}
	if items[1].msg.Content != "收到" {
		t.Fatalf("assistant content = %v", items[1].msg.Content)
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

func TestCompactIfNeededSkipsWithinBudget(t *testing.T) {
	setupChatDB(t)
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: "你好"},
		{Role: model.ChatRoleAssistant, Content: "你好！"},
	}
	insertChatFixture(t, session, history...)
	leg := &fakeLeg{responses: []fakeLegResponse{{result: LegResult{Content: "不应调用"}}}}
	runtime := &Runtime{}
	turn := TurnRequest{Session: session, History: history, Leg: leg, ContextBudget: 100000}

	working, err := runtime.compactIfNeeded(context.Background(), func(Event) error { return nil }, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded failed: %v", err)
	}
	if len(leg.requests) != 0 {
		t.Fatalf("summarizer called %d times within budget", len(leg.requests))
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
	session := &model.ChatSession{UserId: 1, Model: "test-model"}
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
	insertChatFixture(t, session, history...)
	leg := &fakeLeg{responses: []fakeLegResponse{{result: LegResult{Content: " 用户在讨论上下文压缩。"}}}}
	runtime := &Runtime{}
	turn := TurnRequest{Session: session, History: history, Leg: leg, ContextBudget: 900}

	var compacted Event
	working, err := runtime.compactIfNeeded(context.Background(), func(event Event) error {
		compacted = event
		return nil
	}, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded failed: %v", err)
	}
	if len(leg.requests) != 1 {
		t.Fatalf("summarizer called %d times, want 1", len(leg.requests))
	}
	if session.Summary == "" || session.CompactedThrough == 0 {
		t.Fatalf("summary not persisted: %q %d", session.Summary, session.CompactedThrough)
	}
	if compacted.Type != EventCompact || compacted.Content != session.Summary {
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
}

func TestCompactIfNeededSurvivesSummarizerFailure(t *testing.T) {
	setupChatDB(t)
	previousBudget := config.ChatContextBudgetTokens
	config.ChatContextBudgetTokens = 100
	t.Cleanup(func() { config.ChatContextBudgetTokens = previousBudget })

	session := &model.ChatSession{UserId: 1, Model: "test-model"}
	history := []*model.ChatMessage{
		{Role: model.ChatRoleUser, Content: strings.Repeat("这是一个很长的历史消息。", 200)},
		{Role: model.ChatRoleAssistant, Content: strings.Repeat("很长很长的回复内容。", 200)},
	}
	insertChatFixture(t, session, history...)
	leg := &fakeLeg{responses: []fakeLegResponse{{err: context.DeadlineExceeded}}}
	runtime := &Runtime{}
	turn := TurnRequest{Session: session, History: history, Leg: leg, ContextBudget: 100}

	working, err := runtime.compactIfNeeded(context.Background(), func(Event) error { return nil }, turn, newToolSet())
	if err != nil {
		t.Fatalf("compactIfNeeded should survive summarizer failure: %v", err)
	}
	if len(working) != len(history) {
		t.Fatalf("working set = %d, want unchanged %d", len(working), len(history))
	}
}
