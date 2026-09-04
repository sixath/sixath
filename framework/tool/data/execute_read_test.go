package tooldata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/metadata"
	core "github.com/sixath/framework/tool"
)

type fakeExecutor struct {
	calls          []execCall
	ret            *executor.Result
	err            error
	failIfContains string
}

type execCall struct {
	DatasourceID string
	DSL          string
	Opts         executor.ExecuteOptions
}

func (f *fakeExecutor) Execute(ctx context.Context, datasourceID, dsl string, opts executor.ExecuteOptions) (*executor.Result, error) {
	f.calls = append(f.calls, execCall{
		DatasourceID: datasourceID,
		DSL:          dsl,
		Opts:         opts,
	})
	return f.ret, f.err
}

func (f *fakeExecutor) Query(ctx context.Context, datasourceID, dsl string, opts executor.QueryOptions) (*executor.QueryResult, error) {
	f.calls = append(f.calls, execCall{
		DatasourceID: datasourceID,
		DSL:          dsl,
		Opts: executor.ExecuteOptions{
			Timeout: opts.Timeout,
			MaxRows: opts.MaxRows,
			Params:  opts.Params,
		},
	})
	if f.failIfContains != "" && strings.Contains(dsl, f.failIfContains) {
		return nil, f.err
	}
	if f.ret == nil {
		return nil, f.err
	}
	return &executor.QueryResult{
		Columns:        f.ret.Columns,
		Rows:           f.ret.Rows,
		Truncated:      f.ret.Truncated,
		EstimatedTotal: f.ret.EstimatedTotal,
	}, nil
}

func TestExecuteRead_Basic(t *testing.T) {
	f := &fakeExecutor{
		ret: &executor.Result{
			Columns: []string{"id"},
			Rows:    [][]any{{int64(1)}},
		},
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "ds1",
		DefaultTimeoutSec:   10,
		DefaultMaxRows:      100,
	}

	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, cfg); err != nil {
		t.Fatalf("RegisterExecuteReadTool: %v", err)
	}
	tool, ok := reg.Get("execute_read")
	if !ok {
		t.Fatal("execute_read not found")
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"dsl": "SELECT 1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res, ok := out.(*executor.QueryResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	if len(res.Columns) != 1 || res.Columns[0] != "id" {
		t.Errorf("columns: %v", res.Columns)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(f.calls))
	}
	call := f.calls[0]
	if call.DatasourceID != "ds1" {
		t.Errorf("datasourceID: %s", call.DatasourceID)
	}
	if call.DSL != "SELECT 1" {
		t.Errorf("dsl: %s", call.DSL)
	}
	if call.Opts.AllowWrite || call.Opts.ReadOnly {
		t.Errorf("read path should not set write flags on opts")
	}
	if call.Opts.Timeout != 10 || call.Opts.MaxRows != 100 {
		t.Errorf("opts: %+v", call.Opts)
	}
}

func TestExecuteRead_OverrideOptionsAndDatasource(t *testing.T) {
	f := &fakeExecutor{ret: &executor.Result{}}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "default",
		DefaultTimeoutSec:   0,
		DefaultMaxRows:      0,
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tool, _ := reg.Get("execute_read")

	_, err := tool.Execute(context.Background(), map[string]any{
		"query":         "SELECT * FROM t",
		"datasource_id": "other",
		"timeout_sec":   5,
		"max_rows":      2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(f.calls))
	}
	call := f.calls[0]
	if call.DatasourceID != "other" {
		t.Errorf("datasourceID: %s", call.DatasourceID)
	}
	if call.DSL != "SELECT * FROM t" {
		t.Errorf("dsl: %s", call.DSL)
	}
	if call.Opts.Timeout != 5 || call.Opts.MaxRows != 2 {
		t.Errorf("opts: %+v", call.Opts)
	}
}

func TestExecuteRead_NotConfigured(t *testing.T) {
	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tool, _ := reg.Get("execute_read")
	_, err := tool.Execute(context.Background(), map[string]any{
		"dsl": "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error when not configured")
	}
}

func TestExecuteRead_DatasourceRequired(t *testing.T) {
	f := &fakeExecutor{}
	cfg := &ExecuteReadConfig{
		Exec: f,
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tool, _ := reg.Get("execute_read")
	_, err := tool.Execute(context.Background(), map[string]any{
		"dsl": "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error when datasource_id missing and no default")
	}
}

func TestExecuteRead_DslRequired(t *testing.T) {
	f := &fakeExecutor{}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tool, _ := reg.Get("execute_read")
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when dsl/query missing")
	}
}

func TestExecuteRead_InvalidTimeoutOrMaxRows(t *testing.T) {
	f := &fakeExecutor{}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tool, _ := reg.Get("execute_read")

	if _, err := tool.Execute(context.Background(), map[string]any{
		"dsl":         "SELECT 1",
		"timeout_sec": -1,
	}); err == nil {
		t.Fatal("expected error for negative timeout_sec")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{
		"dsl":      "SELECT 1",
		"max_rows": -2,
	}); err == nil {
		t.Fatal("expected error for negative max_rows")
	}
}

