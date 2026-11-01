package model

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openRunQueueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AgentRun{}, &ModelInvocation{}, &RunEvent{},
		&WorkTask{}, &RunInput{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func useRunQueueTestDB(t *testing.T) {
	t.Helper()
	previous := DB
	DB = openRunQueueTestDB(t)
	t.Cleanup(func() { DB = previous })
}

func newWorkRunFixture(t *testing.T, userId int, status string) (*WorkTask, *AgentRun) {
	t.Helper()
	task := &WorkTask{UserId: userId, Title: "整理周报", Status: WorkTaskStatusActive}
	if err := CreateWorkTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	run := &AgentRun{UserId: userId, Kind: "work", Status: status, TaskId: task.Id,
		IdempotencyKey: "key-" + status + "-" + time.Now().Format("150405.000000000")}
	if err := CreateAgentRun(run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	return task, run
}

func TestClaimNextWorkRunBumpsFencingAndSetsLease(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusQueued)

	claimed, err := ClaimNextWorkRun("instance-a", 60, 1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil || claimed.Id != run.Id {
		t.Fatalf("expected to claim run %d, got %+v", run.Id, claimed)
	}
	if claimed.Status != RunStatusRunning || claimed.LeaseOwner != "instance-a" {
		t.Fatalf("claim did not take ownership: %+v", claimed)
	}
	if claimed.FencingToken != run.FencingToken+1 {
		t.Fatalf("fencing must increment on claim: %d -> %d", run.FencingToken, claimed.FencingToken)
	}
	if claimed.LeaseExpireTime <= claimed.UpdatedTime {
		t.Fatalf("lease expiry must be in the future: %d", claimed.LeaseExpireTime)
	}

	// A second claim must not return the same run; with nothing else queued
	// it returns nil.
	again, err := ClaimNextWorkRun("instance-b", 60, 1)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if again != nil {
		t.Fatalf("claimed an already-running run: %+v", again)
	}
}

func TestClaimNextWorkRunRespectsUserConcurrency(t *testing.T) {
	useRunQueueTestDB(t)
	_, first := newWorkRunFixture(t, 7, RunStatusQueued)
	_, second := newWorkRunFixture(t, 7, RunStatusQueued)
	_, other := newWorkRunFixture(t, 8, RunStatusQueued)

	// Mark the first run of user 7 as already running.
	if err := DB.Model(&AgentRun{}).Where("id = ?", first.Id).
		Updates(map[string]any{"status": RunStatusRunning, "lease_owner": "x",
			"lease_expire_time": helperStampNow() + 60}).Error; err != nil {
		t.Fatal(err)
	}

	claimed, err := ClaimNextWorkRun("instance-a", 60, 1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil || claimed.Id != other.Id {
		t.Fatalf("cap must skip user 7's second run and claim user 8's, got %+v", claimed)
	}
	_ = second
}

// helperStampNow is a tiny indirection so the test stays readable.
func helperStampNow() int64 {
	return time.Now().Unix()
}

func TestFencedWritesRejectStaleFencing(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusRunning)
	if err := DB.Model(&AgentRun{}).Where("id = ?", run.Id).
		Updates(map[string]any{"fencing_token": 5, "status": RunStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}

	ok, err := UpdateAgentRunStatusFenced(run.Id, 4, RunStatusRunning, RunStatusFailed, "stale")
	if err != nil {
		t.Fatalf("fenced write: %v", err)
	}
	if ok {
		t.Fatal("stale fencing must not write")
	}
	var reloaded AgentRun
	DB.Where("id = ?", run.Id).First(&reloaded)
	if reloaded.Status != RunStatusRunning {
		t.Fatalf("stale write landed: %s", reloaded.Status)
	}

	ok, err = UpdateAgentRunStatusFenced(run.Id, 5, RunStatusRunning, RunStatusFailed, "current")
	if err != nil || !ok {
		t.Fatalf("matching fencing must write (ok=%v err=%v)", ok, err)
	}

	ok, err = UpdateAgentRunContextFenced(run.Id, 5, "[]")
	if err != nil || !ok {
		t.Fatalf("context write with matching fencing must succeed (ok=%v err=%v)", ok, err)
	}
}

func TestRenewWorkLeaseFailsAfterCancelRequest(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusRunning)
	if err := DB.Model(&AgentRun{}).Where("id = ?", run.Id).
		Updates(map[string]any{"lease_owner": "instance-a", "fencing_token": 3,
			"lease_expire_time": helperStampNow() + 60}).Error; err != nil {
		t.Fatal(err)
	}

	ok, err := RenewWorkLease(run.Id, "instance-a", 3, 60)
	if err != nil || !ok {
		t.Fatalf("renewal must succeed (ok=%v err=%v)", ok, err)
	}
	if _, _, err := RequestRunCancel(run.Id); err != nil {
		t.Fatalf("cancel request: %v", err)
	}
	ok, err = RenewWorkLease(run.Id, "instance-a", 3, 60)
	if err != nil {
		t.Fatalf("renewal after cancel: %v", err)
	}
	if ok {
		t.Fatal("renewal must fail once cancel_requested")
	}
	var reloaded AgentRun
	DB.Where("id = ?", run.Id).First(&reloaded)
	if reloaded.LeaseExpireTime > helperStampNow() {
		t.Fatalf("cancel must expire the lease immediately: %d", reloaded.LeaseExpireTime)
	}
}

func TestRequestRunCancelTransitions(t *testing.T) {
	useRunQueueTestDB(t)

	// queued → cancelled directly
	_, queued := newWorkRunFixture(t, 1, RunStatusQueued)
	status, changed, err := RequestRunCancel(queued.Id)
	if err != nil || !changed || status != RunStatusCancelled {
		t.Fatalf("queued cancel: status=%s changed=%v err=%v", status, changed, err)
	}
	// idempotent repeat
	status, changed, err = RequestRunCancel(queued.Id)
	if err != nil || changed || status != RunStatusCancelled {
		t.Fatalf("repeat cancel must be a no-op: status=%s changed=%v err=%v", status, changed, err)
	}

	// waiting_input → cancelled
	_, waiting := newWorkRunFixture(t, 1, RunStatusWaitingInput)
	status, changed, err = RequestRunCancel(waiting.Id)
	if err != nil || !changed || status != RunStatusCancelled {
		t.Fatalf("waiting cancel: status=%s changed=%v err=%v", status, changed, err)
	}

	// running → cancel_requested
	_, running := newWorkRunFixture(t, 1, RunStatusRunning)
	status, changed, err = RequestRunCancel(running.Id)
	if err != nil || !changed || status != RunStatusCancelRequested {
		t.Fatalf("running cancel: status=%s changed=%v err=%v", status, changed, err)
	}

	// terminal → no-op
	_, done := newWorkRunFixture(t, 1, RunStatusCompleted)
	status, changed, err = RequestRunCancel(done.Id)
	if err != nil || changed || status != RunStatusCompleted {
		t.Fatalf("terminal cancel: status=%s changed=%v err=%v", status, changed, err)
	}
}

func TestRequeueWaitingRunClearsLease(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusWaitingInput)
	if err := DB.Model(&AgentRun{}).Where("id = ?", run.Id).
		Updates(map[string]any{"lease_owner": "instance-a", "lease_expire_time": helperStampNow() + 60}).Error; err != nil {
		t.Fatal(err)
	}
	requeued, err := RequeueWaitingRun(run.Id)
	if err != nil || !requeued {
		t.Fatalf("requeue: %v %v", requeued, err)
	}
	var fresh AgentRun
	DB.Where("id = ?", run.Id).First(&fresh)
	if fresh.Status != RunStatusQueued || fresh.LeaseOwner != "" || fresh.LeaseExpireTime != 0 {
		t.Fatalf("requeue must reset the lease: %+v", fresh)
	}
}

func TestCreateRunInputIdempotency(t *testing.T) {
	useRunQueueTestDB(t)
	_, run := newWorkRunFixture(t, 1, RunStatusRunning)

	first, created, err := CreateRunInput(&RunInput{RunId: run.Id, UserId: 1,
		IdempotencyKey: "input-1", Content: "补充一点"})
	if err != nil || !created {
		t.Fatalf("first insert: created=%v err=%v", created, err)
	}
	replay, created, err := CreateRunInput(&RunInput{RunId: run.Id, UserId: 1,
		IdempotencyKey: "input-1", Content: "补充一点"})
	if err != nil || created || replay.Id != first.Id {
		t.Fatalf("same-key replay must hit the stored row: created=%v ids=%d/%d err=%v",
			created, replay.Id, first.Id, err)
	}
	if _, _, err := CreateRunInput(&RunInput{RunId: run.Id, UserId: 1,
		IdempotencyKey: "input-1", Content: "不同的内容"}); !errors.Is(err, ErrRunInputConflict) {
		t.Fatalf("same key with different content must conflict, got %v", err)
	}
	other, created, err := CreateRunInput(&RunInput{RunId: run.Id, UserId: 1,
		IdempotencyKey: "input-2", Content: "第二条"})
	if err != nil || !created {
		t.Fatalf("different key must insert: created=%v err=%v", created, err)
	}

	pending, err := ListPendingRunInputs(run.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].Id != first.Id || pending[1].Id != other.Id {
		t.Fatalf("pending inputs must follow submission order: %+v", pending)
	}
	if err := ConsumeRunInputs(run.Id, []int{first.Id}); err != nil {
		t.Fatal(err)
	}
	pending, _ = ListPendingRunInputs(run.Id)
	if len(pending) != 1 || pending[0].Id != other.Id {
		t.Fatalf("consumption must remove exactly one input: %+v", pending)
	}
}

func TestReconcileInterruptedRuns(t *testing.T) {
	useRunQueueTestDB(t)
	past := helperStampNow() - 10
	future := helperStampNow() + 600

	mk := func(userId int, kind, status string, lease int64) *AgentRun {
		run := &AgentRun{UserId: userId, Kind: kind, Status: status,
			LeaseExpireTime: lease, LeaseOwner: "dead-instance",
			IdempotencyKey: "recon-" + kind + status + "-" + time.Now().Format("150405.000000000")}
		if err := CreateAgentRun(run); err != nil {
			t.Fatalf("create run: %v", err)
		}
		return run
	}
	deadRunning := mk(1, "work", RunStatusRunning, past)
	deadCancelling := mk(1, "work", RunStatusCancelRequested, past)
	aliveRunning := mk(1, "work", RunStatusRunning, future)
	idleQueued := mk(1, "work", RunStatusQueued, 0)
	chatRunning := mk(1, "chat", RunStatusRunning, 0)

	if err := ReconcileInterruptedRuns(); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	expectStatus := func(run *AgentRun, want string) {
		t.Helper()
		var fresh AgentRun
		if err := DB.Where("id = ?", run.Id).First(&fresh).Error; err != nil {
			t.Fatal(err)
		}
		if fresh.Status != want {
			t.Fatalf("run %d: want %s got %s", run.Id, want, fresh.Status)
		}
	}
	expectStatus(deadRunning, RunStatusNeedsReconciliation)
	expectStatus(deadCancelling, RunStatusCancelled)
	expectStatus(aliveRunning, RunStatusRunning)
	expectStatus(idleQueued, RunStatusQueued)
	expectStatus(chatRunning, RunStatusRunning)

	var events []RunEvent
	if err := DB.Where("run_id = ?", deadRunning.Id).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != RunEventRunNeedsReconcile {
		t.Fatalf("reconciliation must record a control event: %+v", events)
	}
}
