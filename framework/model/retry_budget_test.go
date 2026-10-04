package model

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWaitBudget_ReserveAndSub(t *testing.T) {
	b := NewRetryWaitBudget(100 * time.Millisecond)
	if !b.TryReserve(60 * time.Millisecond) {
		t.Fatal("first reserve must fit")
	}
	if b.TryReserve(50 * time.Millisecond) {
		t.Fatal("reserve beyond remaining must fail")
	}
	if got := b.Remaining(); got != 40*time.Millisecond {
		t.Fatalf("remaining=%v want 40ms (failed reserve must not deduct)", got)
	}
	child := b.Sub(time.Second)
	if got := child.Remaining(); got != 40*time.Millisecond {
		t.Fatalf("child remaining=%v must be capped by parent", got)
	}
	if !child.TryReserve(30 * time.Millisecond) {
		t.Fatal("child reserve must fit")
	}
	if got := b.Remaining(); got != 10*time.Millisecond {
		t.Fatalf("parent remaining=%v want 10ms (child deducts parent)", got)
	}
	var nilBudget *RetryWaitBudget
	if !nilBudget.TryReserve(time.Hour) {
		t.Fatal("nil budget must not limit")
	}
}

func TestWrapResilient_BudgetStopsRetries(t *testing.T) {
	fake := &retryFake{failFirst: 99, err: &APIStatusError{StatusCode: 429, RetryAfter: 40 * time.Millisecond}}
	cfg := testRetryConfig(4)
	cfg.MaxRetryAfter = time.Second
	m := WrapResilient(&retryFakeModel{f: fake}, cfg)

	b := NewRetryWaitBudget(100 * time.Millisecond)
	_, err := m.Chat(WithRetryWaitBudget(context.Background(), b), []Message{{Role: "user", Content: "hi"}})
	var apiErr *APIStatusError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 {
		t.Fatalf("err=%v want 429", err)
	}
	// 40ms + 40ms 在 100ms 预算内，第三次 40ms 超出 → 共 3 次调用。
	if got := fake.callCount(); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
	if b.Retries() != 2 || b.Remaining() != 20*time.Millisecond {
		t.Fatalf("retries=%d remaining=%v", b.Retries(), b.Remaining())
	}
}

func TestWrapResilient_ExhaustedBudgetFailsFast(t *testing.T) {
	fake := &retryFake{failFirst: 99, err: &APIStatusError{StatusCode: 503}}
	m := WrapResilient(&retryFakeModel{f: fake}, testRetryConfig(4))
	ctx := WithRetryWaitBudget(context.Background(), NewRetryWaitBudget(0))
	if _, err := m.Chat(ctx, []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected error")
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("calls=%d want 1 (no budget left to wait)", got)
	}
}
