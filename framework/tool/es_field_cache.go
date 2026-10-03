package tool

import (
	"context"
	"sync"
	"time"
)

const (
	esMappingCacheTTL = 5 * time.Minute
	// esMappingCacheMaxEntries 限制每个集群缓存的索引模式数，防止模型随手写的 index 撑大内存。
	esMappingCacheMaxEntries = 256
	// esMappingRefreshMinInterval 内对同一索引的强制刷新直接返回缓存，避免未知字段反复触发拉 mapping。
	esMappingRefreshMinInterval = 30 * time.Second
)

// cachedFieldMapper 为 ListFields 加 TTL 缓存；空结果（mapping 拉取失败）不缓存。
// 同一索引的并发未命中合并为一次拉取。
type cachedFieldMapper struct {
	inner      ESFieldMapper
	ttl        time.Duration
	maxEntries int
	minRefresh time.Duration
	now        func() time.Time
	mu         sync.Mutex
	lists      map[string]cachedFieldList
	inflight   map[string]*fieldFetch
}

type cachedFieldList struct {
	fields []string
	at     time.Time
}

type fieldFetch struct {
	done   chan struct{}
	fields []string
}

func newCachedFieldMapper(inner ESFieldMapper, ttl time.Duration) *cachedFieldMapper {
	return &cachedFieldMapper{
		inner:      inner,
		ttl:        ttl,
		maxEntries: esMappingCacheMaxEntries,
		minRefresh: esMappingRefreshMinInterval,
		now:        time.Now,
		lists:      map[string]cachedFieldList{},
		inflight:   map[string]*fieldFetch{},
	}
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
	return m.fetch(ctx, index)
}

// Refresh 绕过缓存重新拉取，非空时写回；距上次成功拉取不足 minRefresh 时直接返回缓存。
func (m *cachedFieldMapper) Refresh(ctx context.Context, index string) []string {
	m.mu.Lock()
	c, ok := m.lists[index]
	m.mu.Unlock()
	if ok && m.now().Sub(c.at) < m.minRefresh {
		return c.fields
	}
	return m.fetch(ctx, index)
}

func (m *cachedFieldMapper) fetch(ctx context.Context, index string) []string {
	m.mu.Lock()
	if f, ok := m.inflight[index]; ok {
		m.mu.Unlock()
		select {
		case <-f.done:
			return f.fields
		case <-ctx.Done():
			return nil
		}
	}
	f := &fieldFetch{done: make(chan struct{})}
	m.inflight[index] = f
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.inflight, index)
		if len(f.fields) > 0 {
			m.storeLocked(index, f.fields)
		}
		m.mu.Unlock()
		close(f.done)
	}()
	f.fields = m.inner.ListFields(ctx, index)
	return f.fields
}

func (m *cachedFieldMapper) storeLocked(index string, fields []string) {
	now := m.now()
	if _, exists := m.lists[index]; !exists && m.maxEntries > 0 && len(m.lists) >= m.maxEntries {
		var oldestKey string
		var oldestAt time.Time
		for k, v := range m.lists {
			if now.Sub(v.at) >= m.ttl {
				delete(m.lists, k)
				continue
			}
			if oldestKey == "" || v.at.Before(oldestAt) {
				oldestKey, oldestAt = k, v.at
			}
		}
		if len(m.lists) >= m.maxEntries && oldestKey != "" {
			delete(m.lists, oldestKey)
		}
	}
	m.lists[index] = cachedFieldList{fields: fields, at: now}
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
