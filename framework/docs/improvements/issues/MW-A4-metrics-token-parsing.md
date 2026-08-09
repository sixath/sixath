# [MW-A4] Metrics token 解析有 bug,数据全废

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/middleware、framework/agent |
| **状态** | 已完成 |
| **完成批次** | [2026-06-03-p0-quickwin-batch](../../superpowers/plans/2026-06-03-p0-quickwin-batch.md) |
| **关联报告** | [03-middleware.md A4](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/metrics.go: MetricsMiddleware`

## 现状

```go
if in, _ := resp.Metadata["token_input"].(int); in > 0 || resp.Metadata["token_output"] != nil {
    out, _ := resp.Metadata["token_output"].(int)
    obs.ObserveTokenUsage(agentName, in, out)
}
```

## 问题分析

1. **类型断言失败**: JSON 反序列化数字默认 `float64`,`.(int)` 直接 falsey,`in` 永远 0
2. **条件不对称**: 前面查 `(int)`,后面查 `!= nil`,逻辑断裂
3. **悄悄上报 token=0**: `resp.Metadata["token_output"] != nil` 但 `(int)` 失败时,`out=0` 被上报到 Prometheus → **指标全废,且无错误日志提示**
4. **重复造轮子**: 项目内 `framework/datasource/datasource.go: intFromAny` 已经处理了所有数值类型

## 改进方案

### Step 1 — 立即修复(用现有 helper)

把 `intFromAny` 上提到 `framework/internal/anyx`(或直接放到 `agent` 包),`metrics.go` 用之:

```go
// framework/internal/anyx/anyx.go (新)
func Int64FromAny(v any) (int64, bool) {
    switch x := v.(type) {
    case float64: return int64(x), true
    case float32: return int64(x), true
    case int:     return int64(x), true
    case int32:   return int64(x), true
    case int64:   return x, true
    case uint32:  return int64(x), true
    case uint64:  return int64(x), true
    case json.Number:
        i, err := x.Int64()
        return i, err == nil
    default:
        return 0, false
    }
}

// metrics.go
in, hasIn := anyx.Int64FromAny(resp.Metadata["token_input"])
out, hasOut := anyx.Int64FromAny(resp.Metadata["token_output"])
if hasIn || hasOut {
    obs.ObserveTokenUsage(agentName, int(in), int(out))
}
```

### Step 2 — 长期(typed 字段)

把 `agent.Response` 的 token 字段从 metadata map 提升为 typed 字段:
```go
type Usage struct {
    InputTokens  int64
    OutputTokens int64
    TotalTokens  int64   // 派生,可选
}

type Response struct {
    Text     string
    Parts    []Part
    Usage    Usage          // 新增
    Metadata map[string]any
}
```

各 model adapter(OpenAI / DashScope / Ollama)在响应解析时填充 `Usage`,middleware 直接读 typed 字段,不再触碰 metadata。

## 验收标准

### Step 1
- [ ] `Int64FromAny` 抽到公共位置,`framework/datasource/datasource.go: intFromAny` 也改用之
- [ ] `MetricsMiddleware` 用 `Int64FromAny` 解析 token,删除原 `(int)` 断言
- [ ] 原日志不变(从 metadata map 读),向后兼容

### Step 2(可作为后续 issue)
- [ ] `agent.Response.Usage` 字段加入
- [ ] 至少 OpenAI adapter 填充 Usage(其他 adapter 在后续 PR 跟进)
- [ ] `MetricsMiddleware` 优先读 `resp.Usage`,未填充则回退到 metadata map(兼容期)

## 测试要求

- `TestInt64FromAny`: 表驱动,覆盖所有数值类型(含 json.Number)
- `TestMetricsMiddleware_FloatToken`: metadata 含 `"token_input": float64(100)`(模拟 JSON 反序列化),断言 `obs.ObserveTokenUsage` 被调用且第二参数 = 100
- `TestMetricsMiddleware_NoToken`: metadata 不含 token 字段,断言 `obs.ObserveTokenUsage` **不被调用**(用 mock 计数器)
- `TestMetricsMiddleware_OnlyOutputToken`: 只有 `token_output`,断言 `in=0, out=N`

## 风险

- 现有(错误的)Prometheus 时序数据全是 0,改完后会出现"突然有数据"。监控配置可能需要重新校准告警阈值
- 抽公共 helper 时注意循环依赖(`agent` 不能依赖 `middleware`,反之 OK;放 `internal/anyx` 最干净)

## 关联 issue

- [MW-C5](MW-C5-typed-metadata.md): Metadata typed 化(本 issue 的 Step 2)
