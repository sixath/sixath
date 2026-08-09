package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestPrepareTurnToolSurface_GitLabDropsRCA(t *testing.T) {
	t.Setenv(turnToolSurfaceEnv, "1")
	rcaCfg, err := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "rca-j", Type: biz.ToolTypeRCA, Config: rcaCfg}}
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	active, res := PrepareTurnToolSurface(context.Background(), "查 GitLab 项目列表", tools, servers, nil, nil)
	if res.Source != "rules" {
		t.Fatalf("source=%s reason=%s", res.Source, res.Reason)
	}
	if !FamilyActive(active, "mcp:gitlab") {
		t.Fatalf("active=%v", res.ActiveFamilies)
	}
	if FamilyActive(active, FamilyRCA) {
		t.Fatalf("rca must not be active: %v", res.ActiveFamilies)
	}
}

func TestPrepareTurnToolSurface_Disabled(t *testing.T) {
	t.Setenv(turnToolSurfaceEnv, "0")
	active, res := PrepareTurnToolSurface(context.Background(), "查 GitLab", nil, nil, nil, nil)
	if active != nil || res.Source != "disabled" {
		t.Fatalf("active=%v res=%+v", active, res)
	}
}
