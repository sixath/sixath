package chat

import (
	"testing"

	"backend/internal/biz"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestFilterServersForSurface(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab"}, {ID: "confluence"}}
	got := filterServersForSurface(servers, familySet([]string{FamilyCore, "mcp:gitlab"}))
	if len(got) != 1 || got[0].ID != "gitlab" {
		t.Fatalf("%v", got)
	}
}

func TestFilterToolsForSurface_DropsRCA(t *testing.T) {
	rcaCfg, err := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "jaeger_trace"}})
	if err != nil {
		t.Fatal(err)
	}
	sshCfg, err := structpb.NewStruct(map[string]any{"func_path": "ssh_exec"})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{
		{Name: "rca-j", Type: biz.ToolTypeRCA, Config: rcaCfg},
		{Name: "ssh", Type: biz.ToolTypeBuiltin, Config: sshCfg},
	}
	got := filterToolsForSurface(tools, familySet([]string{FamilyCore}))
	if len(got) != 1 || got[0].Name != "ssh" {
		t.Fatalf("%v", got)
	}
}
