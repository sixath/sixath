package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	unitStatusActive     = "active"
	unitStatusDeleted    = "deleted"
	unitStatusSuperseded = "superseded"
)

type sessionUnit struct {
	id              string
	scope           Scope
	scopeID         string
	sourceSessionID string
	agentID         string
	content         string
	contentHash     string
	metadata        map[string]any
	status          string
	supersedesID    string
	updatedAt       time.Time
}

// SessionMemory is an in-memory SessionUnitsBackend for session-scoped memory.
type SessionMemory struct {
	mu    sync.RWMutex
	units map[string]sessionUnit
}

func NewSessionMemory() *SessionMemory {
	return &SessionMemory{units: make(map[string]sessionUnit)}
}

func effectiveScope(scope Scope) Scope {
	if scope == "" {
		return ScopeSession
	}
	return scope
}

func (u sessionUnit) matchesScope(scope Scope, scopeID string) bool {
	if effectiveScope(u.scope) != effectiveScope(scope) {
		return false
	}
	if scopeID != "" && u.scopeID != scopeID {
		return false
	}
	return true
}

func sourceSessionFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata["source_session_id"].(string); ok {
		return v
	}
	return ""
}

func (m *SessionMemory) Remember(_ context.Context, in RememberInput) (MemoryHit, error) {
	scope := effectiveScope(in.Scope)
	if scope != ScopeSession && scope != ScopeUser {
		return MemoryHit{}, ErrNotSupported
	}
	if in.ScopeID == "" {
		return MemoryHit{}, errors.New("memory: scope ID required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch in.Action {
	case ActionAdd:
		unit := sessionUnit{
			id:          uuid.NewString(),
			scope:       scope,
			scopeID:     in.ScopeID,
			agentID:     in.AgentID,
			content:     in.Content,
			contentHash: ContentHash(in.Content),
			metadata:    cloneMetadata(in.Metadata),
			status:      unitStatusActive,
			updatedAt:   time.Now().UTC(),
		}
		if scope == ScopeSession {
			unit.sourceSessionID = in.ScopeID
		} else {
			unit.sourceSessionID = sourceSessionFromMetadata(in.Metadata)
		}
		m.units[unit.id] = unit
		return unit.hit(), nil
	case ActionReplace:
		if in.UnitID == "" {
			return MemoryHit{}, errors.New("memory: session replace requires unit ID")
		}
		old, ok := m.units[in.UnitID]
		if !ok || old.status != unitStatusActive || !old.matchesScope(scope, in.ScopeID) {
			return MemoryHit{}, fmt.Errorf("memory: unit %q not found", in.UnitID)
		}
		neu := sessionUnit{
			id:              uuid.NewString(),
			scope:           old.scope,
			scopeID:         old.scopeID,
			sourceSessionID: old.sourceSessionID,
			agentID:         old.agentID,
			content:         in.Content,
			contentHash:     ContentHash(in.Content),
			metadata:        cloneMetadata(in.Metadata),
			status:          unitStatusActive,
			supersedesID:    old.id,
			updatedAt:       time.Now().UTC(),
		}
		if in.AgentID != "" {
			neu.agentID = in.AgentID
		}
		old.status = unitStatusSuperseded
		old.updatedAt = time.Now().UTC()
		m.units[old.id] = old
		m.units[neu.id] = neu
		return neu.hit(), nil
	case ActionRemove:
		if in.UnitID == "" {
			return MemoryHit{}, errors.New("memory: session remove requires unit ID")
		}
		if err := m.cascadeSoftDelete(in.UnitID, scope, in.ScopeID); err != nil {
			return MemoryHit{}, err
		}
		return MemoryHit{Scope: scope, Source: SourceUnits, ID: in.UnitID}, nil
	default:
		return MemoryHit{}, fmt.Errorf("memory: unsupported session action %q", in.Action)
	}
}

func (m *SessionMemory) Recall(_ context.Context, q RecallQuery) ([]MemoryHit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	units := make([]sessionUnit, 0, len(m.units))
	query := strings.ToLower(q.Query)
	for _, unit := range m.units {
		if unit.status != unitStatusActive || !unit.matchesScope(q.Scope, q.ScopeID) {
			continue
		}
		if !KindMatchesFilter(UnitKindFromMetadata(unit.metadata), q.Kind) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(unit.content), query) {
			continue
		}
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool {
		if units[i].updatedAt.Equal(units[j].updatedAt) {
			return units[i].id > units[j].id
		}
		return units[i].updatedAt.After(units[j].updatedAt)
	})

	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	if len(units) > limit {
		units = units[:limit]
	}
	hits := make([]MemoryHit, len(units))
	for i, unit := range units {
		hits[i] = unit.hit()
	}
	return hits, nil
}

