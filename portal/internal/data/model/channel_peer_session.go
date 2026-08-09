package model

import "time"

// ChannelPeerSession maps channel_id + peer_id to a Portal chat session.
type ChannelPeerSession struct {
	ChannelID string `gorm:"column:channel_id;primaryKey;size:64"`
	PeerID    string `gorm:"column:peer_id;primaryKey;size:128"`
	SessionID string `gorm:"column:session_id;size:36;not null;index"`
	AgentID   string `gorm:"column:agent_id;size:36;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ChannelPeerSession) TableName() string {
	return "channel_peer_sessions"
}
