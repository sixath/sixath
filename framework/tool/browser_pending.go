package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PendingBrowser holds a danger-matched browser action awaiting confirm_token.
type PendingBrowser struct {
	Token     string
	Action    string // navigate | click | type
	URL       string
	Ref       string
	Text      string
	CreatedAt time.Time
}

// BrowserPendingStore persists propose-phase browser confirmations per chat session.
type BrowserPendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingBrowser) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingBrowser, error)
	DeletePending(ctx context.Context, sessionID, token string) error
}

// InMemoryBrowserPendingStore is a process-local pending store.
type InMemoryBrowserPendingStore struct {
	mu   sync.Mutex
	data map[string]PendingBrowser
}

// NewInMemoryBrowserPendingStore creates an empty in-memory store.
func NewInMemoryBrowserPendingStore() *InMemoryBrowserPendingStore {
	return &InMemoryBrowserPendingStore{data: make(map[string]PendingBrowser)}
}

func (s *InMemoryBrowserPendingStore) key(sessionID, token string) string {
	return sessionID + "\x00" + token
}

func (s *InMemoryBrowserPendingStore) SavePending(_ context.Context, sessionID string, p PendingBrowser) error {
	if sessionID == "" || p.Token == "" {
		return errors.New("browser: session_id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[s.key(sessionID, p.Token)] = p
	return nil
}

func (s *InMemoryBrowserPendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingBrowser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[s.key(sessionID, token)]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (s *InMemoryBrowserPendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, s.key(sessionID, token))
	return nil
}

// BrowserToolsConfig configures confirm for mutate browser tools and optional vision LLM.
type BrowserToolsConfig struct {
	PendingStore      BrowserPendingStore
	TokenGen          TokenGenerator
	ConfirmTTLSeconds int
	Vision            VisionAnalyzer // optional; enables LLM analysis in browser_vision
	// ConfirmNavigate when true requires confirm_token for browser_navigate.
	// Default false: navigate is treated as read-oriented (URL already validated).
	ConfirmNavigate bool
}

func browserToolsConfigOrDefault(cfg *BrowserToolsConfig) *BrowserToolsConfig {
	c := &BrowserToolsConfig{ConfirmTTLSeconds: 300}
	if cfg != nil {
		if cfg.PendingStore != nil {
			c.PendingStore = cfg.PendingStore
		}
		if cfg.TokenGen != nil {
			c.TokenGen = cfg.TokenGen
		}
		if cfg.ConfirmTTLSeconds > 0 {
			c.ConfirmTTLSeconds = cfg.ConfirmTTLSeconds
		}
		c.Vision = cfg.Vision
		c.ConfirmNavigate = cfg.ConfirmNavigate
	}
	return c
}

func (c *BrowserToolsConfig) confirmEnabled() bool {
	return c != nil && c.PendingStore != nil && c.TokenGen != nil
}

func (c *BrowserToolsConfig) confirmNavigateEnabled() bool {
	return c.confirmEnabled() && c.ConfirmNavigate
}
