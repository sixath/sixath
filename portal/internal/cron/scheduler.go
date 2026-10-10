package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

// Scheduler 定时任务调度器：周期性扫描待执行任务并触发执行
type Scheduler struct {
	cronUC      *biz.CronUsecase
	exec        *Executor
	runTask     func(ctx context.Context, task *biz.CronTaskMeta, triggeredAt time.Time)
	interval    time.Duration
	lastSweep   time.Time // touched only by tick, which runs on one goroutine
	evolutionUC *biz.EvolutionUsecase
	log         *log.Helper

	repoUC           *biz.RepoRegistryUsecase
	repoScanInterval time.Duration
	handbookUC       handbookJobs

	// wg covers Start and the loops and handbook jobs it spawns; Wait drains it.
	wg sync.WaitGroup
}

// handbookJobs is the part of biz.HandbookUsecase the scheduler runs.
type handbookJobs interface {
	RebuildStale(ctx context.Context) (int, error)
	EnrichPending(ctx context.Context) (int, error)
}

// DefaultRepoScanInterval is how often code roots are rescanned for repositories.
const DefaultRepoScanInterval = 10 * time.Minute

// maxLoggedRepoScanErrors caps per-scan error lines so one bad root cannot flood the log.
const maxLoggedRepoScanErrors = 5

// NewScheduler 创建调度器，interval 为扫描间隔（如 30s）
func NewScheduler(cronUC *biz.CronUsecase, exec *Executor, interval time.Duration, logger log.Logger) *Scheduler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s := &Scheduler{
		cronUC:   cronUC,
		exec:     exec,
		interval: interval,
		log:      log.NewHelper(logger),
	}
	if exec != nil {
		s.runTask = exec.executeScheduled
	}
	return s
}

// Start 启动调度循环，阻塞直到 ctx 取消
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	defer s.wg.Done()
	s.run(ctx)
}

// startAsync runs Start in a goroutine that Wait already covers when it returns.
func (s *Scheduler) startAsync(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

// Wait blocks until Start, its loops and the handbook jobs they started have returned
// (after ctx passed to Start is cancelled), or ctx is done.
func (s *Scheduler) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// spawn runs fn in a goroutine covered by wg; callers must themselves be covered.
func (s *Scheduler) spawn(fn func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn()
	}()
}

