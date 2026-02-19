package routing

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// lruCache is a generic LRU cache with TTL-based expiration.
type lruCache[V any] struct {
	capacity int
	ttl      time.Duration
	mu       sync.Mutex
	items    map[string]*list.Element
	order    *list.List
}

type cacheEntry[V any] struct {
	key       string
	value     V
	expiresAt time.Time
}

func newLRUCache[V any](capacity int, ttl time.Duration) *lruCache[V] {
	return &lruCache[V]{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

func (c *lruCache[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}

	entry := el.Value.(*cacheEntry[V])
	if time.Now().After(entry.expiresAt) {
		c.order.Remove(el)
		delete(c.items, key)
		var zero V
		return zero, false
	}

	c.order.MoveToFront(el)
	return entry.value, true
}

func (c *lruCache[V]) put(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		entry := el.Value.(*cacheEntry[V])
		entry.value = value
		entry.expiresAt = time.Now().Add(c.ttl)
		return
	}

	if c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*cacheEntry[V]).key)
		}
	}

	entry := &cacheEntry[V]{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	el := c.order.PushFront(entry)
	c.items[key] = el
}

func hashKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}
