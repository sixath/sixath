package tool

import (
	"context"
	"sync"
	"time"
)

const esMappingCacheTTL = 5 * time.Minute

// cachedFieldMapper 为 ListFields 加 TTL 缓存；空结果（mapping 拉取失败）不缓存。
type cachedFieldMapper struct {
	inner ESFieldMapper
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	lists map[string]cachedFieldList
}

type cachedFieldList struct {
	fields []string
	at     time.Time
}

func newCachedFieldMapper(inner ESFieldMapper, ttl time.Duration) *cachedFieldMapper {
	return &cachedFieldMapper{inner: inner, ttl: ttl, now: time.Now, lists: map[string]cachedFieldList{}}
}

func (m *cachedFieldMapper) Lookup(ctx context.Context, index, field string) (ESFieldMapping, bool) {
	return m.inner.Lookup(ctx, index, field)
}

func (m *cachedFieldMapper) ListFields(ctx context.Context, index string) []string {
	m.mu.Lock()
	c, ok := m.lists[index]
	m.mu.Unlock()
	if ok && m.now().Sub(c.at) < m.ttl {
		return c.fields
	}
	return m.Refresh(ctx, index)
}

// Refresh 绕过缓存重新拉取，非空时写回。
func (m *cachedFieldMapper) Refresh(ctx context.Context, index string) []string {
	fields := m.inner.ListFields(ctx, index)
	if len(fields) > 0 {
		m.mu.Lock()
		m.lists[index] = cachedFieldList{fields: fields, at: m.now()}
		m.mu.Unlock()
	}
	return fields
}

// esMapperCache 按集群持有带缓存的 mapper；fixed 非空时所有集群共用它（测试/显式注入）。
type esMapperCache struct {
	mu        sync.Mutex
	fixed     *cachedFieldMapper
	newMapper func(cluster string) ESFieldMapper
	ttl       time.Duration
	byCluster map[string]*cachedFieldMapper
}

func newESMapperCache(fixed ESFieldMapper, newMapper func(string) ESFieldMapper, ttl time.Duration) *esMapperCache {
	mc := &esMapperCache{newMapper: newMapper, ttl: ttl, byCluster: map[string]*cachedFieldMapper{}}
	if fixed != nil {
		mc.fixed = newCachedFieldMapper(fixed, ttl)
	}
	return mc
}

// For 返回该集群的 mapper；集群不支持 mapping 时返回 nil（不缓存，便于数据源后续注册）。
func (mc *esMapperCache) For(cluster string) *cachedFieldMapper {
	if mc.fixed != nil {
		return mc.fixed
	}
	if mc.newMapper == nil {
		return nil
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if m, ok := mc.byCluster[cluster]; ok {
		return m
	}
	inner := mc.newMapper(cluster)
	if inner == nil {
		return nil
	}
	m := newCachedFieldMapper(inner, mc.ttl)
	mc.byCluster[cluster] = m
	return m
}
