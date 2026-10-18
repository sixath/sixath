package biz

import (
	"context"
	"time"
)

// ChannelCreate 创建渠道参数
type ChannelCreate struct {
	ChannelID           string
	Type                string // web, api, webhook, wxpusher, wecom, wecom_bot
	DefaultAgent        string
	AllowedAgents       []string
	AutoRouteEnabled    bool
	AutoRouteMention    bool
	AutoRouteClassifier bool
	Enabled             bool
	WebhookPath         string
	WebhookSecret       string
	IPWhitelist         []string
	// WxPusher
	AppToken    string
	DefaultUids []string
	// WeCom
	WebhookURL string
	// WeCom Bot / webhook gateway
	BotID            string
	BotSecret        string
	BotNames         []string
	WSURL            string
	CorpID           string
	CorpSecret       string
	DefaultReplyMode string // async|sync（webhook）
}

// ChannelMeta 渠道元数据
type ChannelMeta struct {
	ID                  string
	ChannelID           string
	Type                string
	DefaultAgent        string
	AllowedAgents       []string
	AutoRouteEnabled    bool
	AutoRouteMention    bool
	AutoRouteClassifier bool
	Enabled             bool
	WebhookPath         string
	WebhookSecret       string
	IPWhitelist         []string
	AppToken            string
	DefaultUids         []string
	WebhookURL          string
	// WeCom Bot / webhook gateway
	BotID            string
	BotSecret        string
	BotNames         []string
	WSURL            string
	CorpID           string
	CorpSecret       string
	DefaultReplyMode string
	// 微信 iLink（type=weixin_ilink）
	IlinkBotToken         string
	IlinkBotID            string
	IlinkOwnerUserID      string
	IlinkBaseURL          string
	IlinkGeneration       int
	IlinkSessionExpiredAt *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ChannelRepo 渠道存储接口
type ChannelRepo interface {
	Create(ctx context.Context, ch *ChannelCreate) (*ChannelMeta, error)
	GetByID(ctx context.Context, id string) (*ChannelMeta, error)
	GetByChannelID(ctx context.Context, channelID string) (*ChannelMeta, error)
	GetWecomByDefaultAgent(ctx context.Context, agentID string) (*ChannelMeta, error)
	GetOutboundByDefaultAgent(ctx context.Context, agentID string) (*ChannelMeta, error)
	List(ctx context.Context, page, pageSize int32, typ string, enabled *bool) ([]*ChannelMeta, int, error)
	ListGatewayChannels(ctx context.Context) ([]*ChannelMeta, error)
	// ListIlinkByAgent lists enabled weixin_ilink channels whose default or allowed agents include agentID.
	ListIlinkByAgent(ctx context.Context, agentID string) ([]*ChannelMeta, error)
	Update(ctx context.Context, id string, updates map[string]any) (*ChannelMeta, error)
	Delete(ctx context.Context, id string) error
}
