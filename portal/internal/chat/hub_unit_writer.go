package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

const (
	unitDraftPreviewMax = 200
	metaUnitTitle       = "title"
	metaUnitStatus      = "status"
	metaUnitAgentID     = "agent_id"
)

// memoryUnitWriter adapts memory.MemoryStore to local.UnitWriter.
//
// Portal MySQL sessionUnitsBackend rejects ScopeAgent, so drafts are stored as
// ScopeUser with ScopeID=agentID and AgentID=agentID for list/get filtering.
// Draft update / approve require UnitPatcher (in-place same unit ID; no supersede).
//
// Note: ScopeID=agentID means approved units are not recalled via Prefetch's
// real user_id lane; knowledge_search(source=units) / explicit list remain the
// primary read paths for these drafts until a dedicated agent-scope store exists.
type memoryUnitWriter struct {
	store memory.MemoryStore
}

// NewMemoryUnitWriter returns a UnitWriter backed by the given MemoryStore.
func NewMemoryUnitWriter(store memory.MemoryStore) local.UnitWriter {
	return &memoryUnitWriter{store: store}
}

// NewGatedMemoryUnitWriter wraps NewMemoryUnitWriter with memory_write_enabled checks
// (same OR-merge as RuntimeToolsForAgent). ListDrafts is always allowed.
func NewGatedMemoryUnitWriter(store memory.MemoryStore, agents AgentGetter) local.UnitWriter {
	return &gatedUnitWriter{inner: NewMemoryUnitWriter(store), agents: agents}
}

type gatedUnitWriter struct {
	inner  local.UnitWriter
	agents AgentGetter
}

func (g *gatedUnitWriter) requireWriteEnabled(ctx context.Context, agentID string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("chat: empty agent id")
	}
	if g == nil || g.agents == nil {
		return fmt.Errorf("chat: agent lookup not configured for unit writer")
	}
	meta, err := g.agents.Get(ctx, agentID)
	if err != nil {
		return err
	}
	if !RuntimeToolsForAgent(meta).MemoryWriteEnabled {
		return fmt.Errorf("chat: memory write disabled for agent %q", agentID)
	}
	return nil
}

func (g *gatedUnitWriter) WriteDraft(ctx context.Context, agentID, id, title, content string) (string, error) {
	if err := g.requireWriteEnabled(ctx, agentID); err != nil {
		return "", err
	}
	return g.inner.WriteDraft(ctx, agentID, id, title, content)
}

func (g *gatedUnitWriter) ApproveDraft(ctx context.Context, agentID, id string) error {
	if err := g.requireWriteEnabled(ctx, agentID); err != nil {
		return err
	}
	return g.inner.ApproveDraft(ctx, agentID, id)
}

func (g *gatedUnitWriter) ListDrafts(ctx context.Context, agentID string, limit int) ([]local.UnitDraftMeta, error) {
	return g.inner.ListDrafts(ctx, agentID, limit)
}


func (w *memoryUnitWriter) patcher() (memory.UnitPatcher, error) {
	if w == nil || w.store == nil {
		return nil, fmt.Errorf("chat: unit writer store not configured")
	}
	p, ok := w.store.(memory.UnitPatcher)
	if !ok {
		return nil, fmt.Errorf("chat: memory store does not support in-place unit patch")
	}
	return p, nil
}

