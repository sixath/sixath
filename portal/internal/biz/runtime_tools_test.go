package biz

import (
	"testing"

	agentv1 "backend/api/agent/v1"
)

func TestRuntimeToolsRoundTripProto(t *testing.T) {
	in := &agentv1.RuntimeToolsConfig{
		MemoryWriteEnabled:    true,
		WorkspaceFilesEnabled: true,
		TodoEnabled:           true,
		BrowserEnabled:        true,
	}
	bizCfg := RuntimeToolsFromProto(in)
	if !bizCfg.MemoryWriteEnabled || !bizCfg.WorkspaceFilesEnabled || !bizCfg.TodoEnabled || !bizCfg.BrowserEnabled {
		t.Fatalf("from proto: %+v", bizCfg)
	}
	out := RuntimeToolsToProto(bizCfg)
	if !out.GetMemoryWriteEnabled() || !out.GetWorkspaceFilesEnabled() || !out.GetTodoEnabled() || !out.GetBrowserEnabled() {
		t.Fatalf("to proto: %+v", out)
	}
}

func TestRuntimeTools_HybridRecallTriState(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		in := &agentv1.RuntimeToolsConfig{}
		bizCfg := RuntimeToolsFromProto(in)
		if bizCfg.HybridRecall != nil {
			t.Fatalf("unset hybrid_recall: want nil, got %v", *bizCfg.HybridRecall)
		}
		out := RuntimeToolsToProto(bizCfg)
		if out.HybridRecall != nil {
			t.Fatalf("ToProto unset: want HybridRecall nil, got %v", *out.HybridRecall)
		}
	})

	t.Run("explicit_false", func(t *testing.T) {
		f := false
		in := &agentv1.RuntimeToolsConfig{HybridRecall: &f}
		bizCfg := RuntimeToolsFromProto(in)
		if bizCfg.HybridRecall == nil || *bizCfg.HybridRecall {
			t.Fatalf("explicit false: want *bool=false, got %+v", bizCfg.HybridRecall)
		}
		out := RuntimeToolsToProto(bizCfg)
		if out.HybridRecall == nil || *out.HybridRecall {
			t.Fatalf("ToProto false: want *bool=false, got %+v", out.HybridRecall)
		}
	})

	t.Run("explicit_true", func(t *testing.T) {
		tr := true
		in := &agentv1.RuntimeToolsConfig{HybridRecall: &tr}
		bizCfg := RuntimeToolsFromProto(in)
		if bizCfg.HybridRecall == nil || !*bizCfg.HybridRecall {
			t.Fatalf("explicit true: want *bool=true, got %+v", bizCfg.HybridRecall)
		}
		out := RuntimeToolsToProto(bizCfg)
		if out.HybridRecall == nil || !*out.HybridRecall {
			t.Fatalf("ToProto true: want *bool=true, got %+v", out.HybridRecall)
		}
	})
}

func TestRuntimeTools_HubFieldsPresence(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		bizCfg := RuntimeToolsFromProto(&agentv1.RuntimeToolsConfig{})
		if bizCfg.HubGovernance != nil || bizCfg.HubKnowledge != nil || bizCfg.HubFallbackToDefaultOnReadError != nil {
			t.Fatalf("%+v", bizCfg)
		}
		out := RuntimeToolsToProto(bizCfg)
		if out.HubGovernance != nil || out.HubKnowledge != nil || out.HubFallbackToDefaultOnReadError != nil {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("explicit", func(t *testing.T) {
		g, k := "local", "local"
		fb := true
		in := &agentv1.RuntimeToolsConfig{HubGovernance: &g, HubKnowledge: &k, HubFallbackToDefaultOnReadError: &fb}
		bizCfg := RuntimeToolsFromProto(in)
		if bizCfg.HubGovernance == nil || *bizCfg.HubGovernance != "local" {
			t.Fatalf("%+v", bizCfg.HubGovernance)
		}
		out := RuntimeToolsToProto(bizCfg)
		if out.GetHubGovernance() != "local" || out.GetHubKnowledge() != "local" || !out.GetHubFallbackToDefaultOnReadError() {
			t.Fatalf("%+v", out)
		}
	})
}
