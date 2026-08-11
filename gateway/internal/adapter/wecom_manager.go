package adapter

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/runtimeclient"
)

// StatusReporter posts wecom_bot runtime status to Portal.
type StatusReporter interface {
	ReportChannelStatus(ctx context.Context, channelID string, body runtimeclient.StatusBody) error
}

type wecomLoopFunc func(ctx context.Context, ch channel.Channel, deps WecomBotDeps)

type runnerHandle struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

// Drain timeout waiting for a canceled runner before starting another (same bot_id safety).
const runnerDrainTimeout = 15 * time.Second

// WecomBotManager starts/stops per-channel wecom_bot runners based on config diffs.
type WecomBotManager struct {
	parent context.Context
	deps   WecomBotDeps

	mu           sync.Mutex
	active       map[string]*runnerHandle
	loopFn       wecomLoopFunc
	drainTimeout time.Duration
}

// NewWecomBotManager builds a manager bound to parent ctx (process lifetime).
func NewWecomBotManager(parent context.Context, deps WecomBotDeps) *WecomBotManager {
	deps = normalizeWecomBotDeps(deps)
	return &WecomBotManager{
		parent:       parent,
		deps:         deps,
		active:       make(map[string]*runnerHandle),
		loopFn:       runWecomBotLoop,
		drainTimeout: runnerDrainTimeout,
	}
}

// SetLoopForTest replaces the runner loop (tests only).
func (m *WecomBotManager) SetLoopForTest(fn func(ctx context.Context, ch channel.Channel, deps WecomBotDeps)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.loopFn = fn
	m.mu.Unlock()
}

// SetDrainTimeoutForTest overrides how long stop/restart waits for the old goroutine.
func (m *WecomBotManager) SetDrainTimeoutForTest(d time.Duration) {
	if m == nil || d <= 0 {
		return
	}
	m.mu.Lock()
	m.drainTimeout = d
	m.mu.Unlock()
}

// Reconcile applies spec §5.2 wecom_bot start/stop/restart against prev vs next snapshots.
func (m *WecomBotManager) Reconcile(prev, next []channel.Channel) {
	if m == nil {
		return
	}
	prevMap := indexChannels(prev)
	nextMap := indexChannels(next)

	for id, pch := range prevMap {
		if pch.Type != "wecom_bot" {
			continue
		}
		nch, ok := nextMap[id]
		if !ok || nch.Type != "wecom_bot" {
			m.stop(id, false)
			continue
		}
		if !nch.Enabled {
			if pch.Enabled || m.isRunning(id) {
				m.stop(id, true)
			}
			continue
		}
		// enabled in next
		if !pch.Enabled || !m.isRunning(id) {
			m.start(nch)
			continue
		}
		if ConnectionConfigChanged(pch, nch) {
			m.restart(nch)
		}
	}

	for id, nch := range nextMap {
		if nch.Type != "wecom_bot" || !nch.Enabled {
			continue
		}
		if _, ok := prevMap[id]; ok {
			continue
		}
		m.start(nch)
	}
}

// ConnectionConfigChanged reports whether connection-affecting fields differ.
func ConnectionConfigChanged(a, b channel.Channel) bool {
	if a.BotID != b.BotID || a.Secret != b.Secret || a.WSURL != b.WSURL ||
		a.CorpID != b.CorpID || a.CorpSecret != b.CorpSecret {
		return true
	}
	if len(a.BotNames) != len(b.BotNames) {
		return true
	}
	for i := range a.BotNames {
		if a.BotNames[i] != b.BotNames[i] {
			return true
		}
	}
	return false
}

func indexChannels(chs []channel.Channel) map[string]channel.Channel {
	out := make(map[string]channel.Channel, len(chs))
	for _, ch := range chs {
		if ch.ID == "" {
			continue
		}
		out[ch.ID] = ch
	}
	return out
}

func (m *WecomBotManager) isRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[id]
	return ok
}

func (m *WecomBotManager) start(ch channel.Channel) {
	// Always drain any prior runner for this channel_id before dialing again.
	m.stop(ch.ID, false)

	m.mu.Lock()
	ctx, cancel := context.WithCancel(m.parent)
	done := make(chan struct{})
	m.active[ch.ID] = &runnerHandle{cancel: cancel, done: done}
	loop := m.loopFn
	deps := m.deps
	m.mu.Unlock()

	log.Printf("wecom_bot %s: starting runner bot_id=%s", ch.ID, ch.BotID)
	go func() {
		defer close(done)
		loop(ctx, ch, deps)
	}()
}

func (m *WecomBotManager) stop(id string, reportDisabled bool) {
	m.mu.Lock()
	h, ok := m.active[id]
	if ok {
		delete(m.active, id)
	}
	deps := m.deps
	timeout := m.drainTimeout
	m.mu.Unlock()

	if ok && h != nil {
		log.Printf("wecom_bot %s: stopping runner", id)
		h.cancel()
		m.waitDrain(id, h.done, timeout)
	}
	if reportDisabled {
		reportChannelStatus(deps, id, runtimeclient.StatusBody{State: "disabled"})
	}
}

func (m *WecomBotManager) waitDrain(id string, done <-chan struct{}, timeout time.Duration) {
	if done == nil {
		return
	}
	if timeout <= 0 {
		timeout = runnerDrainTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Printf("wecom_bot %s: timed out waiting for runner to exit after %s", id, timeout)
	}
}

func (m *WecomBotManager) restart(ch channel.Channel) {
	m.stop(ch.ID, false)
	m.start(ch)
}

func normalizeWecomBotDeps(deps WecomBotDeps) WecomBotDeps {
	if deps.TurnTimeout <= 0 {
		deps.TurnTimeout = 120 * time.Second
	}
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	return deps
}