func (m *SessionMemory) Get(_ context.Context, ref GetRef) (MemoryHit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	unit, ok := m.units[ref.ID]
	if !ok || unit.status == unitStatusDeleted || !unit.matchesScope(ref.Scope, ref.ScopeID) {
		return MemoryHit{}, fmt.Errorf("memory: unit %q not found", ref.ID)
	}
	if unit.status != unitStatusActive && unit.status != unitStatusSuperseded {
		return MemoryHit{}, fmt.Errorf("memory: unit %q not found", ref.ID)
	}
	return unit.hit(), nil
}

func (m *SessionMemory) List(_ context.Context, filter ListFilter) ([]MemoryHit, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := filter.Status
	if status == "" {
		status = unitStatusActive
	}
	units := make([]sessionUnit, 0, len(m.units))
	for _, unit := range m.units {
		if unit.status == status && unit.matchesScope(filter.Scope, filter.ScopeID) &&
			(filter.AgentID == "" || unit.agentID == filter.AgentID) &&
			KindMatchesFilter(UnitKindFromMetadata(unit.metadata), filter.Kind) {
			units = append(units, unit)
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].updatedAt.After(units[j].updatedAt) })

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(units) {
		return []MemoryHit{}, nil
	}
	units = units[offset:]
	if filter.Limit > 0 && len(units) > filter.Limit {
		units = units[:filter.Limit]
	}
	hits := make([]MemoryHit, len(units))
	for i, unit := range units {
		hits[i] = unit.hit()
	}
	return hits, nil
}

func (m *SessionMemory) Delete(_ context.Context, ref GetRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cascadeSoftDelete(ref.ID, ref.Scope, ref.ScopeID)
}

// PatchUnit updates the same map entry in place (unlike ActionReplace, which supersedes with a new ID).
func (m *SessionMemory) PatchUnit(_ context.Context, ref GetRef, content *string, metadata map[string]any) error {
	scope := effectiveScope(ref.Scope)
	if scope != ScopeSession && scope != ScopeUser {
		return ErrNotSupported
	}
	if strings.TrimSpace(ref.ID) == "" {
		return errors.New("memory: patch requires unit ID")
	}
	if strings.TrimSpace(ref.ScopeID) == "" {
		return errors.New("memory: scope ID required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	unit, ok := m.units[ref.ID]
	if !ok || unit.status != unitStatusActive || !unit.matchesScope(scope, ref.ScopeID) {
		return fmt.Errorf("memory: unit %q not found", ref.ID)
	}
	if content != nil {
		unit.content = *content
		unit.contentHash = ContentHash(*content)
	}
	if metadata != nil {
		unit.metadata = cloneMetadata(metadata)
	}
	unit.updatedAt = time.Now().UTC()
	m.units[ref.ID] = unit
	return nil
}

func (m *SessionMemory) cascadeSoftDelete(id string, scope Scope, scopeID string) error {
	unit, ok := m.units[id]
	if !ok || unit.status == unitStatusDeleted || !unit.matchesScope(scope, scopeID) {
		return fmt.Errorf("memory: unit %q not found", id)
	}

	toDelete := map[string]struct{}{id: {}}
	queue := []string{id}

	for len(queue) > 0 {
		curID := queue[0]
		queue = queue[1:]
		cur, ok := m.units[curID]
		if !ok {
			continue
		}

		// Ancestors: follow supersedesID chain.
		if cur.supersedesID != "" {
			if _, seen := toDelete[cur.supersedesID]; !seen {
				if anc, ok := m.units[cur.supersedesID]; ok &&
					anc.status != unitStatusDeleted &&
					anc.matchesScope(scope, scopeID) {
					toDelete[cur.supersedesID] = struct{}{}
					queue = append(queue, cur.supersedesID)
				}
			}
		}

		// Descendants: units in same scope that supersede the current id.
		for otherID, other := range m.units {
			if other.supersedesID != curID {
				continue
			}
			if other.status == unitStatusDeleted || !other.matchesScope(scope, scopeID) {
				continue
			}
			if _, seen := toDelete[otherID]; seen {
				continue
			}
			toDelete[otherID] = struct{}{}
			queue = append(queue, otherID)
		}
	}

	now := time.Now().UTC()
	for delID := range toDelete {
		u := m.units[delID]
		u.status = unitStatusDeleted
		u.updatedAt = now
		m.units[delID] = u
	}
	return nil
}

func (u sessionUnit) hit() MemoryHit {
	metadata := cloneMetadata(u.metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["content_hash"] = u.contentHash
	metadata["status"] = u.status
	if u.supersedesID != "" {
		metadata["supersedes_id"] = u.supersedesID
	}
	if u.sourceSessionID != "" {
		metadata["source_session_id"] = u.sourceSessionID
	}
	if u.agentID != "" {
		metadata["agent_id"] = u.agentID
	}
	return MemoryHit{
		Scope:    effectiveScope(u.scope),
		Source:   SourceUnits,
		ID:       u.id,
		Content:  u.content,
		Metadata: metadata,
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
