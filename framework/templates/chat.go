package templates

import (
	"context"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
)

// NewChatAgentHandler 构建一个带中间件链的默认对话 Agent 处理器。
// 典型用法：在 HTTP/CLI 等入口中直接复用该处理器。
func NewChatAgentHandler(m model.Model, mem memory.Memory, mws ...middleware.Middleware) middleware.Handler {
	core := agent.NewChatAgent(m, mem)
	final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		return core.Run(ctx, req)
	}
	return middleware.Chain(final, mws...)
}
