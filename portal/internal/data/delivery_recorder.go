package data

import (
	"context"

	"backend/internal/channel"
	"backend/internal/data/model"

	"gorm.io/gorm"
)

// MySQLDeliveryRecorder 把 channel.DeliveryRecord 持久化到 channel_deliveries 表。
type MySQLDeliveryRecorder struct {
	db *gorm.DB
}

// NewDeliveryRecorder 构造投递记录器。db 为空时 Record 为 no-op（便于可选装配）。
func NewDeliveryRecorder(db *gorm.DB) *MySQLDeliveryRecorder {
	return &MySQLDeliveryRecorder{db: db}
}

var _ channel.DeliveryRecorder = (*MySQLDeliveryRecorder)(nil)

// Record 写入一条投递记录。
func (r *MySQLDeliveryRecorder) Record(ctx context.Context, rec channel.DeliveryRecord) error {
	if r == nil || r.db == nil {
		return nil
	}
	row := model.ChannelDelivery{
		ID:          rec.ID,
		ChannelID:   rec.ChannelID,
		SessionID:   rec.SessionID,
		Payload:     rec.Content,
		MsgType:     rec.MsgType,
		Status:      string(rec.Status),
		Attempts:    rec.Attempts,
		LastError:   rec.LastError,
		NextRetryAt: rec.NextRetryAt,
	}
	return r.db.WithContext(ctx).Create(&row).Error
}
