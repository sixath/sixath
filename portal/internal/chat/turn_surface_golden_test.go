package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestGolden_GitLabQuery_NoJaegerInRegistry(t *testing.T) {
	t.Setenv(turnToolSurfaceEnv, "1")
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	rcaCfg, err := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "rca-j", Type: biz.ToolTypeRCA, Config: rcaCfg}}
	bound := BoundFamiliesFrom(tools, servers, false, false)
	res := IntentResolver{}.Resolve(context.Background(), IntentResolveInput{
		UserText:      "查 GitLab 项目列表",
		BoundFamilies: bound,
		Servers:       servers,
	})
	active := familySet(res.ActiveFamilies)
	ft := filterToolsForSurface(tools, active)
	for _, tmeta := range ft {
		if tmeta.Type == biz.ToolTypeRCA {
			t.Fatal("RCA tool must be filtered out")
		}
	}
	fs := filterServersForSurface(servers, active)
	if len(fs) != 1 || fs[0].ID != "gitlab" {
		t.Fatalf("%v", fs)
	}
}

func TestGolden_MultiIntentKeepsGitLabAndRCA(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := append(BoundFamiliesFrom(nil, servers, false, false), FamilyRCA)
	res := IntentResolver{}.Resolve(context.Background(), IntentResolveInput{
		UserText:      "GitLab 部署失败，看下 Jaeger",
		BoundFamilies: bound,
		Servers:       servers,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set["mcp:gitlab"]; !ok {
		t.Fatalf("%+v", res)
	}
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("%+v", res)
	}
}

func TestGolden_SurfaceOff_NoFilter(t *testing.T) {
	t.Setenv(turnToolSurfaceEnv, "0")
	if ToolSurfaceEnabled() {
		t.Fatal("off")
	}
}
