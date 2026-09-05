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
