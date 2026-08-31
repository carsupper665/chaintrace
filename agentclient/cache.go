package agentclient

import (
	"container/list"
	"sync"
)

// lruCache is keyed by values derived from a dataset id. An Analysis Dataset
// never changes after it is published — a new Analysis Run produces a new
// dataset id instead — so a cached entry can never go stale and there is no
// invalidation path. Entries only ever leave by eviction.
type lruCache[V any] struct {
	mu      sync.Mutex
	max     int
	entries map[string]*list.Element
	order   *list.List
}

type cacheEntry[V any] struct {
	key   string
	value V
}

func newLRUCache[V any](max int) *lruCache[V] {
	if max <= 0 {
		max = 128
	}
	return &lruCache[V]{
		max:     max,
		entries: make(map[string]*list.Element, max),
		order:   list.New(),
	}
}

func (c *lruCache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*cacheEntry[V]).value, true
}

func (c *lruCache[V]) Put(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		element.Value.(*cacheEntry[V]).value = value
		c.order.MoveToFront(element)
		return
	}
	c.entries[key] = c.order.PushFront(&cacheEntry[V]{key: key, value: value})
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*cacheEntry[V]).key)
	}
}

func (c *lruCache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
