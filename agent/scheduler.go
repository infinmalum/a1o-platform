package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

// Scheduler is the work queue worker (R3): it claims queued work runs with a
// cross-instance lease, keeps the lease alive by heartbeat, and drives each
// claim through the engine on a worker-owned context — a work run outlives
// its observers by construction. Cancel requests surface through the database
// transition (the next heartbeat fails) and, same-instance, through the
// immediate notify channel.

type Scheduler struct {
	Engine *Engine
	// instanceID identifies this process in lease claims; hostname plus pid
	// is enough to tell concurrent instances apart.
	instanceID string
	// workerSlots bounds concurrently executing work runs on this instance.
	workerSlots chan struct{}
	// wake nudges the claim loop when a new run becomes claimable.
	wake chan struct{}
	// cancels maps an actively executed run id to a channel closed when a
	// cancel request arrived on this instance.
	cancels sync.Map
	// newExecutor builds the run owner's billing identity; tests inject a
	// fake here instead of standing up the relay lifecycle.
	newExecutor func(userId int) ModelInvoker
}

var (
	schedulerOnce    sync.Once
	defaultScheduler *Scheduler
)

// DefaultScheduler returns the process-wide work scheduler.
func DefaultScheduler() *Scheduler {
	schedulerOnce.Do(func() {
		defaultScheduler = NewScheduler(Default())
	})
	return defaultScheduler
}

func NewScheduler(engine *Engine) *Scheduler {
	host, _ := os.Hostname()
	workers := config.WorkWorkerCount
	if workers <= 0 {
		workers = 1
	}
	return &Scheduler{
		Engine:      engine,
		instanceID:  fmt.Sprintf("%s-%d", host, os.Getpid()),
		workerSlots: make(chan struct{}, workers),
		wake:        make(chan struct{}, 1),
		newExecutor: func(userId int) ModelInvoker {
			token, err := model.EnsureChatToken(userId)
			if err != nil {
				return nil
			}
			return &ModelExecutor{UserID: userId, TokenID: token.Id, TokenName: token.Name}
		},
	}
}

// Start runs the startup reconciliation pass and the claim loop.
func (s *Scheduler) Start() {
	if err := model.ReconcileInterruptedRuns(); err != nil {
		logger.SysError("work run reconciliation failed: " + err.Error())
	}
	go s.loop()
}

// Wake prompts an immediate claim pass; safe to call from any goroutine.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// NotifyCancel trips the local fast-path cancellation for a running worker.
func (s *Scheduler) NotifyCancel(runId int) {
	if raw, ok := s.cancels.Load(runId); ok {
		ch := raw.(chan struct{})
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.wake:
		case <-ticker.C:
		}
		s.claimAvailable()
	}
}

// claimAvailable fills every free worker slot with a claimable run.
func (s *Scheduler) claimAvailable() {
	for {
		select {
		case s.workerSlots <- struct{}{}:
		default:
			return
		}
		run, err := model.ClaimNextWorkRun(s.instanceID, config.WorkLeaseSeconds, config.WorkUserConcurrency)
		if err != nil {
			logger.SysError("work claim failed: " + err.Error())
			<-s.workerSlots
			return
		}
		if run == nil {
			<-s.workerSlots
			return
		}
		go s.runWork(run)
	}
}

// runWork executes one claimed run on a worker-owned context: heartbeat
// renewal and cancel notifications both collapse into context cancellation,
// and the finalizer resolves whatever terminal state the engine left behind.
func (s *Scheduler) runWork(run *model.AgentRun) {
	defer func() { <-s.workerSlots }()
	cancelCh := make(chan struct{})
	s.cancels.Store(run.Id, cancelCh)
	defer s.cancels.Delete(run.Id)

	fencing := run.FencingToken
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	go func() {
		select {
		case <-cancelCh:
			cancelBase()
		case <-baseCtx.Done():
		}
	}()
	workCtx, cancelTimeout := context.WithTimeout(baseCtx,
		time.Duration(config.WorkRunTimeoutSeconds)*time.Second)
	defer cancelTimeout()

	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(time.Duration(config.WorkHeartbeatSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-baseCtx.Done():
				return
			case <-ticker.C:
				ok, err := model.RenewWorkLease(run.Id, s.instanceID, fencing, config.WorkLeaseSeconds)
				if err != nil {
					logger.SysLogf("work run %d heartbeat error: %s", run.Id, err.Error())
					cancelBase()
					return
				}
				if !ok {
					// Lease expired, run moved on, or a cancel request was
					// recorded: stop advancing either way; the finalizer
					// re-reads the run to resolve the state.
					logger.SysLogf("work run %d lease lost", run.Id)
					cancelBase()
					return
				}
			}
		}
	}()

	req, buildErr := s.buildTurnRequest(run, func(event EventEnvelope) error {
		PublishRun(run.Id, event)
		return nil
	})
	var err error
	if buildErr != nil {
		// A run whose pinned agent became unavailable between queueing and
		// claiming fails with the explainable cause; finalize records it.
		err = buildErr
	} else if req != nil {
		err = s.Engine.RunWork(workCtx, *req)
	} else {
		err = errors.New("计费令牌不可用")
	}
	cancelBase()
	<-hbDone
	s.finalize(run, fencing, err)
}

