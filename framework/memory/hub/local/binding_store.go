package local

import (
	"sync"

	"github.com/sixath/framework/memory/hub"
)

// Binding is one agent↔asset row.
type Binding struct {
	AgentID   string
	AssetKind hub.AssetKind
	AssetID   string
	Priority  int
	Hub       string
	Name      string
	Status    hub.AssetStatus
	OwnerID   string
	Visibility hub.Visibility
	Meta      map[string]any
}

// BindingStore persists agent asset bindings (P0: in-memory).
type BindingStore interface {
	ListByAgent(agentID string) ([]Binding, error)
	Upsert(b Binding) error
	Delete(agentID string, kind hub.AssetKind, assetID string) error
	Get(kind hub.AssetKind, assetID string) (Binding, bool, error)
	UpdateMeta(kind hub.AssetKind, assetID string, vis *hub.Visibility, st *hub.AssetStatus) error
}

// MemoryBindingStore is a process-local BindingStore.
type MemoryBindingStore struct {
	mu   sync.Mutex
	byKey map[string]Binding // kind\0id
	byAgent map[string]map[string]struct{}
}

func NewMemoryBindingStore() *MemoryBindingStore {
	return &MemoryBindingStore{
		byKey:   map[string]Binding{},
		byAgent: map[string]map[string]struct{}{},
	}
}

func bindKey(kind hub.AssetKind, id string) string {
	return string(kind) + "\x00" + id
}

func (s *MemoryBindingStore) ListByAgent(agentID string) ([]Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.byAgent[agentID]
	out := make([]Binding, 0, len(keys))
	for k := range keys {
		if b, ok := s.byKey[k]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *MemoryBindingStore) Upsert(b Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := bindKey(b.AssetKind, b.AssetID)
	s.byKey[k] = b
	if s.byAgent[b.AgentID] == nil {
		s.byAgent[b.AgentID] = map[string]struct{}{}
	}
	s.byAgent[b.AgentID][k] = struct{}{}
	return nil
}

func (s *MemoryBindingStore) Delete(agentID string, kind hub.AssetKind, assetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := bindKey(kind, assetID)
	delete(s.byKey, k)
	if m := s.byAgent[agentID]; m != nil {
		delete(m, k)
	}
	return nil
}

func (s *MemoryBindingStore) Get(kind hub.AssetKind, assetID string) (Binding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.byKey[bindKey(kind, assetID)]
	return b, ok, nil
}

func (s *MemoryBindingStore) UpdateMeta(kind hub.AssetKind, assetID string, vis *hub.Visibility, st *hub.AssetStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := bindKey(kind, assetID)
	b, ok := s.byKey[k]
	if !ok {
		return hub.ErrNotSupported
	}
	if vis != nil {
		b.Visibility = *vis
	}
	if st != nil {
		b.Status = *st
	}
	s.byKey[k] = b
	return nil
}
