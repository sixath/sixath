package templates

import (
	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/middleware"
	"github.com/sixath/framework/model"
)

// NewChatStreamHandler 构建带 StreamChain 的流式对话处理器（供 portal SSE 等接入）。
func NewChatStreamHandler(m model.Model, mem memory.Memory, mws ...middleware.StreamMiddleware) middleware.StreamHandler {
	core := agent.NewChatAgent(m, mem)
	base := middleware.StringStreamAdapter(core.RunStream)
	return middleware.StreamChain(base, mws...)
}

// NewChatAgentHandlerWithContext 与非流式相同，但显式文档化 AgentContext 已由 Chain 注入。
func NewChatAgentHandlerWithContext(m model.Model, mem memory.Memory, mws ...middleware.Middleware) middleware.Handler {
	return NewChatAgentHandler(m, mem, mws...)
}
