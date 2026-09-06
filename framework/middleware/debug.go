package middleware

import (
	"context"
	"log/slog"
	runtimeDebug "runtime/debug"

	agent "github.com/sixath/framework/harness"
)

func debugHandler(logger *slog.Logger, enabled bool, next Handler) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
		if !enabled {
			return next(ctx, req)
		}
		msgCount := 0
		totalLen := 0
		reqID := ""
		if req != nil {
			reqID = req.RequestID
			msgCount = len(req.Messages)
			for _, m := range req.Messages {
				totalLen += len(m.Content)
				for _, p := range m.Parts {
					totalLen += len(p.Text) + len(p.URL)
				}
			}
		}
		logger.LogAttrs(ctx, slog.LevelDebug, "agent.debug.request",
			slog.String("request_id", reqID),
			slog.Int("message_count", msgCount),
			slog.Int("content_length", totalLen),
		)

		resp, err := next(ctx, req)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelDebug, "agent.debug.error",
				slog.String("request_id", reqID),
				slog.Any("error", err),
				slog.String("stack", string(runtimeDebug.Stack())),
			)
			return resp, err
		}
		replyLen := 0
		if resp != nil {
			replyLen = len(resp.Text)
		}
		logger.LogAttrs(ctx, slog.LevelDebug, "agent.debug.response",
			slog.String("request_id", reqID),
			slog.Int("reply_length", replyLen),
		)
		return resp, err
	}
}

// DebugMiddleware 在 enabled 为 true 时输出脱敏的请求/响应摘要，并在发生错误时输出调用栈。
func DebugMiddleware(enabled bool) Middleware {
	return func(next Handler) Handler {
		return debugHandler(slog.Default(), enabled, next)
	}
}

// DebugMiddlewareWithLogger 使用指定 logger。
func DebugMiddlewareWithLogger(logger *slog.Logger, enabled bool) Middleware {
	return func(next Handler) Handler {
		return debugHandler(logger, enabled, next)
	}
}
