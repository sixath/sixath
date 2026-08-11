package model

import "time"

// ChannelRuntimeStatus 渠道运行时状态（gateway 心跳/重连）
type ChannelRuntimeStatus struct {
	ChannelID         string    `gorm:"column:channel_id;primaryKey;size:64"`
	State             string    `gorm:"column:state;size:32;not null"`
	LastHeartbeatAt   time.Time `gorm:"column:last_heartbeat_at;not null"`
	LastError         string    `gorm:"column:last_error;type:text"`
	ReconnectAttempt  int       `gorm:"column:reconnect_attempt;not null;default:0"`
	ReconnectInMs     int       `gorm:"column:reconnect_in_ms;not null;default:0"`
	GatewayInstanceID string    `gorm:"column:gateway_instance_id;size:128"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (ChannelRuntimeStatus) TableName() string {
	return "channel_runtime_status"
}
