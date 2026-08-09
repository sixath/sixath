package memory

import (
	"context"
	"strings"
	"sync"
)

// EpisodeLocalBuffer holds per-episode scratch (failure signals / notes).
// It must never be used as a MemoryStore backend — Clear discards everything.
type EpisodeLocalBuffer struct {
	key string
	mu  sync.Mutex
	sigs []FailureSignal
	notes []string
}

// NewEpisodeLocalBuffer creates a buffer keyed by session (or other episode id).
func NewEpisodeLocalBuffer(episodeKey string) *EpisodeLocalBuffer {
	return &EpisodeLocalBuffer{key: strings.TrimSpace(episodeKey)}
}

func (b *EpisodeLocalBuffer) Key() string {
	if b == nil {
		return ""
	}
	return b.key
}

func (b *EpisodeLocalBuffer) PutSignal(sig FailureSignal) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sigs = append(b.sigs, sig)
}

func (b *EpisodeLocalBuffer) PutNote(note string) {
	if b == nil {
		return
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notes = append(b.notes, note)
}

func (b *EpisodeLocalBuffer) Signals() []FailureSignal {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]FailureSignal, len(b.sigs))
	copy(out, b.sigs)
	return out
}

func (b *EpisodeLocalBuffer) Notes() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.notes))
	copy(out, b.notes)
	return out
}

// Clear drops all episode-local state (call when the task/turn instance ends).
func (b *EpisodeLocalBuffer) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sigs = nil
	b.notes = nil
}

// EpisodeLocalFailureSink appends FailureSignals into an EpisodeLocalBuffer.
type EpisodeLocalFailureSink struct {
	Buffer *EpisodeLocalBuffer
}

func (s EpisodeLocalFailureSink) OnFailureSignal(_ context.Context, sig FailureSignal) {
	if s.Buffer == nil {
		return
	}
	s.Buffer.PutSignal(sig)
}
