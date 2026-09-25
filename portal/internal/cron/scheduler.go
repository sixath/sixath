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
}

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
