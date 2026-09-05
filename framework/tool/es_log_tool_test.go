package tool

import (
	"bytes"
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
	out, err := tl.Execute(context.Background(), map[string]any{"cluster": "es-logs", "trace_id": "abc"})
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
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "es", "trace_id": "abc"})
	if !out.(map[string]any)["truncated"].(bool) {
		t.Fatal("expected truncated=true passed through from QueryResult.Truncated")
	}
}

func TestESLogQuery_RequiresParam(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "es"})
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
	out, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertRCAEvidenceError(t, out.(map[string]any), ErrorTransient)
}

func TestESLogQuery_JSONTermQueryIsQueryClause(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "backend-cgsession-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	_, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `{"term":{"operation":"DiscardUserArchive"}}`,
		"index":   "backend-cgsession-*",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatalf("DSL not JSON: %v (%s)", err, fr.gotDSL)
	}
	q, _ := dsl["query"].(map[string]any)
	term, _ := q["term"].(map[string]any)
	if term["operation"] != "DiscardUserArchive" {
		t.Fatalf("want term query on operation, got %s", fr.gotDSL)
	}
	if _, wrapped := q["query_string"]; wrapped {
		t.Fatalf("JSON term must not be stuffed into query_string: %s", fr.gotDSL)
	}
}

func TestESLogQuery_FromOffsetInDSL(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: [][]any{{"a"}}}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	_, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   "operation:DiscardUserArchive",
		"from":    100,
		"limit":   50,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatalf("DSL not JSON: %v (%s)", err, fr.gotDSL)
	}
	if jsonNumberAsInt(dsl["from"]) != 100 {
		t.Fatalf("from=%v want 100 in %s", dsl["from"], fr.gotDSL)
	}
	if jsonNumberAsInt(dsl["size"]) != 50 {
		t.Fatalf("size=%v want 50 in %s", dsl["size"], fr.gotDSL)
	}
}

func jsonNumberAsInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return -1
	}
}

func TestESLogQuery_NextFromWhenTruncated(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns:        []string{"message"},
		Rows:           [][]any{{"a"}, {"b"}},
		Truncated:      true,
		EstimatedTotal: 10000,
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   "DiscardUserArchive",
		"from":    100,
		"limit":   2,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if m["from"] != 100 {
		t.Fatalf("from=%v want 100", m["from"])
	}
	if m["next_from"] != 102 {
		t.Fatalf("next_from=%v want 102", m["next_from"])
	}
	if m["total"] != 10000 {
		t.Fatalf("total=%v want 10000", m["total"])
	}
}

func TestESLogQuery_LuceneFieldQueryUsesQueryString(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	_, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "operation:DiscardUserArchive"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatalf("DSL not JSON: %v (%s)", err, fr.gotDSL)
	}
	q, _ := dsl["query"].(map[string]any)
	qs, _ := q["query_string"].(map[string]any)
	if qs["query"] != "operation:DiscardUserArchive" {
		t.Fatalf("want lucene query_string field:value, got %s", fr.gotDSL)
	}
}

func TestExtractIDsFromHits_ArgsFlowIds(t *testing.T) {
	hits := []map[string]any{
		{"args": `{"flowIds":["4103_aaa","4103_bbb"]}`, "reply": strings.Repeat("x", 2000)},
		{"args": map[string]any{"flowIds": []any{"4103_aaa", "4103_ccc"}}},
	}
	got := extractIDsFromHits(hits)
	if !containsStr(got, "4103_aaa") || !containsStr(got, "4103_bbb") || !containsStr(got, "4103_ccc") {
		t.Fatalf("extracted=%v", got)
	}
	if n := countStr(got, "4103_aaa"); n != 1 {
		t.Fatalf("ids must be unique, 4103_aaa appeared %d", n)
	}
}

func countStr(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}

func TestCompactESLogHit_DropsReplyKeepsArgs(t *testing.T) {
	got := compactESLogHit(map[string]any{
		"args":      `{"flowIds":["4103_aaa"]}`,
		"reply":     strings.Repeat("y", 4000),
		"operation": "/cgarchive.ArchiveService/DiscardUserArchive",
		"beat":      map[string]any{"name": "x"},
	})
	if _, ok := got["reply"]; ok {
		t.Fatalf("reply must be dropped: %#v", got)
	}
	if _, ok := got["beat"]; ok {
		t.Fatalf("beat must be dropped: %#v", got)
	}
	if got["args"] == nil || got["operation"] == "" {
		t.Fatalf("need args+operation: %#v", got)
	}
}

