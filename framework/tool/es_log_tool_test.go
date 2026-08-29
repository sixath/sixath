package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
)

type fakeReader struct {
	gotDatasource string
	gotDSL        string
	gotIndex      string
	result        *executor.QueryResult
}

func (f *fakeReader) Query(ctx context.Context, datasourceID string, dsl string, opts executor.QueryOptions) (*executor.QueryResult, error) {
	f.gotDatasource = datasourceID
	f.gotDSL = dsl
	if opts.Extras != nil {
		if v, ok := opts.Extras["index"].(string); ok {
			f.gotIndex = v
		}
	}
	return f.result, nil
}

func TestESLogQuery_ByTraceID(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns: []string{"@timestamp", "level", "service", "message"},
		Rows: [][]any{
			{"2026-07-07T10:00:00Z", "ERROR", "service-a", "NPE at Foo.bar"},
		},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterESLogTool(reg, fr, ESLogConfig{
		DatasourceID: "es-logs", DefaultIndex: "app-logs-*", TraceIDField: "trace_id",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, ok := reg.Get("es_log_query")
	if !ok {
		t.Fatal("es_log_query not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	hits := m["hits"].([]map[string]any)
	if len(hits) != 1 || hits[0]["service"] != "service-a" {
		t.Fatalf("hits mapping wrong: %v", hits)
	}
	if fr.gotDatasource != "es-logs" || fr.gotIndex != "app-logs-*" {
		t.Fatalf("datasource/index wrong: %q %q", fr.gotDatasource, fr.gotIndex)
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatalf("DSL not valid JSON: %v (%s)", err, fr.gotDSL)
	}
	if !strings.Contains(fr.gotDSL, "trace_id") || !strings.Contains(fr.gotDSL, "abc") {
		t.Fatalf("DSL missing trace_id match: %s", fr.gotDSL)
	}
	assertRCAEvidenceOK(t, m, "es_log_query")
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].TraceID != "abc" {
		t.Fatalf("evidence_refs[0].TraceID=%q, want abc", refs[0].TraceID)
	}
}

func TestESLogQuery_TruncatedPassthrough(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns:   []string{"message"},
		Rows:      [][]any{{"x"}},
		Truncated: true,
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if !out.(map[string]any)["truncated"].(bool) {
		t.Fatal("expected truncated=true passed through from QueryResult.Truncated")
	}
}

func TestESLogQuery_RequiresParam(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{})
	m := out.(map[string]any)
	if _, has := m["error"]; !has {
		t.Fatal("expected error when neither trace_id nor query provided")
	}
	assertRCAEvidenceError(t, m, ErrorPermanent)
}

type errReader struct {
	err error
}

func (e *errReader) Query(ctx context.Context, datasourceID string, dsl string, opts executor.QueryOptions) (*executor.QueryResult, error) {
	return nil, e.err
}

func TestESLogQuery_TimeoutTransient(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, &errReader{err: errors.New("context deadline exceeded: i/o timeout")}, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id",
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertRCAEvidenceError(t, out.(map[string]any), ErrorTransient)
}

func TestESLogQuery_EmptyHitsOK(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "es_log_query")
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].Summary != "no hits" {
		t.Fatalf("want empty-result summary, got %#v", refs[0])
	}
}

func TestESLogQuery_SpillsOverFiftyRows(t *testing.T) {
	rows := make([][]any, 51)
	cols := []string{"operation", "args"}
	for i := 0; i < 51; i++ {
		rows[i] = []any{"DiscardUserArchive", nil}
	}
	rows[6] = []any{"DiscardUserArchive", map[string]any{"flowIds": []any{"flow-late"}}}
	fr := &fakeReader{result: &executor.QueryResult{Columns: cols, Rows: rows, Truncated: true, EstimatedTotal: 400}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "backend-cgsession-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-es")
	out, err := tl.Execute(ctx, map[string]any{"query": "operation:DiscardUserArchive", "limit": 51})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := out.(*QuerySpillStub)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if stub.Count != 51 {
		t.Fatalf("count=%d want 51 %#v", stub.Count, stub)
	}
	if !stub.HasMore && !stub.Truncated {
		t.Fatalf("want HasMore or Truncated, %#v", stub)
	}
	if refs := CollectEvidenceRefs(stub); len(refs) == 0 || refs[0].Kind != "es_log_query" || refs[0].Summary == "no hits" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestESLogQuery_SmallPageUnchangedType(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns: []string{"message"}, Rows: [][]any{{"a"}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", out)
	}
	if m["hits"] == nil {
		t.Fatalf("non-spill must keep hits: %#v", m)
	}
}

func TestESLogQueryDescriptionMentionsRunResultScript(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{DatasourceID: "es-logs"}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	if !strings.Contains(tl.Description, "run_result_script") {
		t.Fatalf("%s", tl.Description)
	}
}
