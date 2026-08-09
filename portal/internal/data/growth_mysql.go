package data

import (
	"context"
	"fmt"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.GrowthRepo = (*growthRepo)(nil)

type growthRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewGrowthRepo creates GrowthRepo.
func NewGrowthRepo(data *Data, logger log.Logger) biz.GrowthRepo {
	return &growthRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *growthRepo) GetState(ctx context.Context, sessionID string) (*biz.ChatGrowthState, error) {
	var m model.ChatGrowthState
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return growthModelToBiz(&m), nil
}

func (r *growthRepo) SaveState(ctx context.Context, st *biz.ChatGrowthState) error {
	if st == nil {
		return fmt.Errorf("growth: nil state")
	}
	m := growthBizToModel(st)
	now := time.Now()
	m.UpdatedAt = now
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	return r.db.WithContext(ctx).Save(m).Error
}

// TryAcquireLease 在事务内 SELECT ... FOR UPDATE，过期或空占有人时可抢占；同 holder 可续期。
func (r *growthRepo) TryAcquireLease(ctx context.Context, workspaceKey, holderID string, ttl time.Duration) (bool, error) {
	if workspaceKey == "" || holderID == "" {
		return false, fmt.Errorf("growth: empty workspaceKey or holderID")
	}
	now := time.Now()
	exp := now.Add(ttl)
	var acquired bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.GrowthWorkspaceLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_key = ?", workspaceKey).
			First(&row).Error
		if err == gorm.ErrRecordNotFound {
			acquired = true
			return tx.Create(&model.GrowthWorkspaceLease{
				WorkspaceKey: workspaceKey,
				HolderID:     holderID,
				ExpiresAt:    exp,
				UpdatedAt:    now,
			}).Error
		}
		if err != nil {
			return err
		}
		if row.HolderID == holderID {
			acquired = true
			row.ExpiresAt = exp
			row.UpdatedAt = now
			return tx.Save(&row).Error
		}
		if row.ExpiresAt.After(now) {
			acquired = false
			return nil
		}
		acquired = true
		row.HolderID = holderID
		row.ExpiresAt = exp
		row.UpdatedAt = now
		return tx.Save(&row).Error
	})
	return acquired, err
}

func (r *growthRepo) ListPendingReviewSessions(ctx context.Context, limit int) ([]biz.GrowthPendingSession, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []struct {
		SessionID    string `gorm:"column:session_id"`
		AgentID      string `gorm:"column:agent_id"`
		WorkspaceKey string `gorm:"column:workspace_key"`
	}
	err := r.db.WithContext(ctx).
		Table("chat_growth_states AS g").
		Select("g.session_id AS session_id, s.agent_id AS agent_id, a.workspace AS workspace_key").
		Joins("INNER JOIN chat_sessions AS s ON s.id = g.session_id").
		Joins("INNER JOIN agents AS a ON a.id = s.agent_id").
		Where("(g.pending_skill_review = ? OR g.pending_memory_review = ?)", true, true).
		Where("a.workspace <> ?", "").
		Order("g.updated_at ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]biz.GrowthPendingSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, biz.GrowthPendingSession{
			SessionID:    row.SessionID,
			AgentID:      row.AgentID,
			WorkspaceKey: row.WorkspaceKey,
		})
	}
	return out, nil
}

// ListIdleSessions 返回无 pending 标志且 last_idle_check_at 早于 cutoff 的活动会话。
func (r *growthRepo) ListIdleSessions(ctx context.Context, idleInterval time.Duration, limit int) ([]biz.GrowthPendingSession, error) {
	if limit <= 0 {
		limit = 50
	}
	cutoff := time.Now().Add(-idleInterval)
	var rows []struct {
		SessionID    string `gorm:"column:session_id"`
		AgentID      string `gorm:"column:agent_id"`
		WorkspaceKey string `gorm:"column:workspace_key"`
	}
	err := r.db.WithContext(ctx).
		Table("chat_growth_states AS g").
		Select("g.session_id AS session_id, s.agent_id AS agent_id, a.workspace AS workspace_key").
		Joins("INNER JOIN chat_sessions AS s ON s.id = g.session_id").
		Joins("INNER JOIN agents AS a ON a.id = s.agent_id").
		Where("g.pending_skill_review = ? AND g.pending_memory_review = ?", false, false).
		Where("(g.last_idle_check_at IS NULL OR g.last_idle_check_at < ?)", cutoff).
		Where("a.workspace <> ?", "").
		Order("COALESCE(g.last_idle_check_at, '1970-01-01') ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]biz.GrowthPendingSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, biz.GrowthPendingSession{
			SessionID:    row.SessionID,
			AgentID:      row.AgentID,
			WorkspaceKey: row.WorkspaceKey,
		})
	}
	return out, nil
}

func (r *growthRepo) ReleaseLease(ctx context.Context, workspaceKey, holderID string) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.GrowthWorkspaceLease{}).
		Where("workspace_key = ? AND holder_id = ?", workspaceKey, holderID).
		Updates(map[string]any{
			"holder_id":  "",
			"expires_at": now,
			"updated_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func growthModelToBiz(m *model.ChatGrowthState) *biz.ChatGrowthState {
	return &biz.ChatGrowthState{
		SessionID:              m.SessionID,
		ToolItersSinceReview:   m.ToolItersSinceReview,
		TurnsSinceMemoryReview: m.TurnsSinceMemoryReview,
		PendingSkillReview:     m.PendingSkillReview,
		PendingMemoryReview:    m.PendingMemoryReview,
		LastSkillError:         m.LastSkillError,
		LastMemoryError:        m.LastMemoryError,
		ReviewFailedAt:         m.ReviewFailedAt,
		ReviewRetryCount:       m.ReviewRetryCount,
		LastIdleCheckAt:        m.LastIdleCheckAt,
		LastBackgroundReviewAt: m.LastBackgroundReviewAt,
		LastReviewRequestID:    m.LastReviewRequestID,
		BgReviewInFlight:       m.BgReviewInFlight,
		BgReviewInFlightSince:  m.BgReviewInFlightSince,
		CreatedAt:              m.CreatedAt,
		UpdatedAt:              m.UpdatedAt,
	}
}

func growthBizToModel(b *biz.ChatGrowthState) *model.ChatGrowthState {
	return &model.ChatGrowthState{
		SessionID:              b.SessionID,
		ToolItersSinceReview:   b.ToolItersSinceReview,
		TurnsSinceMemoryReview: b.TurnsSinceMemoryReview,
		PendingSkillReview:     b.PendingSkillReview,
		PendingMemoryReview:    b.PendingMemoryReview,
		LastSkillError:         b.LastSkillError,
		LastMemoryError:        b.LastMemoryError,
		ReviewFailedAt:         b.ReviewFailedAt,
		ReviewRetryCount:       b.ReviewRetryCount,
		LastIdleCheckAt:        b.LastIdleCheckAt,
		LastBackgroundReviewAt: b.LastBackgroundReviewAt,
		LastReviewRequestID:    b.LastReviewRequestID,
		BgReviewInFlight:       b.BgReviewInFlight,
		BgReviewInFlightSince:  b.BgReviewInFlightSince,
		CreatedAt:              b.CreatedAt,
		UpdatedAt:              b.UpdatedAt,
	}
}
