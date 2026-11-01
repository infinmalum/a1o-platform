package model

import (
	"errors"

	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common/helper"
)

// Run lifecycle statuses. R1 exercises the queued/running/terminal subset;
// waiting_input, waiting_approval, cancel_requested, and needs_reconciliation
// are reserved for the Engine (R2/R3) per the runtime reconstruction plan.
const (
	RunStatusQueued              = "queued"
	RunStatusRunning             = "running"
	RunStatusWaitingInput        = "waiting_input"
	RunStatusWaitingApproval     = "waiting_approval"
	RunStatusCancelRequested     = "cancel_requested"
	RunStatusCompleted           = "completed"
	RunStatusFailed              = "failed"
	RunStatusCancelled           = "cancelled"
	RunStatusNeedsReconciliation = "needs_reconciliation"
)

// Invocation lifecycle statuses. pending means the request intent was
// persisted before the upstream call; unknown marks the crash-recovery state
// where the upstream may or may not have accepted the call.
const (
	InvocationStatusPending   = "pending"
	InvocationStatusSucceeded = "succeeded"
	InvocationStatusFailed    = "failed"
	InvocationStatusCancelled = "cancelled"
	InvocationStatusUnknown   = "unknown"
)

// Run event type identifiers (agent-runtime-contract.md §2). Model events use
// the OpenResponses event names verbatim; the run-level types below are the
// a1o control plane.
const (
	RunEventRunQueued    = "run.queued"
	RunEventRunStarted   = "run.started"
	RunEventRunCompleted = "run.completed"
	RunEventRunFailed    = "run.failed"
	RunEventRunCancelled = "run.cancelled"
	// R3 work-runtime control events. waiting_input marks a paused work run
	// that ended its turn without declaring completion; cancel_requested and
	// needs_reconciliation record asynchronous stop and restart-recovery
	// transitions.
	RunEventRunWaitingInput     = "run.waiting_input"
	RunEventRunCancelRequest    = "run.cancel_requested"
	RunEventRunNeedsReconcile   = "run.needs_reconciliation"
	RunEventInvocationStarted   = "invocation.started"
	RunEventInvocationUsage     = "invocation.usage"
	RunEventInvocationCompleted = "invocation.completed"
	RunEventInvocationFailed    = "invocation.failed"
	RunEventInputAppended       = "input.appended"
	// Chat-mode structural events. Text/reasoning deltas stream
	// transiently and are not persisted as RunEvents.
	RunEventMessageStarted   = "message.started"
	RunEventToolCalled       = "tool.called"
	RunEventToolResult       = "tool.result"
	RunEventContextCompacted = "context.compacted"
)

