package mea

import (
	"context"

	"github.com/google/uuid"
)

// CascadeAuditor runs RulesAuditor first when structured checks exist (machine veto).
// LLM is only consulted when AcceptanceChecks are empty, text Acceptance is present,
// and LLM is non-nil. Rules failures are never overridden by the LLM.
type CascadeAuditor struct {
	Rules RulesAuditor
	LLM   Auditor // typically *LLMAuditor or LLMAuditor
}

var _ Auditor = CascadeAuditor{}

// Audit implements Auditor.
func (a CascadeAuditor) Audit(ctx context.Context, s TaskState, c Contract, o ExecutionReport) (AuditReport, error) {
	if len(c.AcceptanceChecks) > 0 {
		return a.Rules.Audit(ctx, s, c, o)
	}
	if len(c.Acceptance) > 0 && a.LLM != nil {
		return a.LLM.Audit(ctx, s, c, o)
	}
	// No checks and no acceptance — same fail-closed posture as RulesAuditor.
	return AuditReport{
		ID:         uuid.NewString(),
		Round:      c.Round,
		Completion: CompletionIncomplete,
		Integrity:  IntegritySuspect,
		Evidence: []Evidence{{
			Type:    "acceptance",
			Excerpt: "no acceptance checks",
		}},
	}, nil
}
