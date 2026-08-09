# [MW-D2] StreamMiddleware 流式接口(为 SSE 铺路)

| 字段 | 值 |
|------|-----|
| **优先级** | P1(SSE 已在 portal 用,语义模糊会越来越痛) |
| **模块** | framework/agent、framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md D2](../03-middleware.md) |
| **预估工作量** | 2 周 |
| **依赖** | [MW-B1](MW-B1-agent-context.md)(共享 AgentContext) |

## 问题位置

- 当前 `Handler` 签名: `func(ctx, *Request) (*Response, error)` —— 一次性返回
- portal `internal/chat/` 已实现 SSE,但绕过了 framework 的中间件链

## 问题分析

流式场景下,现有中间件语义不明:
- **Cache**: 何时写入?整段流完?第一段?
- **ContentSafety**: 流式 token 来一个检测一个还是聚合后检测?
- **Metrics**: elapsed 是首 token 还是末 token?

portal 端因此**绕过了 framework 中间件**自己实现 SSE,导致:
- middleware 提供的 logging / metrics / tracing / cache **流式路径上全部失效**
- 同一份"中间件配置"在非流式和流式两条路径行为不一致

## 改进方案

### Step 1 — 引入 StreamHandler / StreamMiddleware 类型

```go
// framework/agent/stream.go (新)
type ResponseChunk struct {
    Delta    string  // 增量文本
    Parts    []Part  // 增量 multimodal 部分
    Usage    *Usage  // 末 chunk 提供
    Done     bool    // 末 chunk 标记
    Err      error   // 末 chunk 可能为 err
}

// framework/middleware/stream.go (新)
type StreamHandler func(ctx context.Context, req *agent.Request) (<-chan ResponseChunk, error)
type StreamMiddleware func(StreamHandler) StreamHandler

func StreamChain(final StreamHandler, mws ...StreamMiddleware) StreamHandler { ... }
```

### Step 2 — 定义流式中间件契约

| 中间件 | 流式语义 |
|--------|---------|
| Logging | 起始 + 终止各一条;中间记 chunk 计数 |
| Metrics | elapsed = 末 chunk 时间;首 chunk 时间作为 first-token 单独指标 |
| Tracing | 整段一个 span;chunk 数作为 attribute |
| Cache | 流式收集到末 chunk 后整段 set;命中时分块吐回 |
| ContentSafety | 输入按整段检查;输出按 chunk 检查(命中时关闭 channel + Err) |
| RateLimit | 起始时检查一次 |

### Step 3 — Cache 的流式适配

```go
// 命中时把缓存的 *Response 切成单 chunk 流式返回
if cached, ok := store.Get(key); ok {
    ch := make(chan ResponseChunk, 1)
    ch <- ResponseChunk{Delta: cached.Text, Done: true}
    close(ch)
    return ch, nil
}

// miss 时收集 chunk 重组
chOut := make(chan ResponseChunk, 16)
go func() {
    defer close(chOut)
    var sb strings.Builder
    chIn, err := next(ctx, req)
    if err != nil { chOut <- ResponseChunk{Err: err, Done: true}; return }
    for c := range chIn {
        sb.WriteString(c.Delta)
        chOut <- c
    }
    store.Set(key, &agent.Response{Text: sb.String()})
}()
return chOut, nil
```

### Step 4 — portal 接入

portal 把 SSE handler 改为调用 `StreamChain` 而非裸 model adapter,所有 framework 中间件流式路径生效。

## 验收标准

- [ ] `StreamHandler` / `StreamMiddleware` / `StreamChain` 定义
- [ ] 8 个内置中间件(Logging / Recovery / Metrics / Tracing / Cache / RateLimit / ContentSafety / Debug)都有流式版本
- [ ] portal `internal/chat/` 切换到 StreamChain,SSE 路径上 metrics / tracing 起作用
- [ ] 流式 + 非流式两条路径的中间件配置共享同一份(只差 wrapper)

## 测试要求

- `TestStreamChain_Order`: 多 middleware 流式时顺序正确
- `TestStreamCache_HitReplay`: 缓存命中时分块吐回
- `TestStreamCache_MissCollect`: miss 时整段收集后写入
- `TestStreamContentSafety_OutputChunk`: 命中时关闭 channel 并填 Err
- `TestStreamRecovery_Panic`: chunk goroutine panic 被 recover 转 Err

## 风险

- **大改动**: 增加一套并行的 API,文档需要同步双倍
- **Mitigation**: 用 generic 抽 `Handler[Req, Resp]` 可以减少代码重复(Go 1.18+ generics)
- 中间件作者需要学习两种写法。提供 `Lift(mw Middleware) StreamMiddleware` 适配器,把无状态非流式 mw 升级为流式 mw

## 关联 issue

- [MW-B1](MW-B1-agent-context.md): AgentContext 共享通道是流式中间件互相通信的基础设施
