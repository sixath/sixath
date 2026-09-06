package harness

import (
	"encoding/json"
	"testing"
)

func TestContextTraceMerge_PerInvocationFields(t *testing.T) {
	tr := &RunTrace{}
	beginModelInvocation(tr, "tools")
	fn := contextTraceMerge(tr)
	fn("l0_compress", map[string]any{"messages_removed": 2})
	fn("strip_orphan_tools", map[string]any{"messages_removed": 1})
	fn("l2_pre_prune_tool", map[string]any{"runes_removed": 50})
	fn("snip_compact", map[string]any{"messages_removed": 3})

	beginModelInvocation(tr, "plain_after_tools")
	fn("l1_sanitize", map[string]any{"messages_touched": 1})

	if tr.ContextOps.L0DroppedMessages != 2 {
		t.Fatalf("aggregate L0 want 2, got %d", tr.ContextOps.L0DroppedMessages)
	}
	if tr.ContextOps.StripOrphanTools != 1 {
		t.Fatalf("aggregate strip want 1, got %d", tr.ContextOps.StripOrphanTools)
	}
	if tr.ContextOps.L2PrePruneRunesRemoved != 50 {
		t.Fatalf("aggregate pre-prune runes want 50, got %d", tr.ContextOps.L2PrePruneRunesRemoved)
	}
	if tr.ContextOps.SnipCompactRemoved != 3 {
		t.Fatalf("aggregate snip want 3, got %d", tr.ContextOps.SnipCompactRemoved)
	}
	if len(tr.ContextOps.Invocations) != 2 {
		t.Fatalf("invocations len want 2, got %d", len(tr.ContextOps.Invocations))
	}
	i0 := tr.ContextOps.Invocations[0]
	if i0.L0DroppedMessages != 2 || i0.StripOrphanTools != 1 || i0.L2ToolPrePruneRunesRemoved != 50 {
		t.Fatalf("invocation 0: %#v", i0)
	}
	if i0.SanitizeApplied {
		t.Fatal("invocation 0 should not have sanitize")
	}
	i1 := tr.ContextOps.Invocations[1]
	if !i1.SanitizeApplied || i1.L0DroppedMessages != 0 {
		t.Fatalf("invocation 1: %#v", i1)
	}
}

func TestRunTrace_ContextOpsInvocations_JSONRoundTrip(t *testing.T) {
	tr := &RunTrace{RequestID: "rid"}
	beginModelInvocation(tr, "plain")
	contextTraceMerge(tr)("l0_compress", map[string]any{"messages_removed": 1})
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	co, ok := m["context_ops"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_ops, got %#v", m)
	}
	inv, ok := co["invocations"].([]any)
	if !ok || len(inv) != 1 {
		t.Fatalf("expected invocations[1], got %#v", co["invocations"])
	}
	row := inv[0].(map[string]any)
	if row["mode"] != "plain" {
		t.Fatalf("mode: %#v", row["mode"])
	}
	if row["l0_dropped"].(float64) != 1 {
		t.Fatalf("per-invocation l0_dropped: %#v", row["l0_dropped"])
	}
}

func TestContextTraceMerge_CollectsL2SummaryHashesList(t *testing.T) {
	tr := &RunTrace{}
	beginModelInvocation(tr, "tools")
	fn := contextTraceMerge(tr)
	fn("l2_summarize", map[string]any{"summary_hash": "h1", "summary_text": "s1", "middle_removed": 3})
	beginModelInvocation(tr, "plain_after_tools")
	fn("l2_summarize", map[string]any{"summary_hash": "h2", "summary_text": "s2", "middle_removed": 5})
	if tr.ContextOps.L2SummaryHash != "h2" {
		t.Fatalf("latest summary hash mismatch: %#v", tr.ContextOps)
	}
	if tr.LastL2Summary != "s2" {
		t.Fatalf("LastL2Summary want s2, got %q", tr.LastL2Summary)
	}
	if tr.LastL2MiddleRemoved != 5 {
		t.Fatalf("LastL2MiddleRemoved want 5, got %d", tr.LastL2MiddleRemoved)
	}
	if len(tr.ContextOps.L2SummaryHashes) != 2 || tr.ContextOps.L2SummaryHashes[0] != "h1" || tr.ContextOps.L2SummaryHashes[1] != "h2" {
		t.Fatalf("summary hash list mismatch: %#v", tr.ContextOps.L2SummaryHashes)
	}
}
