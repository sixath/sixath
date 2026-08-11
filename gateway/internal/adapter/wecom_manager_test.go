package adapter_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/runtimeclient"
)

type fakeStatusReporter struct {
	mu    sync.Mutex
	calls []statusCall
}

type statusCall struct {
	channelID string
	body      runtimeclient.StatusBody
}

func (f *fakeStatusReporter) ReportChannelStatus(_ context.Context, channelID string, body runtimeclient.StatusBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, statusCall{channelID: channelID, body: body})
	return nil
}

func (f *fakeStatusReporter) statesFor(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.channelID == id {
			out = append(out, c.body.State)
		}
	}
	return out
}

func TestWecomBotManager_ReconcileStartStopRestart(t *testing.T) {
	var mu sync.Mutex
	started := map[string]int{}
	stopped := make(chan string, 16)

	reporter := &fakeStatusReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := adapter.NewWecomBotManager(ctx, adapter.WecomBotDeps{
		Reporter: reporter,
	})
	mgr.SetLoopForTest(func(runCtx context.Context, ch channel.Channel, _ adapter.WecomBotDeps) {
		mu.Lock()
		started[ch.ID]++
		mu.Unlock()
		<-runCtx.Done()
		stopped <- ch.ID
	})

	bot := channel.Channel{
		ID: "bot1", Type: "wecom_bot", Enabled: true,
		BotID: "B1", Secret: "S1", WSURL: "wss://x",
	}
	mgr.Reconcile(nil, []channel.Channel{bot})

	waitStarted := func(id string, n int) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			mu.Lock()
			got := started[id]
			mu.Unlock()
			if got >= n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("start %s count=%d want >=%d", id, got, n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitStarted("bot1", 1)

	// enabled → disabled: stop + report disabled; keep in registry is caller's job
	disabled := bot
	disabled.Enabled = false
	mgr.Reconcile([]channel.Channel{bot}, []channel.Channel{disabled})
	select {
	case id := <-stopped:
		if id != "bot1" {
			t.Fatalf("stopped %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("expected stop on disable")
	}
	states := reporter.statesFor("bot1")
	if len(states) == 0 || states[len(states)-1] != "disabled" {
		t.Fatalf("states=%v want trailing disabled", states)
	}

	// disabled → enabled: start again
	mgr.Reconcile([]channel.Channel{disabled}, []channel.Channel{bot})
	waitStarted("bot1", 2)

	// connection field change while enabled: stop then start
	changed := bot
	changed.Secret = "S2"
	mgr.Reconcile([]channel.Channel{bot}, []channel.Channel{changed})
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("expected stop on secret change")
	}
	waitStarted("bot1", 3)

	// channel disappears: stop, no disabled report required (status reports stop)
	before := len(reporter.statesFor("bot1"))
	mgr.Reconcile([]channel.Channel{changed}, nil)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("expected stop on delete")
	}
	after := reporter.statesFor("bot1")
	if len(after) > before && after[len(after)-1] == "disabled" {
		t.Fatalf("delete should not report disabled; states=%v", after)
	}
}

func TestWecomBotManager_RestartDrainsBeforeStart(t *testing.T) {
	var mu sync.Mutex
	oldExited := false
	overlap := false
	generation := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := adapter.NewWecomBotManager(ctx, adapter.WecomBotDeps{})
	mgr.SetDrainTimeoutForTest(2 * time.Second)
	mgr.SetLoopForTest(func(runCtx context.Context, _ channel.Channel, _ adapter.WecomBotDeps) {
		mu.Lock()
		generation++
		gen := generation
		mu.Unlock()

		if gen == 1 {
			<-runCtx.Done()
			time.Sleep(40 * time.Millisecond) // slow exit to expose races without drain
			mu.Lock()
			oldExited = true
			mu.Unlock()
			return
		}
		mu.Lock()
		if !oldExited {
			overlap = true
		}
		mu.Unlock()
		<-runCtx.Done()
	})

	bot := channel.Channel{
		ID: "bot1", Type: "wecom_bot", Enabled: true,
		BotID: "B1", Secret: "S1",
	}
	mgr.Reconcile(nil, []channel.Channel{bot})

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		g := generation
		mu.Unlock()
		if g >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first runner never started")
		}
		time.Sleep(5 * time.Millisecond)
	}

	changed := bot
	changed.Secret = "S2"
	// Reconcile/restart must block until gen1 exits before starting gen2.
	mgr.Reconcile([]channel.Channel{bot}, []channel.Channel{changed})

	deadline = time.Now().Add(time.Second)
	for {
		mu.Lock()
		g := generation
		exited := oldExited
		hadOverlap := overlap
		mu.Unlock()
		if g >= 2 {
			if !exited {
				t.Fatal("expected old runner to have exited before new runner started")
			}
			if hadOverlap {
				t.Fatal("new runner started before old runner finished (double-connect risk)")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected restart; generation=%d oldExited=%v overlap=%v", g, exited, hadOverlap)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWecomBotManager_ReportsDisabledOnEnableToDisable(t *testing.T) {
	reporter := &fakeStatusReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := adapter.NewWecomBotManager(ctx, adapter.WecomBotDeps{Reporter: reporter})
	mgr.SetLoopForTest(func(runCtx context.Context, _ channel.Channel, _ adapter.WecomBotDeps) {
		<-runCtx.Done()
	})

	bot := channel.Channel{
		ID: "bot1", Type: "wecom_bot", Enabled: true,
		BotID: "B1", Secret: "S1",
	}
	mgr.Reconcile(nil, []channel.Channel{bot})
	time.Sleep(20 * time.Millisecond)

	disabled := bot
	disabled.Enabled = false
	mgr.Reconcile([]channel.Channel{bot}, []channel.Channel{disabled})

	states := reporter.statesFor("bot1")
	if len(states) != 1 || states[0] != "disabled" {
		t.Fatalf("states=%v want [disabled]", states)
	}
}

func TestWecomBotManager_IgnoresWebhook(t *testing.T) {
	var mu sync.Mutex
	started := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := adapter.NewWecomBotManager(ctx, adapter.WecomBotDeps{})
	mgr.SetLoopForTest(func(context.Context, channel.Channel, adapter.WecomBotDeps) {
		mu.Lock()
		started++
		mu.Unlock()
	})
	wh := channel.Channel{ID: "wh", Type: "webhook", Enabled: true}
	mgr.Reconcile(nil, []channel.Channel{wh})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := started
	mu.Unlock()
	if n != 0 {
		t.Fatalf("webhook should not start runner; started=%d", n)
	}
}

func TestConnectionConfigChanged(t *testing.T) {
	base := channel.Channel{
		ID: "b", Type: "wecom_bot", Enabled: true,
		BotID: "1", Secret: "s", WSURL: "wss://a",
		CorpID: "c", CorpSecret: "cs", BotNames: []string{"n"},
	}
	if adapter.ConnectionConfigChanged(base, base) {
		t.Fatal("identical should not change")
	}
	other := base
	other.Secret = "x"
	if !adapter.ConnectionConfigChanged(base, other) {
		t.Fatal("secret change")
	}
	other = base
	other.BotNames = []string{"n", "m"}
	if !adapter.ConnectionConfigChanged(base, other) {
		t.Fatal("bot_names change")
	}
}
