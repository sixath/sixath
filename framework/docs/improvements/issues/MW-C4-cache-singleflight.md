# [MW-C4] CacheMiddleware 内嵌 Singleflight 防 stampede

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md C4](../03-middleware.md) |
| **预估工作量** | 0.5 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/cache.go: CacheMiddleware`

## 现状

缓存 miss 时,N 个相同 prompt 的并发请求**全部落到模型**:
1. miss 1 → 调模型
2. miss 2 → 调模型
3. miss N → 调模型

模型调用是钱、是延迟,**缓存中间件最该做的优化没做**。

## 改进方案

```go
import "golang.org/x/sync/singleflight"

func CacheMiddleware(store *CacheStore) Middleware {
    var sf singleflight.Group
    return func(next Handler) Handler {
        return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
            if req == nil || store == nil {
                return next(ctx, req)
            }
            key := cacheKeyForRequest(req)
            if cached, ok := store.Get(key); ok {
                return cached, nil
            }
            // 同 key 并发只调一次 next,其余等结果
            v, err, _ := sf.Do(key, func() (any, error) {
                resp, err := next(ctx, req)
                if err == nil && resp != nil { store.Set(key, resp) }
                return resp, err
            })
            if v == nil { return nil, err }
            return v.(*agent.Response), err
        }
    }
}
```

## 验收标准

- [ ] 缓存 miss 时,同 key 的并发请求只调用 next 一次
- [ ] 旧 `TestCacheMiddleware_Basic` 0 回归

## 测试要求

`TestCacheMiddleware_Singleflight`:
```go
// 模拟一个慢的 next(sleep 200ms)
// 同时发起 100 个相同请求 → 期望 next 只被调用 1 次
```

`TestCacheMiddleware_DifferentKeysParallel`: 不同 key 的并发各调一次,不互相阻塞

## 风险

- `singleflight.Do` 第一个调用方 ctx 取消会让其他等待者也失败 → 这是 singleflight 的已知 caveat,通常可接受
- `golang.org/x/sync` 是标配,无新外部依赖
