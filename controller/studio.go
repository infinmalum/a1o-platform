package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/agent"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

// Studio run API (R3): persistent Work runs over the /api/studio routes from
// the runtime reconstruction plan. Every route is owner-scoped; creation and
// appended inputs carry idempotency keys; cancel is an idempotent state
// request. Chat runs keep their SSE turn endpoint — POST /api/studio/runs
// only accepts kind "work" for now.

// maxQueuedWorkRunsPerUser bounds one user's queue depth so a client cannot
// flood the worker pool with pending goals.
const maxQueuedWorkRunsPerUser = 10

// studioEventPollInterval is how often the events stream re-reads persisted
// events; this is also the delivery latency ceiling for cross-instance
// observers (same-instance observers additionally receive the live bus).
const studioEventPollInterval = 500 * time.Millisecond

// studioEventPingInterval keeps intermediate proxies from reaping an idle
// observation stream.
const studioEventPingInterval = 15 * time.Second

type studioRunPayload struct {
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	TaskId         int    `json:"task_id"`
	Input          string `json:"input"`
	Model          string `json:"model"`
	IdempotencyKey string `json:"idempotency_key"`
}

// CreateStudioRun accepts a new work run: creates (or reuses) the task,
// freezes the run record, and queues it for the schedulers.
func CreateStudioRun(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	var payload studioRunPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if payload.Kind != "work" {
		chatError(c, errors.New("目前仅支持 kind=work 的运行；聊天请使用会话接口"))
		return
	}
	if payload.IdempotencyKey == "" {
		chatError(c, errors.New("缺少幂等键 idempotency_key"))
		return
	}
	input := trimStudioInput(payload.Input)
	if input == "" {
		chatError(c, errors.New("任务输入不能为空"))
		return
	}
	if len(payload.IdempotencyKey) > 255 {
		payload.IdempotencyKey = payload.IdempotencyKey[:255]
	}
	if payload.Model == "" {
		chatError(c, errors.New("模型不能为空"))
		return
	}

	// Idempotent replay: the same key with the same request returns the
	// original run; a different request under a used key is a conflict.
	existing, err := model.GetAgentRunByIdempotencyKey(userId, payload.IdempotencyKey)
	if err == nil {
		// TaskId only participates when the replay references a task
		// explicitly; an omitted task_id auto-created one on the original
		// request, so the stored value is always more specific.
		taskMatches := payload.TaskId == 0 || existing.TaskId == payload.TaskId
		if existing.Kind != payload.Kind || existing.Model != payload.Model ||
			!taskMatches || !runInputMatches(existing, input) {
			chatError(c, errors.New("幂等键已用于不同的运行请求"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"run": existing, "created": false}})
		return
	}

	queued, err := model.CountQueuedWorkRuns(userId)
	if err != nil {
		chatError(c, err)
		return
	}
	if queued >= maxQueuedWorkRunsPerUser {
		chatError(c, fmt.Errorf("排队任务已达上限（%d），请等待或取消部分任务", maxQueuedWorkRunsPerUser))
		return
	}

	task, err := resolveStudioTask(userId, payload, input)
	if err != nil {
		chatError(c, err)
		return
	}

	inputItems := studioInputItems(input)
	encoded, err := json.Marshal(inputItems)
	if err != nil {
		chatError(c, err)
		return
	}
	frozen := string(encoded)
	run := &model.AgentRun{
		UserId: userId, Kind: "work", Status: model.RunStatusQueued,
		TaskId: task.Id, IdempotencyKey: payload.IdempotencyKey,
		Model: payload.Model, InputItems: &frozen, ContextItems: &frozen,
	}
	if err := model.CreateAgentRun(run); err != nil {
		if errors.Is(err, model.ErrAgentRunIdempotencyConflict) {
			chatError(c, errors.New("幂等键已用于不同的运行请求"))
			return
		}
		chatError(c, fmt.Errorf("记录运行失败: %w", err))
		return
	}
	if err := model.AppendRunControlEvent(run.Id, model.RunEventRunQueued,
		map[string]any{"task_id": task.Id, "model": payload.Model}); err != nil {
		// Queue state is the truth; a missing timeline entry never blocks.
	}
	agent.DefaultScheduler().Wake()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"run": run, "created": true, "task": task}})
}

