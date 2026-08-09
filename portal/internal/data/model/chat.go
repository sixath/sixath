package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ChatSession 会话表
type ChatSession struct {
	ID              string    `gorm:"column:id;primaryKey;size:36"`
	AgentID         string    `gorm:"column:agent_id;size:36;not null;index"`
	UserID          string    `gorm:"column:user_id;size:36;not null;default:'';index"`
	ParentSessionID string    `gorm:"column:parent_session_id;size:36;index"`
	Title           string    `gorm:"column:title;size:256;not null"`
	// RewindCount increments on each successful Rewind (Phase 2).
	RewindCount int `gorm:"column:rewind_count;not null;default:0"`
	// Readonly sessions reject new user messages (archive after L2 fork).
	Readonly  bool      `gorm:"column:readonly;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (ChatSession) TableName() string {
	return "chat_sessions"
}

// JSONMap stores arbitrary JSON object as a MySQL JSON column.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("JSONMap: unsupported Scan source")
	}
	if len(b) == 0 {
		*m = nil
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// ChatMessage 消息表
type ChatMessage struct {
	ID        string  `gorm:"column:id;primaryKey;size:36"`
	SessionID string  `gorm:"column:session_id;size:36;not null;index"`
	Role      string  `gorm:"column:role;size:16;not null"` // user, assistant, system
	Content   string  `gorm:"column:content;type:text;not null"`
	Metadata  JSONMap `gorm:"column:metadata;type:json"`
	// Active=false hides the message from ListMessages after Rewind (soft-hide).
	Active    bool      `gorm:"column:active;not null;default:1;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}
