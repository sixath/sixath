# [MW-A1] `ChainBuilder` 的 Order 排序方向疑似有 bug

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md A1](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/middleware.go: ChainBuilder` 与 `OrderedMiddleware` 注释

## 现状

```go
// OrderedMiddleware 带优先级的中间件,Order 越小越先执行(靠近 Handler)。
type OrderedMiddleware struct {
    Order int
    Mw    Middleware
}

// ChainBuilder 按 Order 排序后与 final 组合成 Handler
func ChainBuilder(final Handler, ordered ...OrderedMiddleware) Handler {
    sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order > ordered[j].Order })  // 降序
    mws := make([]Middleware, len(ordered))
    for i := range ordered {
        mws[i] = ordered[i].Mw
    }
    return Chain(final, mws...)
}
```

## 问题分析

把三段对齐:
1. **注释**: Order 越小越先执行(靠近 Handler)
2. **排序结果**: `Order > Order` 降序 → 大 Order 在前,小 Order 在后(靠近末尾)
3. **`Chain` 行为**: `mws[0]` 在最外层 = 最先看到 request = **最先执行**;`mws[末尾]` 在最内层 = 最后看到 request = **最后执行**

合起来: 大 Order 在 `mws[0]` = 最先执行;小 Order 在 `mws[末尾]` = 最后执行。

→ **小 Order = 最后执行 = 靠近 Handler** ✅(注释括号里这部分对)
→ **小 Order = 先执行** ❌(注释开头错)

注释自相矛盾,而**代码可能并不错**(降序排列 + Chain 逆序遍历 = 大 Order 最外)。但用户读"Order 越小越先执行"会按字面意思理解,与实际行为不符。

## 改进方案

### Step 1 — 单测先行,固化语义

```go
// middleware_test.go
func TestChainBuilder_OrderSemantics(t *testing.T) {
    var trace []int
    mkMw := func(label int) Middleware {
        return func(next Handler) Handler {
            return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
                trace = append(trace, label)
                return next(ctx, req)
            }
        }
    }

    final := func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
        trace = append(trace, 0)
        return &agent.Response{}, nil
    }

    h := ChainBuilder(final,
        OrderedMiddleware{Order: 10, Mw: mkMw(10)},
        OrderedMiddleware{Order: 1,  Mw: mkMw(1)},
        OrderedMiddleware{Order: 5,  Mw: mkMw(5)},
    )
    _, _ = h(context.Background(), &agent.Request{})

    // 期望:大 Order 最外 → trace = [10, 5, 1, 0(final), ...]
    // OR 期望:小 Order 最外 → trace = [1, 5, 10, 0(final), ...]
    // 选一个写死,本测试就是契约
}
```

### Step 2 — 修注释或排序,二选一

读完单测确认实际行为后:
- 若实际行为是"小 Order 离 Handler 最近(最后执行)" → **修注释开头**:"Order 越大越先执行,越小越靠近 Handler"
- 若实际行为是"小 Order 最先执行(最外)" → **修排序方向**:`return ordered[i].Order < ordered[j].Order`

推荐**修注释**,因为现有内置中间件(Logging Order=10、Metrics Order=20 之类约定)如果已经在 portal 使用,改排序会引发二阶 bug。

### Step 3 — 文档化

在包级 doc.go 或 README 里给一张顺序示意图,标清:
- 小 Order = 最内层(最后执行) / 大 Order = 最外层(最先执行)
- 推荐 Order 取值约定:Recovery=100, Tracing=80, Logging=70, Metrics=60, RateLimit=50, ContentSafety=40, Cache=20

## 验收标准

- [ ] 表驱动单测覆盖 `ChainBuilder` 的执行顺序,断言 trace 切片
- [ ] 测试覆盖 `MergeGlobalLocal` 的 global vs local 顺序
- [ ] 注释与排序至少一边修正,**两者必须自洽**
- [ ] doc.go 或 README 添加顺序示意图与 Order 约定表
- [ ] 现有 portal 配置如有依赖,跟随调整(grep `OrderedMiddleware{`)

## 测试要求

- 单测 `TestChainBuilder_OrderSemantics`(必须)
- 单测 `TestChain_OnionOrder`: 验证 `Chain(final, A, B, C)` 时 A 最先看到 request,C 最后(基础契约)
- 单测 `TestMergeGlobalLocal_GlobalFirst`: global 中间件在 local 之外
- 边界: `ChainBuilder(final)` 无 mw 时返回 final 本身

## 风险

- 若现有用户已经按错误注释写了 Order 值,修注释会让他们的配置"语义反转"。但实际行为没变 → 风险只在 mental model
- 强烈不建议改排序方向,除非通过 grep 确认 0 用户依赖当前行为

## 关联 issue

- [MW-C1](MW-C1-test-coverage.md): 测试矩阵补完,本 issue 是其触发点