// resolveStudioTask finds the referenced task or creates one titled from the
// payload (falling back to the input text).
func resolveStudioTask(userId int, payload studioRunPayload, input string) (*model.WorkTask, error) {
	if payload.TaskId > 0 {
		task, err := model.GetWorkTask(payload.TaskId, userId)
		if err != nil {
			return nil, err
		}
		return task, nil
	}
	title := trimStudioInput(payload.Title)
	if title == "" {
		title = truncateChatTitle(input)
	}
	if len(title) > 255 {
		title = title[:255]
	}
	task := &model.WorkTask{UserId: userId, Title: title}
	if err := model.CreateWorkTask(task); err != nil {
		return nil, fmt.Errorf("创建任务失败: %w", err)
	}
	return task, nil
}

// studioInputItems freezes the initial input as one user message Item.
func studioInputItems(input string) []agent.Item {
	return []agent.Item{{
		Type: agent.ItemMessage, Role: "user",
		Content: []agent.ContentPart{{Type: agent.PartInputText, Text: input}},
	}}
}

// runInputMatches compares a replayed request's input against the frozen
// snapshot of the original run.
func runInputMatches(run *model.AgentRun, input string) bool {
	if run.InputItems == nil {
		return false
	}
	var items []agent.Item
	if err := json.Unmarshal([]byte(*run.InputItems), &items); err != nil {
		return false
	}
	return len(items) == 1 && messageItemText(items[0]) == input
}

func messageItemText(item agent.Item) string {
	if item.Type != agent.ItemMessage {
		return ""
	}
	var text string
	for _, part := range item.Content {
		if part.Type == agent.PartInputText || part.Type == agent.PartOutputText {
			text += part.Text
		}
	}
	return text
}

func trimStudioInput(s string) string {
	return strings.TrimSpace(s)
}

// GetStudioRun returns one owned run with its task and a transcript
// projection of the accumulated context.
func GetStudioRun(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的运行 ID"))
		return
	}
	run, err := model.GetAgentRun(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	projection, err := buildRunProjection(run)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": projection})
}

// runProjection is the task UI's server-side view of one run: transcript
// (user/assistant messages), token usage, and pending input count. Tool
// activity lives in the event log, which the client replays separately.
type runProjection struct {
	Run              *model.AgentRun  `json:"run"`
	Task             *model.WorkTask  `json:"task"`
	Messages         []projectionItem `json:"messages"`
	PendingInputs    int64            `json:"pending_inputs"`
	PromptTokens     int64            `json:"prompt_tokens"`
	CompletionTokens int64            `json:"completion_tokens"`
	Invocations      int64            `json:"invocations"`
}

type projectionItem struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func buildRunProjection(run *model.AgentRun) (*runProjection, error) {
	projection := &runProjection{Run: run, Messages: []projectionItem{}}
	if run.TaskId > 0 {
		task, err := model.GetWorkTask(run.TaskId, run.UserId)
		if err == nil {
			projection.Task = task
		}
	}
	if run.ContextItems != nil && *run.ContextItems != "" {
		var items []agent.Item
		if err := json.Unmarshal([]byte(*run.ContextItems), &items); err != nil {
			return nil, fmt.Errorf("运行上下文损坏: %w", err)
		}
		for _, item := range items {
			if item.Type != agent.ItemMessage {
				continue
			}
			projection.Messages = append(projection.Messages, projectionItem{
				Role: item.Role, Text: messageItemText(item),
			})
		}
	}
	invocations, err := model.GetRunInvocations(run.Id)
	if err != nil {
		return nil, err
	}
	projection.Invocations = int64(len(invocations))
	for _, invocation := range invocations {
		if invocation.Usage == nil {
			continue
		}
		// native.Usage marshals with Go field names (no JSON tags); the
		// projection reads the stored shape verbatim.
		var usage struct {
			Input  int64
			Output int64
		}
		if err := json.Unmarshal([]byte(*invocation.Usage), &usage); err == nil {
			projection.PromptTokens += usage.Input
			projection.CompletionTokens += usage.Output
		}
	}
	if pending, err := model.CountPendingRunInputs(run.Id); err == nil {
		projection.PendingInputs = pending
	}
	return projection, nil
}

