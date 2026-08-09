package middleware

import (
	"context"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"go.opentelemetry.io/otel/trace"
)

// TraceparentMiddleware 是一个中间件，用于将 traceparent 添加到响应头中
func TraceparentMiddleware() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// 从上下文中获取当前的 Span
			span := trace.SpanFromContext(ctx)
			if span != nil {
				// 获取 traceparent 信息
				sc := span.SpanContext()
				traceparent := "00-" + sc.TraceID().String() + "-" + sc.SpanID().String() + "-01"

				// 从上下文中获取 http.ResponseWriter
				if tr, ok := transport.FromServerContext(ctx); ok {
					tr.ReplyHeader().Set("traceparent", traceparent)
				}
			}

			// 调用下一个中间件或最终处理器
			return handler(ctx, req)
		}
	}
}
