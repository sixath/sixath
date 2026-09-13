package idempotency

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryStore_BeginAndComplete(t *testing.T) {
	s := NewStore(time.Minute)

	// 首次 Begin：reused=false。
	_, reused, err := s.Begin(context.Background(), "k", "corr-1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if reused {
		t.Fatal("first Begin should not be reused")
	}

	// 再次 Begin：reused=true，返回既有 correlation。
	e, reused, err := s.Begin(context.Background(), "k", "corr-2")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if !reused {
		t.Fatal("second Begin should be reused")
	}
	if e.CorrelationID != "corr-1" {
		t.Fatalf("correlation = %q, want corr-1", e.CorrelationID)
	}
	if e.Status != StatusInProgress {
		t.Fatalf("status = %q, want in_progress", e.Status)
	}

	// Complete 标记 done。
	if err := s.Complete(context.Background(), "k", "result"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, ok, err := s.Get(context.Background(), "k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if got.Status != StatusDone || got.Result != "result" {
		t.Fatalf("entry = %+v", got)
	}
}

func TestMemoryStore_ConcurrentBegin_OnlyOneNew(t *testing.T) {
	s := NewStore(time.Minute)
	const n = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	var news int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, reused, err := s.Begin(context.Background(), "k", "corr")
			if err != nil {
				t.Errorf("Begin: %v", err)
				return
			}
			if !reused {
				atomic.AddInt32(&news, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if news != 1 {
		t.Fatalf("news = %d, want exactly 1 (idempotency reservation)", news)
	}
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	s := NewStore(20 * time.Millisecond)
	if _, _, err := s.Begin(context.Background(), "k", "corr"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok, err := s.Get(context.Background(), "k"); err != nil || ok {
		t.Fatalf("expected expiry, ok=%v err=%v", ok, err)
	}
}

func TestMemoryStore_EmptyKey(t *testing.T) {
	s := NewStore(time.Minute)
	_, reused, err := s.Begin(context.Background(), "", "corr")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if reused {
		t.Fatal("empty key should never be reused (no dedup)")
	}
}