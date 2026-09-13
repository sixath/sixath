// Package observability 为入站网关提供：结构化日志（log/slog）、request_id
// 贯通、以及 W3C traceparent 透传。
//
// 为什么需要 traceparent：Portal 的 HTTP 中间件链已挂
// `tracing.Server(..., propagation.TraceContext{})`
// （portal/internal/server/http.go:51-58）。网关只要把入站 traceparent 继续透传给
// Portal，整条链路（网关 → Portal runtime → framework）就落在同一个 trace 上，
// 于是"慢在最外面还是模型那一段"可以直接从 trace 回答，而不是靠猜。
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sixath/gateway/internal/metrics"
)

const (
	// HeaderRequestID 是贯通网关与 Portal 日志的请求标识。
	HeaderRequestID = "X-Request-Id"
	// HeaderTraceparent 是 W3C Trace Context 头。
	HeaderTraceparent = "traceparent"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	traceparentKey
)

// SetupLogger 配置 slog 默认 logger，并返回它。
// SATH_LOG_FORMAT=json|text（默认 json），SATH_LOG_LEVEL=debug|info|warn|error（默认 info）。
func SetupLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SATH_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SATH_LOG_FORMAT")), "text") {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger
}

// WithRequestID 把 request_id 绑定进 context。
func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom 读取 context 中的 request_id（可能为空）。
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithTraceparent 把 traceparent 绑定进 context。
func WithTraceparent(ctx context.Context, tp string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tp == "" {
		return ctx
	}
	return context.WithValue(ctx, traceparentKey, tp)
}

// TraceparentFrom 读取 context 中的 traceparent（可能为空）。
func TraceparentFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tp, _ := ctx.Value(traceparentKey).(string)
	return tp
}

// Detach 返回一个不继承 parent 取消、但保留其 request_id 与 traceparent 的后台 ctx。
// 用于异步续跑（如 webhook async turn / 幂等重复投递）：goroutine 生命周期独立于
// 入站请求，但仍需贯通 trace 与日志，而不是丢失成 context.Background()。
func Detach(parent context.Context) context.Context {
	ctx := context.Background()
	if id := RequestIDFrom(parent); id != "" {
		ctx = WithRequestID(ctx, id)
	}
	if tp := TraceparentFrom(parent); tp != "" {
		ctx = WithTraceparent(ctx, tp)
	}
	return ctx
}

// Logger 返回带 request_id 字段的 logger，便于日志与 inbound 事件对齐。
func Logger(ctx context.Context) *slog.Logger {
	l := slog.Default()
	if id := RequestIDFrom(ctx); id != "" {
		l = l.With("request_id", id)
	}
	if tp := TraceparentFrom(ctx); tp != "" {
		if traceID := traceIDFromTraceparent(tp); traceID != "" {
			l = l.With("trace_id", traceID)
		}
	}
	return l
}

// Middleware 为入站请求建立 request_id 与 traceparent，并把它们写入 context 与响应头。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		requestID := strings.TrimSpace(r.Header.Get(HeaderRequestID))
		if requestID == "" {
			requestID = randomHex(8)
		}
		ctx = WithRequestID(ctx, requestID)

		// 继承上游 trace id（若有），但为网关自己生成新的 span id，
		// 与真正的 tracer 语义一致：同一条 trace，新的 span。
		traceparent := OutgoingTraceparent(r.Header.Get(HeaderTraceparent))
		ctx = WithTraceparent(ctx, traceparent)

		w.Header().Set(HeaderRequestID, requestID)
		w.Header().Set(HeaderTraceparent, traceparent)

		start := time.Now()
		rec := NewResponseRecorder(w)
		next.ServeHTTP(rec, r.WithContext(ctx))

		metrics.ObserveHTTP(r.Method, rec.Status(), time.Since(start))
		Logger(ctx).Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// ResponseRecorder 捕获响应状态码，供 handler 在 defer 里上报指标。
type ResponseRecorder struct {
	http.ResponseWriter
	status int
}

// NewResponseRecorder 包装 w；默认状态码 200（未调用 WriteHeader 时）。
func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{ResponseWriter: w, status: http.StatusOK}
}

// Status 返回当前已写入的状态码。
func (r *ResponseRecorder) Status() int { return r.status }

func (r *ResponseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 保留 SSE 所需的 flush 能力（/runtime 流式代理会用到）。
func (r *ResponseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// OutgoingTraceparent 基于入站 traceparent 生成本次调用的 traceparent：
// 复用其 trace-id，生成新的 span-id；入站缺失或非法时新建整条 trace。
func OutgoingTraceparent(incoming string) string {
	traceID := traceIDFromTraceparent(incoming)
	if traceID == "" {
		traceID = randomHex(16) // 32 hex chars
	}
	return "00-" + traceID + "-" + randomHex(8) + "-01"
}

// traceIDFromTraceparent 校验并取出 trace-id；非法输入返回空串。
func traceIDFromTraceparent(tp string) string {
	tp = strings.TrimSpace(tp)
	if tp == "" {
		return ""
	}
	parts := strings.Split(tp, "-")
	if len(parts) < 4 {
		return ""
	}
	traceID := parts[1]
	if len(traceID) != 32 || !isHex(traceID) {
		return ""
	}
	if strings.Trim(traceID, "0") == "" {
		return "" // 全零是非法 trace-id
	}
	if len(parts[2]) != 16 || !isHex(parts[2]) {
		return ""
	}
	return strings.ToLower(traceID)
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 退化路径：时间戳十六进制，仍保证非全零
		return strings.TrimLeft(hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano))), "0")[:n*2]
	}
	return hex.EncodeToString(b)
}