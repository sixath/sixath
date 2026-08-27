package mea

import (
	"context"
	"testing"
)

func autoTraceChecks() []AcceptanceCheck {
	return []AcceptanceCheck{{Type: "trace_hit_status"}, {Type: "empty_hit_speak"}}
}

func esEmptyHit() ToolHit {
	return ToolHit{
		ToolName:     "es_log_query",
		HitStatus:    "empty",
		QueriedIndex: "vm-manager-*",
	}
}

func TestEvalGolden_mea_empty_speak(t *testing.T) {
	aud := RulesAuditor{WorkDir: ""}
	c := Contract{Round: 1, TargetRecordID: "r1", AcceptanceChecks: autoTraceChecks()}

	t.Run("deny", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			ClaimComplete: true,
			FinalText:     "该服务从未参与",
			ToolHits:      []ToolHit{esEmptyHit()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionIncomplete {
			t.Fatalf("T-speak %+v", v)
		}
		if len(v.ProposedUpdates) != 0 {
			t.Fatalf("must not propose completed: %+v", v.ProposedUpdates)
		}
	})

	t.Run("ok", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			FinalText: "该索引 0 条，不能据此说从未参与，查了 vm-manager-*",
			ToolHits:  []ToolHit{esEmptyHit()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionComplete || v.Integrity != IntegrityClean {
			t.Fatalf("T-speak-ok %+v", v)
		}
		if len(v.ProposedUpdates) != 1 || v.ProposedUpdates[0].Status != StatusCompleted {
			t.Fatalf("proposed %+v", v.ProposedUpdates)
		}
	})

	t.Run("missing_hit_status", func(t *testing.T) {
		v, err := aud.Audit(context.Background(), TaskState{}, c, ExecutionReport{
			FinalText: "ok",
			ToolHits:  []ToolHit{{ToolName: "es_log_query"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Completion != CompletionIncomplete {
			t.Fatalf("T-hit %+v", v)
		}
	})
}

func TestEvalGolden_mea_claim(t *testing.T) {
	s := TaskState{Records: []TaskRecord{{ID: "r1", Kind: KindRequirement, Status: StatusPending}}}
	_ = ExecutionReport{ClaimComplete: true}
	out := ApplyAudit(s, AuditReport{
		ID:         "a1",
		Completion: CompletionIncomplete,
		Integrity:  IntegritySuspect,
		ProposedUpdates: []ProposedUpdate{{
			RecordID: "r1",
			Status:   StatusCompleted,
			Summary:  "executor claimed",
		}},
	})
	if out.Records[0].Status == StatusCompleted {
		t.Fatal("ClaimComplete/incomplete audit must not complete")
	}
	if out.Records[0].Status != StatusPending {
		t.Fatalf("status=%s", out.Records[0].Status)
	}
}
