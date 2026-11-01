package model

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common/helper"
)

// Work queue operations over the AgentRun table: claims with cross-instance
// lease and fencing, guarded status/context writes, cancellation state
// transitions, and the startup reconciliation pass. Chat runs never enter
// this path — they execute inside their HTTP request and keep the plain
// UpdateAgentRunStatus writes.

// GetAgentRunById loads a run without the owner check; internal use only.
func GetAgentRunById(id int) (*AgentRun, error) {
	if id <= 0 {
		return nil, errors.New("invalid agent run id")
	}
	var run AgentRun
	if err := DB.Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// ClaimNextWorkRun claims one queued work run for the given scheduler
// instance, oldest first. userActiveLimit caps how many work runs of one
// user may run concurrently across all instances (0 = unlimited); the cap is
// enforced by a count before the compare-and-swap, so concurrent instances
// can overshoot it by the width of that race. Returns nil when nothing is
// claimable. Every claim bumps FencingToken, invalidating the previous
// owner's guarded writes.
func ClaimNextWorkRun(instanceId string, leaseSeconds int64, userActiveLimit int) (*AgentRun, error) {
	var candidates []*AgentRun
	if err := DB.Where("kind = ? AND status = ?", "work", RunStatusQueued).
		Order("id ASC").Limit(32).Find(&candidates).Error; err != nil {
		return nil, err
	}
	now := helper.GetTimestamp()
	for _, candidate := range candidates {
		if userActiveLimit > 0 {
			var active int64
			if err := DB.Model(&AgentRun{}).
				Where("user_id = ? AND kind = ? AND status = ?", candidate.UserId, "work", RunStatusRunning).
				Count(&active).Error; err != nil {
				return nil, err
			}
			if active >= int64(userActiveLimit) {
				continue
			}
		}
		result := DB.Model(&AgentRun{}).
			Where("id = ? AND status = ?", candidate.Id, RunStatusQueued).
			Updates(map[string]any{
				"status":            RunStatusRunning,
				"lease_owner":       instanceId,
				"lease_expire_time": now + leaseSeconds,
				"fencing_token":     candidate.FencingToken + 1,
				"updated_time":      now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			return GetAgentRunById(candidate.Id)
		}
		// Another instance won the compare-and-swap; try the next candidate.
	}
	return nil, nil
}

// RenewWorkLease extends the claiming instance's lease. It fails once the
// run left the running state — a cancel request therefore surfaces as a
// failed renewal on the next heartbeat.
func RenewWorkLease(runId int, owner string, fencing int64, leaseSeconds int64) (bool, error) {
	result := DB.Model(&AgentRun{}).
		Where("id = ? AND lease_owner = ? AND fencing_token = ? AND status = ?",
			runId, owner, fencing, RunStatusRunning).
		Updates(map[string]any{"lease_expire_time": helper.GetTimestamp() + leaseSeconds})
	return result.RowsAffected == 1, result.Error
}

// UpdateAgentRunStatusFenced transitions a claimed run's status under its
// fencing token. It returns false when the caller no longer owns the run
// (lost lease or already moved on); the caller must stop advancing.
func UpdateAgentRunStatusFenced(runId int, fencing int64, fromStatus, toStatus, errorText string) (bool, error) {
	updates := map[string]any{"status": toStatus, "updated_time": helper.GetTimestamp()}
	if errorText != "" {
		updates["error"] = errorText
	}
	result := DB.Model(&AgentRun{}).
		Where("id = ? AND fencing_token = ? AND status = ?", runId, fencing, fromStatus).
		Updates(updates)
	return result.RowsAffected == 1, result.Error
}

// UpdateAgentRunContextFenced replaces a claimed run's accumulated Item JSON
// under its fencing token; same ownership contract as the fenced status
// write. The read side of this read-modify-write stays outside — the guard
// makes any interleaved stale write fail instead of landing.
func UpdateAgentRunContextFenced(runId int, fencing int64, contextItems string) (bool, error) {
	result := DB.Model(&AgentRun{}).
		Where("id = ? AND fencing_token = ?", runId, fencing).
		Updates(map[string]any{"context_items": contextItems, "updated_time": helper.GetTimestamp()})
	return result.RowsAffected == 1, result.Error
}

// AppendRunControlEvent persists one run-level control event (cancel,
// reconciliation, queue transitions) outside the engine; the SSE observers
// pick it up through replay or polling.
func AppendRunControlEvent(runId int, eventType string, payload map[string]any) error {
	sequence, err := NextRunEventSequence(runId)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{}`)
	}
	encoded := string(raw)
	return CreateRunEvent(&RunEvent{
		RunId: runId, Sequence: sequence,
		EventId: fmt.Sprintf("evt_%d_%d", runId, sequence),
		Type:    eventType, SchemaVersion: "1",
		Payload: &encoded,
	})
}

// RequestRunCancel applies the idempotent cancel semantics to an active run:
// queued and waiting_input runs stop immediately, a running run flips to
// cancel_requested and its lease is expired so the owner's next heartbeat
// (at most one interval later) observes the stop, terminal states are
// no-ops. Returns the resulting status and whether the call changed it.
func RequestRunCancel(runId int) (string, bool, error) {
	now := helper.GetTimestamp()
	transitions := []struct{ from, to string }{
		{RunStatusQueued, RunStatusCancelled},
		{RunStatusWaitingInput, RunStatusCancelled},
		{RunStatusNeedsReconciliation, RunStatusCancelled},
	}
	for _, t := range transitions {
		result := DB.Model(&AgentRun{}).
			Where("id = ? AND status = ?", runId, t.from).
			Updates(map[string]any{"status": t.to, "updated_time": now})
		if result.Error != nil {
			return "", false, result.Error
		}
		if result.RowsAffected == 1 {
			return t.to, true, nil
		}
	}
	// A running run can only be asked to stop; the worker performs the
	// actual transition once it observes the request.
	result := DB.Model(&AgentRun{}).
		Where("id = ? AND status = ?", runId, RunStatusRunning).
		Updates(map[string]any{"status": RunStatusCancelRequested, "lease_expire_time": now, "updated_time": now})
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 1 {
		return RunStatusCancelRequested, true, nil
	}
	// Terminal, already cancel_requested, or moved on: idempotent no-op.
	var run AgentRun
	if err := DB.Select("status").Where("id = ?", runId).First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, errors.New("run not found")
		}
		return "", false, err
	}
	return run.Status, false, nil
}

// RequeueWaitingRun moves a waiting_input run back to the queue after a user
// input arrived; the next claim consumes it at the run's safe boundary.
func RequeueWaitingRun(runId int) (bool, error) {
	result := DB.Model(&AgentRun{}).
		Where("id = ? AND status = ?", runId, RunStatusWaitingInput).
		Updates(map[string]any{
			"status": RunStatusQueued, "lease_owner": "", "lease_expire_time": 0,
			"updated_time": helper.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// ReconcileInterruptedRuns is the startup pass: work runs left running by a
// dead process move to needs_reconciliation (their worker's model calls and
// tool effects cannot be verified from here), runs already asked to cancel
// whose worker died resolve to cancelled. Runs with a live lease belong to
// another instance and are left alone; queued and waiting_input runs are
// workerless states and stay. Each transition is recorded as a control
// event so reconnecting observers see it on replay.
func ReconcileInterruptedRuns() error {
	now := helper.GetTimestamp()
	var orphans []*AgentRun
	if err := DB.Where("kind = ? AND status IN ? AND lease_expire_time < ?",
		"work", []string{RunStatusRunning, RunStatusCancelRequested}, now).
		Find(&orphans).Error; err != nil {
		return err
	}
	for _, run := range orphans {
		var toStatus, eventType string
		if run.Status == RunStatusRunning {
			toStatus, eventType = RunStatusNeedsReconciliation, RunEventRunNeedsReconcile
		} else {
			toStatus, eventType = RunStatusCancelled, RunEventRunCancelled
		}
		result := DB.Model(&AgentRun{}).
			Where("id = ? AND status = ?", run.Id, run.Status).
			Updates(map[string]any{"status": toStatus, "updated_time": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			continue
		}
		payload := map[string]any{"reason": "restart_reconciliation"}
		if toStatus == RunStatusNeedsReconciliation {
			payload["error"] = "进程重启，上一次执行结果待核对"
		}
		if err := AppendRunControlEvent(run.Id, eventType, payload); err != nil {
			return err
		}
	}
	return nil
}
