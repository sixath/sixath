package model

import "time"

// GrowthCuratorState 记录每个 workspace 上次 Curator 清扫时间（R2b）。
type GrowthCuratorState struct {
	WorkspaceKey  string     `gorm:"column:workspace_key;primaryKey;size:384"`
	LastCuratorAt *time.Time `gorm:"column:last_curator_at"`
	LastError     string     `gorm:"column:last_error;type:text"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null"`
}

func (GrowthCuratorState) TableName() string {
	return "growth_curator_states"
}
