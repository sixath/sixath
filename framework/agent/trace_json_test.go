package agent

import (
	"encoding/json"
	"testing"
)

func TestRunTrace_ContextOps_JSONOmitempty(t *testing.T) {
	tr := &RunTrace{
		RequestID: "r1",
		ContextOps: &ContextOpsTrace{
			L0DroppedMessages: 2,
			StripOrphanTools:  1,
			Invocations: []ContextOpsInvocation{
				{Index: 0, Mode: "tools"},
			},
		},
	}
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
		t.Fatalf("expected context_ops object, got %#v", m)
	}
	if co["l0_dropped"].(float64) != 2 {
		t.Fatalf("expected l0_dropped=2, got %#v", co["l0_dropped"])
	}
}
