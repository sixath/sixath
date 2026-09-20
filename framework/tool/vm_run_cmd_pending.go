package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PendingVMRunCmd holds a danger-matched vm_run_cmd awaiting user confirm_token.
type PendingVMRunCmd struct {
	Token      string
	Command    string
	Host       string
	VMID       int64
	Port       int
	TimeoutSec int
	CreatedAt  time.Time
}

// VMRunCmdPendingStore persists propose-phase vm_run_cmd confirmations per session.
type VMRunCmdPendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingVMRunCmd) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingVMRunCmd, error)
	DeletePending(ctx context.Context, sessionID, token string) error
}

// InMemoryVMRunCmdPendingStore is a process-local pending store for tests and single-node Portal.
type InMemoryVMRunCmdPendingStore struct {
	mu   sync.Mutex
	data map[string]PendingVMRunCmd
}

// NewInMemoryVMRunCmdPendingStore creates an empty in-memory store.
func NewInMemoryVMRunCmdPendingStore() *InMemoryVMRunCmdPendingStore {
	return &InMemoryVMRunCmdPendingStore{data: make(map[string]PendingVMRunCmd)}
}

func (s *InMemoryVMRunCmdPendingStore) key(sessionID, token string) string {
	return sessionID + "\x00" + token
}

func (s *InMemoryVMRunCmdPendingStore) SavePending(_ context.Context, sessionID string, p PendingVMRunCmd) error {
	if sessionID == "" || p.Token == "" {
		return errors.New("vm_run_cmd: session_id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(sessionID, p.Token)] = p
	return nil
}

func (s *InMemoryVMRunCmdPendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingVMRunCmd, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[s.key(sessionID, token)]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (s *InMemoryVMRunCmdPendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, s.key(sessionID, token))
	return nil
}
