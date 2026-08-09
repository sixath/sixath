# [MW-A3] RateLimiter 的 buckets map 永不清理 → 内存泄漏

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md A3](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/rate_limit.go: RateLimiter`

## 现状

```go
type RateLimiter struct {
    mu      sync.Mutex
    buckets map[string]*tokenBucket
    capacity        int
    refillPerSecond float64
}

func (r *RateLimiter) allow(key string) bool {
    r.mu.Lock()
    defer r.mu.Unlock()
    b, ok := r.buckets[key]
    if !ok {
        b = newTokenBucket(r.capacity, r.refillPerSecond)
        r.buckets[key] = b   // 永不删除
    }
    return b.allow(1)
}
```

## 问题分析

按 user_id / IP 做 key 时,**每个独立用户都留下永不释放的 bucket**:
- SaaS 多租户场景,百万 user 上线一次就是百万个 `*tokenBucket` 常驻
- 估算:每个 bucket ~80 bytes + map overhead → 百万 user ≈ 80MB+,**且永不回收**
- 程序运行越久泄漏越严重,典型生产 OOM 路径

## 改进方案

### 方案 A(推荐): LRU 上限

```go
import "github.com/hashicorp/golang-lru/v2/expirable"

type RateLimiter struct {
    buckets *expirable.LRU[string, *tokenBucket]   // 自带 TTL 过期 + LRU 淘汰
    capacity        int
    refillPerSecond float64
}

func NewRateLimiter(capacity int, refillPerSecond float64, opts ...Option) *RateLimiter {
    cfg := defaultConfig()
    for _, o := range opts { o(&cfg) }
    return &RateLimiter{
        buckets:         expirable.NewLRU[string, *tokenBucket](cfg.MaxKeys, nil, cfg.IdleTTL),
        capacity:        capacity,
        refillPerSecond: refillPerSecond,
    }
}

func (r *RateLimiter) allow(key string) bool {
    b, ok := r.buckets.Get(key)
    if !ok {
        b = newTokenBucket(r.capacity, r.refillPerSecond)
        r.buckets.Add(key, b)
    }
    return b.allow(1)
}
```

### 方案 B(轻量): 后台 sweep

如果不想引外部依赖:
```go
type RateLimiter struct {
    ...
    lastUsed map[string]time.Time
}

// 启动一个 goroutine 周期清理 idleTTL 内未访问的 key
func (r *RateLimiter) StartGC(ctx context.Context, idleTTL time.Duration) {
    tick := time.NewTicker(idleTTL / 2)
    go func() {
        defer tick.Stop()
        for {
            select {
            case <-ctx.Done(): return
            case now := <-tick.C:
                r.mu.Lock()
                for k, t := range r.lastUsed {
                    if now.Sub(t) > idleTTL {
                        delete(r.buckets, k)
                        delete(r.lastUsed, k)
                    }
                }
                r.mu.Unlock()
            }
        }
    }()
}
```

### 方案 C(最轻): 容量上限

```go
const maxBuckets = 100_000

func (r *RateLimiter) allow(key string) bool {
    ...
    if !ok {
        if len(r.buckets) >= maxBuckets {
            // 简单: 随机淘汰一个(或按 lastUsed)
            for k := range r.buckets { delete(r.buckets, k); break }
        }
        b = newTokenBucket(...)
        r.buckets[key] = b
    }
}
```

**推荐方案 A**(LRU + TTL),代码最少、行为最准。

## 验收标准

- [ ] `RateLimiter` 引入 LRU 上限或 TTL 清理
- [ ] 默认配置: `MaxKeys=100_000`, `IdleTTL=1*time.Hour`,可通过 Option 覆盖
- [ ] 跑 `TestRateLimiter_NoLeak`: 写 100 万次不同 key,memory usage 不超过预期上限(用 `runtime.MemStats` 断言)
- [ ] 现有 `TestRateLimitMiddleware_Global` 0 回归

## 测试要求

- `TestRateLimiter_LRUEvict`: 超过 MaxKeys 后,最早的 key 被淘汰
- `TestRateLimiter_TTLExpire`: 一个 key idle 超过 TTL 后,新请求拿到一个**新桶**(因此 capacity 重置,这是预期行为)
- `TestRateLimiter_Concurrent`: `go test -race`,1000 个 goroutine 并发 allow 不同 key,无 race
- 内存基准: `BenchmarkRateLimiter_HighCardinality`: 100 万次不同 key 后 `runtime.GC()`,heap inuse 应 < 50MB

## 风险

- TTL 过期会导致一个用户在静默期后**重置桶 = 重置配额**。这是已知行为,文档说明即可
- LRU 淘汰可能让活跃用户被错误淘汰(但 MaxKeys=100k 量级一般不会)。Mitigation: 加 metrics `ratelimit_bucket_evicted_total`
- 引入 `hashicorp/golang-lru` 是新依赖,go.mod 增加 ~200KB
