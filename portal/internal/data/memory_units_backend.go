package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/data/model"

	"github.com/google/uuid"
	"github.com/sixath/framework/memory"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	memoryUnitStatusActive     = "active"
	memoryUnitStatusDeleted    = "deleted"
	memoryUnitStatusSuperseded = "superseded"
)

var _ memory.SessionUnitsBackend = (*sessionUnitsBackend)(nil)

type sessionUnitsBackend struct {
	db *gorm.DB
}

// NewSessionUnitsBackend creates the MySQL-backed session/user memory units store.
func NewSessionUnitsBackend(db *gorm.DB) memory.SessionUnitsBackend {
	return &sessionUnitsBackend{db: db}
}

// NewSessionUnitsBackendFromData exposes the durable units backend to
// dependency injection without leaking Data's database handle.
func NewSessionUnitsBackendFromData(data *Data) memory.SessionUnitsBackend {
	if data == nil || data.db == nil {
		return memory.NewSessionMemory()
	}
	return NewSessionUnitsBackend(data.db)
}

func (b *sessionUnitsBackend) Remember(ctx context.Context, in memory.RememberInput) (memory.MemoryHit, error) {
	scope := in.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	if scope != memory.ScopeSession && scope != memory.ScopeUser {
		return memory.MemoryHit{}, fmt.Errorf("%w: units only support session or user scope", memory.ErrScopeNotEnabled)
	}
	if strings.TrimSpace(in.ScopeID) == "" {
		return memory.MemoryHit{}, errors.New("memory: units require scope ID")
	}

	switch in.Action {
	case memory.ActionAdd:
		meta := cloneMemoryUnitMetadata(in.Metadata)
		kind := memory.UnitKindFromMetadata(meta)
		if meta == nil {
			meta = map[string]any{}
		}
		meta["kind"] = kind
		unit := MemoryUnit{
			ID:              uuid.NewString(),
			ScopeType:       string(scope),
			ScopeID:         in.ScopeID,
			AgentID:         stringPtr(in.AgentID),
			UserID:          userIDPtrForScope(scope, in.ScopeID),
			Content:         in.Content,
			Kind:            kind,
			ContentHash:     memory.ContentHash(in.Content),
			Status:          memoryUnitStatusActive,
			SourceSessionID: sourceSessionIDPtr(scope, in),
			Metadata:        meta,
		}
		if err := b.db.WithContext(ctx).Create(&unit).Error; err != nil {
			return memory.MemoryHit{}, err
		}
		return memoryUnitHit(unit), nil
	case memory.ActionReplace:
		if in.UnitID == "" {
			return memory.MemoryHit{}, errors.New("memory: units replace requires unit ID")
		}
		var neu MemoryUnit
		err := b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var old MemoryUnit
			query := tx
			if tx.Dialector.Name() == "mysql" {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			query = query.Where("id = ? AND scope_type = ? AND status = ?", in.UnitID, scope, memoryUnitStatusActive)
			if in.ScopeID != "" {
				query = query.Where(scopeIDColumn(scope)+" = ?", in.ScopeID)
			}
			if err := query.First(&old).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return memoryUnitNotFound(in.UnitID)
				}
				return err
			}

			oldID := old.ID
			meta := cloneMemoryUnitMetadata(in.Metadata)
			kind := memory.UnitKindFromMetadata(meta)
			if meta == nil {
				meta = map[string]any{}
			}
			meta["kind"] = kind
			neu = MemoryUnit{
				ID:              uuid.NewString(),
				ScopeType:       old.ScopeType,
				ScopeID:         old.ScopeID,
				AgentID:         old.AgentID,
				UserID:          old.UserID,
				Content:         in.Content,
				Kind:            kind,
				ContentHash:     memory.ContentHash(in.Content),
				Status:          memoryUnitStatusActive,
				SupersedesID:    &oldID,
				SourceSessionID: old.SourceSessionID,
				Metadata:        meta,
			}
			if in.AgentID != "" {
				neu.AgentID = stringPtr(in.AgentID)
			}
			if err := tx.Create(&neu).Error; err != nil {
				return err
			}
			result := tx.Model(&MemoryUnit{}).Where("id = ?", old.ID).Update("status", memoryUnitStatusSuperseded)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return memoryUnitNotFound(in.UnitID)
			}
			return nil
		})
		if err != nil {
			return memory.MemoryHit{}, err
		}
		return memoryUnitHit(neu), nil
	case memory.ActionRemove:
		if in.UnitID == "" {
			return memory.MemoryHit{}, errors.New("memory: units remove requires unit ID")
		}
		if err := b.Delete(ctx, memory.GetRef{Scope: scope, ID: in.UnitID, ScopeID: in.ScopeID}); err != nil {
			return memory.MemoryHit{}, err
		}
		return memory.MemoryHit{Scope: scope, Source: memory.SourceUnits, ID: in.UnitID}, nil
	default:
		return memory.MemoryHit{}, fmt.Errorf("memory: unsupported units action %q", in.Action)
	}
}

