# [MW-A2] Cache key 缺漏 Parts / Metadata,导致错误命中

| 字段 | 值 |
|------|-----|
| **优先级** | P0 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md A2](../03-middleware.md) |
| **预估工作量** | 1 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/cache.go: cacheKeyForRequest`

## 现状

```go
func cacheKeyForRequest(req *agent.Request) string {
    h := sha256.New()
    for _, m := range req.Messages {
        h.Write([]byte(m.Role))
        h.Write([]byte{':'})
        h.Write([]byte(m.Content))
        h.Write([]byte{';'})
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

## 问题分析

缺漏:
1. **`m.Parts`(图像/URL multipart)完全没入 hash** → 同 text 不同 image 会命中错误缓存(相同的 caption + 不同图片)
2. **`req.Metadata` 里的 `model_name` / `temperature` / `tools` / `system_prompt` 都没算** → 切换模型后还会拿到旧模型的回答
3. **`m.Role` 大小写敏感**(`User` vs `user` 是不同 key,而 LLM 行为相同) → 缓存命中率下降
4. **没有版本号** → 升级后的旧缓存可能行为不一致仍被复用

## 改进方案

```go
// CacheKeyBuilder 用于自定义 key 计算策略
type CacheKeyBuilder interface {
    BuildKey(req *agent.Request) string
}

// DefaultCacheKey 是默认实现,把 Messages + Parts + 关键 metadata 都纳入
type DefaultCacheKey struct {
    Version int          // 升级时 bump,旧缓存自动失效
    MetadataKeys []string // 哪些 metadata 入 key,默认 ["model", "temperature", "system"]
}

func (k *DefaultCacheKey) BuildKey(req *agent.Request) string {
    h := sha256.New()

    fmt.Fprintf(h, "v=%d;", k.Version)

    for _, m := range req.Messages {
        // role 归一化为小写
        fmt.Fprintf(h, "role=%s;", strings.ToLower(strings.TrimSpace(m.Role)))
        fmt.Fprintf(h, "content=%s;", m.Content)
        // Parts 顺序敏感,完整入 hash
        for _, p := range m.Parts {
            fmt.Fprintf(h, "part_text=%s;", p.Text)
            fmt.Fprintf(h, "part_url=%s;", p.URL)
            // 注意: 图像 byte 也应 hash(如果 Parts 含 inline binary)
        }
        h.Write([]byte("|"))
    }

    // 关键 metadata 字段(按字典序确保确定性)
    keys := make([]string, 0, len(k.MetadataKeys))
    keys = append(keys, k.MetadataKeys...)
    sort.Strings(keys)
    for _, mk := range keys {
        v := req.Metadata[mk]
        fmt.Fprintf(h, "%s=%v;", mk, v)
    }

    return hex.EncodeToString(h.Sum(nil))
}
```

`CacheMiddleware` 增加可选 builder 参数:
```go
func CacheMiddlewareWithKey(store *CacheStore, builder CacheKeyBuilder) Middleware
func CacheMiddleware(store *CacheStore) Middleware  // 用 DefaultCacheKey{Version: 1}
```

## 验收标准

- [ ] 新增 `CacheKeyBuilder` 接口与 `DefaultCacheKey` 实现
- [ ] `CacheMiddleware` 默认使用 `DefaultCacheKey{Version: 1}`
- [ ] role 大小写归一化(`User` vs `user` 同 key)
- [ ] `Parts.Text` / `Parts.URL` 入 hash
- [ ] 默认把 `model` / `temperature` / `system` 三个 metadata 入 hash
- [ ] 用户可自定义 `MetadataKeys` 列表

## 测试要求

新增 `TestDefaultCacheKey`:
- 相同 messages + 相同 parts + 相同 metadata → 同 key
- 不同 parts(同 text)→ 不同 key
- 不同 model metadata → 不同 key
- role 大小写差异 → 同 key
- metadata 字段插入顺序变化 → 同 key(确定性测试)
- bump version → 不同 key(强制失效)

`TestCacheMiddleware_ImageCacheBug`:模拟"相同 caption + 不同图片"必须 cache miss

## 风险

- 改 key 算法会让现有 cache 全部失效一次。**这正是改进目的**(防错误命中比保留旧缓存更重要)
- `Metadata` 用 `%v` 格式化对 nested struct / map 不稳定,需要 `json.Marshal` 兜底

## 关联 issue

- 无强依赖,可独立做
