# [MW-C1] 中间件层测试矩阵补完

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md C1](../03-middleware.md) |
| **预估工作量** | 2 天 |
| **依赖** | 无;P0 issue 自带的测试也归并到此 |

## 现状

`framework/middleware/middleware_test.go` 只有 3 个真实 case:Cache(1) + RateLimit(1) + ContentSafety(1)。`chain_bench_test.go` 有 2 个 benchmark。

## 缺口清单

### 调度核心(零覆盖)
- [ ] `Chain` 的洋葱顺序: `Chain(final, A, B, C)` 中 A 最外
- [ ] `ChainBuilder` 的 Order 语义(详见 [MW-A1](MW-A1-chainbuilder-order-direction.md))
- [ ] `MergeGlobalLocal` 的 global vs local 顺序
- [ ] `Chain(final)` 无 mw 退化为 final 本身

### 中间件实现
- [ ] `Recovery` 的 panic-to-error 转换 + 命名返回值改写
- [ ] `Recovery` 在 panic 是 `runtime.Error` / `string` / 自定义 type 时都正确处理
- [ ] `Metrics` token 解析(详见 [MW-A4](MW-A4-metrics-token-parsing.md))
- [ ] `Tracing` 的 span 创建 + attribute(详见 [MW-B5](MW-B5-tracing-attributes.md))
- [ ] `Debug` 的 enabled flag 行为
- [ ] `Debug` panic 时 stack 字段正确
- [ ] `Logging` 的 err 转义(详见 [MW-B4](MW-B4-logging-slog.md))

### 并发安全
- [ ] `CacheStore.Get/Set` race-free(`go test -race`)
- [ ] `RateLimiter.allow` race-free
- [ ] `Chain` 构造的 Handler 在多 goroutine 调用安全

### 边界 case
- [ ] `Cache` 命中 nil response 时不爆
- [ ] `Cache` Set 时 `req == nil` 不爆
- [ ] `RateLimiter` capacity=0 / refill=0 行为
- [ ] `ContentSafety` filter 为 nil 直接放行

## 验收标准

- [ ] 所有缺口 case 都有对应 `_test.go`
- [ ] `go test -cover ./middleware/...` 行覆盖率 ≥ 70%
- [ ] `go test -race ./middleware/...` 无警告
- [ ] CI 配置加 cover badge(可选)

## 测试要求

每个 case 用表驱动 + 子测试 (`t.Run`),命名规范:
- 调度: `TestChain_*` / `TestChainBuilder_*` / `TestMergeGlobalLocal_*`
- 实现: `TestXxxMiddleware_<Behavior>`
- 边界: `TestXxxMiddleware_<EdgeCase>`

## 风险

- 写 panic recover 测试要小心:用 `defer recover()` 包外层,避免测试自身崩溃
