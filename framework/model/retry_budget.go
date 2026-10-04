package model

import (
	"context"
	"sync"
	"time"
)

// RetryWaitBudget 为一次 Run 内所有模型重试「等待」共享的时间预算（只计退避/冷却，不计生成耗时），
// 使单次调用重试（WrapResilient）与 harness 步骤级恢复合计有硬上限。并发安全。
type RetryWaitBudget struct {
	mu        sync.Mutex
	remaining time.Duration
	retries   int
	parent    *RetryWaitBudget
}

// NewRetryWaitBudget 创建总额为 total 的预算（<0 视为 0）。
func NewRetryWaitBudget(total time.Duration) *RetryWaitBudget {
	if total < 0 {
		total = 0
	}
	return &RetryWaitBudget{remaining: total}
}

// Sub 派生上限为 limit 的子预算：子预算的每次扣减同时扣父预算，
// 用于给局部调用（如 critic）一个更小的等待上限。
func (b *RetryWaitBudget) Sub(limit time.Duration) *RetryWaitBudget {
	child := NewRetryWaitBudget(limit)
	child.parent = b
	return child
}

// Remaining 返回可用等待时长（含父预算约束）。
func (b *RetryWaitBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	r := b.remaining
	b.mu.Unlock()
	if b.parent != nil {
		if pr := b.parent.Remaining(); pr < r {
			r = pr
		}
	}
	return r
}

// TryReserve 在剩余额度足够时扣减 d 并返回 true；否则不扣减并返回 false。
func (b *RetryWaitBudget) TryReserve(d time.Duration) bool {
	if b == nil {
		return true
	}
	if d < 0 {
		d = 0
	}
	if d > b.Remaining() {
		return false
	}
	for cur := b; cur != nil; cur = cur.parent {
		cur.mu.Lock()
		cur.remaining -= d
		cur.mu.Unlock()
	}
	return true
}

// noteRetry 记录一次单次调用层（WrapResilient）重试，沿父链累加。
func (b *RetryWaitBudget) noteRetry() {
	for cur := b; cur != nil; cur = cur.parent {
		cur.mu.Lock()
		cur.retries++
		cur.mu.Unlock()
	}
}

// Retries 返回经由该预算发生的单次调用层重试次数。
func (b *RetryWaitBudget) Retries() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.retries
}

type retryWaitBudgetKey struct{}

// WithRetryWaitBudget 把预算挂到 ctx；WrapResilient 的重试循环会从中扣减每次退避。
func WithRetryWaitBudget(ctx context.Context, b *RetryWaitBudget) context.Context {
	if b == nil {
		return ctx
	}
	return context.WithValue(ctx, retryWaitBudgetKey{}, b)
}

// RetryWaitBudgetFrom 取 ctx 上的预算；没有时返回 nil（不限制）。
func RetryWaitBudgetFrom(ctx context.Context) *RetryWaitBudget {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(retryWaitBudgetKey{}).(*RetryWaitBudget)
	return b
}