// StreamStudioRunEvents observes one run: it replays persisted events after
// the given sequence, then keeps streaming — live through the in-process bus
// when the worker runs here, and via polling for everyone else — until the
// run reaches a terminal state and the log is drained. Disconnecting only
// ends the observation; work runs are unaffected.
func StreamStudioRunEvents(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的运行 ID"))
		return
	}
	run, err := model.GetAgentRun(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	after := int64(0)
	if raw := c.Query("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			chatError(c, errors.New("无效的 after 游标"))
			return
		}
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		chatError(c, errors.New("当前连接不支持流式响应"))
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	write := func(event agent.EventEnvelope) bool {
		if _, err := c.Writer.Write(agent.EncodeEvent(event)); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	lastSequence := after
	replay, err := model.GetRunEventsAfter(run.Id, after)
	if err != nil {
		return
	}
	for _, event := range replay {
		if !write(agent.EventEnvelope{
			RunID: run.Id, EventID: event.EventId, Sequence: event.Sequence,
			Type: event.Type, SchemaVersion: event.SchemaVersion,
			InvocationID: event.InvocationId, Payload: json.RawMessage(orStudioString(event.Payload)),
		}) {
			return
		}
		lastSequence = event.Sequence
	}
	if studioRunTerminal(run.Status) {
		return
	}

	events, cancel := agent.SubscribeRun(run.Id)
	defer cancel()
	ping := time.NewTicker(studioEventPingInterval)
	defer ping.Stop()
	poll := time.NewTicker(studioEventPollInterval)
	defer poll.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ping.C:
			if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case event := <-events:
			// Persisted envelopes dedupe against the poll cursor; deltas
			// carry no sequence and always pass through.
			if event.Sequence > 0 {
				if event.Sequence <= lastSequence {
					continue
				}
				lastSequence = event.Sequence
			}
			if !write(event) {
				return
			}
		case <-poll.C:
			persisted, err := model.GetRunEventsAfter(run.Id, lastSequence)
			if err != nil {
				return
			}
			for _, event := range persisted {
				if !write(agent.EventEnvelope{
					RunID: run.Id, EventID: event.EventId, Sequence: event.Sequence,
					Type: event.Type, SchemaVersion: event.SchemaVersion,
					InvocationID: event.InvocationId, Payload: json.RawMessage(orStudioString(event.Payload)),
				}) {
					return
				}
				lastSequence = event.Sequence
			}
			fresh, err := model.GetAgentRunById(run.Id)
			if err != nil {
				return
			}
			if studioRunTerminal(fresh.Status) && len(persisted) == 0 {
				return
			}
		}
	}
}

func studioRunTerminal(status string) bool {
	switch status {
	case model.RunStatusCompleted, model.RunStatusFailed,
		model.RunStatusCancelled, model.RunStatusNeedsReconciliation:
		return true
	}
	return false
}

func orStudioString(value *string) string {
	if value == nil {
		return "{}"
	}
	return *value
}