// buildTurnRequest assembles the engine request: task row, the pinned agent
// version (frozen spec plus the runner-visible skill intersection), and the
// run owner's billing identity.
func (s *Scheduler) buildTurnRequest(run *model.AgentRun, emit Emitter) (*WorkTurnRequest, error) {
	task, err := model.GetWorkTask(run.TaskId, run.UserId)
	if err != nil {
		logger.SysLogf("work run %d task load failed: %s", run.Id, err.Error())
		task = nil
	}
	executor := s.newExecutor(run.UserId)
	if executor == nil {
		return nil, nil
	}
	req := &WorkTurnRequest{
		Run:      run,
		Fencing:  run.FencingToken,
		Task:     task,
		Executor: executor,
		Emit:     emit,
	}
	if run.AgentVersionId > 0 {
		// Re-resolve at claim time: archiving between queueing and claiming
		// still blocks the start, and skill grants re-check current state.
		_, _, spec, err := model.ResolveAgentVersionForRun(run.UserId, run.AgentVersionId, model.AgentModeWork)
		if err != nil {
			return nil, err
		}
		req.AgentSpec = spec
		skills, err := agentScopedSkills(run.UserId, spec)
		if err != nil {
			return nil, err
		}
		req.Skills = skills
		if len(spec.FileIds) > 0 {
			filesById, err := model.GetChatFilesByIds(spec.FileIds)
			if err != nil {
				return nil, err
			}
			for _, id := range spec.FileIds {
				if file := filesById[id]; file != nil {
					req.ReferenceFiles = append(req.ReferenceFiles, file)
				}
			}
		}
	}
	return req, nil
}

// agentScopedSkills intersects the runner's currently enabled skills with
// the agent version's declared set — actual permissions are the
// intersection, re-resolved at every execution so revocation is immediate.
func agentScopedSkills(userId int, spec *model.AgentVersionSpec) ([]*model.ChatSkill, error) {
	if len(spec.SkillIds) == 0 {
		return nil, nil
	}
	allowed := make(map[int]bool, len(spec.SkillIds))
	for _, id := range spec.SkillIds {
		allowed[id] = true
	}
	enabled, err := model.ListEnabledSkills(userId)
	if err != nil {
		return nil, err
	}
	scoped := make([]*model.ChatSkill, 0, len(enabled))
	for _, skill := range enabled {
		if allowed[skill.Id] {
			scoped = append(scoped, skill)
		}
	}
	return scoped, nil
}

// finalize resolves the terminal state after the engine exits: a pending
// cancel request becomes cancelled, and a run we still own that exited
// without an in-band transition (timeout, worker error) fails with the
// cause. A run whose fencing moved on belongs to someone else — walk away.
func (s *Scheduler) finalize(run *model.AgentRun, fencing int64, err error) {
	fresh, ferr := model.GetAgentRunById(run.Id)
	if ferr != nil {
		logger.SysLogf("work finalize load failed for run %d: %s", run.Id, ferr.Error())
		return
	}
	switch fresh.Status {
	case model.RunStatusCancelRequested:
		ok, uerr := model.UpdateAgentRunStatusFenced(run.Id, fencing,
			model.RunStatusCancelRequested, model.RunStatusCancelled, "")
		if uerr != nil {
			logger.SysLogf("work run %d cancel finalize failed: %s", run.Id, uerr.Error())
			return
		}
		if ok {
			if cerr := model.AppendRunControlEvent(run.Id, model.RunEventRunCancelled,
				map[string]any{"reason": "cancelled_by_user"}); cerr != nil {
				logger.SysLogf("work run %d cancel event failed: %s", run.Id, cerr.Error())
			}
		}
	case model.RunStatusRunning:
		if fresh.FencingToken != fencing || fresh.LeaseOwner != s.instanceID {
			return
		}
		errorText := "任务执行中止"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			errorText = "已达任务时间上限"
		case err != nil:
			errorText = err.Error()
		}
		ok, uerr := model.UpdateAgentRunStatusFenced(run.Id, fencing,
			model.RunStatusRunning, model.RunStatusFailed, errorText)
		if uerr != nil {
			logger.SysLogf("work run %d fail finalize failed: %s", run.Id, uerr.Error())
			return
		}
		if ok {
			if cerr := model.AppendRunControlEvent(run.Id, model.RunEventRunFailed,
				map[string]any{"error": errorText}); cerr != nil {
				logger.SysLogf("work run %d fail event failed: %s", run.Id, cerr.Error())
			}
		}
	}
}
