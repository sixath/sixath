package chat

import (
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"
	"github.com/sixath/framework/tool"
	"google.golang.org/protobuf/types/known/structpb"
)

func rcaHas(reg *tool.Registry, name string) bool {
	_, ok := reg.Get(name)
	return ok
}

func withCodeMount(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestRegisterRCATool_Code(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{"func_path": "rca_code", "roots": []any{"/repos/a", "/repos/b"}}}
	registerRCATool(reg, cfg, withCodeMount(t))
	for _, n := range []string{"rca_grep", "rca_glob", "rca_read"} {
		if !rcaHas(reg, n) {
			t.Fatalf("expected %s registered", n)
		}
	}
}

func TestRegisterRCATool_CodeConfiguredRootsWithoutMountSkips(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{"func_path": "rca_code", "roots": []any{"/repos/a", "/repos/b"}}}
	registerRCATool(reg, cfg, t.TempDir())
	if rcaHas(reg, "rca_grep") {
		t.Fatal("configured roots must not register rca_code without workspace/code")
	}
}

func TestRegisterRCATool_CodeNoRootsSkips(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "rca_code"}}, "")
	if rcaHas(reg, "rca_grep") {
		t.Fatal("rca_code with no roots must register nothing")
	}
}

func TestRegisterRCATool_Symbol(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{
		"func_path":           "rca_symbol",
		"roots":               []any{"/repos/a"},
		"gopls_path":          "gopls",
		"ready_timeout_sec":   10,
		"request_timeout_sec": float64(15),
	}}
	registerRCATool(reg, cfg, withCodeMount(t))
	if !rcaHas(reg, "rca_symbol") {
		t.Fatal("rca_symbol should be registered")
	}
}

func TestRegisterRCATool_SymbolNoRootsSkips(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "rca_symbol"}}, "")
	if rcaHas(reg, "rca_symbol") {
		t.Fatal("rca_symbol with no roots must register nothing")
	}
}

func TestRegisterRCATool_Jaeger(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"}}, "")
	if !rcaHas(reg, "jaeger_trace") {
		t.Fatal("jaeger_trace should be registered")
	}
}

func TestRegisterRCATool_ESFound(t *testing.T) {
	esDS, _ := structpb.NewStruct(map[string]any{
		"datasource": map[string]any{"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200"},
	})
	esLog, _ := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "datasource_id": "es-logs", "default_index": "app-*", "trace_id_field": "trace_id"},
	})
	tools := []*biz.ToolMeta{
		{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: esDS},
		{Name: "rca-es", Type: biz.ToolTypeRCA, Config: esLog},
	}
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "es_log_query", "datasource_id": "es-logs"}}, "")
	if rcaHas(reg, "es_log_query") {
		t.Fatal("registerRCATool must not register es_log_query")
	}
	if _, err := BuildRegistry(tools, nil, reg); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !rcaHas(reg, "es_log_query") {
		t.Fatal("es_log_query should be registered when datasource found")
	}
}

func TestRegisterRCATool_ESNotFoundSkips(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "es_log_query", "datasource_id": "missing"}}, "")
	if rcaHas(reg, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource missing")
	}
	esLog, _ := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "datasource_id": "missing"},
	})
	reg2 := tool.NewRegistry()
	registerESLogFromAgentTools(reg2, []*biz.ToolMeta{{Name: "orphan", Type: biz.ToolTypeRCA, Config: esLog}})
	if rcaHas(reg2, "es_log_query") {
		t.Fatal("es_log_query should be skipped when datasource missing")
	}
}

func TestRegisterRCATool_UnknownFuncPath(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "nope"}}, "")
	if rcaHas(reg, "rca_grep") || rcaHas(reg, "jaeger_trace") || rcaHas(reg, "es_log_query") {
		t.Fatal("unknown func_path must register nothing")
	}
}

