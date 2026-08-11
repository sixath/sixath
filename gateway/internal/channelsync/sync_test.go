package channelsync_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/channelsync"
)

type fakeLister struct {
	mu   sync.Mutex
	chs  []channel.Channel
	err  error
	calls int
}

func (f *fakeLister) ListGatewayChannels(context.Context) ([]channel.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]channel.Channel, len(f.chs))
	copy(out, f.chs)
	return out, nil
}

type reconcileCall struct {
	prev, next []channel.Channel
}

type fakeManager struct {
	mu    sync.Mutex
	calls []reconcileCall
}

func (m *fakeManager) Reconcile(prev, next []channel.Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, reconcileCall{
		prev: append([]channel.Channel(nil), prev...),
		next: append([]channel.Channel(nil), next...),
	})
}

func (m *fakeManager) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestSyncOnce_SuccessReplaceAllAndReconcile(t *testing.T) {
	reg := channel.NewRegistry()
	reg.ReplaceAll([]channel.Channel{{
		ID: "old-webhook", Type: "webhook", Enabled: true, WebhookSecret: "s",
	}})
	lister := &fakeLister{chs: []channel.Channel{
		{ID: "wh1", Type: "webhook", Enabled: true, WebhookSecret: "sec"},
		{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"},
	}}
	mgr := &fakeManager{}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  mgr,
	})

	if err := r.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}

	if _, err := reg.Get("old-webhook"); err == nil {
		t.Fatal("expected old channel removed after ReplaceAll")
	}
	if _, err := reg.Get("wh1"); err != nil {
		t.Fatalf("expected wh1: %v", err)
	}
	if _, err := reg.Get("bot1"); err != nil {
		t.Fatalf("expected bot1: %v", err)
	}
	if mgr.callCount() != 1 {
		t.Fatalf("Reconcile calls=%d want 1", mgr.callCount())
	}
	call := mgr.calls[0]
	if len(call.prev) != 1 || call.prev[0].ID != "old-webhook" {
		t.Fatalf("prev=%v", call.prev)
	}
	if len(call.next) != 2 {
		t.Fatalf("next len=%d", len(call.next))
	}
}

func TestSyncOnce_FailureKeepsPreviousRegistry(t *testing.T) {
	reg := channel.NewRegistry()
	reg.ReplaceAll([]channel.Channel{{
		ID: "keep-me", Type: "webhook", Enabled: true, WebhookSecret: "s",
	}})
	lister := &fakeLister{err: errors.New("portal down")}
	mgr := &fakeManager{}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  mgr,
	})

	err := r.SyncOnce(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if _, getErr := reg.Get("keep-me"); getErr != nil {
		t.Fatalf("registry should keep previous channel: %v", getErr)
	}
	if mgr.callCount() != 0 {
		t.Fatalf("Reconcile should not run on failure; calls=%d", mgr.callCount())
	}
}

func TestSyncOnce_SecondSuccessDiffAgainstPreviousSnapshot(t *testing.T) {
	reg := channel.NewRegistry()
	lister := &fakeLister{chs: []channel.Channel{
		{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"},
	}}
	mgr := &fakeManager{}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  mgr,
	})
	if err := r.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	lister.mu.Lock()
	lister.chs = []channel.Channel{
		{ID: "bot1", Type: "wecom_bot", Enabled: false, BotID: "b", Secret: "s"},
		{ID: "wh1", Type: "webhook", Enabled: true},
	}
	lister.mu.Unlock()

	if err := r.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mgr.callCount() != 2 {
		t.Fatalf("calls=%d", mgr.callCount())
	}
	second := mgr.calls[1]
	if len(second.prev) != 1 || second.prev[0].ID != "bot1" || !second.prev[0].Enabled {
		t.Fatalf("second prev=%v", second.prev)
	}
	if len(second.next) != 2 {
		t.Fatalf("second next=%v", second.next)
	}
}

func TestRunner_RunRetriesAfterFailureWithoutExiting(t *testing.T) {
	reg := channel.NewRegistry()
	lister := &fakeLister{err: errors.New("boom")}
	mgr := &fakeManager{}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  mgr,
		Interval: 20 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		lister.mu.Lock()
		n := lister.calls
		lister.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("expected retries; calls=%d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	lister.mu.Lock()
	lister.err = nil
	lister.chs = []channel.Channel{{ID: "wh", Type: "webhook", Enabled: true}}
	lister.mu.Unlock()

	deadline = time.Now().Add(500 * time.Millisecond)
	for {
		if _, err := reg.Get("wh"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("expected eventual success")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit")
	}
}
