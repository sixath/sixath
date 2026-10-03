package tool

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
)

func esCheckRegistry(t *testing.T, r executor.Reader, mapper ESFieldMapper) Tool {
	t.Helper()
	reg := newTestRegistry()
	err := RegisterESLogTool(reg, r, ESLogConfig{
		FieldMapper:  mapper,
		IndexCatalog: &memIndexCatalog{byPattern: map[string][]string{"logs-*": {"logs-2026.10.01"}}},
		Clusters:     []ESLogCluster{{ID: "es", DefaultIndex: "logs-*", BodyField: "message"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	return tl
}

func TestESLog_UnknownFieldRejectedBeforeExecute(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{}}
	tl := esCheckRegistry(t, r, mapFieldMapper{
		"service":    {Type: "keyword"},
		"message":    {Type: "text"},
		"@timestamp": {Type: "date"},
	})
	res, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "servcie:foo", "sort": "levle:desc"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) {
		t.Fatalf("want InvalidArgumentsError, got %v %+v", err, res)
	}
	if r.gotDSL != "" {
		t.Fatal("query must not be executed")
	}
	var sawQuery, sawSort bool
	for _, e := range iae.Errors {
		if e.Keyword == KeywordUnknownField && e.Path == "query" && len(e.Candidates) > 0 && e.Candidates[0] == "service" && e.Hint != "" {
			sawQuery = true
		}
		if e.Keyword == KeywordUnknownField && e.Path == "sort" {
			sawSort = true
		}
	}
	if !sawQuery || !sawSort {
		t.Fatalf("errors %+v", iae.Errors)
	}
}

func TestESLog_KnownFieldsExecute(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{Rows: [][]any{{"x"}}, Columns: []string{"_source"}}}
	tl := esCheckRegistry(t, r, mapFieldMapper{"service": {Type: "keyword"}, "@timestamp": {Type: "date"}})
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "service:foo"}); err != nil {
		t.Fatal(err)
	}
	if r.gotDSL == "" {
		t.Fatal("expected execution")
	}
}

func TestESLog_FreeTextHasNoFieldRefs(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{Rows: [][]any{{"x"}}, Columns: []string{"_source"}}}
	tl := esCheckRegistry(t, r, mapFieldMapper{"service": {Type: "keyword"}})
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "prestart failed"}); err != nil {
		t.Fatalf("free text must not be rejected: %v", err)
	}
}

func TestESLog_NoMapperFailsOpen(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{Rows: [][]any{{"x"}}, Columns: []string{"_source"}}}
	tl := esCheckRegistry(t, r, nil)
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "servcie:foo"}); err != nil {
		t.Fatalf("without mapper the query must run: %v", err)
	}
	if r.gotDSL == "" {
		t.Fatal("expected execution")
	}
}

type scriptedReader struct {
	calls []string
	reply func(dsl string) *executor.QueryResult
}

func (s *scriptedReader) Query(_ context.Context, _ string, dsl string, _ executor.QueryOptions) (*executor.QueryResult, error) {
	s.calls = append(s.calls, dsl)
	return s.reply(dsl), nil
}

func TestESLog_EmptyProbeMarksSuspect(t *testing.T) {
	sr := &scriptedReader{reply: func(dsl string) *executor.QueryResult {
		if strings.Contains(dsl, `"size":0`) && !strings.Contains(dsl, "service") {
			return &executor.QueryResult{EstimatedTotal: 57}
		}
		return &executor.QueryResult{}
	}}
	reg := newTestRegistry()
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		FieldMapper:  mapFieldMapper{"service": {Type: "keyword"}, "level": {Type: "keyword"}, "@timestamp": {Type: "date"}},
		IndexCatalog: &memIndexCatalog{byPattern: map[string][]string{"logs-*": {"logs-1"}}},
		Clusters:     []ESLogCluster{{ID: "es", DefaultIndex: "logs-*"}},
	})
	tl, _ := reg.Get("es_log_query")
	res, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es", "query": "service:foo AND level:ERROR", "time_from": "now-1h", "time_to": "now",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := HitContractFromResult(res); st != HitStatusSuspect {
		t.Fatalf("status %q res %+v", st, res)
	}
	if d := DiagnosisFromResult(res); d == nil || len(d.Probes) == 0 {
		t.Fatalf("diag %+v", d)
	}
	var probeDSL string
	for _, c := range sr.calls {
		if strings.Contains(c, `"size":0`) {
			probeDSL = c
			break
		}
	}
	if !strings.Contains(probeDSL, `"track_total_hits":true`) || strings.Contains(probeDSL, `"aggs"`) {
		t.Fatalf("probe dsl %s", probeDSL)
	}
}

type parseErrReader struct {
	calls []string
}

func (p *parseErrReader) Query(_ context.Context, _ string, dsl string, _ executor.QueryOptions) (*executor.QueryResult, error) {
	p.calls = append(p.calls, dsl)
	if strings.Contains(dsl, `path:/a/b[1]`) {
		return nil, errors.New("search_phase_execution_exception: parse_exception: Cannot parse")
	}
	if strings.Contains(dsl, `"size":0`) {
		return &executor.QueryResult{EstimatedTotal: 9}, nil
	}
	return &executor.QueryResult{}, nil
}

