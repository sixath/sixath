package data

import (
	"time"

	"backend/internal/data/model"
)

// MemoryUnit persists a session-scoped memory unit.
type MemoryUnit struct {
	ID              string        `gorm:"column:id;primaryKey;size:36"`
	ScopeType       string        `gorm:"column:scope_type;size:16;not null;index:idx_mu_scope,priority:1"`
	ScopeID         string        `gorm:"column:scope_id;size:36;not null;index:idx_mu_scope,priority:2"`
	AgentID         *string       `gorm:"column:agent_id;size:36"`
	UserID          *string       `gorm:"column:user_id;size:36;index:idx_mu_user,priority:1"`
	Content         string        `gorm:"column:content;type:text;not null"`
	Kind            string        `gorm:"column:kind;size:32;not null;default:fact"`
	ContentHash     string        `gorm:"column:content_hash;size:64;not null;index:idx_mu_hash"`
	// MySQL rejects DEFAULT on TEXT/BLOB/JSON; use varchar (migration may use ENUM).
	Status          string        `gorm:"column:status;size:32;not null;default:active;index:idx_mu_scope,priority:3"`
	SupersedesID    *string       `gorm:"column:supersedes_id;size:36"`
	SourceSessionID *string       `gorm:"column:source_session_id;size:36;index:idx_mu_session"`
	Metadata        model.JSONMap `gorm:"column:metadata;type:json"`
	CreatedAt       time.Time     `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time     `gorm:"column:updated_at;not null"`
}

func (MemoryUnit) TableName() string {
	return "memory_units"
}
