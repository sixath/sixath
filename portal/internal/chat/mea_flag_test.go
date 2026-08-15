package chat

import (
	"testing"

	"backend/internal/biz"
)

func TestMEAEnabled_DefaultOff(t *testing.T) {
	t.Setenv("SATH_MEA", "")
	if MEAEnabled() {
		t.Fatal("expected MEAEnabled false when SATH_MEA unset")
	}
}

func TestMEAEnabled_On(t *testing.T) {
	t.Setenv("SATH_MEA", "1")
	if !MEAEnabled() {
		t.Fatal("expected MEAEnabled true when SATH_MEA=1")
	}
}

func TestMEAEnabledForAgent_Pilot(t *testing.T) {
	t.Setenv("SATH_MEA", "0")
	t.Setenv("SATH_MEA_PILOT_AGENTS", "agent-a,agent-b")
	if !MEAEnabledForAgent("agent-a", false) {
		t.Fatal("expected pilot agent-a enabled")
	}
	if MEAEnabledForAgent("other", false) {
		t.Fatal("expected non-pilot agent disabled when global off")
	}
}

func TestMEAEnabledForAgent_RuntimeTools(t *testing.T) {
	t.Setenv("SATH_MEA", "0")
	t.Setenv("SATH_MEA_PILOT_AGENTS", "")
	if MEAEnabledForAgent("agent-x", false) {
		t.Fatal("expected off")
	}
	if !MEAEnabledForAgent("agent-x", true) {
		t.Fatal("expected agent runtime_tools.mea_enabled to enable")
	}
	meta := &biz.AgentMeta{ID: "agent-x", RuntimeTools: biz.RuntimeToolsConfig{MEAEnabled: true}}
	if !MEAEnabledForAgentMeta(meta) {
		t.Fatal("expected meta wrapper enabled")
	}
}