func (b *sessionUnitsBackend) Recall(ctx context.Context, q memory.RecallQuery) ([]memory.MemoryHit, error) {
	scope := q.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	query := b.activeUnits(ctx, scope).Order("updated_at DESC, id DESC")
	if q.ScopeID != "" {
		query = query.Where(scopeIDColumn(scope)+" = ?", q.ScopeID)
	}
	query = applyKindFilter(query, q.Kind)
	if strings.TrimSpace(q.Query) != "" {
		query = query.Where("content LIKE ?", "%"+q.Query+"%")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}

	var units []MemoryUnit
	if err := query.Limit(limit).Find(&units).Error; err != nil {
		return nil, err
	}
	return memoryUnitHits(units), nil
}

func (b *sessionUnitsBackend) Get(ctx context.Context, ref memory.GetRef) (memory.MemoryHit, error) {
	scope := ref.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	var unit MemoryUnit
	query := b.db.WithContext(ctx).
		Where("scope_type = ? AND id = ? AND status IN ?", scope, ref.ID, []string{memoryUnitStatusActive, memoryUnitStatusSuperseded})
	if ref.ScopeID != "" {
		query = query.Where(scopeIDColumn(scope)+" = ?", ref.ScopeID)
	}
	if err := query.First(&unit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return memory.MemoryHit{}, memoryUnitNotFound(ref.ID)
		}
		return memory.MemoryHit{}, err
	}
	return memoryUnitHit(unit), nil
}

// PatchUnit updates the same MySQL row in place (unlike ActionReplace, which inserts a superseding row).
func (b *sessionUnitsBackend) PatchUnit(ctx context.Context, ref memory.GetRef, content *string, metadata map[string]any) error {
	scope := ref.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	if scope != memory.ScopeSession && scope != memory.ScopeUser {
		return fmt.Errorf("%w: units only support session or user scope", memory.ErrScopeNotEnabled)
	}
	if strings.TrimSpace(ref.ID) == "" {
		return errors.New("memory: units patch requires unit ID")
	}
	if strings.TrimSpace(ref.ScopeID) == "" {
		return errors.New("memory: units require scope ID")
	}
	if content == nil && metadata == nil {
		return nil
	}

	return b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var unit MemoryUnit
		query := tx
		if tx.Dialector.Name() == "mysql" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		query = query.Where("id = ? AND scope_type = ? AND status = ?", ref.ID, scope, memoryUnitStatusActive)
		if ref.ScopeID != "" {
			query = query.Where(scopeIDColumn(scope)+" = ?", ref.ScopeID)
		}
		if err := query.First(&unit).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return memoryUnitNotFound(ref.ID)
			}
			return err
		}

		updates := map[string]any{}
		if content != nil {
			updates["content"] = *content
			updates["content_hash"] = memory.ContentHash(*content)
		}
		if metadata != nil {
			meta := cloneMemoryUnitMetadata(metadata)
			kind := memory.UnitKindFromMetadata(meta)
			if meta == nil {
				meta = map[string]any{}
			}
			meta["kind"] = kind
			updates["metadata"] = model.JSONMap(meta)
			updates["kind"] = kind
		}
		result := tx.Model(&MemoryUnit{}).Where("id = ? AND status = ?", unit.ID, memoryUnitStatusActive).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return memoryUnitNotFound(ref.ID)
		}
		return nil
	})
}

func (b *sessionUnitsBackend) List(ctx context.Context, filter memory.ListFilter) ([]memory.MemoryHit, error) {
	scope := filter.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	status := filter.Status
	if status == "" {
		status = memoryUnitStatusActive
	}
	query := b.db.WithContext(ctx).Where("scope_type = ? AND status = ?", scope, status)
	if filter.ScopeID != "" {
		query = query.Where(scopeIDColumn(scope)+" = ?", filter.ScopeID)
	}
	if filter.AgentID != "" {
		query = query.Where("agent_id = ?", filter.AgentID)
	}
	query = applyKindFilter(query, filter.Kind)
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query = query.Order("updated_at DESC, id DESC").Offset(offset)
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	var units []MemoryUnit
	if err := query.Find(&units).Error; err != nil {
		return nil, err
	}
	return memoryUnitHits(units), nil
}

func (b *sessionUnitsBackend) Delete(ctx context.Context, ref memory.GetRef) error {
	scope := ref.Scope
	if scope == "" {
		scope = memory.ScopeSession
	}
	return b.cascadeSoftDelete(ctx, ref.ID, scope, ref.ScopeID)
}

