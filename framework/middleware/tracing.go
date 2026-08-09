package middleware

import (
	"context"

	"github.com/sixath/framework/agent"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware 在每次 Agent 调用时创建一个新的 Span，便于调用链追踪。
// 需要在应用启动时先调用 obs.InitTracer 配置全局 TracerProvider。
func TracingMiddleware(next Handler) Handler {
	tracer := otel.Tracer("github.com/sixath/framework/agent")
	return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		agentName := "default"
		if req != nil {
			agentName = req.EffectiveAgentName()
		}

		ctx, span := tracer.Start(ctx, "Agent.Run/"+agentName, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		if req != nil {
			span.SetAttributes(
				attribute.String("agent.name", agentName),
				attribute.Int("agent.messages_count", len(req.Messages)),
			)
			if req.RequestID != "" {
				span.SetAttributes(attribute.String("agent.request_id", req.RequestID))
			}
			if req.UserID != "" {
				span.SetAttributes(attribute.String("enduser.id", req.UserID))
			}
			if req.ModelName != "" {
				span.SetAttributes(attribute.String("agent.model", req.ModelName))
			}
		}

		resp, err := next(ctx, req)
		if ac := agent.ContextFrom(ctx); ac != nil {
			if ac.CacheHit {
				span.SetAttributes(attribute.Bool("agent.cache_hit", true))
			}
			if ac.BlockReason != "" {
				span.SetAttributes(attribute.String("agent.block_reason", ac.BlockReason))
			}
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return resp, err
		}

		span.SetStatus(codes.Ok, "")
		if resp != nil {
			span.SetAttributes(attribute.Int("agent.response_length", len(resp.Text)))
			resp.FillUsageFromMetadata()
			if resp.Usage.InputTokens > 0 {
				span.SetAttributes(attribute.Int64("agent.token.input", resp.Usage.InputTokens))
			}
			if resp.Usage.OutputTokens > 0 {
				span.SetAttributes(attribute.Int64("agent.token.output", resp.Usage.OutputTokens))
			}
		}
		return resp, err
	}
}
