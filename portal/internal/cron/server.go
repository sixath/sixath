package cron

import (
	"context"
	"sync"
	"time"
)

// schedulerStopTimeout bounds Stop when the caller's context has no deadline.
const schedulerStopTimeout = 15 * time.Second

// Server 实现 kratos Server 接口，用于在应用生命周期内运行 Cron 调度器
type Server struct {
	scheduler *Scheduler
	mu        sync.Mutex
	cancel    context.CancelFunc
}

// NewServer 创建 Cron 调度器 Server
func NewServer(scheduler *Scheduler) *Server {
	return &Server{scheduler: scheduler}
}

// Start 在 goroutine 中启动调度器；kratos 传入的 ctx 不会在停止时取消，由 Stop 取消
func (s *Server) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	s.scheduler.startAsync(ctx)
	return nil
}

// Stop 取消调度器并等待其循环与 handbook 任务退出；超时只记日志，不让 app.Run 返回错误
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if _, ok := ctx.Deadline(); !ok {
		var c context.CancelFunc
		ctx, c = context.WithTimeout(ctx, schedulerStopTimeout)
		defer c()
	}
	if err := s.scheduler.Wait(ctx); err != nil {
		s.scheduler.log.Warnf("cron scheduler stop: %v", err)
	}
	return nil
}
