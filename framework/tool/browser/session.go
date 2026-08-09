package browser

import (
	"context"
	"fmt"
	"sync"
)

// SessionStore holds one Backend per chat session_id.
type SessionStore struct {
	sessions sync.Map // sessionID -> Backend
}

// NewSessionStore returns an empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{}
}

// GetOrCreate returns the existing backend for sessionID or creates one via factory.
func (s *SessionStore) GetOrCreate(sessionID string, factory func() (Backend, error)) (Backend, error) {
	if v, ok := s.sessions.Load(sessionID); ok {
		return v.(Backend), nil
	}
	b, err := factory()
	if err != nil {
		return nil, err
	}
	actual, loaded := s.sessions.LoadOrStore(sessionID, b)
	if loaded {
		_ = b.Close(context.Background())
		return actual.(Backend), nil
	}
	return b, nil
}

// Close closes and removes the backend for sessionID.
func (s *SessionStore) Close(sessionID string) error {
	v, ok := s.sessions.LoadAndDelete(sessionID)
	if !ok {
		return nil
	}
	if err := v.(Backend).Close(context.Background()); err != nil {
		return fmt.Errorf("close browser session %q: %w", sessionID, err)
	}
	return nil
}

// CloseAll closes and removes every stored backend.
func (s *SessionStore) CloseAll() error {
	var firstErr error
	s.sessions.Range(func(key, value any) bool {
		s.sessions.Delete(key)
		if err := value.(Backend).Close(context.Background()); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close browser session %v: %w", key, err)
		}
		return true
	})
	return firstErr
}
