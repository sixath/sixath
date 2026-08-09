package memory

import "context"

type SemanticConflictVerdict struct {
	Decision     ConflictDecision
	TargetUnitID string
}

type SemanticConflictResolver interface {
	ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error)
}

// StubSemanticConflictResolver for tests; Decision/TargetUnitID/Err control output.
type StubSemanticConflictResolver struct {
	Decision     ConflictDecision
	TargetUnitID string
	Err          error
	Calls        int
	LastPeers    []MemoryHit
}

func (s *StubSemanticConflictResolver) ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error) {
	s.Calls++
	s.LastPeers = peers
	if s.Err != nil {
		return SemanticConflictVerdict{}, s.Err
	}
	return SemanticConflictVerdict{Decision: s.Decision, TargetUnitID: s.TargetUnitID}, nil
}
