package biz

import (
	"context"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

type fakeCuratorRepo struct {
	state *CuratorState
}

func (f *fakeCuratorRepo) GetState(ctx context.Context, workspaceKey string) (*CuratorState, error) {
	_ = ctx
	if f.state == nil || f.state.WorkspaceKey != workspaceKey {
		return nil, pkgErrors.ErrNotFound
	}
	return f.state, nil
}

func (f *fakeCuratorRepo) SaveState(ctx context.Context, st *CuratorState) error {
	_ = ctx
	cp := *st
	f.state = &cp
	return nil
}

func TestCuratorUsecase_IsDue(t *testing.T) {
	repo := &fakeCuratorRepo{}
	uc := NewCuratorUsecase(repo, nil, nil)
	due, err := uc.IsDue(context.Background(), "ws1", time.Hour)
	if err != nil || !due {
		t.Fatalf("new workspace should be due: due=%v err=%v", due, err)
	}
	past := time.Now().Add(-2 * time.Hour)
	repo.state = &CuratorState{WorkspaceKey: "ws1", LastCuratorAt: &past}
	due, err = uc.IsDue(context.Background(), "ws1", time.Hour)
	if err != nil || !due {
		t.Fatalf("old curator should be due")
	}
	recent := time.Now().Add(-10 * time.Minute)
	repo.state.LastCuratorAt = &recent
	due, err = uc.IsDue(context.Background(), "ws1", time.Hour)
	if err != nil || due {
		t.Fatalf("recent curator should not be due: %v", due)
	}
}
