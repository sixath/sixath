package model

import "time"

// ChannelIlinkPeer is the weixin_ilink roster; Allowed is the only source of truth for the peer allowlist.
type ChannelIlinkPeer struct {
	ChannelUUID    string     `gorm:"column:channel_uuid;primaryKey;size:36"`
	PeerID         string     `gorm:"column:peer_id;primaryKey;size:128"`
	Allowed        bool       `gorm:"column:allowed;not null;default:false"`
	Remark         string     `gorm:"column:remark;size:128"`
	ContextToken   string     `gorm:"column:context_token;size:512"`
	ContextTokenAt *time.Time `gorm:"column:context_token_at"`
	LastInboundAt  *time.Time `gorm:"column:last_inbound_at"`
	LastPreview    string     `gorm:"column:last_preview;size:160"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null"`
}

func (ChannelIlinkPeer) TableName() string {
	return "channel_ilink_peers"
}

// ChannelIlinkState holds the getupdates cursor; Generation must match channels.ilink_generation.
type ChannelIlinkState struct {
	ChannelUUID   string    `gorm:"column:channel_uuid;primaryKey;size:36"`
	Generation    int       `gorm:"column:generation;not null;default:0"`
	GetUpdatesBuf string    `gorm:"column:get_updates_buf;type:text"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null"`
}

func (ChannelIlinkState) TableName() string {
	return "channel_ilink_state"
}
