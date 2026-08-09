package data

import (
	"context"
	"fmt"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

var _ biz.CuratorRepo = (*curatorRepo)(nil)

type curatorRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewCuratorRepo creates CuratorRepo.
func NewCuratorRepo(data *Data, logger log.Logger) biz.CuratorRepo {
	return &curatorRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *curatorRepo) GetState(ctx context.Context, workspaceKey string) (*biz.CuratorState, error) {
	var m model.GrowthCuratorState
	if err := r.db.WithContext(ctx).Where("workspace_key = ?", workspaceKey).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return curatorModelToBiz(&m), nil
}

func (r *curatorRepo) SaveState(ctx context.Context, st *biz.CuratorState) error {
	if st == nil || st.WorkspaceKey == "" {
		return fmt.Errorf("curator: nil state")
	}
	m := curatorBizToModel(st)
	now := time.Now()
	m.UpdatedAt = now
	return r.db.WithContext(ctx).Save(m).Error
}

func curatorModelToBiz(m *model.GrowthCuratorState) *biz.CuratorState {
	if m == nil {
		return nil
	}
	return &biz.CuratorState{
		WorkspaceKey:  m.WorkspaceKey,
		LastCuratorAt: m.LastCuratorAt,
		LastError:     m.LastError,
		UpdatedAt:     m.UpdatedAt,
	}
}

func curatorBizToModel(b *biz.CuratorState) *model.GrowthCuratorState {
	return &model.GrowthCuratorState{
		WorkspaceKey:  b.WorkspaceKey,
		LastCuratorAt: b.LastCuratorAt,
		LastError:     b.LastError,
		UpdatedAt:     b.UpdatedAt,
	}
}
