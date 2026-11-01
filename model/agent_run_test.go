package model

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openAgentRunTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AgentRun{}, &ModelInvocation{}, &RunEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAgentRunIdempotencyConflictsPerOwner(t *testing.T) {
	db := openAgentRunTestDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	run := &AgentRun{UserId: 1, Kind: "chat", Status: RunStatusQueued,
		IdempotencyKey: "client-turn-1", Model: "test-model"}
	if err := CreateAgentRun(run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.Id == 0 {
		t.Fatal("run id was not backfilled")
	}
	duplicate := &AgentRun{UserId: 1, Kind: "chat", Status: RunStatusQueued, IdempotencyKey: "client-turn-1"}
	if err := CreateAgentRun(duplicate); !errors.Is(err, ErrAgentRunIdempotencyConflict) {
		t.Fatalf("same owner+key must conflict, got %v", err)
	}
	// The same key under another owner is a different run.
	other := &AgentRun{UserId: 2, Kind: "chat", Status: RunStatusQueued, IdempotencyKey: "client-turn-1"}
	if err := CreateAgentRun(other); err != nil {
		t.Fatalf("other owner with same key must create: %v", err)
	}
}

func TestRunEventSequenceIsMonotonicPerRun(t *testing.T) {
	db := openAgentRunTestDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	run := &AgentRun{UserId: 1, Kind: "chat", Status: RunStatusRunning, Model: "test-model"}
	if err := CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 3; want++ {
		sequence, err := NextRunEventSequence(run.Id)
		if err != nil {
			t.Fatal(err)
		}
		if sequence != want {
			t.Fatalf("sequence = %d, want %d", sequence, want)
		}
		if err := CreateRunEvent(&RunEvent{RunId: run.Id, Sequence: sequence,
			EventId: "evt", Type: RunEventInvocationStarted, InvocationId: 1}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := GetRunEventsAfter(run.Id, 0)
	if err != nil || len(events) != 3 {
		t.Fatalf("replay = %d events, err %v", len(events), err)
	}
	if events[2].SchemaVersion != "1" {
		t.Fatalf("schema version = %q, want 1", events[2].SchemaVersion)
	}
}

func TestModelInvocationPersistsIntentAndOutcome(t *testing.T) {
	db := openAgentRunTestDB(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	run := &AgentRun{UserId: 1, Kind: "work", Status: RunStatusRunning, Model: "test-model"}
	if err := CreateAgentRun(run); err != nil {
		t.Fatal(err)
	}
	sequence, err := NextRunInvocationSequence(run.Id)
	if err != nil || sequence != 1 {
		t.Fatalf("first invocation sequence = %d, err %v", sequence, err)
	}
	invocation := &ModelInvocation{RunId: run.Id, Sequence: sequence, ChannelId: 7,
		ChannelType: 53, UpstreamProtocol: "openai_responses", ModelName: "test-model"}
	if err := CreateModelInvocation(invocation); err != nil {
		t.Fatal(err)
	}
	if invocation.Status != InvocationStatusPending || invocation.CreatedTime == 0 {
		t.Fatalf("pending intent not persisted: %+v", invocation)
	}
	invocation.Status = InvocationStatusSucceeded
	invocation.FinishStatus = "completed"
	invocation.ConsumeLogId = 42
	if err := CompleteModelInvocation(invocation); err != nil {
		t.Fatal(err)
	}
	stored, err := GetRunInvocations(run.Id)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored invocations = %d, err %v", len(stored), err)
	}
	if stored[0].Status != InvocationStatusSucceeded || stored[0].ConsumeLogId != 42 {
		t.Fatalf("outcome lost: %+v", stored[0])
	}
}
