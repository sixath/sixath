package adapter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/runtimeclient"
)

type recordingReporter struct {
	mu    sync.Mutex
	calls []statusCall
}

type statusCall struct {
	channelID string
	body      runtimeclient.StatusBody
}

func (f *recordingReporter) ReportChannelStatus(_ context.Context, channelID string, body runtimeclient.StatusBody) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// copy pointer fields so later mutations don't affect assertions
	cp := body
	if body.LastError != nil {
		s := *body.LastError
		cp.LastError = &s
	}
	if body.ReconnectAttempt != nil {
		n := *body.ReconnectAttempt
		cp.ReconnectAttempt = &n
	}
	if body.ReconnectInMs != nil {
		n := *body.ReconnectInMs
		cp.ReconnectInMs = &n
	}
	f.calls = append(f.calls, statusCall{channelID: channelID, body: cp})
	return nil
}

func (f *recordingReporter) snapshot() []statusCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]statusCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func waitStatus(t *testing.T, r *recordingReporter, pred func([]statusCall) bool) []statusCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		calls := r.snapshot()
		if pred(calls) {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for status; calls=%+v", calls)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunWecomBotLoop_ReportsReconnectingBeforeDial(t *testing.T) {
	rep := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sawDial := make(chan struct{})
	ch := channel.Channel{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "B", Secret: "S"}
	deps := WecomBotDeps{
		Reporter: rep,
		RunOnce: func(runCtx context.Context, _ channel.Channel, _ WecomBotDeps) error {
			close(sawDial)
			<-runCtx.Done()
			return runCtx.Err()
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWecomBotLoop(ctx, ch, deps)
	}()

	select {
	case <-sawDial:
	case <-time.After(2 * time.Second):
		t.Fatal("dial never started")
	}

	calls := waitStatus(t, rep, func(calls []statusCall) bool {
		return len(calls) >= 1 && calls[0].body.State == "reconnecting"
	})
	if calls[0].body.ReconnectAttempt == nil || *calls[0].body.ReconnectAttempt != 0 {
		t.Fatalf("pre-dial attempt=%v want 0", calls[0].body.ReconnectAttempt)
	}
	if calls[0].body.ReconnectInMs == nil || *calls[0].body.ReconnectInMs != 0 {
		t.Fatalf("pre-dial reconnect_in_ms=%v want 0", calls[0].body.ReconnectInMs)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit")
	}
}

func TestRunWecomBotLoop_ReportsDisconnectedThenReconnecting(t *testing.T) {
	rep := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := channel.Channel{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "B", Secret: "S"}
	deps := WecomBotDeps{
		Reporter: rep,
		RunOnce: func(context.Context, channel.Channel, WecomBotDeps) error {
			return errors.New("dial failed: bad secret")
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWecomBotLoop(ctx, ch, deps)
	}()

	calls := waitStatus(t, rep, func(calls []statusCall) bool {
		if len(calls) < 3 {
			return false
		}
		return calls[0].body.State == "reconnecting" &&
			calls[1].body.State == "disconnected" &&
			calls[2].body.State == "reconnecting"
	})

	if calls[1].body.LastError == nil || *calls[1].body.LastError == "" {
		t.Fatalf("disconnected missing last_error: %+v", calls[1].body)
	}
	if calls[2].body.ReconnectAttempt == nil || *calls[2].body.ReconnectAttempt != 1 {
		t.Fatalf("reconnecting attempt=%v want 1", calls[2].body.ReconnectAttempt)
	}
	if calls[2].body.ReconnectInMs == nil || *calls[2].body.ReconnectInMs <= 0 {
		t.Fatalf("reconnecting reconnect_in_ms=%v", calls[2].body.ReconnectInMs)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit")
	}
}

func TestReportWecomConnected_ClearsReconnectFields(t *testing.T) {
	rep := &recordingReporter{}
	deps := WecomBotDeps{Reporter: rep}
	reportWecomConnected(deps, "bot1")

	calls := rep.snapshot()
	if len(calls) != 1 || calls[0].body.State != "connected" {
		t.Fatalf("calls=%+v", calls)
	}
	body := calls[0].body
	if body.LastError == nil || *body.LastError != "" {
		t.Fatalf("last_error=%v want empty string", body.LastError)
	}
	if body.ReconnectAttempt == nil || *body.ReconnectAttempt != 0 {
		t.Fatalf("reconnect_attempt=%v want 0", body.ReconnectAttempt)
	}
	if body.ReconnectInMs == nil || *body.ReconnectInMs != 0 {
		t.Fatalf("reconnect_in_ms=%v want 0", body.ReconnectInMs)
	}
}

func TestRunWecomBotOnce_OnConnectedReportsConnected(t *testing.T) {
	// Drive OnConnected via injectable RunOnce that mirrors production callback.
	rep := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := channel.Channel{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "B", Secret: "S"}
	deps := WecomBotDeps{
		Reporter: rep,
		RunOnce: func(runCtx context.Context, ch channel.Channel, deps WecomBotDeps) error {
			reportWecomConnected(deps, ch.ID)
			<-runCtx.Done()
			return runCtx.Err()
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWecomBotLoop(ctx, ch, deps)
	}()

	waitStatus(t, rep, func(calls []statusCall) bool {
		if len(calls) < 2 {
			return false
		}
		return calls[0].body.State == "reconnecting" && calls[1].body.State == "connected"
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit")
	}
	// cancel during connected should not emit disconnected or a post-cancel reconnecting
	time.Sleep(20 * time.Millisecond)
	calls := rep.snapshot()
	for i, c := range calls {
		if c.body.State == "disconnected" {
			t.Fatalf("unexpected disconnected after cancel: %+v", c.body)
		}
		if i > 0 && c.body.State == "reconnecting" {
			t.Fatalf("unexpected reconnecting after connected: %+v", c.body)
		}
	}
}
