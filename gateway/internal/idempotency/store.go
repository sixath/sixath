package idempotency

import (
	"sync"
	"time"
)

// Status of an idempotency entry.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// Entry holds the first-seen correlation and optional final result.
type Entry struct {
	CorrelationID string
	Status        Status
	Result        any
	ExpiresAt     time.Time
}

// Store is an in-process idempotency map with TTL.
type Store struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]Entry
}

// NewStore creates a store. ttl <= 0 defaults to 10 minutes.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Store{
		ttl: ttl,
		m:   make(map[string]Entry),
	}
}

// Begin reserves a key. ok=false means the key already exists (entry returned).
func (s *Store) Begin(key, correlationID string) (entry Entry, ok bool) {
	if key == "" {
		return Entry{CorrelationID: correlationID, Status: StatusInProgress}, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	if e, exists := s.m[key]; exists {
		return e, false
	}
	e := Entry{
		CorrelationID: correlationID,
		Status:        StatusInProgress,
		ExpiresAt:     time.Now().Add(s.ttl),
	}
	s.m[key] = e
	return e, true
}

// Complete marks a key done and stores the result payload.
func (s *Store) Complete(key string, result any) {
	if key == "" || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.m[key]
	if !exists {
		return
	}
	e.Status = StatusDone
	e.Result = result
	e.ExpiresAt = time.Now().Add(s.ttl)
	s.m[key] = e
}

// Get returns a non-expired entry.
func (s *Store) Get(key string) (Entry, bool) {
	if key == "" || s == nil {
		return Entry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	e, ok := s.m[key]
	return e, ok
}

func (s *Store) purgeLocked(now time.Time) {
	for k, e := range s.m {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			delete(s.m, k)
		}
	}
}
