package middleware

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/errs"
)

func recoveryHandler(logger *slog.Logger, next Handler) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req *agent.Request) (resp *agent.Response, err error) {
		defer func() {
			if r := recover(); r != nil {
				attrs := requestAttrs(req)
				attrs = append(attrs, slog.Any("panic", r), slog.String("status", "panic"))
				logger.LogAttrs(ctx, slog.LevelError, "agent.panic", attrs...)
				err = fmt.Errorf("%w: %v", errs.ErrInternal, r)
			}
		}()
		return next(ctx, req)
	}
}

// RecoveryMiddleware 捕获 panic，避免进程崩溃。
func RecoveryMiddleware(next Handler) Handler {
	return recoveryHandler(slog.Default(), next)
}

// RecoveryMiddlewareWithLogger 使用指定 logger 记录 panic。
func RecoveryMiddlewareWithLogger(logger *slog.Logger) Middleware {
	return func(next Handler) Handler {
		return recoveryHandler(logger, next)
	}
}
