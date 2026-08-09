package chat

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub/local"
)

// KnowledgeDraftItem is the shared JSON shape for wiki/units drafts (HTTP + UI).
type KnowledgeDraftItem struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	Preview   string `json:"preview,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ApproveKnowledgeDraftRequest is POST body for hub knowledge approve.
type ApproveKnowledgeDraftRequest struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	Overwrite bool   `json:"overwrite"`
}

func localKnowledgeWriters(rt biz.RuntimeToolsConfig) (wiki local.WikiWriter, units local.UnitWriter, err error) {
	_, know, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return nil, nil, err
	}
	lk, ok := know.(*local.LocalKnowledge)
	if !ok {
		return nil, nil, fmt.Errorf("hub: knowledge provider %q does not support draft writers", know.Name())
	}
	return lk.WikiWriter(), lk.UnitWriter(), nil
}

// ListKnowledgeDrafts lists pending wiki and/or units drafts for an agent.
// source empty = wiki + units; "wiki" / "units" = single source.
// Missing writer for an explicitly requested source returns a clear error;
// when source is empty, unavailable backends are skipped.
func ListKnowledgeDrafts(ctx context.Context, rt biz.RuntimeToolsConfig, agentID, source string, limit int) ([]KnowledgeDraftItem, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("hub: agent_id required")
	}
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "", "wiki", "units":
	default:
		return nil, fmt.Errorf("hub: invalid knowledge draft source %q (want wiki|units)", source)
	}
	if limit <= 0 {
		limit = 50
	}

	ww, uw, err := localKnowledgeWriters(rt)
	if err != nil {
		return nil, err
	}

	wantWiki := source == "" || source == "wiki"
	wantUnits := source == "" || source == "units"
	out := make([]KnowledgeDraftItem, 0)

	if wantWiki {
		if ww == nil {
			if source == "wiki" {
				return nil, fmt.Errorf("hub: wiki writer not configured")
			}
		} else {
			drafts, err := ww.ListDrafts(ctx, limit)
			if err != nil {
				return nil, err
			}
			for _, d := range drafts {
				out = append(out, KnowledgeDraftItem{
					Source:    "wiki",
					ID:        d.ID,
					Preview:   d.Preview,
					UpdatedAt: d.UpdatedAt,
				})
			}
		}
	}

	if wantUnits {
		if uw == nil {
			if source == "units" {
				return nil, fmt.Errorf("hub: units writer not configured")
			}
		} else {
			remaining := limit
			if source == "" {
				remaining = limit - len(out)
				if remaining <= 0 {
					return out, nil
				}
			}
			drafts, err := uw.ListDrafts(ctx, agentID, remaining)
			if err != nil {
				return nil, err
			}
			for _, d := range drafts {
				out = append(out, KnowledgeDraftItem{
					Source:    "units",
					ID:        d.ID,
					Title:     d.Title,
					Preview:   d.Preview,
					UpdatedAt: d.UpdatedAt,
				})
			}
		}
	}

	return out, nil
}

// ApproveKnowledgeDraft promotes a draft to active/formal for the given source.
// source must be "wiki" or "units". Wiki uses overwrite; units ignores it.
func ApproveKnowledgeDraft(ctx context.Context, rt biz.RuntimeToolsConfig, agentID, source, id string, overwrite bool) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("hub: agent_id required")
	}
	source = strings.ToLower(strings.TrimSpace(source))
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("hub: draft id required")
	}

	ww, uw, err := localKnowledgeWriters(rt)
	if err != nil {
		return err
	}

	switch source {
	case "wiki":
		if ww == nil {
			return fmt.Errorf("hub: wiki writer not configured")
		}
		return ww.ApproveDraft(ctx, id, overwrite)
	case "units":
		if uw == nil {
			return fmt.Errorf("hub: units writer not configured")
		}
		return uw.ApproveDraft(ctx, agentID, id)
	default:
		return fmt.Errorf("hub: invalid knowledge draft source %q (want wiki|units)", source)
	}
}
