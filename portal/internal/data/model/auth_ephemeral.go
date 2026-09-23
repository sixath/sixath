package model

import "time"

// AuthEphemeral holds one-time OAuth state / exchange tickets.
type AuthEphemeral struct {
	ID          string     `gorm:"column:id;primaryKey;size:64"`
	Kind        string     `gorm:"column:kind;size:32;not null;index"` // wecom_state | wecom_ticket
	PayloadJSON string     `gorm:"column:payload_json;type:text;not null"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;not null;index"`
	ConsumedAt  *time.Time `gorm:"column:consumed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
}

func (AuthEphemeral) TableName() string { return "auth_ephemeral" }
