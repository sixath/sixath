# Middleware 中间件链体系 改进报告

> 模块: `framework/middleware/`
> 评审日期: 2026-06-03
> 综合评分: **6.5 / 10**

## 模块定位

为 `agent.Run` 提供函数式洋葱模型中间件链,内置 Logging / Recovery / Metrics / Tracing / Cache / RateLimit / ContentSafety / Debug 8 个。`ChainBuilder` 支持 Order 优先级,`MergeGlobalLocal` 支持全局 + 局部合并。

## 评分

| 维度 | 分数 | 备注 |
|------|------|------|
| 抽象设计 | 9 / 10 | Handler/Middleware 函数式形态最 idiomatic;Order + GlobalLocal 是亮点 |
| 实现正确性 | 6 / 10 | A1(Order 方向疑似错)、A4(metrics token 解析 bug)是硬伤 |
| 健壮性 | 5 / 10 | RateLimit 内存泄漏、Cache 不淘汰、Logging 手拼 JSON |
| 安全性 | 6 / 10 | ContentSafety 思路对(不暴露命中词),实现弱(Contains + 同步两路) |
| 性能 | 6 / 10 | 有 benchmark,但 closure 分配未优化,缺 singleflight |
| 可观测性 | 5 / 10 | metrics/tracing 都接了但 attribute 单薄,日志靠手拼 |
| 测试 | 4 / 10 | 只 3 个 case,核心 ChainBuilder/Order 完全没覆盖 |
| 可扩展性 | 8 / 10 | 工厂模式 + Order 优先级让中间件接得很顺 |
| LLM 友好度 | 6 / 10 | Cache key 漏 Parts/Metadata、流式语义未定义 |

---

## 🔴 A 严重 — 影响生产可用

### A1. `ChainBuilder` 的 Order 排序方向疑似有 bug

**位置**: `framework/middleware/middleware.go: ChainBuilder` + `OrderedMiddleware` 注释

**现状**:
```go
// OrderedMiddleware: Order 越小越先执行(靠近 Handler)。  ← 注释
sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order > ordered[j].Order })  ← 实现
```

**矛盾分析**:
- 注释: Order 小 = 先执行
- 排序结果: Order 小 = 在 mws 末尾
- `Chain` 行为: `mws[0]` 在最外层 = 最先执行
- 因此 mws 末尾 = **最后才看到 request** = **最后执行**

**注释与实际行为相反**。优先级语义"谁先谁后"是中间件框架的根本契约,不能靠用户猜。

**改进**:
1. 给 `ChainBuilder` 写表驱动单测,固化 Order 语义
2. 若发现注释与行为冲突,**以测试为准修一边**(注释 OR 排序方向)
3. 修后在文档明确给出"小 Order = 最外层" or "小 Order = 最内层"的最终结论

**优先级**: P0
**工作量**: 0.5 天

---

### A2. Cache key 没把"会影响输出的非消息内容"算进去

**位置**: `framework/middleware/cache.go: cacheKeyForRequest`

