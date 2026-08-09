package memory

import "context"

type ConflictDecision int

const (
	ConflictIgnore ConflictDecision = iota
	ConflictSupersede
	ConflictKeepBoth
)

type ConflictResolver interface {
	Resolve(ctx context.Context, existing MemoryHit, candidate RememberInput) (ConflictDecision, error)
}

type StructuralReplaceResolver struct{}

func (StructuralReplaceResolver) Resolve(ctx context.Context, existing MemoryHit, candidate RememberInput) (ConflictDecision, error) {
	return ConflictSupersede, nil
}
