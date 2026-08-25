package chat

import (
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestToolSurfaceEnabled_DefaultOn(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	resetTurnToolSurfaceOverride()
	t.Cleanup(resetTurnToolSurfaceOverride)
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

func TestToolSurfaceEnabled_YAMLOffWhenEnvUnset(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	resetTurnToolSurfaceOverride()
	t.Cleanup(resetTurnToolSurfaceOverride)
	SetTurnToolSurfaceEnabled(false)
	if ToolSurfaceEnabled() {
		t.Fatal("yaml false should disable when env unset")
	}
}

func TestToolSurfaceEnabled_EnvOverridesYAML(t *testing.T) {
	resetTurnToolSurfaceOverride()
	t.Cleanup(resetTurnToolSurfaceOverride)
	SetTurnToolSurfaceEnabled(false)
	t.Setenv("SATH_TURN_TOOL_SURFACE", "1")
	if !ToolSurfaceEnabled() {
		t.Fatal("env 1 should win over yaml false")
	}
}

func TestFamilyForBuiltinToolName_CodeVsRCA(t *testing.T) {
	if FamilyForBuiltinToolName("rca_grep") != FamilyCode {
		t.Fatal("rca_grep → code")
	}
	if FamilyForBuiltinToolName("rca_glob") != FamilyCode {
		t.Fatal("rca_glob → code")
	}
	if FamilyForBuiltinToolName("rca_read") != FamilyCode {
		t.Fatal("rca_read → code")
	}
	if FamilyForBuiltinToolName("rca_symbol") != FamilyCode {
		t.Fatal("rca_symbol → code")
	}
	if FamilyForBuiltinToolName("jaeger_trace") != FamilyRCA {
		t.Fatal("jaeger → rca")
	}
	if FamilyForBuiltinToolName("es_log_query") != FamilyRCA {
		t.Fatal("es_log_query → rca")
	}
}

func mustRCAStruct(t *testing.T, funcPath string, extra map[string]any) *structpb.Struct {
	t.Helper()
	rca := map[string]any{"func_path": funcPath}
	for k, v := range extra {
		rca[k] = v
	}
	st, err := structpb.NewStruct(map[string]any{"rca": rca})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestBoundFamiliesFrom_SplitsRCACodeAndLogs(t *testing.T) {
	codeTool := &biz.ToolMeta{Type: biz.ToolTypeRCA, Config: mustRCAStruct(t, "rca_code", map[string]any{
		"roots": []any{"D:\\workspace\\migu"},
	})}
	esTool := &biz.ToolMeta{Type: biz.ToolTypeRCA, Config: mustRCAStruct(t, "es_log_query", map[string]any{
		"endpoint": "http://es",
	})}
	bound := BoundFamiliesFrom([]*biz.ToolMeta{codeTool, esTool}, nil, false, false)
	set := familySet(bound)
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("want code, got %v", bound)
	}
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("want rca, got %v", bound)
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

func TestFamilyForBuiltinToolName_SplitDataSkillsMemory(t *testing.T) {
	t.Setenv(toolFamilySplitEnv, "1")
	if FamilyForBuiltinToolName("list_tables") != FamilyData {
		t.Fatal("list_tables → data")
	}
	if FamilyForBuiltinToolName("skill_view") != FamilySkills {
		t.Fatal("skill_view → skills")
	}
	if FamilyForBuiltinToolName("memory_recall") != FamilyMemory {
		t.Fatal("memory_recall → memory")
	}
	t.Setenv(toolFamilySplitEnv, "0")
	if FamilyForBuiltinToolName("list_tables") != FamilyCore {
		t.Fatal("split off: list_tables → core")
	}
}

func TestBoundFamiliesFrom_DatasourceIsData(t *testing.T) {
	t.Setenv(toolFamilySplitEnv, "1")
	tools := []*biz.ToolMeta{{Name: "migu_mongodb", Type: biz.ToolTypeDatasource}}
	set := familySet(BoundFamiliesFrom(tools, nil, false, false))
	if _, ok := set[FamilyData]; !ok {
		t.Fatalf("want data, got %v", set)
	}
}

func TestInferPrimaryFamilies(t *testing.T) {
	got := familySet(InferPrimaryFamilies([]string{FamilyCore, FamilyCode, FamilyData}))
	if _, ok := got[FamilyCode]; !ok {
		t.Fatal("code primary")
	}
	if _, ok := got[FamilyData]; ok {
		t.Fatal("data is not primary when code is bound")
	}
	got = familySet(InferPrimaryFamilies([]string{FamilyCore, FamilyData}))
	if _, ok := got[FamilyData]; !ok {
		t.Fatal("data-only agent primary is data")
	}
}

func TestFamilyForRegisteredTool_MCPBinding(t *testing.T) {
	tl := tool.Tool{Name: "list_projects", Bindings: map[string]string{"mcp_server": "gitlab"}}
	if FamilyForRegisteredTool(tl) != "mcp:gitlab" {
		t.Fatalf("got %q", FamilyForRegisteredTool(tl))
	}
}
