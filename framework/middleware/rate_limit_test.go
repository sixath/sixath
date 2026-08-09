package middleware

import (
	"runtime"
	"testing"
	"time"
)

func TestRateLimiter_LRUEvict(t *testing.T) {
	lim := NewRateLimiter(10, 1, WithMaxKeys(2), WithIdleTTL(time.Hour))
	if !lim.allow("a") {
		t.Fatal("a should pass")
	}
	time.Sleep(2 * time.Millisecond)
	if !lim.allow("b") {
		t.Fatal("b should pass")
	}
	lim.allow("c")
	lim.mu.Lock()
	_, hasA := lim.buckets["a"]
	_, hasC := lim.buckets["c"]
	n := len(lim.buckets)
	lim.mu.Unlock()
	if n != 2 {
		t.Fatalf("bucket count = %d, want 2", n)
	}
	if hasA {
		t.Fatal("expected oldest key a evicted under maxKeys=2")
	}
	if !hasC {
		t.Fatal("expected newest key c retained")
	}
}

func TestRateLimiter_TTLExpire(t *testing.T) {
	lim := NewRateLimiter(1, 0, WithMaxKeys(100), WithIdleTTL(20*time.Millisecond))
	if !lim.allow("u") {
		t.Fatal("first allow")
	}
	time.Sleep(30 * time.Millisecond)
	if !lim.allow("u") {
		t.Fatal("after TTL new bucket should allow once")
	}
}

func TestRateLimiter_NoLeak(t *testing.T) {
	lim := NewRateLimiter(1, 0, WithMaxKeys(1000), WithIdleTTL(time.Millisecond))
	for i := 0; i < 5000; i++ {
		lim.allow(string(rune('a' + (i % 26))))
	}
	runtime.GC()
	lim.mu.Lock()
	n := len(lim.buckets)
	lim.mu.Unlock()
	if n > 1000 {
		t.Fatalf("bucket count %d exceeds maxKeys", n)
	}
}
