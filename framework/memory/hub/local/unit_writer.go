package local

import "context"

// UnitDraftMeta describes a pending memory-unit draft.
type UnitDraftMeta struct {
	ID        string
	Title     string
	UpdatedAt string // RFC3339 or empty
	Preview   string
}

// UnitWriter is agent-scoped: every method takes agentID (from hub.Identity.AgentID / HTTP path).
type UnitWriter interface {
	WriteDraft(ctx context.Context, agentID, id, title, content string) (unitID string, err error)
	ApproveDraft(ctx context.Context, agentID, id string) error
	ListDrafts(ctx context.Context, agentID string, limit int) ([]UnitDraftMeta, error)
}

// WikiWriter extends wiki search with draft write / approve / read helpers.
type WikiWriter interface {
	WikiSearcher
	WriteDraft(ctx context.Context, id, content string) (canonicalID string, err error)
	ApproveDraft(ctx context.Context, id string, overwrite bool) error
	ListDrafts(ctx context.Context, limit int) ([]WikiDraftMeta, error)
	Read(ctx context.Context, id string) (*KnowledgeHit, error)
	ReadPreferDraft(ctx context.Context, id string) (*KnowledgeHit, error)
}
