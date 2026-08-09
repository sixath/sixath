package biz

import (
	"context"
	"errors"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

// CuratorWorkspace 待清扫的 workspace 行（一个 workspace 可对应多个 agent，取代表 agent_id）。
type CuratorWorkspace struct {
	WorkspaceKey string
	AgentID      string
}

// CuratorRepo Curator 游标持久化。
type CuratorRepo interface {
	GetState(ctx context.Context, workspaceKey string) (*CuratorState, error)
	SaveState(ctx context.Context, st *CuratorState) error
}

// CuratorState workspace 级 Curator 游标。
type CuratorState struct {
	WorkspaceKey  string
	LastCuratorAt *time.Time
	LastError     string
	UpdatedAt     time.Time
}

// CuratorUsecase R2b workspace Curator 编排（租约复用 GrowthUsecase）。
type CuratorUsecase struct {
	curatorRepo CuratorRepo
	agentRepo   AgentRepo
	growthUC    *GrowthUsecase
}

// NewCuratorUsecase constructs CuratorUsecase.
func NewCuratorUsecase(curatorRepo CuratorRepo, agentRepo AgentRepo, growthUC *GrowthUsecase) *CuratorUsecase {
	return &CuratorUsecase{
		curatorRepo: curatorRepo,
		agentRepo:   agentRepo,
		growthUC:    growthUC,
	}
}

// ListWorkspaces 返回所有非空 workspace（去重）。
func (uc *CuratorUsecase) ListWorkspaces(ctx context.Context, limit int) ([]CuratorWorkspace, error) {
	if uc == nil || uc.agentRepo == nil {
		return nil, nil
	}
	return uc.agentRepo.ListDistinctWorkspaces(ctx, limit)
}

// IsDue 距上次成功 Curator 是否已超过 interval。
func (uc *CuratorUsecase) IsDue(ctx context.Context, workspaceKey string, interval time.Duration) (bool, error) {
	if workspaceKey == "" {
		return false, nil
	}
	st, err := uc.curatorRepo.GetState(ctx, workspaceKey)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	if st.LastCuratorAt == nil {
		return true, nil
	}
	return time.Since(*st.LastCuratorAt) >= interval, nil
}

// MarkCuratorDone 记录成功清扫时间并清空错误。
func (uc *CuratorUsecase) MarkCuratorDone(ctx context.Context, workspaceKey string) error {
	if workspaceKey == "" {
		return nil
	}
	now := time.Now()
	st, err := uc.curatorRepo.GetState(ctx, workspaceKey)
	if err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	if st == nil {
		st = &CuratorState{WorkspaceKey: workspaceKey}
	}
	st.LastCuratorAt = &now
	st.LastError = ""
	st.UpdatedAt = now
	return uc.curatorRepo.SaveState(ctx, st)
}

// RecordCuratorFailure 记录失败信息，不推进 last_curator_at（便于重试）。
func (uc *CuratorUsecase) RecordCuratorFailure(ctx context.Context, workspaceKey string, runErr error) error {
	if workspaceKey == "" || runErr == nil {
		return nil
	}
	msg := runErr.Error()
	const maxLen = 2048
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	st, err := uc.curatorRepo.GetState(ctx, workspaceKey)
	if err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return err
	}
	if st == nil {
		st = &CuratorState{WorkspaceKey: workspaceKey}
	}
	st.LastError = msg
	st.UpdatedAt = time.Now()
	return uc.curatorRepo.SaveState(ctx, st)
}

// TryAcquireWorkspaceLease 委托 Growth 租约（与复盘 worker 共用 CAS）。
func (uc *CuratorUsecase) TryAcquireWorkspaceLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (bool, error) {
	if uc.growthUC == nil {
		return false, nil
	}
	return uc.growthUC.TryAcquireWorkspaceLease(ctx, workspaceKey, holderID, ttl)
}

// ReleaseWorkspaceLease 释放租约。
func (uc *CuratorUsecase) ReleaseWorkspaceLease(ctx context.Context, workspaceKey, holderID string) error {
	if uc.growthUC == nil {
		return nil
	}
	return uc.growthUC.ReleaseWorkspaceLease(ctx, workspaceKey, holderID)
}