func (b *sessionUnitsBackend) cascadeSoftDelete(ctx context.Context, id string, scope memory.Scope, scopeID string) error {
	return b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var seed MemoryUnit
		seedQuery := tx.Where("id = ? AND scope_type = ? AND status <> ?", id, scope, memoryUnitStatusDeleted)
		if scopeID != "" {
			seedQuery = seedQuery.Where(scopeIDColumn(scope)+" = ?", scopeID)
		}
		if err := seedQuery.First(&seed).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return memoryUnitNotFound(id)
			}
			return err
		}

		toDelete := map[string]struct{}{id: {}}
		queue := []string{id}

		for len(queue) > 0 {
			curID := queue[0]
			queue = queue[1:]

			var cur MemoryUnit
			if err := tx.Where("id = ?", curID).First(&cur).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}

			// Ancestors: follow supersedes_id chain.
			if cur.SupersedesID != nil && *cur.SupersedesID != "" {
				ancID := *cur.SupersedesID
				if _, seen := toDelete[ancID]; !seen {
					var anc MemoryUnit
					ancQuery := tx.Where("id = ? AND scope_type = ? AND status <> ?", ancID, scope, memoryUnitStatusDeleted)
					if scopeID != "" {
						ancQuery = ancQuery.Where(scopeIDColumn(scope)+" = ?", scopeID)
					}
					if err := ancQuery.First(&anc).Error; err == nil {
						toDelete[ancID] = struct{}{}
						queue = append(queue, ancID)
					} else if !errors.Is(err, gorm.ErrRecordNotFound) {
						return err
					}
				}
			}

			// Descendants: units that supersede the current id.
			var descendants []MemoryUnit
			descQuery := tx.Where("supersedes_id = ? AND scope_type = ? AND status <> ?", curID, scope, memoryUnitStatusDeleted)
			if scopeID != "" {
				descQuery = descQuery.Where(scopeIDColumn(scope)+" = ?", scopeID)
			}
			if err := descQuery.Find(&descendants).Error; err != nil {
				return err
			}
			for _, d := range descendants {
				if _, seen := toDelete[d.ID]; seen {
					continue
				}
				toDelete[d.ID] = struct{}{}
				queue = append(queue, d.ID)
			}
		}

		ids := make([]string, 0, len(toDelete))
		for delID := range toDelete {
			ids = append(ids, delID)
		}
		result := tx.Model(&MemoryUnit{}).Where("id IN ?", ids).Update("status", memoryUnitStatusDeleted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return memoryUnitNotFound(id)
		}
		return nil
	})
}

func (b *sessionUnitsBackend) activeUnits(ctx context.Context, scope memory.Scope) *gorm.DB {
	return b.db.WithContext(ctx).Where("scope_type = ? AND status = ?", scope, memoryUnitStatusActive)
}

// applyKindFilter: default excludes procedural; Kind=procedural|any overrides.
func applyKindFilter(query *gorm.DB, kind string) *gorm.DB {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case memory.KindFilterAny:
		return query
	case memory.KindProcedural: // KindFilterProcedural aliases same value
		return query.Where("kind = ?", memory.KindProcedural)
	default:
		return query.Where("(kind = ? OR kind = '' OR kind IS NULL)", memory.KindFact)
	}
}

// scopeIDColumn: session keeps filtering by source_session_id (written as ScopeID on add);
// user filters by scope_id (= user_id).
func scopeIDColumn(scope memory.Scope) string {
	if scope == memory.ScopeUser {
		return "scope_id"
	}
	return "source_session_id"
}

func userIDPtrForScope(scope memory.Scope, scopeID string) *string {
	if scope != memory.ScopeUser {
		return nil
	}
	return stringPtr(scopeID)
}

func sourceSessionIDPtr(scope memory.Scope, in memory.RememberInput) *string {
	if scope == memory.ScopeSession {
		return stringPtr(in.ScopeID)
	}
	if in.Metadata != nil {
		if s, ok := in.Metadata["source_session_id"].(string); ok {
			return stringPtr(strings.TrimSpace(s))
		}
	}
	return nil
}

func memoryUnitHit(unit MemoryUnit) memory.MemoryHit {
	metadata := cloneMemoryUnitMetadata(map[string]any(unit.Metadata))
	if metadata == nil {
		metadata = make(map[string]any)
	}
	kind := strings.TrimSpace(unit.Kind)
	if kind == "" {
		kind = memory.UnitKindFromMetadata(metadata)
	}
	metadata["kind"] = kind
	metadata["content_hash"] = unit.ContentHash
	metadata["status"] = unit.Status
	if unit.SupersedesID != nil && *unit.SupersedesID != "" {
		metadata["supersedes_id"] = *unit.SupersedesID
	}
	if unit.SourceSessionID != nil {
		metadata["source_session_id"] = *unit.SourceSessionID
	}
	if unit.AgentID != nil && *unit.AgentID != "" {
		metadata["agent_id"] = *unit.AgentID
	}
	if unit.UserID != nil && *unit.UserID != "" {
		metadata["user_id"] = *unit.UserID
	}
	scope := memory.Scope(unit.ScopeType)
	if scope == "" {
		scope = memory.ScopeSession
	}
	return memory.MemoryHit{
		Scope:    scope,
		Source:   memory.SourceUnits,
		ID:       unit.ID,
		Content:  unit.Content,
		Metadata: metadata,
	}
}

func memoryUnitHits(units []MemoryUnit) []memory.MemoryHit {
	hits := make([]memory.MemoryHit, len(units))
	for i, unit := range units {
		hits[i] = memoryUnitHit(unit)
	}
	return hits
}

func cloneMemoryUnitMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func memoryUnitNotFound(id string) error {
	return fmt.Errorf("memory: unit %q not found", id)
}