func TestESLogQuery_ExtractsFlowIdsAndSummaryKeys(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns:        []string{"args", "operation", "reply"},
		Rows:           [][]any{{`{"flowIds":["4103_aaa"]}`, "/cgarchive.ArchiveService/DiscardUserArchive", strings.Repeat("z", 3000)}},
		Truncated:      true,
		EstimatedTotal: 432,
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "backend-cgsession-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `operation:"/cgarchive.ArchiveService/DiscardUserArchive"`,
		"limit":   50,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if m["count"] != 432 {
		t.Fatalf("count=%v want 432 so the model sees total before hits", m["count"])
	}
	if m["has_more"] != true {
		t.Fatalf("has_more=%v", m["has_more"])
	}
	if m["continue_from"] != 1 && m["continue_from"] != 50 {
		// one row returned, from=0 → continue_from = 1
		if m["continue_from"] != 1 {
			t.Fatalf("continue_from=%v want 1", m["continue_from"])
		}
	}
	ids, _ := m["extracted_ids"].([]string)
	if !containsStr(ids, "4103_aaa") {
		t.Fatalf("extracted_ids=%v", m["extracted_ids"])
	}
	hits := m["hits"].([]map[string]any)
	if len(hits) != 1 {
		t.Fatalf("hits=%d", len(hits))
	}
	if _, ok := hits[0]["reply"]; ok {
		t.Fatalf("compact hits must drop reply: %#v", hits[0])
	}
	raw, _ := json.Marshal(m)
	countAt := bytes.Index(raw, []byte(`"count"`))
	hitsAt := bytes.Index(raw, []byte(`"hits"`))
	if countAt < 0 || hitsAt < 0 || countAt > hitsAt {
		t.Fatalf("count must marshal before hits so 8KB truncation keeps paging metadata: %s", raw[:min(len(raw), 200)])
	}
}

func TestESLogQuery_EmptyHitsOK(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"message"}, Rows: nil}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	assertRCAEvidenceOK(t, m, "es_log_query")
	if m["cluster"] != "es" {
		t.Fatalf("empty-hit cluster=%v want es", m["cluster"])
	}
	refs := m["evidence_refs"].([]EvidenceRef)
	if refs[0].Summary != "no hits" {
		t.Fatalf("want empty-result summary, got %#v", refs[0])
	}
}

func TestESLogQuery_DoesNotSpillOverFiftyRows(t *testing.T) {
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
	out, err := tl.Execute(ctx, map[string]any{"cluster": "es", "query": "operation:DiscardUserArchive", "limit": 51})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*QuerySpillStub); ok {
		t.Fatal("default es_log_query must not spill")
	}
}

func TestESLogQuery_SmallPageUnchangedType(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns: []string{"message"}, Rows: [][]any{{"a"}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "x"})
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

type seqReader struct {
	calls       []string
	datasources []string
	results     []*executor.QueryResult
}

func (s *seqReader) Query(_ context.Context, datasourceID string, dsl string, _ executor.QueryOptions) (*executor.QueryResult, error) {
	s.calls = append(s.calls, dsl)
	s.datasources = append(s.datasources, datasourceID)
	i := len(s.calls) - 1
	if i >= len(s.results) {
		return &executor.QueryResult{}, nil
	}
	return s.results[i], nil
}

