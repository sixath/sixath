package memory

import (
	"context"
	"log/slog"
	"sync"

	"github.com/sixath/framework/events"
)

// FailureSignalSink receives mapped failure signals (P3-A; no MemoryStore write).
type FailureSignalSink interface {
	OnFailureSignal(ctx context.Context, sig FailureSignal)
}

// RecordingFailureSink stores signals for tests.
type RecordingFailureSink struct {
	mu   sync.Mutex
	sigs []FailureSignal
}

func (r *RecordingFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sigs = append(r.sigs, sig)
}

func (r *RecordingFailureSink) Snapshot() []FailureSignal {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FailureSignal, len(r.sigs))
	copy(out, r.sigs)
	return out
}

// LoggingFailureSink logs structured fields (code, agent_id, tool_name, …).
type LoggingFailureSink struct {
	Logger *slog.Logger // nil → slog.Default()
}

func (l LoggingFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("failure_signal",
		"code", sig.Code,
		"agent_id", sig.AgentID,
		"session_id", sig.SessionID,
		"task_family", sig.TaskFamily,
		"tool_name", sig.ToolName,
		"skill_id", sig.SkillID,
		"message", sig.Message,
		"evidence", sig.Evidence,
		"at", sig.At,
	)
}

// MultiFailureSink fans out to multiple sinks.
type MultiFailureSink []FailureSignalSink

func (m MultiFailureSink) OnFailureSignal(ctx context.Context, sig FailureSignal) {
	for _, s := range m {
		if s != nil {
			s.OnFailureSignal(ctx, sig)
		}
	}
}

// RingFailureSink keeps the last N signals for debugging / future P3-E evidence lookup.
type RingFailureSink struct {
	N    int
	mu   sync.Mutex
	ring []FailureSignal
}

func (r *RingFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	if r == nil || r.N <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ring = append(r.ring, sig)
	if len(r.ring) > r.N {
		r.ring = r.ring[len(r.ring)-r.N:]
	}
}

func (r *RingFailureSink) Snapshot() []FailureSignal {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FailureSignal, len(r.ring))
	copy(out, r.ring)
	return out
}

// AttachFailureSignalBridge registers a sync subscriber on bus.
// Safe with nil bus or nil sink (no-op). Multiple attaches = multiple subscribers
// (Portal attaches once per turnBus instance).
func AttachFailureSignalBridge(bus *events.Bus, sink FailureSignalSink) {
	if bus == nil || sink == nil {
		return
	}
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		sig, ok := FailureSignalFromEvent(ctx, e)
		if !ok {
			return
		}
		sink.OnFailureSignal(ctx, sig)
	})
}
