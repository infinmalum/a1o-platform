package model

import (
	"errors"

	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common/helper"
)

// Work runtime tables: WorkTask is the user's goal (one task may be executed
// by several runs over its lifetime), RunInput queues user input appended to
// an active run. The run itself lives in AgentRun (single run fact source for
// Chat and Work), linked through AgentRun.TaskId.

const (
	WorkTaskStatusActive   = "active"
	WorkTaskStatusArchived = "archived"
)

// WorkTask is a user goal. Summary carries the latest completed run's
// result summary so task lists render a deliverable without touching the
// event log; it is a projection, not a settlement fact.
type WorkTask struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"index"`
	Title       string `json:"title" gorm:"type:varchar(255)"`
	Status      string `json:"status" gorm:"type:varchar(16)"`
	Summary     string `json:"summary" gorm:"type:text"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

// RunInput is one user input queued for consumption at the run's next safe
// boundary. (RunId, IdempotencyKey) is the owner-scoped idempotency contract:
// the same key with the same content resolves to the stored row, the same
// key with different content is a conflict.
type RunInput struct {
	Id             int    `json:"id"`
	RunId          int    `json:"run_id" gorm:"index;uniqueIndex:idx_run_input_idem,priority:1"`
	UserId         int    `json:"user_id"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(255);uniqueIndex:idx_run_input_idem,priority:2"`
	Content        string `json:"content" gorm:"type:text"`
	Consumed       bool   `json:"consumed" gorm:"index"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
}

// ErrRunInputConflict reports a reused idempotency key with different
// content; the same key with identical content is an idempotent hit.
var ErrRunInputConflict = errors.New("run input idempotency key reused with different content")

func CreateWorkTask(task *WorkTask) error {
	now := helper.GetTimestamp()
	task.CreatedTime, task.UpdatedTime = now, now
	task.Status = WorkTaskStatusActive
	return DB.Create(task).Error
}

// GetWorkTask loads one owned task.
func GetWorkTask(id, userId int) (*WorkTask, error) {
	if id <= 0 {
		return nil, errors.New("invalid work task id")
	}
	var task WorkTask
	if err := DB.Where("id = ? AND user_id = ?", id, userId).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ListWorkTasks lists the caller's tasks, newest first.
func ListWorkTasks(userId int) ([]*WorkTask, error) {
	var tasks []*WorkTask
	err := DB.Where("user_id = ?", userId).Order("id DESC").Find(&tasks).Error
	return tasks, err
}

// UpdateWorkTaskSummary rewrites the projected task summary.
func UpdateWorkTaskSummary(taskId int, summary string) error {
	return DB.Model(&WorkTask{}).Where("id = ?", taskId).Updates(map[string]any{
		"summary": summary, "updated_time": helper.GetTimestamp(),
	}).Error
}

// CreateRunInput applies the idempotency contract and returns the stored row
// plus whether this call created it.
func CreateRunInput(input *RunInput) (*RunInput, bool, error) {
	input.CreatedTime = helper.GetTimestamp()
	var existing RunInput
	err := DB.Where("run_id = ? AND idempotency_key = ?", input.RunId, input.IdempotencyKey).First(&existing).Error
	if err == nil {
		if existing.Content != input.Content {
			return nil, false, ErrRunInputConflict
		}
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err := DB.Create(input).Error; err != nil {
		// A concurrent request with the same key won the insert race; apply
		// the same semantics to whatever row it stored.
		var raced RunInput
		if ferr := DB.Where("run_id = ? AND idempotency_key = ?", input.RunId, input.IdempotencyKey).
			First(&raced).Error; ferr == nil {
			if raced.Content != input.Content {
				return nil, false, ErrRunInputConflict
			}
			return &raced, false, nil
		}
		return nil, false, err
	}
	return input, true, nil
}

// ListPendingRunInputs returns the run's unconsumed inputs in submission
// order.
func ListPendingRunInputs(runId int) ([]RunInput, error) {
	var inputs []RunInput
	err := DB.Where("run_id = ? AND consumed = ?", runId, false).Order("id ASC").Find(&inputs).Error
	return inputs, err
}

// CountPendingRunInputs reports how many inputs wait for consumption.
func CountPendingRunInputs(runId int) (int64, error) {
	var count int64
	err := DB.Model(&RunInput{}).Where("run_id = ? AND consumed = ?", runId, false).Count(&count).Error
	return count, err
}

// ConsumeRunInputs marks the given inputs consumed after the run appended
// them to its context.
func ConsumeRunInputs(runId int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&RunInput{}).
		Where("run_id = ? AND id IN ?", runId, ids).
		Updates(map[string]any{"consumed": true}).Error
}
