# [MW-B4] Logging 改用 slog,err 安全转义

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md B4](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 与 [DS-A3](DS-A3-dsn-and-log-leak.md) / [EX-C1](EX-C1-log-printf-replace.md) 同一 PR 提交 |

## 问题位置

- `framework/middleware/logging.go`
- `framework/middleware/recovery.go`(也手拼 JSON)
- `framework/middleware/debug.go`(也手拼 JSON)

## 现状

```go
// logging.go
log.Printf(`{"level":"info","msg":"agent_request","elapsed_ms":%d,"error":"%v"}`, elapsed.Milliseconds(), err)

// recovery.go
log.Printf(`{"level":"error","msg":"agent_panic","panic":"%v"}`, r)

// debug.go
log.Printf(`{"level":"debug","msg":"request","request_id":%q,"message_count":%d,"content_length":%d}`, ...)
```

## 问题分析

1. **err 含特殊字符直接产出非法 JSON**: `err.Error()` 若含 `"`、`\`、`\n`、控制字符,生成的 JSON 解析失败,日志 pipeline 打不进去
2. **不是真正的结构化日志**: 缺 request_id、user_id、agent_name、status 等关键字段,排查事故时拉不出来
3. **不经过项目 logger**: `obs/` 包据观察有 logger,metrics/tracing 都用了,但 logging/recovery/debug 没用 → 日志风格半新半旧
4. **缺级别控制**: `log.Printf` 全部走 stderr,生产无法降噪

## 改进方案

```go
// framework/middleware/logging.go (改写)
package middleware

import (
    "context"
    "log/slog"
    "time"

    "github.com/sixath/framework/agent"
)

// LoggingMiddleware 用结构化 logger 输出请求日志
func LoggingMiddleware(logger *slog.Logger) Middleware {
    if logger == nil { logger = slog.Default() }
    return func(next Handler) Handler {
        return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
            start := time.Now()
            resp, err := next(ctx, req)
            elapsed := time.Since(start)

            attrs := []any{
                slog.Int64("elapsed_ms", elapsed.Milliseconds()),
            }
            if req != nil {
                if req.RequestID != "" { attrs = append(attrs, slog.String("request_id", req.RequestID)) }
                if v, ok := req.Metadata["agent_name"].(string); ok && v != "" {
                    attrs = append(attrs, slog.String("agent", v))
                }
                if v, ok := req.Metadata["user_id"].(string); ok && v != "" {
                    attrs = append(attrs, slog.String("user_id", v))
                }
                attrs = append(attrs, slog.Int("messages", len(req.Messages)))
            }
            if err != nil {
                attrs = append(attrs, slog.Any("error", err))   // slog 安全转义
                logger.LogAttrs(ctx, slog.LevelError, "agent.request", argsToAttrs(attrs)...)
            } else {
                logger.LogAttrs(ctx, slog.LevelInfo, "agent.request", argsToAttrs(attrs)...)
            }
            return resp, err
        }
    }
}
```

注意:由于 `LoggingMiddleware` 的签名要变化(从 `func(Handler) Handler` 改为 `func(*slog.Logger) Middleware`),为保持向后兼容,**保留旧无参签名**:

```go
// 旧签名,使用 slog.Default()
func LoggingMiddleware(next Handler) Handler { ... }

// 新签名(推荐)
func LoggingMiddlewareWithLogger(logger *slog.Logger) Middleware { ... }
```

`recovery.go` / `debug.go` 同模式改造。

## 验收标准

- [ ] 三个 middleware(logging / recovery / debug)全部使用 `*slog.Logger` 输出
- [ ] err 含特殊字符时日志仍是合法 JSON(用 `"` / `\n` / 中文等测试 payload)
- [ ] 字段化:`request_id` / `agent` / `user_id` / `messages` / `elapsed_ms` / `error` 都是独立 attribute,而非拼在 message 里
- [ ] 旧签名 `LoggingMiddleware(next Handler) Handler` 保留兼容,使用 `slog.Default()`
- [ ] CHANGELOG 记录新签名 `LoggingMiddlewareWithLogger`

## 测试要求

- `TestLoggingMiddleware_SafeEscape`: err 含 `"\\\n` 测试 payload,捕获 stderr,断言输出是合法 JSON(`json.Valid`)
- `TestLoggingMiddleware_StructuredFields`: 用 `slog.NewJSONHandler` + `bytes.Buffer`,断言 JSON 含期望字段
- `TestRecoveryMiddleware_PanicLogged`: 触发 panic,断言 logger 收到 error 级别 + panic 字段

## 风险

- 项目 Go 版本若 < 1.21 没有 `log/slog`,需要先升级。**已确认 framework go.mod 是 1.25.0,不是问题**
- `slog.Default()` 输出 handler 行为可能与现有 `log.Printf` 风格不同(默认是 text not JSON)。**Mitigation**: 在 framework 启动入口设置 `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))`,作为 framework 初始化的一部分

## 关联 issue

- [DS-A3](DS-A3-dsn-and-log-leak.md): datasource 层 logger,同一 PR
- [EX-C1](EX-C1-log-printf-replace.md): executor 层 logger(实际并入 DS-A3)
- 三个一起改完,framework 内 `log.Printf` 出现次数应降到 0(可在 CI 加 grep 校验)
