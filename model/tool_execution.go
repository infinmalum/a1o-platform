package model

import (
	"github.com/infinmalum/a1o-platform/common/helper"
)

// ToolExecution is the persisted intent of one tool call (runtime plan §4):
// the row lands before the executor runs the tool, so a crash between intent
// and result leaves a pending row that names what may already have acted on
// the outside world. Results are announcements (tool.called/tool.result
// events); this row is the before-the-fact record.

const (
	ToolExecutionStatusRunning   = "running"
	ToolExecutionStatusCompleted = "completed"
	ToolExecutionStatusFailed    = "failed"
	ToolExecutionStatusUnknown   = "unknown"
)

type ToolExecution struct {
	Id int `json:"id"`
	// RunId associates the execution with its run fact source; InvocationId
	// links it to the model leg that requested the call (0 when the leg had
	// no persisted invocation).
	RunId        int    `json:"run_id" gorm:"index"`
	InvocationId int    `json:"invocation_id"`
	CallId       string `json:"call_id" gorm:"type:varchar(64)"`
	Name         string `json:"name" gorm:"type:varchar(255)"`
	// Arguments is the normalized argument JSON handed to the executor.
	Arguments   string `json:"arguments" gorm:"type:text"`
	Status      string `json:"status" gorm:"type:varchar(16);index"`
	IsError     bool   `json:"is_error"`
	DurationMs  int64  `json:"duration_ms"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

// CreateToolExecution persists the execution intent before the tool runs.
func CreateToolExecution(execution *ToolExecution) error {
	execution.CreatedTime = helper.GetTimestamp()
	execution.Status = ToolExecutionStatusRunning
	return DB.Create(execution).Error
}

// CompleteToolExecution records the observed outcome of one tool call.
func CompleteToolExecution(id int, status string, isError bool, durationMs int64) error {
	return DB.Model(&ToolExecution{}).Where("id = ?", id).Updates(map[string]any{
		"status":      status,
		"is_error":    isError,
		"duration_ms": durationMs,
	}).Error
}

// ListPendingToolExecutions returns the run's executions that started but
// never recorded an outcome — the "may have executed" set used by the
// reconciliation resume flow.
func ListPendingToolExecutions(runId int) ([]ToolExecution, error) {
	var executions []ToolExecution
	err := DB.Where("run_id = ? AND status = ?", runId, ToolExecutionStatusRunning).
		Order("id ASC").Find(&executions).Error
	return executions, err
}

// ListRunToolExecutions lists all of a run's tool executions in call order.
func ListRunToolExecutions(runId int) ([]ToolExecution, error) {
	var executions []ToolExecution
	err := DB.Where("run_id = ?", runId).Order("id ASC").Find(&executions).Error
	return executions, err
}