func TestESLogQuery_EmptyHitRewritesTermToKeywordSubfield(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
		{Columns: []string{"message"}, Rows: [][]any{{"discarded"}}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id",
		FieldMapper: mapFieldMapper{"operation": {Type: "text", KeywordSubfield: true}},
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `{"term":{"operation":"/cgarchive.ArchiveService/DiscardUserArchive"}}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sr.calls) != 2 {
		t.Fatalf("want 1 retry after mapping rewrite, calls=%d %v", len(sr.calls), sr.calls)
	}
	if !strings.Contains(sr.calls[0], `"operation"`) || strings.Contains(sr.calls[0], "operation.keyword") {
		t.Fatalf("first query should keep model term on operation: %s", sr.calls[0])
	}
	if !strings.Contains(sr.calls[1], "operation.keyword") {
		t.Fatalf("retry must term on operation.keyword, not blind match: %s", sr.calls[1])
	}
	if strings.Contains(sr.calls[1], `"match"`) {
		t.Fatalf("text+keyword empty hit must not fall back to match: %s", sr.calls[1])
	}
	m := out.(map[string]any)
	if m["query_rewritten"] != true {
		t.Fatalf("want query_rewritten, got %#v", m)
	}
	hits := m["hits"].([]map[string]any)
	if len(hits) != 1 {
		t.Fatalf("hits after rewrite: %#v", hits)
	}
}

func TestESLogQuery_EmptyHitTermOnTextOnlyUsesMatchPhrase(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
		{Columns: []string{"message"}, Rows: [][]any{{"hit"}}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id",
		FieldMapper: mapFieldMapper{"operation": {Type: "text"}},
	})
	tl, _ := reg.Get("es_log_query")
	_, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `{"term":{"operation":"DiscardUserArchive"}}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sr.calls) != 2 {
		t.Fatalf("calls=%d", len(sr.calls))
	}
	if !strings.Contains(sr.calls[1], "match_phrase") {
		t.Fatalf("text-only term should become match_phrase: %s", sr.calls[1])
	}
}

func TestESLogQuery_EmptyHitUnknownTermsField(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "cgsession-*", TraceIDField: "trace_id",
		FieldMapper: mapFieldMapper{
			"trace_id": {Type: "keyword"},
			"message":  {Type: "text"},
			"gid":      {Type: "keyword"},
			"vmid":     {Type: "keyword"},
		},
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `{"terms":{"flowId":["4103_a","4103_b"]}}`,
		"limit":   5,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	unknown, _ := m["unknown_fields"].([]string)
	if !containsStr(unknown, "flowId") {
		t.Fatalf("empty terms hit must check mapping for flowId, got %#v", m["unknown_fields"])
	}
	note, _ := m["mapping_error"].(string)
	if !strings.Contains(note, "flowId") {
		t.Fatalf("mapping_error=%q", note)
	}
	if _, ok := m["query_rewritten"]; ok {
		t.Fatalf("unknown field must not be rewritten into a guessed name: %#v", m)
	}
}

func TestESLogQuery_EmptyHitUnknownFieldNotInvented(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "backend-logs-*", TraceIDField: "trace_id",
		FieldMapper: mapFieldMapper{
			"trace_id":  {Type: "keyword"},
			"flowId":    {Type: "keyword"},
			"operation": {Type: "text"},
			"message":   {Type: "text"},
		},
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   "flow_id: 4103_j0qjifnv99pq",
		"limit":   5,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sr.calls) != 1 {
		t.Fatalf("unknown field must not retry a guessed rewrite, calls=%d %v", len(sr.calls), sr.calls)
	}
	m := out.(map[string]any)
	unknown, _ := m["unknown_fields"].([]string)
	if !containsStr(unknown, "flow_id") {
		t.Fatalf("want unknown_fields flow_id, got %#v", m["unknown_fields"])
	}
	similar, _ := m["similar_fields"].([]string)
	if !containsStr(similar, "flowId") {
		t.Fatalf("want similar flowId, got %#v", m["similar_fields"])
	}
	if containsStr(similar, "trace_id") {
		t.Fatalf("must not suggest unrelated _id fields: %v", similar)
	}
	note, _ := m["mapping_error"].(string)
	if note == "" || !strings.Contains(note, "flow_id") {
		t.Fatalf("want mapping_error naming flow_id, got %#v", m["mapping_error"])
	}
}