func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.spawn(func() { s.evolutionCleanupLoop(ctx) })
	if s.repoUC != nil {
		s.spawn(func() { s.repoScanLoop(ctx) })
	}

	for {
		select {
		case <-ctx.Done():
			s.log.Info("cron scheduler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// staleSweepInterval is how often runs left "running" are checked; the first tick always checks.
const staleSweepInterval = 5 * time.Minute

// maxMissedRecords caps the missed-slot runs recorded for one late claim.
const maxMissedRecords = 20

// tick starts every due task once: a run starts only after its slot is claimed by moving
// next_run_at past now, so runs still in progress are not started again by later ticks.
// A late claim runs the earliest due slot and records later slots that passed as missed.
// One-shot tasks get no next run; a task whose schedule no longer yields a next run is
// recorded as failed and stopped without running.
func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now()
	s.sweepStaleRuns(ctx, now)
	tasks, err := s.cronUC.ListDue(ctx, now)
	if err != nil {
		s.log.Errorf("list due tasks: %v", err)
		return
	}
	for _, t := range tasks {
		var next *time.Time
		var schedErr error
		if t.ScheduleKind != "at" {
			next, schedErr = biz.CronNextRun(t.ScheduleKind, t.ScheduleExpr, t.Timezone, now)
			if schedErr == nil && next == nil {
				schedErr = fmt.Errorf("计划类型 %q 或表达式 %q 无效", t.ScheduleKind, t.ScheduleExpr)
			}
		}
		claimed, err := s.cronUC.ClaimDue(ctx, t.ID, now, next)
		if err != nil {
			s.log.Errorf("claim cron task %s: %v", t.ID, err)
			continue
		}
		if !claimed {
			continue
		}
		due := *t.NextRunAt
		if schedErr != nil {
			s.log.Errorf("cron task %s: invalid schedule %q, stopping it: %v", t.ID, t.ScheduleExpr, schedErr)
			s.recordRun(ctx, t, due, now, biz.CronRunFailed, "计划表达式无法解析，任务已停止调度："+schedErr.Error())
			continue
		}
		if t.ScheduleKind != "at" {
			s.recordMissed(ctx, t, due, now)
		}
		if s.runTask != nil {
			go s.runTask(ctx, t, due)
		}
	}
}

// recordMissed records the slots after due that passed more than one tick ago, i.e. while
// the backend was not running; they are not run.
func (s *Scheduler) recordMissed(ctx context.Context, t *biz.CronTaskMeta, due, now time.Time) {
	cutoff := now.Add(-s.interval)
	var slots []time.Time
	more := false
	for at := due; ; {
		n, err := biz.CronNextRun(t.ScheduleKind, t.ScheduleExpr, t.Timezone, at)
		if err != nil || n == nil || !n.Before(cutoff) {
			break
		}
		if len(slots) == maxMissedRecords {
			more = true
			break
		}
		slots = append(slots, *n)
		at = *n
	}
	if len(slots) == 0 {
		return
	}
	s.log.Warnf("cron task %s: %d scheduled runs missed while the backend was down", t.ID, len(slots))
	for i, at := range slots {
		msg := "错过执行：计划时间时后端未运行；恢复后只补跑了最早的一次（" + due.Format(time.RFC3339) + "）"
		if more && i == len(slots)-1 {
			msg += fmt.Sprintf("；此后还有更多错过的执行未逐条记录（最多记录 %d 条）", maxMissedRecords)
		}
		s.recordRun(ctx, t, at, now, biz.CronRunMissed, msg)
	}
}

// recordRun records a finished schedule-triggered run that did not execute.
func (s *Scheduler) recordRun(ctx context.Context, t *biz.CronTaskMeta, triggeredAt, now time.Time, status, msg string) {
	finished := now
	run := &biz.CronRunMeta{
		ID:          uuid.New().String(),
		TaskID:      t.ID,
		Trigger:     biz.CronTriggerSchedule,
		TriggeredAt: triggeredAt,
		StartedAt:   now,
		FinishedAt:  &finished,
		Status:      status,
		Error:       msg,
	}
	if err := s.cronUC.CreateRun(ctx, run); err != nil {
		s.log.Errorf("record %s run for cron task %s at %s: %v", status, t.ID, triggeredAt.Format(time.RFC3339), err)
	}
}

func (s *Scheduler) sweepStaleRuns(ctx context.Context, now time.Time) {
	if !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < staleSweepInterval {
		return
	}
	s.lastSweep = now
	n, err := s.cronUC.SweepStaleRuns(ctx, now)
	if err != nil {
		s.log.Errorf("sweep stale cron runs: %v", err)
	}
	if n > 0 {
		s.log.Warnf("marked %d stale cron runs as interrupted", n)
	}
}

// SetEvolutionUsecase wires the evolution usecase for weekly proposal expiry cleanup.
func (s *Scheduler) SetEvolutionUsecase(uc *biz.EvolutionUsecase) {
	s.evolutionUC = uc
}

// SetRepoRegistry enables periodic repository scans.
func (s *Scheduler) SetRepoRegistry(uc *biz.RepoRegistryUsecase, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRepoScanInterval
	}
	s.repoUC = uc
	s.repoScanInterval = interval
}

// SetHandbook rebuilds stale repository handbooks, then runs their pending LLM layer, after
// every successful repo scan.
func (s *Scheduler) SetHandbook(uc *biz.HandbookUsecase) {
	if uc == nil {
		s.handbookUC = nil
		return
	}
	s.handbookUC = uc
}

// repoScanLoop scans once at startup, then every repoScanInterval.
func (s *Scheduler) repoScanLoop(ctx context.Context) {
	s.runRepoScan(ctx)
	ticker := time.NewTicker(s.repoScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRepoScan(ctx)
		}
	}
}

func (s *Scheduler) runRepoScan(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Errorf("repo scan panic: %v", r)
		}
	}()
	rep, err := s.repoUC.Scan(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.log.Warnf("repo scan: %v", err)
		return
	}
	errs := rep.Errors
	if len(errs) > maxLoggedRepoScanErrors {
		errs = errs[:maxLoggedRepoScanErrors]
	}
	s.log.Infof("repo scan: roots=%d found=%d added=%d restored=%d missing=%d errors=%d first_errors=%q",
		rep.Roots, rep.Found, rep.Added, rep.Restored, rep.Missing, len(rep.Errors), errs)
	if s.handbookUC != nil {
		s.spawn(func() { s.runHandbookRebuild(ctx) })
	}
}

func (s *Scheduler) runHandbookRebuild(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Errorf("handbook rebuild panic: %v", r)
		}
	}()
	n, err := s.handbookUC.RebuildStale(ctx)
	if err != nil && ctx.Err() == nil {
		s.log.Warnf("handbook rebuild: %v", err)
	}
	if n > 0 {
		s.log.Infof("handbook rebuild: %d repos", n)
	}
	// Runs in this scan's goroutine: later scans rebuild meanwhile, and overlapping
	// EnrichPending calls return at once.
	n, err = s.handbookUC.EnrichPending(ctx)
	if err != nil && ctx.Err() == nil {
		s.log.Warnf("handbook enrich: %v", err)
	}
	if n > 0 {
		s.log.Infof("handbook enrich: %d repos", n)
	}
}

// evolutionCleanupLoop runs weekly expiry of old pending proposals.
func (s *Scheduler) evolutionCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.evolutionUC == nil {
				continue
			}
			expired, err := s.evolutionUC.ExpireOld(ctx, time.Now().Add(-30*24*time.Hour))
			if err != nil {
				s.log.Errorf("evolution cleanup: %v", err)
			} else if expired > 0 {
				s.log.Infof("evolution cleanup: expired %d proposals", expired)
			}
		}
	}
}
