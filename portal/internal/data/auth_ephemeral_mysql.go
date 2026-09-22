package data

import (
	"context"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

var _ biz.AuthEphemeralRepo = (*authEphemeralRepo)(nil)

type authEphemeralRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewAuthEphemeralRepo creates a MySQL/GORM auth ephemeral repository.
func NewAuthEphemeralRepo(data *Data, logger log.Logger) biz.AuthEphemeralRepo {
	if data == nil || data.db == nil {
		panic("NewAuthEphemeralRepo: Data.db is nil, database config required")
	}
	return &authEphemeralRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *authEphemeralRepo) Put(ctx context.Context, id, kind, payloadJSON string, expiresAt time.Time) error {
	now := time.Now()
	m := &model.AuthEphemeral{
		ID:          id,
		Kind:        kind,
		PayloadJSON: payloadJSON,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *authEphemeralRepo) Consume(ctx context.Context, id, kind string, now time.Time) (string, error) {
	var payload string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.AuthEphemeral{}).
			Where("id = ? AND kind = ? AND consumed_at IS NULL AND expires_at > ?", id, kind, now).
			Update("consumed_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		var m model.AuthEphemeral
		if err := tx.Where("id = ? AND kind = ?", id, kind).First(&m).Error; err != nil {
			return err
		}
		payload = m.PayloadJSON
		return nil
	})
	return payload, err
}
