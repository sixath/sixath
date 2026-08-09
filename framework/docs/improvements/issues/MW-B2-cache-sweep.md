# [MW-B2] CacheStore 加 sweep / LRU,过期项真正删除

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md B2](../03-middleware.md) |
| **预估工作量** | 1 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/cache.go: CacheStore`

## 现状

```go
func (c *CacheStore) Get(key string) (*agent.Response, bool) {
    e, ok := c.entries[key]
    if !ok { return nil, false }
    if !e.ExpireAt.IsZero() && time.Now().After(e.ExpireAt) {
        return nil, false   // 只是不返回,没真的删
    }
    return e.Response, true
}

func (c *CacheStore) Set(key string, resp *agent.Response) {
    c.entries[key] = e   // 只新增不删除
}
```

## 问题分析

过期项**永不删除**,长期运行内存只增不减,TTL 形同虚设。

## 改进方案

直接 import `hashicorp/golang-lru/v2/expirable`,与 [MW-A3](MW-A3-ratelimiter-leak.md) 同款方案:

```go
import "github.com/hashicorp/golang-lru/v2/expirable"

type CacheStore struct {
    cache *expirable.LRU[string, *agent.Response]
}

func NewCacheStore(ttl time.Duration, opts ...Option) *CacheStore {
    cfg := defaultConfig()
    for _, o := range opts { o(&cfg) }
    return &CacheStore{
        cache: expirable.NewLRU[string, *agent.Response](cfg.MaxEntries, nil, ttl),
    }
}
```

`Get` / `Set` 直接转发到 LRU。

## 验收标准

- [ ] `CacheStore` 使用 LRU + TTL,过期项被真正释放
- [ ] 默认配置 `MaxEntries=10_000`,可通过 Option 覆盖
- [ ] 现有 `TestCacheMiddleware_Basic` 0 回归

## 测试要求

- `TestCacheStore_TTLEvict`: 写入 + 等待 TTL 过期 + GC,memory inuse 应回落
- `TestCacheStore_LRUEvict`: 超过 MaxEntries 后,最早的 key 被淘汰
- `TestCacheStore_Concurrent`: `go test -race`

## 风险

- 与 MW-A3 引入同一外部依赖,go.mod 增加一份
- 行为变化:**满了之后不再无限增长** —— 这是改进目的
