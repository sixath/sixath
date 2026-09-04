package chat

import (
	"testing"

	"backend/internal/conf"
)

func resetInvestigationOverrides(t *testing.T) {
	t.Helper()
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	t.Setenv("SATH_TURN_INTENT_GATE", "")
	t.Setenv("SATH_TASK_LOCK", "")
	resetTurnToolSurfaceOverride()
	resetTurnIntentGateOverride()
	resetTaskLockOverride()
	t.Cleanup(resetTurnToolSurfaceOverride)
	t.Cleanup(resetTurnIntentGateOverride)
	t.Cleanup(resetTaskLockOverride)
}

func TestApplyInvestigationGates_OffDisablesAll(t *testing.T) {
	resetInvestigationOverrides(t)

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "off"})
	if ToolSurfaceEnabled() {
		t.Fatal("surface")
	}
	if NewTurnIntentGate() != nil {
		t.Fatal("intent gate")
	}
	if TaskLockEnabled() {
		t.Fatal("task lock")
	}
}

func TestApplyInvestigationGates_OffIgnoresYAMLSurface(t *testing.T) {
	resetInvestigationOverrides(t)

	on := true
	ApplyInvestigationGates(&conf.ChatConfig{
		InvestigationGates:     "off",
		TurnToolSurfaceEnabled: &on,
	})
	if ToolSurfaceEnabled() {
		t.Fatal("yaml turn_tool_surface_enabled must not reopen B when master off")
	}
}

func TestApplyInvestigationGates_IntentEnvOverridesOff(t *testing.T) {
	resetInvestigationOverrides(t)
	t.Setenv("SATH_TURN_INTENT_GATE", "1")

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "off"})
	if NewTurnIntentGate() == nil {
		t.Fatal("SATH_TURN_INTENT_GATE=1 must install gate")
	}
}

func TestApplyInvestigationGates_OnEnablesAll(t *testing.T) {
	resetInvestigationOverrides(t)

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "on"})
	if !ToolSurfaceEnabled() || NewTurnIntentGate() == nil || !TaskLockEnabled() {
		t.Fatal("master on")
	}
}
