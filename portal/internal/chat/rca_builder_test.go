package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"
	"github.com/sixath/framework/executor"
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

func TestRegisterRCATool_ExistingConfiguredRootsWithoutMountRegisters(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	cfg := map[string]any{"rca": map[string]any{"func_path": "rca_code", "roots": []any{root}}}
	registerRCATool(reg, cfg, t.TempDir())
	if !rcaHas(reg, "rca_grep") {
		t.Fatal("existing configured roots should register rca_code when workspace/code is missing")
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
	if rcaHas(reg, "rca_grep") || rcaHas(reg, "jaeger_trace") || rcaHas(reg, "es_log_query") || rcaHas(reg, "vm_run_cmd") {
		t.Fatal("unknown func_path must register nothing")
	}
}

func TestRegisterRCATool_VMRunCmdDeferred(t *testing.T) {
	reg := tool.NewRegistry()
	registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path": "vm_run_cmd"}}, "")
	if rcaHas(reg, "vm_run_cmd") {
		t.Fatal("registerRCATool must defer vm_run_cmd to BuildRegistry")
	}
}

func TestBuildRegistry_VMRunCmdWithoutMySQL(t *testing.T) {
	vm, err := structpb.NewStruct(map[string]any{"rca": map[string]any{"func_path": "vm_run_cmd"}})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	if _, err := BuildRegistry([]*biz.ToolMeta{{Name: "rca-vm", Type: biz.ToolTypeRCA, Config: vm}}, nil, reg); err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	if !rcaHas(reg, "vm_run_cmd") {
		t.Fatal("vm_run_cmd must register without a MySQL datasource")
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

type fakeVMIPExec struct {
	res  *executor.Result
	err  error
	dsID string
	sql  string
	opts executor.ExecuteOptions
}

func (f *fakeVMIPExec) Execute(_ context.Context, datasourceID string, dsl string, opts executor.ExecuteOptions) (*executor.Result, error) {
	f.dsID = datasourceID
	f.sql = dsl
	f.opts = opts
	return f.res, f.err
}

func TestVMIPLookup_ScansRows(t *testing.T) {
	t.Run("nil_executor", func(t *testing.T) {
		lookup := vmIPLookup(nil)
		if _, _, err := lookup(context.Background(), "ds", 1); err == nil {
			t.Fatal("nil executor must error")
		}
	})
	t.Run("zero_addresses", func(t *testing.T) {
		fake := &fakeVMIPExec{res: &executor.Result{Columns: []string{"mgr_ipv4_address"}, Rows: [][]any{{""}, {nil}}}}
		_, _, err := vmIPLookup(fake)(context.Background(), "game-mysql", 42)
		if err == nil {
			t.Fatal("0 non-empty addresses must error")
		}
		if fake.sql != tool.VMIPLookupSQL {
			t.Fatalf("sql=%q", fake.sql)
		}
		if fake.dsID != "game-mysql" || fake.opts.Timeout != 10 || fake.opts.MaxRows != 8 {
			t.Fatalf("opts ds=%s timeout=%d maxRows=%d", fake.dsID, fake.opts.Timeout, fake.opts.MaxRows)
		}
		if len(fake.opts.PositionalParams) != 1 || fake.opts.PositionalParams[0] != int64(42) {
			t.Fatalf("params=%v", fake.opts.PositionalParams)
		}
	})
	t.Run("one_address", func(t *testing.T) {
		fake := &fakeVMIPExec{res: &executor.Result{Columns: []string{"mgr_ipv4_address"}, Rows: [][]any{{"10.1.2.3"}}}}
		host, amb, err := vmIPLookup(fake)(context.Background(), "ds", 7)
		if err != nil {
			t.Fatal(err)
		}
		if host != "10.1.2.3" || amb {
			t.Fatalf("host=%q ambiguous=%v", host, amb)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		fake := &fakeVMIPExec{res: &executor.Result{Columns: []string{"other", "mgr_ipv4_address"}, Rows: [][]any{
			{"x", "10.0.0.1"},
			{"y", "10.0.0.2"},
		}}}
		host, amb, err := vmIPLookup(fake)(context.Background(), "ds", 8)
		if err != nil {
			t.Fatal(err)
		}
		if host != "10.0.0.1" || !amb {
			t.Fatalf("host=%q ambiguous=%v", host, amb)
		}
	})
	t.Run("column_zero_fallback", func(t *testing.T) {
		fake := &fakeVMIPExec{res: &executor.Result{Columns: []string{"ip"}, Rows: [][]any{{"192.168.1.9"}}}}
		host, amb, err := vmIPLookup(fake)(context.Background(), "ds", 9)
		if err != nil {
			t.Fatal(err)
		}
		if host != "192.168.1.9" || amb {
			t.Fatalf("host=%q ambiguous=%v", host, amb)
		}
	})
}
