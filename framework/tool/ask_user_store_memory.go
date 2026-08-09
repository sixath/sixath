package tool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PendingInputRequest 表示一次待用户填写的输入请求。
type PendingInputRequest struct {
	RequestID         string    `json:"request_id"`
	Token             string    `json:"token"`
	SessionID         string    `json:"session_id"`
	ToolCallID        string    `json:"tool_call_id"`
	ReasoningContent  string    `json:"reasoning_content,omitempty"` // thinking 模式回放必需
	Kind              string    `json:"kind"`
	Field             string    `json:"field"`
	Prompt            string    `json:"prompt"`
	Title             string    `json:"title"`
	Options           []string  `json:"options,omitempty"`
	Required          bool      `json:"required"`
	CreatedAt         time.Time `json:"created_at"`
}

// AskUserPendingStore 抽象会话内 pending 输入请求的存取。
type AskUserPendingStore interface {
	SavePending(ctx context.Context, sessionID string, p PendingInputRequest) error
	GetPending(ctx context.Context, sessionID, token string) (*PendingInputRequest, error)
	DeletePending(ctx context.Context, sessionID, token string) error
}

// AskUserFulfillmentStore 会话内短期 secret 槽；禁止写入 chat_messages。
type AskUserFulfillmentStore interface {
	PutSecret(ctx context.Context, sessionID, field, value string, ttl time.Duration) error
	GetSecret(ctx context.Context, sessionID, field string) (string, error)
	DeleteSecret(ctx context.Context, sessionID, field string) error
}

var ErrAskUserSecretNotFound = errors.New("ask_user: secret not found")

type pendingEntry struct {
	req     PendingInputRequest
	expires time.Time
}

type secretEntry struct {
	value   string
	expires time.Time
}

// InMemoryAskUserPendingStore 进程内 pending 存储，供 dev/test 与单实例 portal 使用。
type InMemoryAskUserPendingStore struct {
	mu    sync.RWMutex
	items map[string]pendingEntry
}

func NewInMemoryAskUserPendingStore() *InMemoryAskUserPendingStore {
	return &InMemoryAskUserPendingStore{
		items: make(map[string]pendingEntry),
	}
}

func pendingKey(sessionID, token string) string {
	return sessionID + ":" + token
}

func (s *InMemoryAskUserPendingStore) SavePending(_ context.Context, sessionID string, p PendingInputRequest) error {
	if sessionID == "" || p.Token == "" {
		return errors.New("ask_user: session_id and token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 同 session + field 的新 pending 使旧 token 失效（spec Q2）。
	if p.Field != "" {
		for k, e := range s.items {
			if e.req.SessionID == sessionID && e.req.Field == p.Field && e.req.Token != p.Token {
				delete(s.items, k)
			}
		}
	}
	s.items[pendingKey(sessionID, p.Token)] = pendingEntry{req: p}
	return nil
}

func (s *InMemoryAskUserPendingStore) GetPending(_ context.Context, sessionID, token string) (*PendingInputRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.items[pendingKey(sessionID, token)]
	if !ok {
		return nil, nil
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		return nil, nil
	}
	cp := e.req
	return &cp, nil
}

func (s *InMemoryAskUserPendingStore) DeletePending(_ context.Context, sessionID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, pendingKey(sessionID, token))
	return nil
}

// InMemoryAskUserFulfillmentStore 进程内 secret 存储，带 TTL。
type InMemoryAskUserFulfillmentStore struct {
	mu    sync.RWMutex
	items map[string]secretEntry
}

func NewInMemoryAskUserFulfillmentStore() *InMemoryAskUserFulfillmentStore {
	return &InMemoryAskUserFulfillmentStore{
		items: make(map[string]secretEntry),
	}
}

func secretKey(sessionID, field string) string {
	return sessionID + ":" + field
}

func (s *InMemoryAskUserFulfillmentStore) PutSecret(_ context.Context, sessionID, field, value string, ttl time.Duration) error {
	if sessionID == "" || field == "" {
		return errors.New("ask_user: session_id and field required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var expires time.Time
	if ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	s.items[secretKey(sessionID, field)] = secretEntry{value: value, expires: expires}
	return nil
}

func (s *InMemoryAskUserFulfillmentStore) GetSecret(_ context.Context, sessionID, field string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.items[secretKey(sessionID, field)]
	if !ok {
		return "", ErrAskUserSecretNotFound
	}
	if !e.expires.IsZero() && time.Now().After(e.expires) {
		return "", ErrAskUserSecretNotFound
	}
	return e.value, nil
}

func (s *InMemoryAskUserFulfillmentStore) DeleteSecret(_ context.Context, sessionID, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, secretKey(sessionID, field))
	return nil
}
