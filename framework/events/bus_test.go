package events

import (
	"context"
	"sync"
	"testing"
)

func TestBus_PublishSyncAndAsync(t *testing.T) {
	bus := NewBus()
	var syncSeen bool
	asyncDone := make(chan struct{})

	bus.Subscribe(false, func(ctx context.Context, e Event) {
		syncSeen = true
	})
	var once sync.Once
	bus.Subscribe(true, func(ctx context.Context, e Event) {
		once.Do(func() { close(asyncDone) })
	})

	ctx := context.Background()
	bus.Publish(ctx, Event{Kind: RunStarted, RequestID: "r1"})

	if !syncSeen {
		t.Error("sync listener did not run")
	}
	<-asyncDone
}

func TestGrowthReviewKindsDistinct(t *testing.T) {
	kinds := []Kind{
		GrowthReviewScheduled,
		GrowthReviewCompleted,
		GrowthReviewFailed,
	}
	seen := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		if string(k) == "" {
			t.Fatalf("empty kind constant")
		}
		if _, ok := seen[string(k)]; ok {
			t.Fatalf("duplicate kind string: %q", k)
		}
		seen[string(k)] = struct{}{}
	}
}

func TestHookBlockedKind(t *testing.T) {
	if HookBlocked != "agent.hook.blocked" {
		t.Fatalf("HookBlocked=%q", HookBlocked)
	}
}
