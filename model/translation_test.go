package model

import (
	"errors"
	"testing"
)

func openTranslationTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	db := openAgentRunTestDB(t)
	if err := db.AutoMigrate(&TranslationRecord{}); err != nil {
		t.Fatal(err)
	}
	DB = db
	t.Cleanup(func() { DB = previous })
}

func TestTranslationIdempotentReplayAndConflict(t *testing.T) {
	openTranslationTestDB(t)
	first := &TranslationRecord{UserId: 7, Engine: TranslationEngineDeepL, TargetLang: "ZH",
		InputText: "hello world", Status: TranslationStatusRunning, IdempotencyKey: "key-1"}
	existing, created, err := CreateTranslationRecord(first)
	if err != nil || !created {
		t.Fatalf("first insert: created=%v err=%v", created, err)
	}
	replay, created, err := CreateTranslationRecord(&TranslationRecord{UserId: 7, Engine: TranslationEngineDeepL,
		TargetLang: "ZH", InputText: "hello world", IdempotencyKey: "key-1"})
	if err != nil || created {
		t.Fatalf("same key + same content must replay: created=%v err=%v", created, err)
	}
	if replay.Id != existing.Id {
		t.Fatalf("replay returned record %d, want %d", replay.Id, existing.Id)
	}
	if _, _, err := CreateTranslationRecord(&TranslationRecord{UserId: 7, Engine: TranslationEngineDeepL,
		TargetLang: "ZH", InputText: "different text", IdempotencyKey: "key-1"}); !errors.Is(err, ErrTranslationConflict) {
		t.Fatalf("same key + different content must conflict, got %v", err)
	}
	// Another user's key never collides.
	if _, created, err := CreateTranslationRecord(&TranslationRecord{UserId: 8, Engine: TranslationEngineLLM,
		Model: "m", TargetLang: "ZH", InputText: "hello world", IdempotencyKey: "key-1"}); err != nil || !created {
		t.Fatalf("cross-user key must insert: created=%v err=%v", created, err)
	}
}

func TestTranslationTerminalTransitions(t *testing.T) {
	openTranslationTestDB(t)
	record := &TranslationRecord{UserId: 7, Engine: TranslationEngineLLM, Model: "m",
		TargetLang: "EN", InputText: "你好", Status: TranslationStatusRunning, IdempotencyKey: "key-2"}
	if _, _, err := CreateTranslationRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := FailTranslationRecord(record.Id, "上游不可用"); err != nil {
		t.Fatal(err)
	}
	stored, err := GetTranslationRecordById(record.Id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != TranslationStatusFailed || stored.Error != "上游不可用" {
		t.Fatalf("failed record: %+v", stored)
	}
	if err := CompleteTranslationRecord(record.Id, "hello", "", 0, 5, 3, 91); err != nil {
		t.Fatal(err)
	}
	stored, err = GetTranslationRecordById(record.Id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != TranslationStatusCompleted || stored.OutputText != "hello" || stored.ConsumeLogId != 91 {
		t.Fatalf("completed record: %+v", stored)
	}
	// Owner scoping: another user cannot read it.
	if _, err := GetTranslationRecordById(record.Id, 8); err == nil {
		t.Fatal("foreign user must not read the record")
	}
}
