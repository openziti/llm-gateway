package routing

import (
	"testing"
	"time"
)

func TestCacheGetPut(t *testing.T) {
	c := newLRUCache[string](10, time.Hour)

	c.put("key1", "value1")
	v, ok := c.get("key1")
	if !ok || v != "value1" {
		t.Errorf("expected ('value1', true), got (%q, %v)", v, ok)
	}

	_, ok = c.get("missing")
	if ok {
		t.Error("expected miss for unknown key")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	c := newLRUCache[int](3, time.Hour)

	c.put("a", 1)
	c.put("b", 2)
	c.put("c", 3)

	// access "a" to make it most recently used
	c.get("a")

	// adding "d" should evict "b" (least recently used)
	c.put("d", 4)

	if _, ok := c.get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}
	if v, ok := c.get("a"); !ok || v != 1 {
		t.Error("expected 'a' to still be present")
	}
	if v, ok := c.get("c"); !ok || v != 3 {
		t.Error("expected 'c' to still be present")
	}
	if v, ok := c.get("d"); !ok || v != 4 {
		t.Error("expected 'd' to still be present")
	}
}

func TestCacheTTLExpiration(t *testing.T) {
	c := newLRUCache[string](10, 50*time.Millisecond)

	c.put("key", "value")
	v, ok := c.get("key")
	if !ok || v != "value" {
		t.Errorf("expected immediate hit, got (%q, %v)", v, ok)
	}

	time.Sleep(60 * time.Millisecond)

	_, ok = c.get("key")
	if ok {
		t.Error("expected miss after TTL expiration")
	}
}

func TestCacheUpdate(t *testing.T) {
	c := newLRUCache[string](10, time.Hour)

	c.put("key", "v1")
	c.put("key", "v2")

	v, ok := c.get("key")
	if !ok || v != "v2" {
		t.Errorf("expected updated value 'v2', got (%q, %v)", v, ok)
	}
}

func TestHashKeyDeterministic(t *testing.T) {
	h1 := hashKey("hello world")
	h2 := hashKey("hello world")
	if h1 != h2 {
		t.Errorf("hashKey not deterministic: %q != %q", h1, h2)
	}

	h3 := hashKey("different input")
	if h1 == h3 {
		t.Error("hashKey collision on different inputs")
	}
}
