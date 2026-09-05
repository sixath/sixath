package biz

import (
	"context"
	"time"
)

// ModelConfig for LLM
type ModelConfig struct {
	Provider        string
	Model           string
	APIKey          string
	BaseURL         string
	MaxOutputTokens int // 单次回复 max_tokens；<=0 使用 Portal 默认
	// Code* 已退役的切模字段（FamilyCode 不再激活）；proto/DB 死键，不 regen。
	CodeProvider string
	CodeModel    string
	CodeAPIKey   string
	CodeBaseURL  string
}

// AgentMeta represents an agent entity
type AgentMeta struct {
	ID             string
	Name           string
	Description    string
	SystemPrompt   string
	ModelConfig    ModelConfig
	Workspace      string
	DebugRun       bool
	WecomChannelID string
	RuntimeTools   RuntimeToolsConfig
	ToolIDs        []string
	McpServerIDs   []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AgentRepo interface for agent storage
type AgentRepo interface {
	Create(ctx context.Context, id, name, description, systemPrompt, workspace string, modelConfig ModelConfig, debugRun bool, wecomChannelID string, runtimeTools RuntimeToolsConfig, toolIDs []string) (*AgentMeta, error)
	CountByWecomChannelID(ctx context.Context, channelID string) (int, error)
	GetByID(ctx context.Context, id string) (*AgentMeta, error)
	GetByName(ctx context.Context, name string) (*AgentMeta, error)
	List(ctx context.Context, page, pageSize int32) ([]*AgentMeta, int, error)
	// ListByIDs lists agents whose id is in ids (ACL-visible set), ordered by created_at DESC.
	ListByIDs(ctx context.Context, ids []string, page, pageSize int32) ([]*AgentMeta, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*AgentMeta, error)
	Delete(ctx context.Context, id string) error
	BindTools(ctx context.Context, agentID string, toolIDs []string) error
	UnbindTools(ctx context.Context, agentID string, toolIDs []string) error
}
