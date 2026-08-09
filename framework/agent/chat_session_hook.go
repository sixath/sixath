package agent

import (
	"context"
	"errors"
	"sync"
)

// ChatSessionHook ChatSession 结束时回调（规格 §3.1；非 AgentRun、非 BrowserSession）。
type ChatSessionHook interface {
	OnChatSessionEnd(ctx context.Context, sessionID string) error
}

type ChatSessionHookFunc func(ctx context.Context, sessionID string) error

func (f ChatSessionHookFunc) OnChatSessionEnd(ctx context.Context, sessionID string) error {
	return f(ctx, sessionID)
}

type ChatSessionHookRegistry struct {
	mu    sync.Mutex
	hooks []ChatSessionHook
}

func NewChatSessionHookRegistry() *ChatSessionHookRegistry {
	return &ChatSessionHookRegistry{}
}

func (r *ChatSessionHookRegistry) Register(h ChatSessionHook) {
	if r == nil || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, h)
}

// OnChatSessionEnd 依次调用；单 hook 失败不阻止后续；返回 errors.Join。
func (r *ChatSessionHookRegistry) OnChatSessionEnd(ctx context.Context, sessionID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	hooks := append([]ChatSessionHook(nil), r.hooks...)
	r.mu.Unlock()
	var errs []error
	for _, h := range hooks {
		if h == nil {
			continue
		}
		if err := h.OnChatSessionEnd(ctx, sessionID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
