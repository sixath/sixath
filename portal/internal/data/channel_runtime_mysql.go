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

var _ biz.ChannelRuntimeRepo = (*channelRuntimeRepo)(nil)

type channelRuntimeRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewChannelRuntimeRepo creates ChannelRuntimeRepo (MySQL/GORM).
func NewChannelRuntimeRepo(data *Data, logger log.Logger) biz.ChannelRuntimeRepo {
	if data == nil || data.db == nil {
		panic("NewChannelRuntimeRepo: Data.db is nil")
	}
	return &channelRuntimeRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *channelRuntimeRepo) Get(ctx context.Context, channelID string) (*biz.RuntimeStatusRow, error) {
	var m model.ChannelRuntimeStatus
	if err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelRuntimeModelToBiz(&m), nil
}

func (r *channelRuntimeRepo) Upsert(ctx context.Context, channelID string, patch biz.RuntimeStatusPatch) error {
	if channelID == "" || patch.State == "" {
		return gorm.ErrInvalidData
	}
	now := time.Now()

	var existing model.ChannelRuntimeStatus
	err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	found := err == nil

	m := model.ChannelRuntimeStatus{
		ChannelID:         channelID,
		State:             patch.State,
		LastHeartbeatAt:   now,
		UpdatedAt:         now,
		LastError:         "",
		ReconnectAttempt:  0,
		ReconnectInMs:     0,
		GatewayInstanceID: "",
	}
	if found {
		m.LastError = existing.LastError
		m.ReconnectAttempt = existing.ReconnectAttempt
		m.ReconnectInMs = existing.ReconnectInMs
		m.GatewayInstanceID = existing.GatewayInstanceID
	}
	if patch.LastError != nil {
		m.LastError = *patch.LastError
	}
	if patch.ReconnectAttempt != nil {
		m.ReconnectAttempt = *patch.ReconnectAttempt
	}
	if patch.ReconnectInMs != nil {
		m.ReconnectInMs = *patch.ReconnectInMs
	}
	if patch.GatewayInstanceID != nil {
		m.GatewayInstanceID = *patch.GatewayInstanceID
	}
	if patch.State == "connected" {
		m.LastError = ""
		m.ReconnectAttempt = 0
		m.ReconnectInMs = 0
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"state",
			"last_heartbeat_at",
			"last_error",
			"reconnect_attempt",
			"reconnect_in_ms",
			"gateway_instance_id",
			"updated_at",
		}),
	}).Create(&m).Error
}

func channelRuntimeModelToBiz(m *model.ChannelRuntimeStatus) *biz.RuntimeStatusRow {
	if m == nil {
		return nil
	}
	return &biz.RuntimeStatusRow{
		ChannelID:         m.ChannelID,
		State:             m.State,
		LastHeartbeatAt:   m.LastHeartbeatAt,
		LastError:         m.LastError,
		ReconnectAttempt:  m.ReconnectAttempt,
		ReconnectInMs:     m.ReconnectInMs,
		GatewayInstanceID: m.GatewayInstanceID,
		UpdatedAt:         m.UpdatedAt,
	}
}
