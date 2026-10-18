package adapter

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/runtimeclient"
)

const weixinIlinkType = "weixin_ilink"

type weixinIlinkLoopFunc func(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps)

// WeixinIlinkManager starts/stops one long-poll runner per runnable weixin_ilink channel.
// A runner that hit -14 or a generation conflict suspends in place, so it stays in active
// and Reconcile does not respawn it until the channel config changes.
type WeixinIlinkManager struct {
	parent context.Context
	deps   WeixinIlinkDeps

	mu           sync.Mutex
	active       map[string]*runnerHandle
	loopFn       weixinIlinkLoopFunc
	drainTimeout time.Duration
}

// NewWeixinIlinkManager builds a manager bound to parent ctx (process lifetime).
func NewWeixinIlinkManager(parent context.Context, deps WeixinIlinkDeps) *WeixinIlinkManager {
	return &WeixinIlinkManager{
		parent:       parent,
		deps:         normalizeWeixinIlinkDeps(deps),
		active:       make(map[string]*runnerHandle),
		loopFn:       RunWeixinIlink,
		drainTimeout: runnerDrainTimeout,
	}
}

// SetLoopForTest replaces the runner loop (tests only).
func (m *WeixinIlinkManager) SetLoopForTest(fn func(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.loopFn = fn
	m.mu.Unlock()
}

// SetDrainTimeoutForTest overrides how long stop/restart waits for the old goroutine.
func (m *WeixinIlinkManager) SetDrainTimeoutForTest(d time.Duration) {
	if m == nil || d <= 0 {
		return
	}
	m.mu.Lock()
	m.drainTimeout = d
	m.mu.Unlock()
}

func ilinkRunnable(ch channel.Channel) bool {
	return ch.Type == weixinIlinkType && ch.Enabled && ch.BotToken != "" && !ch.SessionExpired
}

// IlinkConnectionChanged reports whether a running weixin_ilink runner must restart.
// AllowedPeers is deliberately excluded: the runner re-reads it from the Registry per message.
func IlinkConnectionChanged(a, b channel.Channel) bool {
	return a.BotToken != b.BotToken || a.BaseURL != b.BaseURL || a.Generation != b.Generation
}

// Reconcile applies weixin_ilink start/stop/restart against prev vs next snapshots.
func (m *WeixinIlinkManager) Reconcile(prev, next []channel.Channel) {
	if m == nil {
		return
	}
	prevMap := indexChannels(prev)
	nextMap := indexChannels(next)

	for id, pch := range prevMap {
		if pch.Type != weixinIlinkType {
			continue
		}
		nch, ok := nextMap[id]
		if !ok || nch.Type != weixinIlinkType {
			m.stop(id, false)
			continue
		}
		if !nch.Enabled {
			if pch.Enabled || m.isRunning(id) {
				m.stop(id, true)
			}
			continue
		}
		if !ilinkRunnable(nch) {
			// Missing token / session expired: Portal derives login_required, so do not report disabled.
			m.stop(id, false)
			continue
		}
		if !m.isRunning(id) {
			m.start(nch)
			continue
		}
		if IlinkConnectionChanged(pch, nch) {
			m.restart(nch)
		}
	}

	for id, nch := range nextMap {
		if !ilinkRunnable(nch) {
			continue
		}
		if _, ok := prevMap[id]; ok {
			continue
		}
		m.start(nch)
	}
}

func (m *WeixinIlinkManager) isRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[id]
	return ok
}

func (m *WeixinIlinkManager) start(ch channel.Channel) {
	m.stop(ch.ID, false)

	m.mu.Lock()
	ctx, cancel := context.WithCancel(m.parent)
	done := make(chan struct{})
	m.active[ch.ID] = &runnerHandle{cancel: cancel, done: done}
	loop := m.loopFn
	deps := m.deps
	m.mu.Unlock()

	log.Printf("weixin_ilink %s: starting runner generation=%d", ch.ID, ch.Generation)
	go func() {
		defer close(done)
		loop(ctx, ch, deps)
	}()
}

func (m *WeixinIlinkManager) stop(id string, reportDisabled bool) {
	m.mu.Lock()
	h, ok := m.active[id]
	if ok {
		delete(m.active, id)
	}
	deps := m.deps
	timeout := m.drainTimeout
	m.mu.Unlock()

	if ok && h != nil {
		log.Printf("weixin_ilink %s: stopping runner", id)
		h.cancel()
		waitRunnerDrain("weixin_ilink", id, h.done, timeout)
	}
	if reportDisabled {
		reportIlinkStatus(deps, id, runtimeclient.StatusBody{State: "disabled"})
	}
}

func (m *WeixinIlinkManager) restart(ch channel.Channel) {
	m.stop(ch.ID, false)
	m.start(ch)
}

func waitRunnerDrain(kind, id string, done <-chan struct{}, timeout time.Duration) {
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
		log.Printf("%s %s: timed out waiting for runner to exit after %s", kind, id, timeout)
	}
}