func TestExecuteRead_ExecutorErrorWrapped(t *testing.T) {
	inner := errors.New("boom")
	f := &fakeExecutor{
		err: inner,
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tool, _ := reg.Get("execute_read")

	_, err := tool.Execute(context.Background(), map[string]any{
		"dsl": "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, inner) {
		t.Fatalf("expected wrapped inner error, got: %v", err)
	}
}

func TestExecuteRead_SpillsOverFiftyRows(t *testing.T) {
	rows := make([][]any, 51)
	for i := range rows {
		rows[i] = []any{int64(i)}
	}
	f := &fakeExecutor{
		ret: &executor.Result{
			Columns: []string{"id"},
			Rows:    rows,
		},
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		DefaultDatasourceID: "ds1",
		DefaultTimeoutSec:   10,
		DefaultMaxRows:      100,
	}

	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, cfg); err != nil {
		t.Fatalf("RegisterExecuteReadTool: %v", err)
	}
	tl, ok := reg.Get("execute_read")
	if !ok {
		t.Fatal("execute_read not found")
	}

	root := t.TempDir()
	ctx := context.WithValue(context.Background(), core.ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, core.ContextKeySessionID, "sess-1")

	out, err := tl.Execute(ctx, map[string]any{
		"dsl": "SELECT id FROM t",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stub, ok := out.(*core.QuerySpillStub)
	if !ok {
		t.Fatalf("unexpected result type: %T", out)
	}
	if stub.Count != 51 {
		t.Fatalf("Count=%d", stub.Count)
	}
	raw, err := json.Marshal(stub)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"Rows"`) {
		t.Fatal("spill stub must not dump Rows")
	}
}

func testHealStore(t *testing.T) *metadata.InMemoryStore {
	t.Helper()
	store := metadata.NewInMemoryStore(func(ctx context.Context) (*metadata.Schema, error) {
		return vmSchema(), nil
	})
	if _, err := store.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestExecuteRead_healsUnknownSelectColumn(t *testing.T) {
	f := &fakeExecutor{
		ret: &executor.Result{
			Columns: []string{"vmid", "mgr_ipv4_address"},
			Rows:    [][]any{{int64(9076), "10.1.2.3"}},
		},
		err:            schemaErr("Error 1054 (42S22): Unknown column 'ecn_id' in 'field list'"),
		failIfContains: "ecn_id",
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		Store:               testHealStore(t),
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("execute_read")
	out, err := tl.Execute(context.Background(), map[string]any{
		"dsl": "SELECT vmid, mgr_ipv4_address, ecn_id FROM t_game_virtual_machine_info WHERE vmid = 9076",
	})
	if err != nil {
		t.Fatalf("expected heal success, got %v", err)
	}
	res := out.(*executor.QueryResult)
	if res.RepairNote == "" || !strings.Contains(res.RepairNote, "ecn_id") {
		t.Fatalf("repair note: %q", res.RepairNote)
	}
	if len(f.calls) < 2 {
		t.Fatalf("want retry after heal, calls=%d", len(f.calls))
	}
	if strings.Contains(f.calls[len(f.calls)-1].DSL, "ecn_id") {
		t.Fatalf("retried SQL still has ecn_id: %s", f.calls[len(f.calls)-1].DSL)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows=%v", res.Rows)
	}
}

func TestExecuteRead_healsSchemaUsedAsTable(t *testing.T) {
	f := &fakeExecutor{
		ret: &executor.Result{
			Columns: []string{"vmid", "mgr_ipv4_address"},
			Rows:    [][]any{{int64(9076), "10.1.2.3"}},
		},
		err:            schemaErr("Error 1146 (42S02): Table 'd_1000_game_virtual_machine_info.d_1000_game_virtual_machine_info' doesn't exist"),
		failIfContains: "FROM d_1000_game_virtual_machine_info WHERE",
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		Store:               testHealStore(t),
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tl, _ := reg.Get("execute_read")
	out, err := tl.Execute(context.Background(), map[string]any{
		"query": "SELECT * FROM d_1000_game_virtual_machine_info WHERE flow_id = '9999_zjvplfx19vdv'",
	})
	if err != nil {
		t.Fatalf("expected heal success, got %v", err)
	}
	res := out.(*executor.QueryResult)
	if !strings.Contains(res.RepairedSQL, "t_game_virtual_machine_info") {
		t.Fatalf("repaired SQL: %s", res.RepairedSQL)
	}
	if strings.Contains(f.calls[len(f.calls)-1].DSL, "_test") {
		t.Fatalf("picked test table: %s", f.calls[len(f.calls)-1].DSL)
	}
}

func TestExecuteRead_unknownTableHint(t *testing.T) {
	f := &fakeExecutor{
		err: schemaErr("Error 1146 (42S02): Table 'd_1000_game_virtual_machine_info.t_flow' doesn't exist"),
	}
	cfg := &ExecuteReadConfig{
		Reader:              f,
		Exec:                f,
		Store:               testHealStore(t),
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	_ = RegisterExecuteReadTool(reg, cfg)
	tl, _ := reg.Get("execute_read")
	_, err := tl.Execute(context.Background(), map[string]any{
		"dsl": "SELECT * FROM t_flow WHERE flow_id = 'x'",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "t_game_virtual_machine_info") {
		t.Fatalf("hint missing candidate tables: %s", msg)
	}
}
