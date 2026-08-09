package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PendingTerminal holds a danger-matched command awaiting user confirm_token.
type PendingTerminal struct {
	Token            string
	Command          string
	Workdir          string // relative workdir param as proposed (may be empty)
	Timeout          int    // seconds; 0 = use default at confirm time
	Background       bool
	NotifyOnComplete bool
	Pattern          string // matched DangerPatterns regex
	CreatedAt        time.Time
}

// TerminalPendingStore persists propose-phase terminal confirmations per session.
type TerminalPendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingTerminal) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingTerminal, error)
	DeletePending(ctx context.Context, sessionID, token string) error
}

// InMemoryTerminalPendingStore is a process-local pending store for tests and single-node Portal.
type InMemoryTerminalPendingStore struct {
	mu   sync.Mutex
	data map[string]PendingTerminal
}

// NewInMemoryTerminalPendingStore creates an empty in-memory store.
func NewInMemoryTerminalPendingStore() *InMemoryTerminalPendingStore {
	return &InMemoryTerminalPendingStore{data: make(map[string]PendingTerminal)}
}

func (s *InMemoryTerminalPendingStore) key(sessionID, token string) string {
	return sessionID + "\x00" + token
}

func (s *InMemoryTerminalPendingStore) SavePending(_ context.Context, sessionID string, p PendingTerminal) error {
	if sessionID == "" || p.Token == "" {
		return errors.New("terminal: session_id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(sessionID, p.Token)] = p
	return nil
}

func (s *InMemoryTerminalPendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingTerminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[s.key(sessionID, token)]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (s *InMemoryTerminalPendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, s.key(sessionID, token))
	return nil
}
