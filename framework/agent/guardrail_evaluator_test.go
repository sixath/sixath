package agent

import (
	"testing"

	"github.com/sixath/framework/events"
)

func TestNewGuardrailEvaluator_DisabledIsNoop(t *testing.T) {
	ev := NewGuardrailEvaluator(&ToolGuardrailsConfig{Enabled: false})
	var emitted bool
	d := ev.Evaluate([]ToolCallRecord{{ToolName: "ssh_exec", Error: "e"}}, 0, func(events.Kind, map[string]any) {
		emitted = true
	})
	if d.Halt || d.Warn {
		t.Fatalf("expected no warn/halt, got %#v", d)
	}
	if emitted {
		t.Fatal("emit should not run when guardrails disabled")
	}
}

func TestNewGuardrailEvaluator_EnabledDelegatesToRules(t *testing.T) {
	cfg := &ToolGuardrailsConfig{
		Enabled: true, HardHalt: false,
		SameArgsFailureWarn: 2,
	}
	ev := NewGuardrailEvaluator(cfg)
	h := []ToolCallRecord{
		{ToolName: "x", Error: "e", Arguments: map[string]any{"a": float64(1)}},
		{ToolName: "x", Error: "e", Arguments: map[string]any{"a": float64(1)}},
	}
	var warns int
	d := ev.Evaluate(h, 0, func(k events.Kind, _ map[string]any) {
		if k == events.ToolGuardrailWarn {
			warns++
		}
	})
	if d.Halt {
		t.Fatal("unexpected halt")
	}
	if !d.Warn || warns < 1 {
		t.Fatalf("expected warn on R1 streak=2, decision=%#v warns=%d", d, warns)
	}
}