func TestESLog_ProbeUsesQuotedRetryQuery(t *testing.T) {
	pr := &parseErrReader{}
	tl := esCheckRegistry(t, pr, mapFieldMapper{"path": {Type: "keyword"}, "level": {Type: "keyword"}, "@timestamp": {Type: "date"}})
	res, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es", "query": "path:/a/b[1] AND level:ERROR",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := HitContractFromResult(res); st != HitStatusSuspect {
		t.Fatalf("status %q res %+v", st, res)
	}
	d := DiagnosisFromResult(res)
	if d == nil || len(d.Errors) != 0 || len(d.Probes) == 0 {
		t.Fatalf("probes must retry with quoted query, diag %+v", d)
	}
}

func TestESLogFieldRefs(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{"two fields", map[string]any{"query": "service:foo AND level:ERROR"}, []string{"service", "level"}},
		{"url value", map[string]any{"query": "url:http://x"}, []string{"url"}},
		{"quoted colon", map[string]any{"query": `msg:"a:b"`}, []string{"msg"}},
		{"free text", map[string]any{"query": "prestart failed"}, nil},
		{"hyphen in value", map[string]any{"query": "msg:pre-start"}, []string{"msg"}},
		{"negation and group", map[string]any{"query": "-level:DEBUG AND (host:a OR !pod:b)"}, []string{"level", "host", "pod"}},
		{"keyword subfield", map[string]any{"query": "service.keyword:foo"}, []string{"service"}},
		{"wildcard and meta", map[string]any{"query": "kube*:x AND _exists_:service AND _id:1"}, nil},
		{"escaped colon", map[string]any{"query": `a\:b`}, nil},
		{"fields", map[string]any{"fields": []any{"kubernetes.*", "_id", "host"}}, []string{"host"}},
		{"meta sort", map[string]any{"sort": "_score:desc"}, nil},
		{"field sort", map[string]any{"sort": "level:asc"}, []string{"level"}},
		{"plain sort", map[string]any{"sort": "desc"}, nil},
		{"agg and time field", map[string]any{"agg_field": "host.keyword", "time_field": "ts"}, []string{"host", "ts"}},
		{"json query", map[string]any{"query": `{"term":{"x":1}}`}, nil},
		{"trace id skips only query", map[string]any{"trace_id": "abc", "query": "x:1", "agg_field": "y", "sort": "z:desc", "time_field": "ts", "fields": []any{"f"}}, []string{"y", "ts", "z", "f"}},
		{"bare sort field", map[string]any{"sort": "levle"}, []string{"levle"}},
		{"bare sort order upper", map[string]any{"sort": "ASC"}, nil},
		{"url after space", map[string]any{"query": "url: http://x AND level:e"}, []string{"url", "level"}},
		{"regex literal", map[string]any{"query": `path:/api\/v1:x/ AND level:e`}, []string{"path", "level"}},
		{"regex with field-like text", map[string]any{"query": `/foo bar:baz/ AND host:a`}, []string{"host"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range esLogFieldRefs(tc.params) {
				got = append(got, r.Field)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestESLogRelax(t *testing.T) {
	labels := func(vs []ProbeVariant) []string {
		var out []string
		for _, v := range vs {
			out = append(out, v.Label)
		}
		return out
	}
	cases := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{"no window single clause", map[string]any{"query": "a:1"}, nil},
		{"no window two clauses", map[string]any{"query": "a:1 AND b:2"}, []string{"without:a:1", "without:b:2"}},
		{"short window", map[string]any{"query": "a:1 AND b:2", "time_from": "now-1h"}, []string{"time_window_only", "without:a:1", "without:b:2", "time_window_24h"}},
		{"long window", map[string]any{"query": "a:1", "time_from": "now-7d"}, []string{"time_window_only"}},
		{"trace id", map[string]any{"trace_id": "t", "time_from": "now-1h"}, nil},
		{"json query", map[string]any{"query": `{"term":{"a":1}}`, "time_from": "now-1h"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := labels(esLogRelax(tc.params)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}

	vs := esLogRelax(map[string]any{"query": "a:1 AND b:2", "time_from": "now-1h", "time_to": "now"})
	if vs[1].Params["query"] != "b:2" || vs[0].Params["query"] != "*" {
		t.Fatalf("variant params %+v", vs)
	}
	last := vs[len(vs)-1].Params
	if last["time_from"] != "now-24h" {
		t.Fatalf("24h variant %+v", last)
	}
	if _, ok := last["time_to"]; ok {
		t.Fatalf("24h variant must drop time_to: %+v", last)
	}
}

func TestSplitTopLevelAND(t *testing.T) {
	got := splitTopLevelAND(`(a AND b) AND c:"x AND y"`)
	want := []string{"(a AND b)", `c:"x AND y"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := splitTopLevelAND("a OR b"); !reflect.DeepEqual(got, []string{"a OR b"}) {
		t.Fatalf("got %q", got)
	}
}

func TestRelativeWindowUnder24h(t *testing.T) {
	for in, want := range map[string]bool{
		"now-1h": true, "now-90m": true, "now-24h": false, "now-7d": false,
		"2026-10-01T00:00:00Z": false, "": false,
	} {
		if got := relativeWindowUnder24h(in); got != want {
			t.Fatalf("%q: got %v", in, got)
		}
	}
}
