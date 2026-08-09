package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/errs"
)

// 简单的基于内存的令牌桶限流实现。

type tokenBucket struct {
	capacity int
	tokens   float64
	refill   float64 // tokens per second
	last     time.Time
}

func newTokenBucket(capacity int, refillPerSecond float64) *tokenBucket {
	return &tokenBucket{
		capacity: capacity,
		tokens:   float64(capacity),
		refill:   refillPerSecond,
		last:     time.Now(),
	}
}

func (b *tokenBucket) allow(amount float64) bool {
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.refill
	if b.tokens > float64(b.capacity) {
		b.tokens = float64(b.capacity)
	}
	b.last = now
	if b.tokens >= amount {
		b.tokens -= amount
		return true
	}
	return false
}

// RateLimiterOption 配置 RateLimiter。
type RateLimiterOption func(*RateLimiter)

// WithMaxKeys 设置 bucket 数量上限（默认 100_000）。
func WithMaxKeys(n int) RateLimiterOption {
	return func(r *RateLimiter) {
		if n > 0 {
			r.maxKeys = n
		}
	}
}

// WithIdleTTL 设置 key 空闲淘汰时间（默认 1h）。
func WithIdleTTL(d time.Duration) RateLimiterOption {
	return func(r *RateLimiter) {
		if d > 0 {
			r.idleTTL = d
		}
	}
}

// RateLimiter 支持按 key（如 userID/IP）限流。
type RateLimiter struct {
	mu              sync.Mutex
	buckets         map[string]*tokenBucket
	lastUsed        map[string]time.Time
	capacity        int
	refillPerSecond float64
	maxKeys         int
	idleTTL         time.Duration
}

func NewRateLimiter(capacity int, refillPerSecond float64, opts ...RateLimiterOption) *RateLimiter {
	r := &RateLimiter{
		buckets:         make(map[string]*tokenBucket),
		lastUsed:        make(map[string]time.Time),
		capacity:        capacity,
		refillPerSecond: refillPerSecond,
		maxKeys:         100_000,
		idleTTL:         time.Hour,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *RateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.evictIdleLocked(now)

	b, ok := r.buckets[key]
	if !ok {
		b = newTokenBucket(r.capacity, r.refillPerSecond)
		r.buckets[key] = b
	}
	r.lastUsed[key] = now
	return b.allow(1)
}

func (r *RateLimiter) evictIdleLocked(now time.Time) {
	for k, t := range r.lastUsed {
		if now.Sub(t) > r.idleTTL {
			delete(r.buckets, k)
			delete(r.lastUsed, k)
		}
	}
	for len(r.buckets) >= r.maxKeys {
		var oldestKey string
		var oldest time.Time
		for k, t := range r.lastUsed {
			if oldestKey == "" || t.Before(oldest) {
				oldestKey, oldest = k, t
			}
		}
		if oldestKey == "" {
			break
		}
		delete(r.buckets, oldestKey)
		delete(r.lastUsed, oldestKey)
	}
}

// KeyFunc 从请求中提取限流 key（例如 Metadata 中的 user_id 或 IP）。
type KeyFunc func(*agent.Request) string

// RateLimitMiddleware 创建一个限流中间件。
func RateLimitMiddleware(limiter *RateLimiter, keyFn KeyFunc) Middleware {
	if keyFn == nil {
		keyFn = func(_ *agent.Request) string { return "global" }
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, req *agent.Request) (*agent.Response, error) {
			if limiter == nil {
				return next(ctx, req)
			}
			key := keyFn(req)
			if !limiter.allow(key) {
				return nil, errs.ErrRateLimited
			}
			return next(ctx, req)
		}
	}
}