// AgentRun is one persistent execution record. It is the single run fact
// source for Chat and Work; consume logs stay the settlement source of truth
// and this row only carries associations.
type AgentRun struct {
	Id int `json:"id"`
	// UserId plus IdempotencyKey form the owner-scoped idempotency contract:
	// the same key under the same owner resolves to this run instead of
	// creating a second one.
	UserId int    `json:"user_id" gorm:"index;uniqueIndex:idx_agent_run_idempotency,priority:1"`
	Kind   string `json:"kind" gorm:"type:varchar(16)"`
	Status string `json:"status" gorm:"type:varchar(32);index"`
	// SessionId links a chat-mode run to its conversation; 0 for runs
	// without a session.
	SessionId int `json:"session_id" gorm:"index"`
	// TaskId links a work-mode run to its WorkTask; 0 for chat runs.
	TaskId         int    `json:"task_id" gorm:"index"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(255);uniqueIndex:idx_agent_run_idempotency,priority:2"`
	// Lease and fencing for work-mode queue claims: LeaseOwner identifies
	// the claiming scheduler instance, LeaseExpireTime the heartbeat
	// deadline, and FencingToken increments on every claim so a stale
	// worker's guarded writes no-op instead of clobbering the new owner's
	// state. Chat runs execute inside the HTTP request and never claim.
	LeaseOwner      string `json:"lease_owner" gorm:"type:varchar(64)"`
	LeaseExpireTime int64  `json:"lease_expire_time" gorm:"bigint"`
	FencingToken    int64  `json:"fencing_token"`
	Model           string `json:"model" gorm:"type:varchar(255)"`
	Instructions    string `json:"instructions" gorm:"type:text"`
	// InputItems is the frozen JSON snapshot of the run's input Items.
	InputItems *string `json:"input_items" gorm:"type:text"`
	// ContextItems is the accumulated standard Item list the next model call
	// replays; the agent package owns its JSON schema.
	ContextItems *string `json:"context_items" gorm:"type:text"`
	Error        string  `json:"error" gorm:"type:text"`
	CreatedTime  int64   `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64   `json:"updated_time" gorm:"bigint"`
}

// ModelInvocation is one billed model call within a run. Request intent is
// persisted before the upstream call so a crash between the quota reservation
// and settlement is identifiable afterwards; it is not treated as recoverable.
type ModelInvocation struct {
	Id       int `json:"id"`
	RunId    int `json:"run_id" gorm:"index"`
	Sequence int `json:"sequence"`
	// ChannelId/ChannelType/UpstreamAccountId record where the call went;
	// opaque native state produced by the call is bound to this attribution.
	ChannelId         int    `json:"channel_id"`
	ChannelType       int    `json:"channel_type"`
	UpstreamAccountId int    `json:"upstream_account_id"`
	UpstreamProtocol  string `json:"upstream_protocol" gorm:"type:varchar(32)"`
	ModelName         string `json:"model_name" gorm:"type:varchar(255)"`
	MappedModel       string `json:"mapped_model" gorm:"type:varchar(255)"`
	Status            string `json:"status" gorm:"type:varchar(16);index"`
	// FinishStatus carries the protocol-level terminal status
	// (completed/incomplete/failed) when one arrived.
	FinishStatus string `json:"finish_status" gorm:"type:varchar(16)"`
	// RequestItems is the JSON Item list sent upstream; ResponseItems the
	// standard Items the call produced (salvaged partials included).
	RequestItems  *string `json:"request_items" gorm:"type:text"`
	ResponseItems *string `json:"response_items" gorm:"type:text"`
	// Usage snapshots the native.Usage that drove settlement.
	Usage         *string `json:"usage" gorm:"type:text"`
	Error         string  `json:"error" gorm:"type:text"`
	ConsumeLogId  int     `json:"consume_log_id"`
	CreatedTime   int64   `json:"created_time" gorm:"bigint"`
	CompletedTime int64   `json:"completed_time" gorm:"bigint"`
}

// RunEvent is the append-only versioned event envelope per run. Sequence is
// monotonic per run and drives reconnect replay; it is not the model's
// sequence_number.
type RunEvent struct {
	Id            int     `json:"id"`
	RunId         int     `json:"run_id" gorm:"index;uniqueIndex:idx_agent_run_event_sequence,priority:1"`
	Sequence      int64   `json:"sequence" gorm:"uniqueIndex:idx_agent_run_event_sequence,priority:2"`
	EventId       string  `json:"event_id" gorm:"type:varchar(64)"`
	Type          string  `json:"type" gorm:"type:varchar(64)"`
	SchemaVersion string  `json:"schema_version" gorm:"type:varchar(16)"`
	InvocationId  int     `json:"invocation_id"`
	Payload       *string `json:"payload" gorm:"type:text"`
	CreatedTime   int64   `json:"created_time" gorm:"bigint"`
}

// ErrAgentRunIdempotencyConflict reports that the owner already used the
// idempotency key on a different run.
var ErrAgentRunIdempotencyConflict = errors.New("agent run idempotency key already used")

// CreateAgentRun inserts the run; a repeated (owner, key) pair resolves to a
// conflict so the caller can fetch the original run.
func CreateAgentRun(run *AgentRun) error {
	now := helper.GetTimestamp()
	run.CreatedTime, run.UpdatedTime = now, now
	if run.IdempotencyKey != "" {
		var existing AgentRun
		err := DB.Where("user_id = ? AND idempotency_key = ?", run.UserId, run.IdempotencyKey).First(&existing).Error
		if err == nil {
			return ErrAgentRunIdempotencyConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return DB.Create(run).Error
}

// GetAgentRun loads the run for owner checks.
func GetAgentRun(id, userId int) (*AgentRun, error) {
	if id <= 0 {
		return nil, errors.New("invalid agent run id")
	}
	var run AgentRun
	if err := DB.Where("id = ? AND user_id = ?", id, userId).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// GetAgentRunByIdempotencyKey resolves the owner's original run for a
// replayed creation request.
func GetAgentRunByIdempotencyKey(userId int, key string) (*AgentRun, error) {
	var run AgentRun
	if err := DB.Where("user_id = ? AND idempotency_key = ?", userId, key).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// CountQueuedWorkRuns reports how many of the user's work runs wait in the
// queue; the creation endpoint uses it as a flood guard.
func CountQueuedWorkRuns(userId int) (int64, error) {
	var count int64
	err := DB.Model(&AgentRun{}).
		Where("user_id = ? AND kind = ? AND status = ?", userId, "work", RunStatusQueued).
		Count(&count).Error
	return count, err
}

// UpdateAgentRunStatus transitions the run status and records the terminal
// error text.
func UpdateAgentRunStatus(runId int, status, errorText string) error {
	updates := map[string]any{"status": status, "updated_time": helper.GetTimestamp()}
	if errorText != "" {
		updates["error"] = errorText
	}
	return DB.Model(&AgentRun{}).Where("id = ?", runId).Updates(updates).Error
}

// UpdateAgentRunContext replaces the run's accumulated Item JSON and bumps
// the updated timestamp.
func UpdateAgentRunContext(runId int, contextItems string) error {
	return DB.Model(&AgentRun{}).Where("id = ?", runId).Updates(map[string]any{
		"context_items": contextItems, "updated_time": helper.GetTimestamp(),
	}).Error
}

// NextRunEventSequence and NextRunInvocationSequence allocate per-run
// counters. The single-writer rule per run keeps the read-increment race
// free; the (run_id, sequence) unique index backstops a violation.
func NextRunEventSequence(runId int) (int64, error) {
	var current int64
	err := DB.Model(&RunEvent{}).Where("run_id = ?", runId).
		Select("COALESCE(MAX(sequence), 0)").Scan(&current).Error
	return current + 1, err
}

func NextRunInvocationSequence(runId int) (int, error) {
	var current int
	err := DB.Model(&ModelInvocation{}).Where("run_id = ?", runId).
		Select("COALESCE(MAX(sequence), 0)").Scan(&current).Error
	return current + 1, err
}

// CreateRunEvent persists one envelope row; the caller assigns Sequence.
func CreateRunEvent(event *RunEvent) error {
	event.CreatedTime = helper.GetTimestamp()
	if event.SchemaVersion == "" {
		event.SchemaVersion = "1"
	}
	return DB.Create(event).Error
}

// GetRunEventsAfter replays events with a sequence greater than after.
func GetRunEventsAfter(runId int, after int64) ([]RunEvent, error) {
	var events []RunEvent
	err := DB.Where("run_id = ? AND sequence > ?", runId, after).
		Order("sequence ASC").Find(&events).Error
	return events, err
}

// CreateModelInvocation persists the pending request intent.
func CreateModelInvocation(invocation *ModelInvocation) error {
	invocation.CreatedTime = helper.GetTimestamp()
	invocation.Status = InvocationStatusPending
	return DB.Create(invocation).Error
}

// CompleteModelInvocation records the terminal state of one model call.
func CompleteModelInvocation(invocation *ModelInvocation) error {
	invocation.CompletedTime = helper.GetTimestamp()
	return DB.Model(&ModelInvocation{}).Where("id = ?", invocation.Id).Updates(map[string]any{
		"status":         invocation.Status,
		"finish_status":  invocation.FinishStatus,
		"response_items": invocation.ResponseItems,
		"usage":          invocation.Usage,
		"error":          invocation.Error,
		"consume_log_id": invocation.ConsumeLogId,
		"completed_time": invocation.CompletedTime,
	}).Error
}

// GetRunInvocations lists a run's model calls in execution order.
func GetRunInvocations(runId int) ([]ModelInvocation, error) {
	var invocations []ModelInvocation
	err := DB.Where("run_id = ?", runId).Order("sequence ASC").Find(&invocations).Error
	return invocations, err
}
