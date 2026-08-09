package memory

import (
	"container/list"
	"sync"
)

// queryEmbedCache is a process-local LRU for query embedding vectors.
// Capacity <= 0 disables the cache (get returns nil; put is a no-op).
type queryEmbedCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List // front = most recent
	items    map[string]*list.Element
}

type queryEmbedEntry struct {
	key string
	vec []float32
}

func newQueryEmbedCache(capacity int) *queryEmbedCache {
	return &queryEmbedCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

func (c *queryEmbedCache) get(key string) []float32 {
	if c == nil || c.capacity <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil
	}
	c.ll.MoveToFront(el)
	return copyFloat32(el.Value.(*queryEmbedEntry).vec)
}

func (c *queryEmbedCache) put(key string, vec []float32) {
	if c == nil || c.capacity <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*queryEmbedEntry).vec = copyFloat32(vec)
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&queryEmbedEntry{key: key, vec: copyFloat32(vec)})
	c.items[key] = el
	for c.ll.Len() > c.capacity {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.ll.Remove(back)
		delete(c.items, back.Value.(*queryEmbedEntry).key)
	}
}

func copyFloat32(in []float32) []float32 {
	if in == nil {
		return nil
	}
	out := make([]float32, len(in))
	copy(out, in)
	return out
}
