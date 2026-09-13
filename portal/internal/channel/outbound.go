package channel

import (
	"context"
	"fmt"
)

// OutboundMessage 出站消息（渠道无关的通用形态）。
type OutboundMessage struct {
	Content    string   // 必填正文
	MsgType    string   // 可选：text | markdown（wecom 用，其他渠道忽略）
	Summary    string   // 可选：通知栏摘要（wxpusher 用）
	Recipients []string // 可选：接收者（wxpusher uids；为空时用渠道默认接收者）
}

// DeliveryReceipt 出站投递回执。
type DeliveryReceipt struct {
	Status    string // sent | skipped | failed
	Attempts  int
	LastError string
}

// OutboundChannel 出站渠道抽象：统一 wecom / wxpusher / 未来渠道的发送入口。
type OutboundChannel interface {
	// Type 返回渠道类型标识（wecom | wxpusher | ...）。
	Type() string
	// Send 发送消息。
	// 返回 error 表示「配置缺失」或「发送失败」；空正文返回 skipped（nil error）。
	Send(ctx context.Context, msg OutboundMessage) (DeliveryReceipt, error)
}

// ConfigError 表示渠道配置缺失/非法，调用方可将其区别于发送失败（例如映射为参数错误）。
type ConfigError struct{ msg string }

func (e *ConfigError) Error() string {
	if e == nil {
		return "outbound channel config error"
	}
	return e.msg
}

func newConfigError(format string, args ...any) *ConfigError {
	return &ConfigError{msg: fmt.Sprintf(format, args...)}
}

// OutboundConfig 构造出站渠道所需的最小配置（由 model/biz.Channel 映射而来）。
type OutboundConfig struct {
	Type        string
	WebhookURL  string   // wecom
	AppToken    string   // wxpusher
	DefaultUids []string // wxpusher 默认接收者
}

// OutboundFactory 按配置构造出站渠道。
type OutboundFactory func(cfg OutboundConfig) (OutboundChannel, error)

// OutboundRegistry 出站渠道注册表：按类型注册工厂，便于扩展新渠道。
type OutboundRegistry struct {
	factories map[string]OutboundFactory
}

// NewOutboundRegistry 构造内置 wecom + wxpusher 的注册表。
func NewOutboundRegistry() *OutboundRegistry {
	r := &OutboundRegistry{factories: make(map[string]OutboundFactory)}
	r.Register("wecom", func(cfg OutboundConfig) (OutboundChannel, error) {
		return NewWeComOutbound(cfg.WebhookURL), nil
	})
	r.Register("wxpusher", func(cfg OutboundConfig) (OutboundChannel, error) {
		return NewWxPusherOutbound(cfg.AppToken, cfg.DefaultUids), nil
	})
	return r
}

// Register 注册一个渠道工厂（覆盖同名）。
func (r *OutboundRegistry) Register(typ string, f OutboundFactory) {
	r.factories[typ] = f
}

// New 按配置构造出站渠道。
func (r *OutboundRegistry) New(cfg OutboundConfig) (OutboundChannel, error) {
	f, ok := r.factories[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported outbound channel type %q", cfg.Type)
	}
	return f(cfg)
}

var defaultRegistry = NewOutboundRegistry()

// NewOutbound 用内置注册表构造出站渠道。
func NewOutbound(cfg OutboundConfig) (OutboundChannel, error) {
	return defaultRegistry.New(cfg)
}
