package middleware

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sixath/framework/agent"
	"golang.org/x/sync/singleflight"
)

const defaultCacheMaxEntries = 10_000

// CacheStoreOption 配置 CacheStore。
type CacheStoreOption func(*cacheStoreConfig)

type cacheStoreConfig struct {
	maxEntries int
}

// WithCacheMaxEntries 设置 LRU 容量上限（默认 10_000）。
func WithCacheMaxEntries(n int) CacheStoreOption {
	return func(c *cacheStoreConfig) {
		if n > 0 {
			c.maxEntries = n
		}
	}
}

// CacheStore 是基于 expirable LRU 的内存缓存，过期项与超容量项会被真正淘汰。
type CacheStore struct {
	cache *lru.LRU[string, *agent.Response]
}

// noExpiryTTL 用于 ttl<=0：expirable.LRU 要求正数 interval，用极大 TTL 表示仅 LRU 淘汰。
const noExpiryTTL = 100 * 365 * 24 * time.Hour

// NewCacheStore 创建带 TTL 的缓存；ttl <= 0 表示条目不因时间过期（仍受 MaxEntries 约束）。
func NewCacheStore(ttl time.Duration, opts ...CacheStoreOption) *CacheStore {
	cfg := cacheStoreConfig{maxEntries: defaultCacheMaxEntries}
	for _, o := range opts {
		o(&cfg)
	}
	effectiveTTL := ttl
	if effectiveTTL <= 0 {
		effectiveTTL = noExpiryTTL
	} else if effectiveTTL < time.Millisecond {
		// expirable 后台 ticker 为 ttl/100；过小会导致 NewTicker(0) panic。
		effectiveTTL = time.Millisecond
	}
	return &CacheStore{
		cache: lru.NewLRU[string, *agent.Response](cfg.maxEntries, nil, effectiveTTL),
	}
}

func (c *CacheStore) Get(key string) (*agent.Response, bool) {
	if c == nil || c.cache == nil {
		return nil, false
	}
	resp, ok := c.cache.Get(key)
	return resp, ok
}

func (c *CacheStore) Set(key string, resp *agent.Response) {
	if c == nil || c.cache == nil || resp == nil {
		return
	}
	c.cache.Add(key, resp)
}

// Len 返回当前缓存条目数（测试与观测用）。
func (c *CacheStore) Len() int {
	if c == nil || c.cache == nil {
		return 0
	}
	return c.cache.Len()
}

// CacheMiddleware 根据请求内容缓存 Agent 响应（默认 DefaultCacheKey）；miss 时同 key 并发只调用 next 一次。
func CacheMiddleware(store *CacheStore) Middleware {
	return CacheMiddlewareWithKey(store, &DefaultCacheKey{Version: 1})
}

// CacheMiddlewareWithKey 使用自定义 CacheKeyBuilder。
func CacheMiddlewareWithKey(store *CacheStore, builder CacheKeyBuilder) Middleware {
	if builder == nil {
		builder = &DefaultCacheKey{Version: 1}
	}
	var sf singleflight.Group
	return func(next Handler) Handler {
		return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			if req == nil || store == nil {
				return next(ctx, req)
			}
			key := builder.BuildKey(req)
			if cached, ok := store.Get(key); ok {
				if ac := agent.ContextFrom(ctx); ac != nil {
					ac.CacheHit = true
				}
				return cached, nil
			}
			v, err, _ := sf.Do(key, func() (any, error) {
				if cached, ok := store.Get(key); ok {
					return cached, nil
				}
				resp, err := next(ctx, req)
				if err == nil && resp != nil {
					store.Set(key, resp)
				}
				return resp, err
			})
			if v == nil {
				return nil, err
			}
			return v.(*agent.Response), err
		}
	}
}
