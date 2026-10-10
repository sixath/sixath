package biz

import (
	"context"
	"errors"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrCronTaskNotFound = kratosErrors.NotFound("CRON_TASK_NOT_FOUND", "cron task not found")
)

// CronUsecase 定时任务用例
type CronUsecase struct {
	taskRepo CronTaskRepo
	runRepo  CronRunRepo
	log      *log.Helper
}

// NewCronUsecase creates a CronUsecase
func NewCronUsecase(taskRepo CronTaskRepo, runRepo CronRunRepo, logger log.Logger) *CronUsecase {
	return &CronUsecase{taskRepo: taskRepo, runRepo: runRepo, log: log.NewHelper(logger)}
}

// Create 创建定时任务
func (uc *CronUsecase) Create(ctx context.Context, t *CronTaskCreate) (*CronTaskMeta, error) {
	return uc.taskRepo.Create(ctx, t)
}

// Get 获取任务
func (uc *CronUsecase) Get(ctx context.Context, id string) (*CronTaskMeta, error) {
	t, err := uc.taskRepo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrCronTaskNotFound
	}
	return t, err
}

// List 列表
func (uc *CronUsecase) List(ctx context.Context, page, pageSize int32, agentID string, enabled *bool) ([]*CronTaskMeta, int, error) {
	return uc.taskRepo.List(ctx, page, pageSize, agentID, enabled)
}

// Update 更新
func (uc *CronUsecase) Update(ctx context.Context, id string, updates map[string]any) (*CronTaskMeta, error) {
	t, err := uc.taskRepo.Update(ctx, id, updates)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrCronTaskNotFound
	}
	return t, err
}

// Delete 删除
func (uc *CronUsecase) Delete(ctx context.Context, id string) error {
	err := uc.taskRepo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrCronTaskNotFound
	}
	return err
}

// ListDue 获取待执行任务
func (uc *CronUsecase) ListDue(ctx context.Context, before time.Time) ([]*CronTaskMeta, error) {
	return uc.taskRepo.ListDue(ctx, before)
}

// ClaimDue 领取到期的一次执行：把 next_run_at 推进到 next（nil 表示不再执行），只有领取成功的调用方可以执行。
func (uc *CronUsecase) ClaimDue(ctx context.Context, id string, now time.Time, next *time.Time) (bool, error) {
	return uc.taskRepo.ClaimDue(ctx, id, now, next)
}

// CreateRun 创建执行记录
func (uc *CronUsecase) CreateRun(ctx context.Context, run *CronRunMeta) error {
	return uc.runRepo.Create(ctx, run)
}

// UpdateRun 更新执行记录
func (uc *CronUsecase) UpdateRun(ctx context.Context, id string, updates map[string]any) error {
	return uc.runRepo.Update(ctx, id, updates)
}

// ListRuns 获取任务执行历史
func (uc *CronUsecase) ListRuns(ctx context.Context, taskID string, page, pageSize int32) ([]*CronRunMeta, int, error) {
	return uc.runRepo.ListByTask(ctx, taskID, page, pageSize)
}
