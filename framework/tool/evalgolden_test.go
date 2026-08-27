package tool

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/obs"
)

func TestEvalGolden_empty_hit_stamp(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "app-logs-*", TraceIDField: "trace_id"}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "x", "index": "vm-manager-*"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["hit_status"] != HitStatusEmpty || m["queried_index"] != "vm-manager-*" {
		t.Fatalf("G1 %#v", m)
	}
	if m["ok"] != true {
		t.Fatalf("empty must remain ok=true")
	}

	fr.result = &executor.QueryResult{Columns: []string{"message"}, Rows: [][]any{{"a"}}}
	out, _ = tl.Execute(context.Background(), map[string]any{"query": "x", "index": "vm-manager-*"})
	if out.(map[string]any)["hit_status"] != HitStatusHits {
		t.Fatalf("G2 %#v", out)
	}

	reg2 := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg2, &errReader{err: errors.New("es down")}, ESLogConfig{DatasourceID: "es", DefaultIndex: "vm-manager-*", TraceIDField: "trace_id"})
	tl2, _ := reg2.Get("es_log_query")
	out, _ = tl2.Execute(context.Background(), map[string]any{"query": "x"})
	m = out.(map[string]any)
	if m["hit_status"] != HitStatusError {
		t.Fatalf("G3 %#v", m)
	}
	if m["queried_index"] != "vm-manager-*" {
		t.Fatalf("G3 index %#v", m)
	}
}

func TestEvalGolden_empty_hit_stamp_grep(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "a.go"), "package a\n")
	reg := newRCARegistry(t, []string{repoA})
	tl, _ := reg.Get("rca_grep")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NoSuchTokenXYZ"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["hit_status"] != HitStatusEmpty {
		t.Fatalf("G5 status %#v", m)
	}
	if _, ok := m["repo"]; !ok {
		t.Fatal("G5 missing top-level repo")
	}
	if m["repo"] != "service-a" {
		t.Fatalf("G5 repo=%v", m["repo"])
	}
	if m["ok"] != true {
		t.Fatal("empty grep must stay ok")
	}
}

func TestEvalGolden_obs_hit(t *testing.T) {
	var got []obs.HitContractLog
	restore := obs.SetHitContractHook(func(rec obs.HitContractLog) {
		got = append(got, rec)
	})
	defer restore()

	_ = StampHitContract(map[string]any{"hits": []any{}}, HitStamp{
		Status:       HitStatusEmpty,
		QueriedIndex: "vm-manager-*",
		Tool:         "es_log_query",
	})
	if len(got) != 1 {
		t.Fatalf("StampHitContract must LogHitContract, got %#v", got)
	}
	if got[0].Status != HitStatusEmpty || got[0].Index != "vm-manager-*" || got[0].Tool != "es_log_query" {
		t.Fatalf("%#v", got[0])
	}
}
