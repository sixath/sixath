package data

import (
	"context"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.InviteRepo = (*inviteRepo)(nil)

type inviteRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewInviteRepo creates a MySQL/GORM org invite repository.
func NewInviteRepo(data *Data, logger log.Logger) biz.InviteRepo {
	if data == nil || data.db == nil {
		panic("NewInviteRepo: Data.db is nil, database config required")
	}
	return &inviteRepo{db: data.db, log: log.NewHelper(logger)}
}

func inviteModelToBiz(m *model.OrgInvite) *biz.OrgInvite {
	return &biz.OrgInvite{
		ID:        m.ID,
		OrgID:     m.OrgID,
		CreatedBy: m.CreatedBy,
		MaxUses:   m.MaxUses,
		UsedCount: m.UsedCount,
		ExpiresAt: m.ExpiresAt,
		RevokedAt: m.RevokedAt,
		CreatedAt: m.CreatedAt,
	}
}

func (r *inviteRepo) CreateInvite(ctx context.Context, orgID, createdBy string, maxUses int, expiresAt *time.Time) (*biz.OrgInvite, string, error) {
	plain, err := biz.GenerateOpaqueToken()
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	m := &model.OrgInvite{
		ID:        uuid.NewString(),
		OrgID:     orgID,
		TokenHash: biz.HashTokenSHA256Hex(plain),
		CreatedBy: createdBy,
		MaxUses:   maxUses,
		UsedCount: 0,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	values := map[string]any{
		"id":          m.ID,
		"org_id":      m.OrgID,
		"token_hash":  m.TokenHash,
		"created_by":  m.CreatedBy,
		"max_uses":    maxUses,
		"used_count":  0,
		"expires_at":  expiresAt,
		"created_at":  now,
	}
	if err := r.db.WithContext(ctx).Model(&model.OrgInvite{}).Create(values).Error; err != nil {
		return nil, "", err
	}
	return inviteModelToBiz(m), plain, nil
}

func (r *inviteRepo) GetInviteByTokenHash(ctx context.Context, tokenHash string) (*biz.OrgInvite, error) {
	var m model.OrgInvite
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return inviteModelToBiz(&m), nil
}

func (r *inviteRepo) ListInvitesByOrg(ctx context.Context, orgID string) ([]*biz.OrgInvite, error) {
	var rows []model.OrgInvite
	if err := r.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.OrgInvite, len(rows))
	for i := range rows {
		out[i] = inviteModelToBiz(&rows[i])
	}
	return out, nil
}

func (r *inviteRepo) IncrementInviteUsed(ctx context.Context, id string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&model.OrgInvite{}).
		Where("id = ?", id).
		Where("revoked_at IS NULL").
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Where("(max_uses = 0 OR used_count < max_uses)").
		UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

func (r *inviteRepo) RevokeInvite(ctx context.Context, id string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&model.OrgInvite{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
