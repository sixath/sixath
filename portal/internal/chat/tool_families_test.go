package chat

import (
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestToolSurfaceEnabled_DefaultOn(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	if !ToolSurfaceEnabled() {
		t.Fatal("default should be enabled")
	}
}

func TestToolSurfaceEnabled_Off(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "0")
	if ToolSurfaceEnabled() {
		t.Fatal("0 should disable")
	}
}

func TestFamilyForBuiltinToolName(t *testing.T) {
	if FamilyForBuiltinToolName("jaeger_trace") != FamilyRCA {
		t.Fatal("jaeger → rca")
	}
	if FamilyForBuiltinToolName("web_search") != FamilyWeb {
		t.Fatal("web_search → web")
	}
	if FamilyForBuiltinToolName("knowledge_read") != FamilyKnowledge {
		t.Fatal("knowledge → knowledge")
	}
	if FamilyForBuiltinToolName("todo") != FamilyCore {
		t.Fatal("todo → core")
	}
}

func TestBoundFamiliesFromBindings(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}, {ID: "confluence", Name: "Confluence"}}
	rcaCfg, err := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "jaeger_trace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{
		{Name: "rca-j", Type: biz.ToolTypeRCA, Config: rcaCfg},
	}
	bound := BoundFamiliesFrom(tools, servers, true /* web */, true /* knowledge */)
	want := []string{FamilyCore, FamilyRCA, FamilyWeb, FamilyKnowledge, "mcp:gitlab", "mcp:confluence"}
	set := familySet(bound)
	for _, id := range want {
		if _, ok := set[id]; !ok {
			t.Fatalf("missing %s in %#v", id, bound)
		}
	}
}

func TestFamilyForRegisteredTool_MCPBinding(t *testing.T) {
	tl := tool.Tool{Name: "list_projects", Bindings: map[string]string{"mcp_server": "gitlab"}}
	if FamilyForRegisteredTool(tl) != "mcp:gitlab" {
		t.Fatalf("got %q", FamilyForRegisteredTool(tl))
	}
}
