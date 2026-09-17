package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStreamRunContext_IgnoresDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	ctx, stop := streamRunContext(parent)
	defer stop()

	select {
	case <-parent.Done():
	case <-time.After(time.Second):
		t.Fatal("parent deadline did not fire")
	}
	if !errors.Is(parent.Err(), context.DeadlineExceeded) {
		t.Fatalf("parent err = %v, want deadline", parent.Err())
	}

	select {
	case <-ctx.Done():
		t.Fatalf("stream ctx canceled on HTTP deadline: %v", ctx.Err())
	case <-time.After(40 * time.Millisecond):
	}
}

func TestStreamRunContext_PropagatesCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := streamRunContext(parent)
	defer stop()

	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stream ctx should cancel on client disconnect")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("stream ctx err = %v, want canceled", ctx.Err())
	}
}

func TestStreamRunContext_StopCancels(t *testing.T) {
	ctx, stop := streamRunContext(context.Background())
	stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop() should cancel stream ctx")
	}
}
