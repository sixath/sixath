package agent

import (
	"context"
	"sync"
	"time"
)

type agentContextKey struct{}

// AgentContext 为单次 Agent 请求在中间件间共享的可变状态（通过 context 传递）。
type AgentContext struct {
	StartTime   time.Time
	AgentName   string
	UserID      string
	ModelName   string
	CacheHit    bool
	BlockReason string

	mu     sync.Mutex
	extras map[string]any
}

// Extra 读写扩展字段。
func (ac *AgentContext) Extra(key string) (any, bool) {
	if ac == nil {
		return nil, false
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.extras == nil {
		return nil, false
	}
	v, ok := ac.extras[key]
	return v, ok
}

func (ac *AgentContext) SetExtra(key string, val any) {
	if ac == nil || key == "" {
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.extras == nil {
		ac.extras = make(map[string]any)
	}
	ac.extras[key] = val
}

// ContextFrom 从 ctx 取出 AgentContext；不存在时返回 nil（nil-safe）。
func ContextFrom(ctx context.Context) *AgentContext {
	if ctx == nil {
		return nil
	}
	ac, _ := ctx.Value(agentContextKey{}).(*AgentContext)
	return ac
}

// WithAgentContext 将 ac 绑定到 ctx。
func WithAgentContext(ctx context.Context, ac *AgentContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, agentContextKey{}, ac)
}

// EnsureContext 保证 ctx 上存在 AgentContext 并返回。
func EnsureContext(ctx context.Context) (context.Context, *AgentContext) {
	if ac := ContextFrom(ctx); ac != nil {
		return ctx, ac
	}
	ac := &AgentContext{StartTime: time.Now()}
	return WithAgentContext(ctx, ac), ac
}

// RequestSource 根据 AgentContext 推导 metrics/tracing 用的来源标签。
func RequestSource(ac *AgentContext, err error) string {
	if ac != nil && ac.BlockReason != "" {
		return "blocked"
	}
	if ac != nil && ac.CacheHit {
		return "cache"
	}
	if err != nil {
		return "error"
	}
	return "model"
}
