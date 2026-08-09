package middleware

import (
	"context"
	"time"

	"github.com/sixath/framework/agent"
)

// AgentContextMiddleware 为每次请求注入 agent.AgentContext 并同步 Request 高频字段。
func AgentContextMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			ctx, ac := agent.EnsureContext(ctx)
			if req != nil {
				req.Normalize()
				if ac != nil {
					if ac.StartTime.IsZero() {
						ac.StartTime = time.Now()
					}
					ac.AgentName = req.EffectiveAgentName()
					ac.UserID = req.UserID
					ac.ModelName = req.ModelName
				}
			} else if ac != nil && ac.StartTime.IsZero() {
				ac.StartTime = time.Now()
			}
			return next(ctx, req)
		}
	}
}
