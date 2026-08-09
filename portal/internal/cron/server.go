package cron

import (
	"context"
)

// Server 实现 kratos Server 接口，用于在应用生命周期内运行 Cron 调度器
type Server struct {
	scheduler *Scheduler
}

// NewServer 创建 Cron 调度器 Server
func NewServer(scheduler *Scheduler) *Server {
	return &Server{scheduler: scheduler}
}

// Start 启动调度器（在 goroutine 中运行，ctx 取消时自动停止）
func (s *Server) Start(ctx context.Context) error {
	go s.scheduler.Start(ctx)
	return nil
}

// Stop 停止调度器（ctx 取消后 Scheduler 会自动退出）
func (s *Server) Stop(ctx context.Context) error {
	return nil
}
