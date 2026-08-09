# [MW-B1+D1] 引入 agent.Context 作为 per-request 共享通道,支持 short-circuit

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/agent、framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md B1 / D1](../03-middleware.md) |
| **预估工作量** | 1-2 周 |
| **依赖** | 无;但是 [MW-D2](MW-D2-streaming-middleware.md) 的前置 |

## 问题位置

- `framework/agent/types.go: Request` / `Response`
- `framework/middleware/cache.go`(命中时外层无法识别)
- `framework/middleware/content_safety.go`(短路只能靠 err)

## 现状

中间件之间想传递信息只能塞 `ctx.Value` 或 `req.Metadata`,两者都是 `any` 类型,缺类型安全。Cache 命中时外层 metrics / logging 把它当成"ms=0 的真实请求",导致**统计错误归因**。

## 改进方案(分两块)

### A. `agent.Context` 共享通道

```go
// framework/agent/context.go (新)
type AgentContext struct {
    // 中间件可以读写
    StartTime    time.Time
    AgentName    string
    UserID       string
    ModelName    string
    CacheHit     bool
    BlockReason  string  // ContentSafety 拦截时填充
    // 扩展点
    extras map[string]any
}

func ContextFrom(ctx context.Context) *AgentContext { ... }
func WithAgentContext(ctx context.Context, ac *AgentContext) context.Context { ... }
```

中间件用法:
```go
func MetricsMiddleware(next Handler) Handler {
    return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
        ac := agent.ContextFrom(ctx)
        resp, err := next(ctx, req)
        source := "model"
        if ac.CacheHit { source = "cache" }
        if ac.BlockReason != "" { source = "blocked" }
        obs.ObserveAgentRequest(ac.AgentName, source, time.Since(ac.StartTime))
        return resp, err
    }
}
```

### B. Short-circuit 显式标记

```go
func CacheMiddleware(store *CacheStore) Middleware {
    return func(next Handler) Handler {
        return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
            if cached, ok := store.Get(...); ok {
                agent.ContextFrom(ctx).CacheHit = true
                return cached, nil
            }
            ...
        }
    }
}
```

ContentSafety 同理:
```go
if err := filter.CheckInput(...); err != nil {
    agent.ContextFrom(ctx).BlockReason = "input_filter"
    return nil, errs.ErrContentBlocked
}
```

## 验收标准

- [ ] `agent.AgentContext` 类型 + helpers
- [ ] 在 `agent.Run` 入口自动 `WithAgentContext` 注入
- [ ] Cache / ContentSafety / Metrics / Logging 全部改用 `AgentContext`
- [ ] 监控面板能区分 source: model / cache / blocked
- [ ] 现有不感知 AgentContext 的中间件 0 改动(向后兼容)

## 测试要求

- `TestAgentContext_CacheHitSource`: 缓存命中时 metrics 上报 `source="cache"`
- `TestAgentContext_BlockedSource`: 内容拦截时 metrics 上报 `source="blocked"`
- `TestAgentContext_NotInheritedAcrossRequests`: 一个请求改了 AgentContext 不影响下个请求(go-routine 隔离)

## 风险

- 改 `Request` / `Response` 结构是 breaking change,推荐**只**通过 ctx 注入(不改 struct)
- 中间件依赖 `AgentContext` 时,如果没在 `agent.Run` 里注入会 nil panic → helper 必须 nil-safe
