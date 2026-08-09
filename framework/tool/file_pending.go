package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PendingWorkspaceFile holds a danger-path write/patch awaiting confirm_token.
type PendingWorkspaceFile struct {
	Token      string
	Action     string // "write_file" | "patch"
	Path       string
	Content    string // write_file
	OldString  string // patch
	NewString  string // patch
	ReplaceAll bool   // patch
	Pattern    string
	CreatedAt  time.Time
}

// WorkspaceFilePendingStore persists propose-phase file confirmations per session.
type WorkspaceFilePendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingWorkspaceFile) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingWorkspaceFile, error)
	DeletePending(ctx context.Context, sessionID, token string) error
}

// InMemoryWorkspaceFilePendingStore is a process-local pending store.
type InMemoryWorkspaceFilePendingStore struct {
	mu   sync.Mutex
	data map[string]PendingWorkspaceFile
}

// NewInMemoryWorkspaceFilePendingStore creates an empty in-memory store.
func NewInMemoryWorkspaceFilePendingStore() *InMemoryWorkspaceFilePendingStore {
	return &InMemoryWorkspaceFilePendingStore{data: make(map[string]PendingWorkspaceFile)}
}

func (s *InMemoryWorkspaceFilePendingStore) key(sessionID, token string) string {
	return sessionID + "\x00" + token
}

func (s *InMemoryWorkspaceFilePendingStore) SavePending(_ context.Context, sessionID string, p PendingWorkspaceFile) error {
	if sessionID == "" || p.Token == "" {
		return errors.New("workspace file: session_id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(sessionID, p.Token)] = p
	return nil
}

func (s *InMemoryWorkspaceFilePendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingWorkspaceFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[s.key(sessionID, token)]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (s *InMemoryWorkspaceFilePendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, s.key(sessionID, token))
	return nil
}

// WorkspaceFileConfig configures danger-path confirm for write_file / patch.
type WorkspaceFileConfig struct {
	PendingStore       WorkspaceFilePendingStore
	TokenGen           TokenGenerator
	DangerPathPatterns []string
	ConfirmTTLSeconds  int
}
