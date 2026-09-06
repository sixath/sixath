package templates

import (
	"fmt"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
)

// NewChatAgentHandlerFromConfig 根据 Config 装配对话 Agent 与中间件链（B.2.3）。
// middlewareByName 为名称到中间件的映射，cfg.Middlewares 中列出的名称会按顺序加入链；
// 若某名称不存在则跳过。模型通过 model.NewFromIdentifier(cfg.ModelName) 创建。
func NewChatAgentHandlerFromConfig(cfg config.Config, middlewareByName map[string]middleware.Middleware) (middleware.Handler, error) {
	m, err := model.NewFromIdentifier(cfg.ModelName)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	mem := memory.NewBufferMemory(cfg.MaxHistory)
	mws := make([]middleware.Middleware, 0, len(cfg.Middlewares)+2)
	mws = append(mws, middleware.RecoveryMiddleware, middleware.LoggingMiddleware)
	for _, name := range cfg.Middlewares {
		if mw, ok := middlewareByName[name]; ok {
			mws = append(mws, mw)
		}
	}
	return NewChatAgentHandlerWithWorkspace(m, mem, cfg.Workspace, mws...), nil
}

// DefaultMiddlewareMap 返回内置中间件名称到实现的映射，供 FromConfig 使用。
func DefaultMiddlewareMap() map[string]middleware.Middleware {
	return map[string]middleware.Middleware{
		"logging":  middleware.LoggingMiddleware,
		"recovery": middleware.RecoveryMiddleware,
		"metrics":  middleware.MetricsMiddleware,
		"tracing":  middleware.TracingMiddleware,
		"debug":    middleware.DebugMiddleware(true),
	}
}
