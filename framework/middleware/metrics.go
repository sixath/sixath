package middleware

import (
	"context"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/internal/anyx"
	"github.com/sixath/framework/obs"
)

// observeTokenUsage 是 obs.ObserveTokenUsage 的间接引用,便于测试注入。
var observeTokenUsage = obs.ObserveTokenUsage

// observeAgentRequest 是 obs.ObserveAgentRequest 的间接引用,便于测试注入。
var observeAgentRequest = obs.ObserveAgentRequest

// MetricsMiddleware 使用 obs 中的 Prometheus 指标记录请求次数与耗时。
// agent 名称通过 Metadata["agent_name"] 传递，默认为 "default"。
func MetricsMiddleware(next Handler) Handler {
	return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		elapsed := time.Since(start)

		agentName := "default"
		if req != nil {
			agentName = req.EffectiveAgentName()
		}
		ac := agent.ContextFrom(ctx)
		status := agent.RequestSource(ac, err)
		observeAgentRequest(agentName, status, elapsed)
		if resp != nil {
			resp.FillUsageFromMetadata()
			if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
				observeTokenUsage(agentName, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens))
			} else if resp.Metadata != nil {
				in, hasIn := anyx.Int64FromAny(resp.Metadata[agent.MetaTokenInput])
				out, hasOut := anyx.Int64FromAny(resp.Metadata[agent.MetaTokenOutput])
				if hasIn || hasOut {
					observeTokenUsage(agentName, int(in), int(out))
				}
			}
		}
		return resp, err
	}
}
