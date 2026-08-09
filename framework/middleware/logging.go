package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/sixath/framework/agent"
)

func requestAttrs(req *agent.Request) []slog.Attr {
	if req == nil {
		return nil
	}
	attrs := []slog.Attr{slog.Int("messages", len(req.Messages))}
	if req.RequestID != "" {
		attrs = append(attrs, slog.String("request_id", req.RequestID))
	}
	req.Normalize()
	if req.AgentName != "" {
		attrs = append(attrs, slog.String("agent", req.AgentName))
	}
	if req.UserID != "" {
		attrs = append(attrs, slog.String("user_id", req.UserID))
	}
	return attrs
}

func loggingHandler(logger *slog.Logger, next Handler) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		attrs := requestAttrs(req)
		attrs = append(attrs, slog.Int64("elapsed_ms", time.Since(start).Milliseconds()))
		if err != nil {
			attrs = append(attrs, slog.Any("error", err), slog.String("status", "error"))
			logger.LogAttrs(ctx, slog.LevelError, "agent.request", attrs...)
		} else {
			attrs = append(attrs, slog.String("status", "ok"))
			logger.LogAttrs(ctx, slog.LevelInfo, "agent.request", attrs...)
		}
		return resp, err
	}
}

// LoggingMiddleware 使用 slog.Default() 输出结构化请求日志。
func LoggingMiddleware(next Handler) Handler {
	return loggingHandler(slog.Default(), next)
}

// LoggingMiddlewareWithLogger 使用指定 logger。
func LoggingMiddlewareWithLogger(logger *slog.Logger) Middleware {
	return func(next Handler) Handler {
		return loggingHandler(logger, next)
	}
}