func (w *memoryUnitWriter) WriteDraft(ctx context.Context, agentID, id, title, content string) (string, error) {
	if w == nil || w.store == nil {
		return "", fmt.Errorf("chat: unit writer store not configured")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", fmt.Errorf("chat: empty agent id")
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("chat: empty content")
	}
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)

	meta := map[string]any{}
	if title != "" {
		meta[metaUnitTitle] = title
	}
	meta = local.ApplyHubStatusMeta(meta, hub.AssetDraft)

	if id != "" {
		existing, err := w.getAgentUnit(ctx, agentID, id)
		if err != nil {
			return "", err
		}
		dbStatus := unitDBStatus(existing.Metadata)
		if local.MapUnitToAssetStatus(dbStatus, existing.Metadata) != hub.AssetDraft {
			return "", fmt.Errorf("chat: unit %q is not a draft", id)
		}
		// Preserve non-hub metadata keys from the existing unit.
		merged := cloneAnyMap(existing.Metadata)
		if title != "" {
			merged[metaUnitTitle] = title
		} else if t, ok := existing.Metadata[metaUnitTitle].(string); ok && strings.TrimSpace(t) != "" {
			merged[metaUnitTitle] = strings.TrimSpace(t)
		}
		merged = local.ApplyHubStatusMeta(merged, hub.AssetDraft)
		patcher, err := w.patcher()
		if err != nil {
			return "", err
		}
		contentCopy := content
		if err := patcher.PatchUnit(ctx, memory.GetRef{
			Scope:   memory.ScopeUser,
			ScopeID: agentID,
			ID:      id,
			AgentID: agentID,
		}, &contentCopy, scrubSystemMeta(merged)); err != nil {
			return "", err
		}
		return id, nil
	}

	hit, err := w.store.Remember(ctx, memory.RememberInput{
		Scope:    memory.ScopeUser,
		ScopeID:  agentID,
		AgentID:  agentID,
		Action:   memory.ActionAdd,
		Content:  content,
		Metadata: meta,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(hit.ID) == "" {
		return "", fmt.Errorf("chat: write draft returned empty id")
	}
	return hit.ID, nil
}

func (w *memoryUnitWriter) ApproveDraft(ctx context.Context, agentID, id string) error {
	if w == nil || w.store == nil {
		return fmt.Errorf("chat: unit writer store not configured")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("chat: empty agent id")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("chat: empty unit id")
	}
	existing, err := w.getAgentUnit(ctx, agentID, id)
	if err != nil {
		return err
	}
	dbStatus := unitDBStatus(existing.Metadata)
	if local.MapUnitToAssetStatus(dbStatus, existing.Metadata) != hub.AssetDraft {
		return fmt.Errorf("chat: unit %q is not a draft", id)
	}
	meta := local.ApplyHubStatusMeta(cloneAnyMap(existing.Metadata), hub.AssetActive)
	patcher, err := w.patcher()
	if err != nil {
		return err
	}
	// In-place metadata clear (delete hub_status); keep same unit ID — no supersede.
	return patcher.PatchUnit(ctx, memory.GetRef{
		Scope:   memory.ScopeUser,
		ScopeID: agentID,
		ID:      id,
		AgentID: agentID,
	}, nil, scrubSystemMeta(meta))
}

func (w *memoryUnitWriter) ListDrafts(ctx context.Context, agentID string, limit int) ([]local.UnitDraftMeta, error) {
	if w == nil || w.store == nil {
		return nil, fmt.Errorf("chat: unit writer store not configured")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("chat: empty agent id")
	}
	if limit <= 0 {
		limit = 20
	}
	// Over-fetch then filter hub_status=draft in Go (SQL has no metadata filter).
	fetch := limit * 4
	if fetch < 40 {
		fetch = 40
	}
	hits, err := w.store.List(ctx, memory.ListFilter{
		Scope:   memory.ScopeUser,
		ScopeID: agentID,
		AgentID: agentID,
		Status:  local.UnitDBActive,
		Kind:    memory.KindFilterAny,
		Limit:   fetch,
	})
	if err != nil {
		return nil, err
	}
	out := make([]local.UnitDraftMeta, 0, limit)
	for _, h := range hits {
		dbStatus := unitDBStatus(h.Metadata)
		if local.MapUnitToAssetStatus(dbStatus, h.Metadata) != hub.AssetDraft {
			continue
		}
		title, _ := h.Metadata[metaUnitTitle].(string)
		updated, _ := h.Metadata["updated_at"].(string)
		out = append(out, local.UnitDraftMeta{
			ID:        h.ID,
			Title:     strings.TrimSpace(title),
			UpdatedAt: strings.TrimSpace(updated),
			Preview:   previewContent(h.Content, unitDraftPreviewMax),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (w *memoryUnitWriter) getAgentUnit(ctx context.Context, agentID, id string) (memory.MemoryHit, error) {
	hit, err := w.store.Get(ctx, memory.GetRef{
		Scope:   memory.ScopeUser,
		ScopeID: agentID,
		ID:      id,
		AgentID: agentID,
	})
	if err != nil {
		return memory.MemoryHit{}, err
	}
	owner := strings.TrimSpace(agentIDFromHit(hit))
	if owner != "" && owner != agentID {
		return memory.MemoryHit{}, fmt.Errorf("chat: unit %q not owned by agent %q", id, agentID)
	}
	// DB status must be active for draft ops (superseded/deleted are not writable drafts).
	if st := unitDBStatus(hit.Metadata); st != "" && st != local.UnitDBActive {
		return memory.MemoryHit{}, fmt.Errorf("chat: unit %q not found", id)
	}
	return hit, nil
}

func agentIDFromHit(hit memory.MemoryHit) string {
	if hit.Metadata == nil {
		return ""
	}
	if s, ok := hit.Metadata[metaUnitAgentID].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func unitDBStatus(meta map[string]any) string {
	if meta == nil {
		return local.UnitDBActive
	}
	s, _ := meta[metaUnitStatus].(string)
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return local.UnitDBActive
	}
	return s
}

func previewContent(content string, max int) string {
	s := strings.TrimSpace(content)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// scrubSystemMeta drops keys that backends re-derive on read (status, hashes, ids).
func scrubSystemMeta(meta map[string]any) map[string]any {
	out := cloneAnyMap(meta)
	delete(out, metaUnitStatus)
	delete(out, "content_hash")
	delete(out, "supersedes_id")
	delete(out, metaUnitAgentID)
	delete(out, "user_id")
	return out
}
