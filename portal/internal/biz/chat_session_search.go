package biz

import (
	"context"
	"sort"
	"strings"
	"time"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// SessionSearchBackend is the wiring-owned adapter for the transcript index.
// Business logic owns access filtering and result shaping, not FTS details.
type SessionSearchBackend interface {
	SearchSessions(ctx context.Context, agentIDs []string, query string, limit int) []SessionSearchCandidate
	SearchAnchored(ctx context.Context, opts TranscriptSearchOpts) ([]AnchoredHit, error)
}

// SessionSearchCandidate is an index hit independent of the FTS implementation.
type SessionSearchCandidate struct {
	SessionID       string
	RootSessionID   string
	AgentID         string
	Title           string
	Preview         string
	MatchedSnippets []string
	UpdatedAt       time.Time
}

// TranscriptSearchOpts is the portal-facing SearchAnchored request.
type TranscriptSearchOpts struct {
	AgentID          string
	Query            string
	ExcludeSessionID string
	IncludeTools     bool
	Window           int
	Limit            int
}

// TranscriptMessageDoc mirrors an indexed message for UI JSON.
type TranscriptMessageDoc struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolName  string    `json:"tool_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AnchoredHit is a message-level FTS hit with local context (UI / memory_recall shape).
type AnchoredHit struct {
	SessionID     string                 `json:"session_id"`
	RootSessionID string                 `json:"root_session_id"`
	Title         string                 `json:"title"`
	Anchor        TranscriptMessageDoc   `json:"anchor"`
	Window        []TranscriptMessageDoc `json:"window"`
	BookendStart  []TranscriptMessageDoc `json:"bookend_start"`
	BookendEnd    []TranscriptMessageDoc `json:"bookend_end"`
	Score         float64                `json:"score"`
}

// TranscriptSearchResult wraps AnchoredHit for the read-only API.
type TranscriptSearchResult struct {
	Hits  []AnchoredHit `json:"hits"`
	Count int           `json:"count"`
}

const (
	searchSessionsDefaultLimit = 20
	searchSessionsMaxLimit     = 50
	searchAgentsCap            = 100
)

// SearchSessions 跨 Agent FTS 搜索会话；FTS 未启用时返回空列表。
func (uc *ChatUsecase) SearchSessions(ctx context.Context, query, agentIDFilter string, limit int) ([]SearchHit, string, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, "query required", nil
	}
	if limit <= 0 {
		limit = searchSessionsDefaultLimit
	}
	if limit > searchSessionsMaxLimit {
		limit = searchSessionsMaxLimit
	}

	if uc.sessionSearch == nil {
		return []SearchHit{}, "session search disabled", nil
	}

	var agentIDs []string
	agentNames := map[string]string{}

	if agentIDFilter != "" {
		if err := uc.requireAgentUse(ctx, caller, agentIDFilter); err != nil {
			return nil, "", err
		}
		agent, err := uc.agentRepo.GetByID(ctx, agentIDFilter)
		if err != nil {
			return nil, "", err
		}
		agentIDs = []string{agentIDFilter}
		agentNames[agent.ID] = agent.Name
	} else {
		agents, _, err := uc.agentRepo.List(ctx, 1, searchAgentsCap)
		if err != nil {
			return nil, "", err
		}
		agentIDs = make([]string, 0, len(agents))
		for _, a := range agents {
			resource, err := uc.resources.GetByPayload(ctx, ResourceTypeAgent, a.ID)
			if err != nil {
				continue
			}
			canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, "")
			if err != nil {
				return nil, "", err
			}
			if !canUse {
				continue
			}
			agentIDs = append(agentIDs, a.ID)
			agentNames[a.ID] = a.Name
		}
	}

	type ranked struct {
		hit SearchHit
		at  time.Time
	}
	var merged []ranked

	for _, h := range uc.sessionSearch.SearchSessions(ctx, agentIDs, query, limit) {
		sid := h.RootSessionID
		if sid == "" {
			sid = h.SessionID
		}
		sess, err := uc.GetSession(ctx, sid)
		if err != nil {
			continue
		}
		merged = append(merged, ranked{
			hit: SearchHit{
				SessionID:       sid,
				RootSessionID:   sid,
				AgentID:         h.AgentID,
				AgentName:       agentNames[h.AgentID],
				Title:           firstNonEmpty(h.Title, sess.Title),
				Preview:         truncatePreview(h.Preview, sessionPreviewMaxRunes),
				MatchedSnippets: h.MatchedSnippets,
				UpdatedAt:       pickTime(h.UpdatedAt, sess.UpdatedAt),
			},
			at: pickTime(h.UpdatedAt, sess.UpdatedAt),
		})
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].at.After(merged[j].at)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	out := make([]SearchHit, len(merged))
	for i, r := range merged {
		out[i] = r.hit
	}
	return out, "ok", nil
}

// SearchTranscript runs message-level SearchAnchored for one agent.
// AuthZ matches agent read (PermView). Empty query returns empty hits.
func (uc *ChatUsecase) SearchTranscript(ctx context.Context, opts TranscriptSearchOpts) (*TranscriptSearchResult, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	opts.AgentID = strings.TrimSpace(opts.AgentID)
	if opts.AgentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
	}
	if err := uc.requireAgentView(ctx, caller, opts.AgentID); err != nil {
		return nil, err
	}
	opts.Query = strings.TrimSpace(opts.Query)
	empty := &TranscriptSearchResult{Hits: []AnchoredHit{}, Count: 0}
	if opts.Query == "" {
		return empty, nil
	}
	if uc.sessionSearch == nil {
		return empty, nil
	}
	hits, err := uc.sessionSearch.SearchAnchored(ctx, opts)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []AnchoredHit{}
	}
	return &TranscriptSearchResult{Hits: hits, Count: len(hits)}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func pickTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	return fallback
}
