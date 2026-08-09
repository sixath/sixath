package model

import "time"

// AgentMcpServer Agent-MCP Server 绑定表
type AgentMcpServer struct {
	AgentID   string    `gorm:"column:agent_id;primaryKey;size:36"`
	ServerID  string    `gorm:"column:server_id;primaryKey;size:36"`
	SortOrder int       `gorm:"column:sort_order;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (AgentMcpServer) TableName() string {
	return "agent_mcp_servers"
}
