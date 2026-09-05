package harness

import (
	"testing"

	"github.com/sixath/framework/config"
)

func TestToolGuardrailsFromConfig_nilOrDisabled(t *testing.T) {
	if ToolGuardrailsFromConfig(nil) != nil {
		t.Fatal("expected nil")
	}
	if ToolGuardrailsFromConfig(&config.ToolGuardrails{Enabled: false}) != nil {
		t.Fatal("expected nil when disabled")
	}
}

func TestToolGuardrailsFromConfig_warningsOnlyDefaultWhenNoHalt(t *testing.T) {
	g := ToolGuardrailsFromConfig(&config.ToolGuardrails{
		Enabled: true,
		// WarningsOnly 零值为 false，但未配置 halt 阈值时应视为仅告警
		SameArgsFailureWarn: 2,
	})
	if g == nil || g.HardHalt {
		t.Fatalf("expected warnings-only semantics, got %#v", g)
	}
}

func TestToolGuardrailsFromConfig_hardHaltWhenConfigured(t *testing.T) {
	g := ToolGuardrailsFromConfig(&config.ToolGuardrails{
		Enabled:             true,
		WarningsOnly:        false,
		SameArgsFailureWarn: 2,
		SameArgsFailureHalt: 2,
	})
	if g == nil || !g.HardHalt {
		t.Fatalf("expected HardHalt, got %#v", g)
	}
}
