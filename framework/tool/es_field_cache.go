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
	// esMappingFailureTTL 内同一索引拉取失败的结果直接复用，避免每次调用都等满拉取超时。
	esMappingFailureTTL = 30 * time.Second
)

// cachedFieldMapper 为 ListFields 加 TTL 缓存；空结果不缓存，拉取失败（inner 实现 ESFieldListerErr 且返回 error）
// 按 failureTTL 负缓存。同一索引的并发未命中合并为一次拉取。
type cachedFieldMapper struct {
	inner      ESFieldMapper
	ttl        time.Duration
	maxEntries int
	minRefresh time.Duration
	failureTTL time.Duration
	now        func() time.Time
	mu         sync.Mutex
	lists      map[string]cachedFieldList
	failures   map[string]cachedFieldFailure
	inflight   map[string]*fieldFetch
}

type cachedFieldList struct {
	fields []string
	at     time.Time
}

type cachedFieldFailure struct {
	err error
	at  time.Time
}

type fieldFetch struct {
	done   chan struct{}
	fields []string
	err    error
}

func newCachedFieldMapper(inner ESFieldMapper, ttl time.Duration) *cachedFieldMapper {
	return &cachedFieldMapper{
		inner:      inner,
		ttl:        ttl,
		maxEntries: esMappingCacheMaxEntries,
		minRefresh: esMappingRefreshMinInterval,
		failureTTL: esMappingFailureTTL,
		now:        time.Now,
		lists:      map[string]cachedFieldList{},
		failures:   map[string]cachedFieldFailure{},
		inflight:   map[string]*fieldFetch{},
	}
}

func (m *cachedFieldMapper) Lookup(ctx context.Context, index, field string) (ESFieldMapping, bool) {
	return m.inner.Lookup(ctx, index, field)
}

func (m *cachedFieldMapper) ListFields(ctx context.Context, index string) []string {
	fields, _ := m.ListFieldsErr(ctx, index)
	return fields
}

func (m *cachedFieldMapper) ListFieldsErr(ctx context.Context, index string) ([]string, error) {
	m.mu.Lock()
	c, ok := m.lists[index]
	err := m.recentFailureLocked(index)
	m.mu.Unlock()
	if ok && m.now().Sub(c.at) < m.ttl {
		return c.fields, nil
	}
	if err != nil {
		return nil, err
	}
	return m.fetch(ctx, index)
}

// Refresh 绕过缓存重新拉取，非空时写回；距上次成功拉取不足 minRefresh 时直接返回缓存。
func (m *cachedFieldMapper) Refresh(ctx context.Context, index string) []string {
	fields, _ := m.RefreshErr(ctx, index)
	return fields
}

func (m *cachedFieldMapper) RefreshErr(ctx context.Context, index string) ([]string, error) {
	m.mu.Lock()
	c, ok := m.lists[index]
	err := m.recentFailureLocked(index)
	m.mu.Unlock()
	if ok && m.now().Sub(c.at) < m.minRefresh {
		return c.fields, nil
	}
	if err != nil {
		return nil, err
	}
	return m.fetch(ctx, index)
}

func (m *cachedFieldMapper) recentFailureLocked(index string) error {
	f, ok := m.failures[index]
	if !ok {
		return nil
	}
	if m.now().Sub(f.at) < m.failureTTL {
		return f.err
	}
	delete(m.failures, index)
	return nil
}

func (m *cachedFieldMapper) fetch(ctx context.Context, index string) ([]string, error) {
	m.mu.Lock()
	if f, ok := m.inflight[index]; ok {
		m.mu.Unlock()
		select {
		case <-f.done:
			return f.fields, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f := &fieldFetch{done: make(chan struct{})}
	m.inflight[index] = f
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.inflight, index)
		switch {
		case f.err != nil:
			m.storeFailureLocked(index, f.err)
		case len(f.fields) > 0:
			delete(m.failures, index)
			m.storeLocked(index, f.fields)
		}
		m.mu.Unlock()
		close(f.done)
	}()
	if le, ok := m.inner.(ESFieldListerErr); ok {
		f.fields, f.err = le.ListFieldsErr(ctx, index)
	} else {
		f.fields = m.inner.ListFields(ctx, index)
	}
	return f.fields, f.err
}

func (m *cachedFieldMapper) storeFailureLocked(index string, err error) {
	now := m.now()
	if _, exists := m.failures[index]; !exists && m.maxEntries > 0 && len(m.failures) >= m.maxEntries {
		for k, v := range m.failures {
			if now.Sub(v.at) >= m.failureTTL {
				delete(m.failures, k)
			}
		}
		if len(m.failures) >= m.maxEntries {
			return
		}
	}
	m.failures[index] = cachedFieldFailure{err: err, at: now}
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

// esMapperCache 按集群持有带缓存的 mapper，缓存（含上限与刷新限速）按集群隔离；
// fixed 非空时所有集群共用同一个底层 mapper（测试/显式注入），但各自缓存，不同集群的同名索引互不串用。
type esMapperCache struct {
	mu        sync.Mutex
	fixed     ESFieldMapper
	newMapper func(cluster string) ESFieldMapper
	ttl       time.Duration
	byCluster map[string]*cachedFieldMapper
}

func newESMapperCache(fixed ESFieldMapper, newMapper func(string) ESFieldMapper, ttl time.Duration) *esMapperCache {
	return &esMapperCache{fixed: fixed, newMapper: newMapper, ttl: ttl, byCluster: map[string]*cachedFieldMapper{}}
}

// For 返回该集群的 mapper；集群不支持 mapping 时返回 nil（不缓存，便于数据源后续注册）。
func (mc *esMapperCache) For(cluster string) *cachedFieldMapper {
	if mc.fixed == nil && mc.newMapper == nil {
		return nil
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if m, ok := mc.byCluster[cluster]; ok {
		return m
	}
	inner := mc.fixed
	if inner == nil {
		inner = mc.newMapper(cluster)
	}
	if inner == nil {
		return nil
	}
	m := newCachedFieldMapper(inner, mc.ttl)
	mc.byCluster[cluster] = m
	return m
}