func TestESLogQuery_EmptyHitCorrectTermKeepsSingleQuery(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id",
		FieldMapper: mapFieldMapper{"status": {Type: "keyword"}},
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es",
		"query":   `{"term":{"status":"ok"}}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sr.calls) != 1 {
		t.Fatalf("already-correct term must not retry, calls=%d", len(sr.calls))
	}
	m := out.(map[string]any)
	if _, ok := m["query_rewritten"]; ok {
		t.Fatalf("should not mark rewritten: %#v", m)
	}
	hints, _ := m["field_hints"].([]ESFieldHint)
	if len(hints) != 1 || hints[0].Type != "keyword" {
		t.Fatalf("want mapping hint on true 0-hit, got %#v", m["field_hints"])
	}
}

func TestESLogQuery_RequiresCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"m"}, Rows: [][]any{{"x"}}}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "zj-elk", DefaultIndex: "app-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "a:b"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("missing cluster must fail")
	}
	if _, has := m["cluster"]; !has {
		t.Fatal("error payload must include cluster")
	}
	msg, _ := m["error"].(string)
	if !strings.Contains(msg, "zj-elk") || !strings.Contains(msg, "cluster") {
		t.Fatalf("error should list cluster names, got %q", msg)
	}
}

func TestESLogQuery_RoutesByCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{Columns: []string{"m"}, Rows: [][]any{{"x"}}}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "app-*", Purpose: "应用日志"},
		{ID: "zj-elk_flow", DefaultIndex: "1_game_flow_all-*", Purpose: "流水"},
	}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, _ := reg.Get("es_log_query")
	if !strings.Contains(tl.Description, "`zj-elk_flow`") || !strings.Contains(tl.Description, "`1_game_flow_all-*`") {
		t.Fatalf("description should backtick cluster id and default index, got %s", tl.Description)
	}
	out, err := tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk_flow", "query": "a:b"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fr.gotDatasource != "zj-elk_flow" || fr.gotIndex != "1_game_flow_all-*" {
		t.Fatalf("got ds=%q index=%q", fr.gotDatasource, fr.gotIndex)
	}
	m := out.(map[string]any)
	if m["cluster"] != "zj-elk_flow" {
		t.Fatalf("success cluster=%v want zj-elk_flow", m["cluster"])
	}
}

func TestESLogQuery_UnknownCluster(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "app-*", Purpose: "应用"},
	}})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk_flow", "query": "a:b"})
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("unknown cluster must fail")
	}
	if m["cluster"] != "zj-elk_flow" {
		t.Fatalf("error cluster=%v want requested zj-elk_flow", m["cluster"])
	}
	if fr.gotDatasource != "" {
		t.Fatal("must not query any cluster")
	}
}

func TestESLogQuery_MissingDefaultIndexRequiresIndexParam(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", DefaultIndex: "", Purpose: "应用"},
	}})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{"cluster": "zj-elk", "query": "a:b"})
	m := out.(map[string]any)
	if m["ok"] != false {
		t.Fatal("empty default_index without index param must fail")
	}
	if m["cluster"] != "zj-elk" {
		t.Fatalf("error cluster=%v want zj-elk", m["cluster"])
	}
	if fr.gotDatasource != "" {
		t.Fatal("must not Query with empty index")
	}
}

func TestESLogQuery_EmptyHitRewriteUsesSameCluster(t *testing.T) {
	sr := &seqReader{results: []*executor.QueryResult{
		{Columns: []string{"message"}, Rows: nil},
		{Columns: []string{"message"}, Rows: [][]any{{"discarded"}}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		Clusters: []ESLogCluster{
			{ID: "zj-elk", DefaultIndex: "app-*", Purpose: "应用日志"},
			{ID: "zj-elk_flow", DefaultIndex: "1_game_flow_all-*", Purpose: "流水"},
		},
		FieldMapper: mapFieldMapper{"operation": {Type: "text", KeywordSubfield: true}},
	})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "zj-elk_flow",
		"query":   `{"term":{"operation":"/cgarchive.ArchiveService/DiscardUserArchive"}}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(sr.datasources) != 2 {
		t.Fatalf("want 2 Query calls, datasources=%v calls=%v", sr.datasources, sr.calls)
	}
	if sr.datasources[0] != "zj-elk_flow" || sr.datasources[1] != "zj-elk_flow" {
		t.Fatalf("rewrite retry must stay on selected cluster, got %v", sr.datasources)
	}
	m := out.(map[string]any)
	if m["cluster"] != "zj-elk_flow" {
		t.Fatalf("rewrite cluster=%v want zj-elk_flow", m["cluster"])
	}
	if m["query_rewritten"] != true {
		t.Fatalf("want query_rewritten, got %#v", m)
	}
}

func TestESLogQuery_RegisterRejectsEmptyClusterID(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "  ", DefaultIndex: "app-*"},
	}})
	if err == nil {
		t.Fatal("empty cluster id must fail register")
	}
}
