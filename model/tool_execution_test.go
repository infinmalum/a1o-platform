package model

import (
	"testing"
)

func TestToolExecutionLifecycle(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusRunning)

	execution := &ToolExecution{RunId: run.Id, InvocationId: 3, CallId: "c1",
		Name: "sandbox", Arguments: `{"code":"print(1)"}`}
	if err := CreateToolExecution(execution); err != nil {
		t.Fatalf("create intent: %v", err)
	}
	if execution.Id <= 0 || execution.Status != ToolExecutionStatusRunning {
		t.Fatalf("intent must land as running: %+v", execution)
	}
	if execution.CreatedTime <= 0 {
		t.Fatalf("created time must be stamped: %+v", execution)
	}

	pending, err := ListPendingToolExecutions(run.Id)
	if err != nil || len(pending) != 1 || pending[0].Name != "sandbox" {
		t.Fatalf("pending must list the in-flight execution: %+v err=%v", pending, err)
	}

	if err := CompleteToolExecution(execution.Id, ToolExecutionStatusCompleted, false, 42); err != nil {
		t.Fatalf("complete: %v", err)
	}
	pending, err = ListPendingToolExecutions(run.Id)
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed execution must leave pending: %+v err=%v", pending, err)
	}
	all, err := ListRunToolExecutions(run.Id)
	if err != nil || len(all) != 1 || all[0].Status != ToolExecutionStatusCompleted ||
		all[0].DurationMs != 42 || all[0].IsError {
		t.Fatalf("completed record mismatch: %+v err=%v", all, err)
	}

	// An unknown outcome — the crash-recovery state — also clears pending
	// but stays distinguishable from a clean result.
	other := &ToolExecution{RunId: run.Id, CallId: "c2", Name: "mcp_tool", Arguments: `{}`}
	if err := CreateToolExecution(other); err != nil {
		t.Fatal(err)
	}
	if err := CompleteToolExecution(other.Id, ToolExecutionStatusUnknown, false, 0); err != nil {
		t.Fatal(err)
	}
	if pending, _ := ListPendingToolExecutions(run.Id); len(pending) != 0 {
		t.Fatalf("unknown outcome must clear pending: %+v", pending)
	}
	all, _ = ListRunToolExecutions(run.Id)
	if all[1].Status != ToolExecutionStatusUnknown {
		t.Fatalf("unknown outcome must be preserved: %+v", all[1])
	}
}

func TestGetRunEventBounds(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusRunning)

	if _, _, exists, err := GetRunEventBounds(run.Id); err != nil || exists {
		t.Fatalf("empty log must report no bounds: exists=%v err=%v", exists, err)
	}

	for _, seq := range []int64{5, 2, 9} {
		if err := CreateRunEvent(&RunEvent{RunId: run.Id, Sequence: seq,
			EventId: "evt", Type: RunEventRunQueued}); err != nil {
			t.Fatal(err)
		}
	}
	minSeq, maxSeq, exists, err := GetRunEventBounds(run.Id)
	if err != nil || !exists {
		t.Fatalf("bounds: exists=%v err=%v", exists, err)
	}
	if minSeq != 2 || maxSeq != 9 {
		t.Fatalf("bounds must span min/max: min=%d max=%d", minSeq, maxSeq)
	}
}

func TestPruneQuietRunEvents(t *testing.T) {
	useRunQueueTestDB(t)
	cutoff := helperStampNow()
	_, quiet := newWorkRunFixture(t, 1, RunStatusCompleted)
	_, fresh := newWorkRunFixture(t, 1, RunStatusRunning)

	for _, run := range []*AgentRun{quiet, fresh} {
		for _, seq := range []int64{1, 2, 3} {
			if err := CreateRunEvent(&RunEvent{RunId: run.Id, Sequence: seq,
				EventId: "evt", Type: RunEventRunQueued}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Only the quiet run ages past the cutoff; the fresh run's state moves
	// on every leg, so its log stays available for reconnect replay.
	if err := DB.Model(&AgentRun{}).Where("id = ?", quiet.Id).
		Update("updated_time", cutoff-3600).Error; err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneQuietRunEvents(cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 3 {
		t.Fatalf("prune must remove exactly the quiet run's log: %d", pruned)
	}
	var count int64
	DB.Model(&RunEvent{}).Where("run_id = ?", fresh.Id).Count(&count)
	if count != 3 {
		t.Fatalf("fresh run's events must survive: %d", count)
	}
}
