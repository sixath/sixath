package biz

import (
	"context"
	"time"
)

// EvolutionProposal is the business model for an evolution proposal.
type EvolutionProposal struct {
	ID                  string
	AgentID             string
	SessionID           string
	TurnIndex           int
	SignalType          string
	Confidence          float64
	ProblemSummary      string
	ProposedContent     string
	TargetPath          string
	TargetAction        string // create, patch, deprecate
	Conflict            bool
	ConflictDetail      *string
	ConflictCheckFailed bool
	DedupSkipped        bool
	Status              string // pending, approved, rejected, expired
	ReviewComment       *string
	CreatedAt           time.Time
	ReviewedAt          *time.Time
	ReviewedBy          *string
}

// EvolutionProposalRepo defines the storage interface for evolution proposals.
type EvolutionProposalRepo interface {
	Create(ctx context.Context, p *EvolutionProposal) error
	GetByID(ctx context.Context, id string) (*EvolutionProposal, error)
	List(ctx context.Context, page, pageSize int32, status string) ([]*EvolutionProposal, int, error)
	Update(ctx context.Context, id string, updates map[string]any) error
	CountPending(ctx context.Context) (int, error)
	ListExpired(ctx context.Context, before time.Time) ([]*EvolutionProposal, error)
}

// EvolutionUsecase handles evolution proposal business logic.
type EvolutionUsecase struct {
	repo EvolutionProposalRepo
}

func NewEvolutionUsecase(repo EvolutionProposalRepo) *EvolutionUsecase {
	return &EvolutionUsecase{repo: repo}
}

func (uc *EvolutionUsecase) List(ctx context.Context, page, pageSize int32, status string) ([]*EvolutionProposal, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return uc.repo.List(ctx, page, pageSize, status)
}

func (uc *EvolutionUsecase) Get(ctx context.Context, id string) (*EvolutionProposal, error) {
	return uc.repo.GetByID(ctx, id)
}

func (uc *EvolutionUsecase) Approve(ctx context.Context, id, reviewer string) error {
	now := time.Now()
	return uc.repo.Update(ctx, id, map[string]any{
		"status":      "approved",
		"reviewed_at": now,
		"reviewed_by": reviewer,
	})
}

func (uc *EvolutionUsecase) Reject(ctx context.Context, id, reviewer, comment string) error {
	now := time.Now()
	updates := map[string]any{
		"status":      "rejected",
		"reviewed_at": now,
		"reviewed_by": reviewer,
	}
	if comment != "" {
		updates["review_comment"] = comment
	}
	return uc.repo.Update(ctx, id, updates)
}

func (uc *EvolutionUsecase) Patch(ctx context.Context, id string, proposedContent string) error {
	return uc.repo.Update(ctx, id, map[string]any{
		"proposed_content": proposedContent,
	})
}

func (uc *EvolutionUsecase) CountPending(ctx context.Context) (int, error) {
	return uc.repo.CountPending(ctx)
}

func (uc *EvolutionUsecase) ExpireOld(ctx context.Context, olderThan time.Time) (int, error) {
	items, err := uc.repo.ListExpired(ctx, olderThan)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if err := uc.repo.Update(ctx, item.ID, map[string]any{"status": "expired"}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}