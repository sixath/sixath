package tooldata

import (
	"context"
	"testing"

	"github.com/sixath/framework/executor"
	core "github.com/sixath/framework/tool"
)

func TestEvalGolden_empty_hit_stamp_read(t *testing.T) {
	f := &fakeExecutor{ret: &executor.Result{Columns: []string{"id"}, Rows: nil}}
	cfg := &ExecuteReadConfig{Reader: f, Exec: f, DefaultDatasourceID: "ds1"}
	reg := core.NewRegistry()
	if err := RegisterExecuteReadTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("execute_read")
	out, err := tl.Execute(context.Background(), map[string]any{"dsl": "SELECT 1 WHERE 0"})
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(*executor.QueryResult)
	if !ok {
		t.Fatalf("%T", out)
	}
	if res.HitStatus != core.HitStatusEmpty {
		t.Fatalf("G4 status %q", res.HitStatus)
	}
	if res.QueriedIndex != "" {
		t.Fatalf("G4 no index param, got %q", res.QueriedIndex)
	}
}

func TestEvalGolden_deny_write_pending(t *testing.T) {
	store := newMemoryPendingStore()
	ex := &fakeWriteExecutor{ret: &executor.Result{AffectedRows: 1}}
	cfg := &ExecuteWriteConfig{
		Writer:              ex,
		Exec:                ex,
		Checker:             &fakeChecker{allow: true},
		PendingStore:        store,
		TokenGen:            &fakeTokenGen{next: "t-deny"},
		DefaultDatasourceID: "ds1",
	}
	reg := core.NewRegistry()
	if err := RegisterExecuteWriteTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("execute_write")
	out, err := tl.Execute(context.Background(), map[string]any{
		"dsl":        "UPDATE t SET a=1",
		"session_id": "s1",
		"user_id":    "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, ok := out.(ExecuteWritePendingResponse)
	if !ok || resp.Status != "pending" || resp.Token == "" {
		t.Fatalf("%T %#v", out, out)
	}
	if len(ex.calls) != 0 {
		t.Fatalf("propose must not Exec, calls=%d", len(ex.calls))
	}
}
