package growth

import (
	"sync"
	"time"
)

// RuntimeWriteLease is a short-lived in-process lease for runtime skill_manage writes.
type RuntimeWriteLease struct {
	mu      sync.Mutex
	entries map[string]leaseEntry
}

type leaseEntry struct {
	holder    string
	expiresAt time.Time
}

// DefaultRuntimeWriteLease is shared by runtime skill_manage (process-local).
var DefaultRuntimeWriteLease = NewRuntimeWriteLease()

// NewRuntimeWriteLease returns an empty lease map.
func NewRuntimeWriteLease() *RuntimeWriteLease {
	return &RuntimeWriteLease{entries: make(map[string]leaseEntry)}
}

// TryAcquire attempts to acquire a write lease for workspace. retryAfterSec is set when not acquired.
func (l *RuntimeWriteLease) TryAcquire(workspace, holder string, ttl time.Duration) (acquired bool, retryAfterSec int) {
	if l == nil || workspace == "" || holder == "" {
		return true, 0
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[workspace]; ok {
		if now.Before(e.expiresAt) && e.holder != holder {
			sec := int(e.expiresAt.Sub(now).Seconds())
			if sec < 1 {
				sec = 1
			}
			return false, sec
		}
	}
	l.entries[workspace] = leaseEntry{holder: holder, expiresAt: now.Add(ttl)}
	return true, 0
}

// Release releases the lease when held by holder.
func (l *RuntimeWriteLease) Release(workspace, holder string) {
	if l == nil || workspace == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[workspace]
	if !ok || e.holder != holder {
		return
	}
	delete(l.entries, workspace)
}
