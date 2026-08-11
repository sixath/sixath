package biz

import (
	"context"
	"time"
)

// ChannelCreate 创建渠道参数
type ChannelCreate struct {
	ChannelID     string
	Type          string // web, api, webhook, wxpusher, wecom
	DefaultAgent  string
	AllowedAgents []string
	AutoRouteEnabled    bool
	AutoRouteMention    bool
	AutoRouteClassifier bool
	Enabled       bool
	WebhookPath   string
	WebhookSecret string
	IPWhitelist   []string
	// WxPusher
	AppToken    string
	DefaultUids []string
	// WeCom
	WebhookURL string
}

// ChannelMeta 渠道元数据
type ChannelMeta struct {
	ID            string
	ChannelID     string
	Type          string
	DefaultAgent  string
	AllowedAgents []string
	AutoRouteEnabled    bool
	AutoRouteMention    bool
	AutoRouteClassifier bool
	Enabled       bool
	WebhookPath   string
	WebhookSecret string
	IPWhitelist   []string
	AppToken      string
	DefaultUids   []string
	WebhookURL    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ChannelRepo 渠道存储接口
type ChannelRepo interface {
	Create(ctx context.Context, ch *ChannelCreate) (*ChannelMeta, error)
	GetByID(ctx context.Context, id string) (*ChannelMeta, error)
	GetByChannelID(ctx context.Context, channelID string) (*ChannelMeta, error)
	GetWecomByDefaultAgent(ctx context.Context, agentID string) (*ChannelMeta, error)
	List(ctx context.Context, page, pageSize int32, typ string, enabled *bool) ([]*ChannelMeta, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*ChannelMeta, error)
	Delete(ctx context.Context, id string) error
}
