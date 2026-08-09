package model

import "time"

// AgentTool Agent-工具绑定表
type AgentTool struct {
	AgentID   string    `gorm:"column:agent_id;primaryKey;size:36"`
	ToolID    string    `gorm:"column:tool_id;primaryKey;size:36"`
	SortOrder int       `gorm:"column:sort_order;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (AgentTool) TableName() string {
	return "agent_tools"
}
