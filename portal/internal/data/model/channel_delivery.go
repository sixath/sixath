package model

import "time"

// ChannelDelivery 出站投递记录表（Task 10）。
//
// Status 取值：pending | sent | failed。当前同步投递直接写终态（sent/failed）；
// NextRetryAt 为后续异步 outbox worker 预留（pending + 下次重试时间）。
type ChannelDelivery struct {
	ID          string     `gorm:"column:id;primaryKey;size:36"`
	ChannelID   string     `gorm:"column:channel_id;size:64;index"`
	SessionID   string     `gorm:"column:session_id;size:36;index"`
	Payload     string     `gorm:"column:payload;type:text"`
	MsgType     string     `gorm:"column:msg_type;size:16"`
	Status      string     `gorm:"column:status;size:16;index"`
	Attempts    int        `gorm:"column:attempts;not null;default:0"`
	LastError   string     `gorm:"column:last_error;type:text"`
	NextRetryAt *time.Time `gorm:"column:next_retry_at;index"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
}

func (ChannelDelivery) TableName() string {
	return "channel_deliveries"
}