// AppendStudioRunInput queues one user input onto an active run. The same
// idempotency key with identical content is a no-op hit; with different
// content it is a conflict. A waiting_input run requeues on arrival.
func AppendStudioRunInput(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的运行 ID"))
		return
	}
	var payload struct {
		Content        string `json:"content"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		chatError(c, err)
		return
	}
	if payload.IdempotencyKey == "" {
		chatError(c, errors.New("缺少幂等键 idempotency_key"))
		return
	}
	if len(payload.IdempotencyKey) > 255 {
		payload.IdempotencyKey = payload.IdempotencyKey[:255]
	}
	content := trimStudioInput(payload.Content)
	if content == "" {
		chatError(c, errors.New("输入内容不能为空"))
		return
	}
	run, err := model.GetAgentRun(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	switch run.Status {
	case model.RunStatusQueued, model.RunStatusRunning, model.RunStatusWaitingInput:
	default:
		chatError(c, fmt.Errorf("运行已%s，无法追加输入", studioRunStateText(run.Status)))
		return
	}
	input := &model.RunInput{RunId: run.Id, UserId: userId,
		IdempotencyKey: payload.IdempotencyKey, Content: content}
	stored, created, err := model.CreateRunInput(input)
	if err != nil {
		if errors.Is(err, model.ErrRunInputConflict) {
			chatError(c, errors.New("幂等键已用于不同的输入内容"))
			return
		}
		chatError(c, err)
		return
	}
	// The run may have parked in waiting_input between the status check and
	// the insert (the pause boundary raced this input); requeue whenever the
	// input is actually pending so it can never sit unconsumed forever.
	fresh, err := model.GetAgentRunById(run.Id)
	if err == nil && fresh.Status == model.RunStatusWaitingInput {
		if pending, perr := model.CountPendingRunInputs(run.Id); perr == nil && pending > 0 {
			requeued, rerr := model.RequeueWaitingRun(run.Id)
			if rerr != nil {
				chatError(c, rerr)
				return
			}
			if requeued {
				_ = model.AppendRunControlEvent(run.Id, model.RunEventRunQueued,
					map[string]any{"reason": "input_arrived"})
				agent.DefaultScheduler().Wake()
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"input": stored, "created": created}})
}

// CancelStudioRun requests cancellation: idempotent, and it does not promise
// the in-flight step stops instantly.
func CancelStudioRun(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的运行 ID"))
		return
	}
	run, err := model.GetAgentRun(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	status, changed, err := model.RequestRunCancel(run.Id)
	if err != nil {
		chatError(c, err)
		return
	}
	if changed {
		switch status {
		case model.RunStatusCancelRequested:
			_ = model.AppendRunControlEvent(run.Id, model.RunEventRunCancelRequest,
				map[string]any{})
			agent.DefaultScheduler().NotifyCancel(run.Id)
		case model.RunStatusCancelled:
			_ = model.AppendRunControlEvent(run.Id, model.RunEventRunCancelled,
				map[string]any{"reason": "cancelled_by_user"})
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"status": status, "changed": changed}})
}

// ListStudioTasks lists the caller's tasks with each task's latest run.
func ListStudioTasks(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	tasks, err := model.ListWorkTasks(userId)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": studioTaskViews(userId, tasks)})
}

// GetStudioTask returns one owned task with its runs, newest first.
func GetStudioTask(c *gin.Context) {
	userId := c.GetInt(ctxkey.Id)
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		chatError(c, errors.New("无效的任务 ID"))
		return
	}
	task, err := model.GetWorkTask(id, userId)
	if err != nil {
		chatError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"task": task, "runs": studioTaskRuns(task.Id),
	}})
}

// studioTaskView decorates a task with its latest run for list rendering.
type studioTaskView struct {
	*model.WorkTask
	LatestRun *model.AgentRun `json:"latest_run"`
}

func studioTaskViews(userId int, tasks []*model.WorkTask) []studioTaskView {
	views := make([]studioTaskView, 0, len(tasks))
	if len(tasks) == 0 {
		return views
	}
	ids := make([]int, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.Id)
	}
	var runs []*model.AgentRun
	if err := model.DB.Where("user_id = ? AND task_id IN ?", userId, ids).
		Order("id ASC").Find(&runs).Error; err != nil {
		runs = nil
	}
	latest := map[int]*model.AgentRun{}
	for _, run := range runs {
		latest[run.TaskId] = run
	}
	for _, task := range tasks {
		views = append(views, studioTaskView{WorkTask: task, LatestRun: latest[task.Id]})
	}
	return views
}

func studioTaskRuns(taskId int) []*model.AgentRun {
	var runs []*model.AgentRun
	if err := model.DB.Where("task_id = ?", taskId).Order("id DESC").Find(&runs).Error; err != nil {
		return []*model.AgentRun{}
	}
	return runs
}

// studioRunStateText renders a run state for user-facing messages.
func studioRunStateText(status string) string {
	switch status {
	case model.RunStatusCompleted:
		return "完成"
	case model.RunStatusFailed:
		return "失败"
	case model.RunStatusCancelled:
		return "已取消"
	case model.RunStatusNeedsReconciliation:
		return "待核对"
	default:
		return "结束"
	}
}
