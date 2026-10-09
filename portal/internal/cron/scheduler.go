package cron

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// Scheduler 定时任务调度器：周期性扫描待执行任务并触发执行
type Scheduler struct {
	cronUC      *biz.CronUsecase
	exec        *Executor
	interval    time.Duration
	evolutionUC *biz.EvolutionUsecase
	log         *log.Helper

	repoUC           *biz.RepoRegistryUsecase
	repoScanInterval time.Duration
	handbookUC       *biz.HandbookUsecase
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
	return &Scheduler{
		cronUC:   cronUC,
		exec:     exec,
		interval: interval,
		log:      log.NewHelper(logger),
	}
}

// Start 启动调度循环，阻塞直到 ctx 取消
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	go s.evolutionCleanupLoop(ctx)
	if s.repoUC != nil {
		go s.repoScanLoop(ctx)
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

func (s *Scheduler) tick(ctx context.Context) {
	tasks, err := s.cronUC.ListDue(ctx, time.Now())
	if err != nil {
		s.log.Errorf("list due tasks: %v", err)
		return
	}
	for _, t := range tasks {
		task := t
		go s.exec.Execute(ctx, task)
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

// SetHandbook rebuilds stale repository handbooks after every successful repo scan.
func (s *Scheduler) SetHandbook(uc *biz.HandbookUsecase) {
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
		go s.runHandbookRebuild(ctx)
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
