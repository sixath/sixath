package adapter_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
)

type ilinkLoopRecorder struct {
	mu          sync.Mutex
	starts      map[string]int
	generations []int
	stopped     chan string
}

func newIlinkLoopRecorder() *ilinkLoopRecorder {
	return &ilinkLoopRecorder{starts: map[string]int{}, stopped: make(chan string, 16)}
}

func (r *ilinkLoopRecorder) loop(ctx context.Context, ch channel.Channel, _ adapter.WeixinIlinkDeps) {
	r.mu.Lock()
	r.starts[ch.ID]++
	r.generations = append(r.generations, ch.Generation)
	r.mu.Unlock()
	<-ctx.Done()
	r.stopped <- ch.ID
}

func (r *ilinkLoopRecorder) startCount(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts[id]
}

func (r *ilinkLoopRecorder) expectStop(t *testing.T, what string) {
	t.Helper()
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatalf("expected stop on %s", what)
	}
}

func (r *ilinkLoopRecorder) expectNoStop(t *testing.T, what string) {
	t.Helper()
	select {
	case id := <-r.stopped:
		t.Fatalf("unexpected stop of %s on %s", id, what)
	case <-time.After(50 * time.Millisecond):
	}
}

func newTestIlinkManager(t *testing.T, reporter *fakeStatusReporter) (*adapter.WeixinIlinkManager, *ilinkLoopRecorder) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr := adapter.NewWeixinIlinkManager(ctx, adapter.WeixinIlinkDeps{Reporter: reporter})
	rec := newIlinkLoopRecorder()
	mgr.SetLoopForTest(rec.loop)
	return mgr, rec
}

func ilinkChannel() channel.Channel {
	return channel.Channel{
		ID: "wx-1", Type: "weixin_ilink", Enabled: true,
		BotToken: "t1", BaseURL: "https://ilinkai.weixin.qq.com", Generation: 1, AllowedPeers: []string{"a"},
	}
}

func TestWeixinIlinkManager_RestartOnGenerationNotOnAllowedPeers(t *testing.T) {
	reporter := &fakeStatusReporter{}
	mgr, rec := newTestIlinkManager(t, reporter)

	ch := ilinkChannel()
	mgr.Reconcile(nil, []channel.Channel{ch})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 1 }, "first start")

	peers := ch
	peers.AllowedPeers = []string{"a", "b"}
	mgr.Reconcile([]channel.Channel{ch}, []channel.Channel{peers})
	rec.expectNoStop(t, "allowed_peers change")
	if rec.startCount("wx-1") != 1 {
		t.Fatalf("starts=%d want 1 after allowed_peers change", rec.startCount("wx-1"))
	}

	relogin := peers
	relogin.Generation, relogin.BotToken = 2, "t2"
	mgr.Reconcile([]channel.Channel{peers}, []channel.Channel{relogin})
	rec.expectStop(t, "generation change")
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 2 }, "restart")
	rec.mu.Lock()
	gens := slices.Clone(rec.generations)
	rec.mu.Unlock()
	if !slices.Equal(gens, []int{1, 2}) {
		t.Fatalf("generations=%v want [1 2]", gens)
	}

	disabled := relogin
	disabled.Enabled = false
	mgr.Reconcile([]channel.Channel{relogin}, []channel.Channel{disabled})
	rec.expectStop(t, "disable")
	if states := reporter.statesFor("wx-1"); len(states) == 0 || states[len(states)-1] != "disabled" {
		t.Fatalf("states=%v want trailing disabled", states)
	}
}

func TestWeixinIlinkManager_NotRunnableWithoutTokenOrWhenExpired(t *testing.T) {
	reporter := &fakeStatusReporter{}
	mgr, rec := newTestIlinkManager(t, reporter)

	noToken := ilinkChannel()
	noToken.ID, noToken.BotToken = "wx-none", ""
	expired := ilinkChannel()
	expired.ID, expired.SessionExpired = "wx-exp", true
	wecom := channel.Channel{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"}
	mgr.Reconcile(nil, []channel.Channel{noToken, expired, wecom})
	time.Sleep(50 * time.Millisecond)
	for _, id := range []string{"wx-none", "wx-exp", "bot1"} {
		if n := rec.startCount(id); n != 0 {
			t.Fatalf("%s starts=%d want 0", id, n)
		}
	}

	ch := ilinkChannel()
	mgr.Reconcile(nil, []channel.Channel{ch})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 1 }, "start")
	nowExpired := ch
	nowExpired.SessionExpired = true
	mgr.Reconcile([]channel.Channel{ch}, []channel.Channel{nowExpired})
	rec.expectStop(t, "session expired")
	if slices.Contains(reporter.statesFor("wx-1"), "disabled") {
		t.Fatalf("session expiry must not report disabled: %v", reporter.statesFor("wx-1"))
	}

	relogin := ch
	relogin.Generation = 2
	mgr.Reconcile([]channel.Channel{nowExpired}, []channel.Channel{relogin})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 2 }, "start after re-login")
}

func TestIlinkConnectionChanged(t *testing.T) {
	base := ilinkChannel()
	if adapter.IlinkConnectionChanged(base, base) {
		t.Fatal("identical should not change")
	}
	for name, mutate := range map[string]func(*channel.Channel){
		"bot_token":  func(c *channel.Channel) { c.BotToken = "x" },
		"base_url":   func(c *channel.Channel) { c.BaseURL = "https://other.weixin.qq.com" },
		"generation": func(c *channel.Channel) { c.Generation++ },
	} {
		other := base
		mutate(&other)
		if !adapter.IlinkConnectionChanged(base, other) {
			t.Fatalf("%s change should restart", name)
		}
	}
	other := base
	other.AllowedPeers = []string{"z"}
	if adapter.IlinkConnectionChanged(base, other) {
		t.Fatal("allowed_peers change must not restart")
	}
}

func TestWeixinIlinkManager_SuspendedRunnerNotRespawned(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.push(`{"ret":-14}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := adapter.NewWeixinIlinkManager(ctx, f.deps)
	t.Cleanup(func() { mgr.Reconcile([]channel.Channel{f.ch}, nil) })

	mgr.Reconcile(nil, []channel.Channel{f.ch})
	waitFor(t, 2*time.Second, func() bool { _, _, exp := f.portal.counts(); return exp == 1 }, "session-expired report")
	for range 3 {
		mgr.Reconcile([]channel.Channel{f.ch}, []channel.Channel{f.ch})
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, exp := f.portal.counts(); f.ilink.pollCount() != 1 || exp != 1 {
		t.Fatalf("polls=%d expired=%d want 1/1 (runner must stay suspended)", f.ilink.pollCount(), exp)
	}

	expired := f.ch
	expired.SessionExpired = true
	mgr.Reconcile([]channel.Channel{f.ch}, []channel.Channel{expired})
	time.Sleep(50 * time.Millisecond)
	if f.ilink.pollCount() != 1 {
		t.Fatalf("polls=%d after expired sync want 1", f.ilink.pollCount())
	}
}
