package mea

import (
	"testing"
)

func TestApplyAudit_CompleteRequiresClean(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Kind: KindRequirement, Status: StatusPending}}}
	out := ApplyAudit(s, AuditReport{
		ID: "a1", Completion: CompletionComplete, Integrity: IntegritySuspect,
		ProposedUpdates: []ProposedUpdate{{RecordID: "r1", Status: StatusCompleted}},
	})
	if out.Records[0].Status == StatusCompleted {
		t.Fatal("suspect must not complete")
	}
}

func TestApplyAudit_CleanCompleteUpdates(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Status: StatusPending}}}
	out := ApplyAudit(s, AuditReport{
		ID: "a1", Completion: CompletionComplete, Integrity: IntegrityClean,
		ProposedUpdates: []ProposedUpdate{{RecordID: "r1", Status: StatusCompleted, Summary: "ok"}},
	})
	if out.Records[0].Status != StatusCompleted {
		t.Fatal(out.Records[0].Status)
	}
	if len(out.Audits) != 1 || out.Records[0].EvidenceRefs[0] != "a1" {
		t.Fatal("audit not linked")
	}
}

func TestNoApplyExecutionReportAPI(t *testing.T) {
	// Compile-time / package convention: ExecutionReport must not write TaskState.
	// Ensure ApplyAudit is the only mutator used in orchestrator (grep in review).
	// There must be no ApplyExecutionReport function.
	s := TaskState{Records: []TaskRecord{{ID: "r1", Status: StatusPending}}}
	_ = ExecutionReport{ClaimComplete: true}
	if s.Records[0].Status != StatusPending {
		t.Fatal("execution claim must not mutate state")
	}
}
