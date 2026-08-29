package tooldata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	core "github.com/sixath/framework/tool"
)

type fakeExecutor struct {
	calls []execCall
	ret   *executor.Result
	err   error
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
	if f.ret == nil {
		return nil, f.err
	}
	return &executor.QueryResult{
		Columns:        f.ret.Columns,
		Rows:           f.ret.Rows,
		Truncated:      f.ret.Truncated,
		EstimatedTotal: f.ret.EstimatedTotal,
	}, f.err
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
