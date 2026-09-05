package templates

import (
	"context"
	"strings"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
)

// NewChatAgentHandler 构建一个带中间件链的默认对话 Agent 处理器。
// 典型用法：在 HTTP/CLI 等入口中直接复用该处理器。
func NewChatAgentHandler(m model.Model, mem memory.Memory, mws ...middleware.Middleware) middleware.Handler {
	return NewChatAgentHandlerWithWorkspace(m, mem, "", mws...)
}

// NewChatAgentHandlerWithWorkspace 与 NewChatAgentHandler 相同，非空 workspace 交给 ChatAgent。
func NewChatAgentHandlerWithWorkspace(m model.Model, mem memory.Memory, workspace string, mws ...middleware.Middleware) middleware.Handler {
	var opts []agent.Option
	if ws := strings.TrimSpace(workspace); ws != "" {
		opts = append(opts, agent.WithChatWorkspace(ws))
	}
	core := agent.NewChatAgent(m, mem, opts...)
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return core.Run(ctx, req)
	}
	return middleware.Chain(final, mws...)
}