func TestRegisterRCATool_ESFlatDatasourceConfig(t *testing.T) {
	flat, _ := structpb.NewStruct(map[string]any{"id": "es-logs", "type": "elasticsearch", "dsn": "http://localhost:9200"})
	esLog, _ := structpb.NewStruct(map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "datasource_id": "es-logs", "default_index": "app-*", "trace_id_field": "trace_id"},
	})
	tools := []*biz.ToolMeta{
		{Name: "es-logs", Type: biz.ToolTypeDatasource, Config: flat},
		{Name: "rca-es", Type: biz.ToolTypeRCA, Config: esLog},
	}
	reg := tool.NewRegistry()
	registerESLogFromAgentTools(reg, tools)
	if !rcaHas(reg, "es_log_query") {
		t.Fatal("es_log_query should resolve datasource with flat config too")
	}
}

func TestRegisterRCATool_ESEmptyDatasourceIDSkips(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "es_log_query"}}, "")
	if rcaHas(reg, "es_log_query") {
		t.Fatal("es_log_query must skip when datasource_id empty")
	}
	esLog, _ := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "es_log_query"}})
	reg2 := tool.NewRegistry()
	registerESLogFromAgentTools(reg2, []*biz.ToolMeta{{Name: "empty", Type: biz.ToolTypeRCA, Config: esLog}})
	if rcaHas(reg2, "es_log_query") {
		t.Fatal("collect must skip when both endpoint and datasource_id empty")
	}
}

func TestRegisterRCATool_ESInlineEndpoint(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{
		"func_path":     "es_log_query",
		"endpoint":      "http://localhost:9200",
		"default_index": "app-*",
	}}
	registerRCATool(reg, cfg, "")
	if rcaHas(reg, "es_log_query") {
		t.Fatal("registerRCATool alone must not register es_log_query")
	}

	esLog, err := structpb.NewStruct(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "zj-elk", Type: biz.ToolTypeRCA, Config: esLog}}
	reg2 := tool.NewRegistry()
	if _, err := BuildRegistry(tools, nil, reg2); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !rcaHas(reg2, "es_log_query") {
		t.Fatal("inline endpoint should register es_log_query without agent datasource")
	}
}

func TestRegisterRCATool_ESBothSkip(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{
		"func_path": "es_log_query", "endpoint": "http://es:9200", "datasource_id": "es-logs",
	}}, "")
	if rcaHas(reg, "es_log_query") {
		t.Fatal("both endpoint and datasource_id must skip")
	}
	esLog, _ := structpb.NewStruct(map[string]any{"rca": map[string]any{
		"func_path": "es_log_query", "endpoint": "http://es:9200", "datasource_id": "es-logs",
	}})
	reg2 := tool.NewRegistry()
	registerESLogFromAgentTools(reg2, []*biz.ToolMeta{{Name: "bad", Type: biz.ToolTypeRCA, Config: esLog}})
	if rcaHas(reg2, "es_log_query") {
		t.Fatal("collect must skip when both endpoint and datasource_id set")
	}
}

func TestRegisterRCATool_NoRCASection(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"func_path": "jaeger_trace"}, "") // top-level, no "rca" wrapper
	if rcaHas(reg, "jaeger_trace") {
		t.Fatal("must skip when config has no rca section")
	}
}

func TestBuildRegistry_RCADispatch(t *testing.T) {
	jaeger, _ := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"}})
	tools := []*biz.ToolMeta{
		{Name: "rca-jaeger", Type: biz.ToolTypeRCA, Config: jaeger},
	}
	reg := tool.NewRegistry()
	if _, err := BuildRegistry(tools, nil, reg); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !rcaHas(reg, "jaeger_trace") {
		t.Fatal("BuildRegistry should dispatch rca type to registerRCATool")
	}
}

func TestRegisterRCATool_WorkspaceCodeWithoutConfiguredRoots(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "rca_code"}}, ws)
	if !rcaHas(reg, "rca_grep") {
		t.Fatal("workspace/code should supply rca roots")
	}
}

func TestBuildRegistry_WorkspaceCodeRegistersRCA(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, WorkspaceCodeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	rca, err := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "rca_code"}})
	if err != nil {
		t.Fatal(err)
	}
	tools := []*biz.ToolMeta{{Name: "rca-code", Type: biz.ToolTypeRCA, Config: rca}}
	reg := tool.NewRegistry()
	if _, err := BuildRegistry(tools, nil, reg, RegistryBuildOptions{Workspace: ws}); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !rcaHas(reg, "rca_grep") {
		t.Fatal("BuildRegistry should register rca_code from workspace/code")
	}
}