**现状**:
```go
func cacheKeyForRequest(req *agent.Request) string {
    h := sha256.New()
    for _, m := range req.Messages {
        h.Write([]byte(m.Role))
        h.Write([]byte(m.Content))
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

**缺漏**:
- `m.Parts`(图像/URL multipart)**完全没入 hash** —— 同 text 不同 image 会命中错误缓存
- `req.Metadata` 里的 `model_name` / `temperature` / `tools` 都没算 —— 切换模型后还会拿到旧模型答案
- `m.Role` 大小写敏感(`User` vs `user` 会 miss),应做归一化

**改进**:
1. 做规范化序列化(sort metadata key,canonical JSON)
2. 把 Parts、相关 metadata 一起 hash
3. 定义 `CacheKey(req)` 接口让上层定制

**优先级**: P0
**工作量**: 1 天

---

### A3. RateLimiter 的 buckets map **永远不清理**

**位置**: `framework/middleware/rate_limit.go: RateLimiter.buckets`

**现状**:
```go
type RateLimiter struct {
    buckets map[string]*tokenBucket  // 按 user_id / IP 做 key
}
```

**风险**:
- 用 user_id / IP 做 key 时,每个独立用户都留下永不释放的 bucket
- SaaS 多租户场景,百万 user 上线一次就 ~80MB 常驻 + 永不回收

**改进**:
1. 加 LRU 上限(`hashicorp/golang-lru`)
2. 或定期 sweep 长时间未访问的 key(`time.AfterFunc` 触发)
3. 也可换用 `golang.org/x/time/rate` + sync.Map + idle eviction

**优先级**: P0
**工作量**: 0.5 天

---

### A4. Metrics 中间件 token 解析有 bug,数据全废

**位置**: `framework/middleware/metrics.go: MetricsMiddleware`

**现状**:
```go
if in, _ := resp.Metadata["token_input"].(int); in > 0 || resp.Metadata["token_output"] != nil {
    out, _ := resp.Metadata["token_output"].(int)
    obs.ObserveTokenUsage(agentName, in, out)
}
```

**问题**:
- `if ...; ...` 前后两个判断不对称
- JSON 反序列化 number 默认 `float64`,`.(int)` 直接 falsey,导致 metrics **永远 0**
- `Metadata["token_output"] != nil` 但 `(int)` 断言失败时,`out` 也是 0 —— **悄悄上报 token=0**

**改进**:
1. 抽 helper `int64FromAny`(项目内 `datasource.intFromAny` 已有 —— **避免重复造轮子**)
2. 或者把 `agent.Response` 的 token 字段从 metadata map 提升为 typed 字段:
   ```go
   type Usage struct{ InputTokens, OutputTokens int64 }
   type Response struct { ...; Usage Usage }
   ```

**优先级**: P0
**工作量**: 0.5 天

---

## 🟠 B 中等 — 设计完整性

### B1. 没有"中断 / short-circuit"的一等机制

**位置**: 影响 `cache.go` + `content_safety.go` 等

**现状**: 所有中间件想中断都靠 `return nil, err`。但现实里:
- Cache 命中要"返回正常 response 不再下钻" —— 通过 `return cached, nil` 实现,但**外层无法区分"命中 cache" 与 "真实请求"**(metrics、logging 都把 cache 命中当成 ms=0 的请求)
- ContentSafety 的"输入命中黑名单"想返回固定礼貌话术而非错误 —— 当前只能 err

**改进方向**:
1. `Response` 加 `Source string`(`"cache" / "filter" / "model"`)
2. 或更彻底: cache 中间件改成**装饰最内层 model 调用**而非整个 Agent,让 metrics / log 仍走外层

**优先级**: P1
**工作量**: 1-2 天

---

### B2. Cache **不主动清理过期项**

**位置**: `framework/middleware/cache.go: CacheStore`

**现状**:
```go
func (c *CacheStore) Get(key string) (...) {
    if !e.ExpireAt.IsZero() && time.Now().After(e.ExpireAt) {
        return nil, false  // 只是不返回,没真的删
    }
}
func (c *CacheStore) Set(key string, resp *agent.Response) {
    c.entries[key] = e   // 只新增不删除
}
```

**问题**: 过期项永不删除,长期运行内存只增不减,TTL 形同虚设。

**改进**:
1. 后台 goroutine 周期 sweep
2. Set 时检查容量上限做 LRU 淘汰
3. 或直接 import `groupcache/lru` / `bigcache`

**优先级**: P1
**工作量**: 1 天

---

### B3. ContentSafety 的 `strings.Contains` 是**最弱实现**

**位置**: `framework/middleware/content_safety.go: SimpleBlocklistFilter.CheckInput`

**问题**:
- false positive 概率高(代码片段、错误日志原文、URL 路径)
- 对中文/Unicode 边界完全无概念
- 黑名单平铺数组,n*m 复杂度
- 没区分用户输入 / assistant 输出 / 多模态 Parts

**改进**:
1. 用 Aho-Corasick(`cloudflare/ahocorasick`)做多模式匹配,O(n)
2. 按 Role 过滤(已检查 user,但 assistant 输出多模态 `Parts` 没检查)
3. Filter interface 改返回 `*Result` 而非 `error`,让上层能区分"命中 + 替换" vs "命中 + 拒绝"

**优先级**: P1
**工作量**: 1 天

---

### B4. `LoggingMiddleware` 用 `log.Printf` 手拼 JSON

**位置**: `framework/middleware/logging.go`

**现状**:
```go
log.Printf(`{"level":"info","msg":"agent_request","elapsed_ms":%d,"error":"%v"}`, elapsed.Milliseconds(), err)
```

**问题**:
- err 带引号 / 反斜杠 / 控制字符时,**生成的 JSON 直接非法**,日志 pipeline 解析失败
- 不是真正的结构化日志(缺 request_id、user_id、agent_name、status_code)
- 项目 `obs/` 据观察有 logger(metrics/tracing 都用了) —— 但 logging 没用

**改进**: 换 `slog`(Go 1.21+)或项目自身 logger;字段化输出,err 用 `slog.Any`。

**优先级**: P0
**工作量**: 0.5 天

---

### B5. Tracing span 名固定为 `"Agent.Run"`,粒度太粗

**位置**: `framework/middleware/tracing.go`

**现状**: 所有 agent 的 span 名都一样,APM 工具聚合时无法按 agent 分组。

**改进**:
1. 读 `Metadata["agent_name"]` 做后缀: `"Agent.Run/<name>"`,或 attribute `agent.name`
2. 补缺失 attribute: `messages.count`、`token.input`、`token.output`、`model.name`

**优先级**: P1
**工作量**: 0.5 天

---

### B6. Debug 中间件**总是 stdlib log**

**位置**: `framework/middleware/debug.go`

**问题**:
- 整段 stack 用 `%q` 转义后超长单行,几乎不可读
- enabled flag 全局 boolean,无 per-agent / per-route 精细控制
- `runtimeDebug.Stack()` 含部署路径,可能泄漏部署信息

**改进**:
1. 换 `slog` 输出 `stack` 为 multi-line 字段
2. enabled 改为 `func(*agent.Request) bool` 谓词,支持按请求决定
3. 部署路径用 `runtime.GOROOT()` / `GOPATH` 做 prefix 替换

**优先级**: P2
**工作量**: 1 天

---

### B7. 无 `Once` / `If` / `Skip` 这类组合器

**位置**: 模块级缺失

**真实需求**:
- "Recovery 只在 production 装"
- "RateLimit 仅对带 `user_id` 的请求生效"
- "Tracing 在被 sampler 选中才执行"

**改进**:
```go
func If(cond func(*agent.Request) bool, mw Middleware) Middleware
func Skip(cond func(*agent.Request) bool, mw Middleware) Middleware
```

**优先级**: P2
**工作量**: 0.5 天

---

## 🟡 C 较小但应做

### C1. 测试覆盖很薄 —— 只有 3 个真实测试

**位置**: `framework/middleware/middleware_test.go`

**没覆盖**:
- `Chain` 顺序(洋葱模型方向)
- `ChainBuilder` 的 Order 语义(A1 直接相关)
- `MergeGlobalLocal` 的 global vs local 顺序
- `Recovery` 的 panic-to-error 转换
- `Metrics` 的 token 解析(A4 直接相关)
- `Tracing` 的 span 创建
- `Debug` 的 enabled flag 行为
- 并发安全(`CacheStore` 和 `RateLimiter` 都有 mutex,值得 race 测试)

**改进**: 测试覆盖率底线 70%,核心调度路径必须有用例。

**优先级**: P1
**工作量**: 2 天

---

### C2. `Middleware` 的 closure 引发 N 次堆分配

**位置**: 所有中间件实现

**现状**:
```go
return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
    ...
}
```

每个 mw 1 次 closure escape 到 heap,3 个 mw 链就是 3 次 alloc。

**改进**:
1. 把不变状态提到外层(部分已做,如 tracer)
2. Benchmark 加 `b.ReportAllocs()` 暴露分配数
3. 无状态 mw(Logging / Recovery)可考虑结构体形态减少 closure

**优先级**: P3
**工作量**: 1 天

---

### C3. `ContentFilter.CheckOutput` 复用 `CheckInput` 的潜在问题

**位置**: `framework/middleware/content_safety.go: SimpleBlocklistFilter`

**问题**: 注释说"基于关键字黑名单",但**输出可能合理出现"badword"作为对用户输入的引用**(用户问"为什么 X 是 badword?")。这种"用户能说但 AI 不能复述"的非对称需求是真实场景。

**改进**:
1. 提供两套不同默认实现
2. 或在文档明确告警 + 给出非对称示例

**优先级**: P2
**工作量**: 0.5 天

---

### C4. Cache 没有内嵌 `Singleflight`

**位置**: `framework/middleware/cache.go: CacheMiddleware`

**问题**: 缓存 miss 时,N 个同 prompt 请求同时打模型 —— 全部 miss,全部调用,全部写入。模型调用是钱、是延迟,**缓存中间件最值得做的优化**。

**改进**: `golang.org/x/sync/singleflight` 一行接入。

**优先级**: P1
**工作量**: 0.5 天

---

### C5. `agent.Request` / `agent.Response` 用 `Metadata map[string]any` 太多

**位置**: 跨 `agent` + `middleware` 模块

**现状**: 中间件强依赖 string key,但**没 const 定义**:
- 有人写 `"AgentName"` / `"agentName"` —— 默认值生效,**测试无法发现**
- 框架其他地方塞了什么 key,中间件层不知道

**改进**:
1. 在 `agent` 包定义 `MetadataKey` 类型 + `const MetadataKeyAgentName = "agent_name"`
2. 所有中间件用常量
3. 长期把高频字段提升为 typed 字段(`AgentName`、`UserID`、`Usage` 三个最值钱)

**优先级**: P1
**工作量**: 1 天

---

## 🔵 D 架构方向(更大一步)

### D1. 中间件应有"上下文传递"通道(per-request 共享数据)

**现状**: 中间件之间想传递信息(如 RateLimit 算出 key 想给 Logging 用)只能塞 `ctx.Value` 或 `req.Metadata`。

**改进**: 框架级约定,引入 `agent.Context` 包装类型,或类似 net/http `context` 的标准化用法。

**优先级**: P2
**工作量**: 3-5 天

---

### D2. 流式响应(SSE/stream)对中间件的语义没定义

**现状**: portal 里有 SSE 流,但 `Handler returns *Response` —— 一次性返回。流式场景下:
- Cache: 何时写入?整段流完?第一段?
- ContentSafety: 流式 token 来一个检测一个还是聚合后检测?
- Metrics: elapsed 是首 token 还是末 token?

**改进**:
```go
type StreamHandler = func(ctx, *agent.Request) (<-chan ResponseChunk, error)
type StreamMiddleware = func(StreamHandler) StreamHandler
```
或用 Go generics 抽 `Handler[Req, Resp]`。

**优先级**: P1(SSE 已在 portal 用了,语义模糊会越来越痛)
**工作量**: 2 周

---

### D3. 中间件"可观测自身"的能力

**现状**: 所有中间件都装饰 Handler,但**没人装饰中间件本身**。运维想知道:
- 每个 mw 自己耗时多少
- 哪个 mw 短路了请求
- ChainBuilder 排序后的最终顺序

**改进**:
```go
func WithName(name string, mw Middleware) Middleware
func Inspect(handler Handler) []string  // 返回链路顺序
```

**优先级**: P2
**工作量**: 1 天

---

## 验收标准

每条 P0 / P1 改进项必须满足:
1. 有专门单测(table-driven)
2. CHANGELOG 记录行为变化
3. 若动 `agent.Request/Response` 形态,跨模块统一升级
4. P0 项必须有 race detector 测试(并发场景)

---

## 实施清单(可直接转 issue)

### Phase 1(本周, ~1 人天)
- [ ] **P0** A1: 给 `ChainBuilder` 加表驱动单测,固化 Order 语义
- [ ] **P0** A4: metrics token 解析改 `int64FromAny`(项目内已有)+ 补单测
- [ ] **P0** A2: Cache key 把 `req.Metadata` 和 `m.Parts` 算进去
- [ ] **P0** A3: RateLimiter 加 LRU 上限
- [ ] **P0** B4: Logging 换 `slog`,err 安全转义

### Phase 2(1-2 周)
- [ ] **P1** B1 + D1: 抽 `agent.Context` per-request 共享通道 + short-circuit 机制
- [ ] **P1** B2: Cache 加后台 sweep 或 LRU
- [ ] **P1** B3: ContentSafety 换 Aho-Corasick + 按 Role 区分
- [ ] **P1** B5: Tracing span 加 agent.name + attribute
- [ ] **P1** C1: 测试矩阵补完(覆盖率 70%+)
- [ ] **P1** C4: Cache 加 Singleflight
- [ ] **P1** C5: `Metadata` 高频字段 typed 化

### Phase 3(1-2 月)
- [ ] **P1** D2: `StreamMiddleware` 流式接口(SSE 已上,优先级实际很高)
- [ ] **P2** B6 / B7: Debug 谓词化 + `If`/`Skip` 组合器
- [ ] **P2** C3: ContentFilter 输入输出分离实现
- [ ] **P2** D3: 中间件自身可观测
- [ ] **P3** C2: closure 分配优化
