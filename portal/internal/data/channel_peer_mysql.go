package data

import (
	"context"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.ChannelPeerSessionRepo = (*channelPeerSessionRepo)(nil)

type channelPeerSessionRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewChannelPeerSessionRepo creates ChannelPeerSessionRepo (MySQL/GORM).
func NewChannelPeerSessionRepo(data *Data, logger log.Logger) biz.ChannelPeerSessionRepo {
	if data == nil || data.db == nil {
		panic("NewChannelPeerSessionRepo: Data.db is nil")
	}
	return &channelPeerSessionRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *channelPeerSessionRepo) Get(ctx context.Context, channelID, peerID string) (*biz.ChannelPeerSession, error) {
	var m model.ChannelPeerSession
	if err := r.db.WithContext(ctx).
		Where("channel_id = ? AND peer_id = ?", channelID, peerID).
		First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelPeerModelToBiz(&m), nil
}

func (r *channelPeerSessionRepo) Create(ctx context.Context, row *biz.ChannelPeerSession) error {
	if row == nil {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	m := &model.ChannelPeerSession{
		ChannelID: row.ChannelID,
		PeerID:    row.PeerID,
		SessionID: row.SessionID,
		AgentID:   row.AgentID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		if isDuplicateKey(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (r *channelPeerSessionRepo) Upsert(ctx context.Context, row *biz.ChannelPeerSession) error {
	if row == nil {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	m := &model.ChannelPeerSession{
		ChannelID: row.ChannelID,
		PeerID:    row.PeerID,
		SessionID: row.SessionID,
		AgentID:   row.AgentID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "peer_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "agent_id", "updated_at"}),
	}).Create(m).Error
}

func (r *channelPeerSessionRepo) Delete(ctx context.Context, channelID, peerID string) error {
	res := r.db.WithContext(ctx).
		Where("channel_id = ? AND peer_id = ?", channelID, peerID).
		Delete(&model.ChannelPeerSession{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func channelPeerModelToBiz(m *model.ChannelPeerSession) *biz.ChannelPeerSession {
	if m == nil {
		return nil
	}
	return &biz.ChannelPeerSession{
		ChannelID: m.ChannelID,
		PeerID:    m.PeerID,
		SessionID: m.SessionID,
		AgentID:   m.AgentID,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}
