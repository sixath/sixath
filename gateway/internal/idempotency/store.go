package idempotency

import (
	"context"
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

// Store 幂等存储接口。部署形态已确认为单副本（见 docs/adr/0002-gateway-single-replica-lease.md），
// 因此 MemoryStore 即生产实现；若未来多副本，替换为 Redis 实现（SETNX + EXPIRE，drop-in 满足本接口即可）。
type Store interface {
	// Begin 预留 key；reused=true 表示 key 已存在并返回既有 entry（调用方应据此去重）。
	Begin(ctx context.Context, key, correlationID string) (entry Entry, reused bool, err error)
	// Complete 标记 key 完成并保存结果。
	Complete(ctx context.Context, key string, result any) error
	// Get 读取（不创建）一个未过期的 entry。
	Get(ctx context.Context, key string) (Entry, bool, error)
}

// MemoryStore 进程内幂等存储（TTL 过期清理）。
type MemoryStore struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]Entry
}

// NewStore 创建内存幂等存储；ttl<=0 默认 10 分钟。
func NewStore(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &MemoryStore{ttl: ttl, m: make(map[string]Entry)}
}

var _ Store = (*MemoryStore)(nil)

func (s *MemoryStore) Begin(_ context.Context, key, correlationID string) (Entry, bool, error) {
	if key == "" {
		return Entry{CorrelationID: correlationID, Status: StatusInProgress}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	if e, exists := s.m[key]; exists {
		return e, true, nil
	}
	e := Entry{
		CorrelationID: correlationID,
		Status:        StatusInProgress,
		ExpiresAt:     time.Now().Add(s.ttl),
	}
	s.m[key] = e
	return e, false, nil
}

func (s *MemoryStore) Complete(_ context.Context, key string, result any) error {
	if key == "" || s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.m[key]
	if !exists {
		return nil
	}
	e.Status = StatusDone
	e.Result = result
	e.ExpiresAt = time.Now().Add(s.ttl)
	s.m[key] = e
	return nil
}

func (s *MemoryStore) Get(_ context.Context, key string) (Entry, bool, error) {
	if key == "" || s == nil {
		return Entry{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now())
	e, ok := s.m[key]
	return e, ok, nil
}

func (s *MemoryStore) purgeLocked(now time.Time) {
	for k, e := range s.m {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			delete(s.m, k)
		}
	}
}
