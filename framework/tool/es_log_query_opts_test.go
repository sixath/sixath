package tool

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sixath/framework/executor"
)

func TestParseESLogQueryOpts(t *testing.T) {
	o, err := parseESLogQueryOpts(map[string]any{"sort": "asc"}, "")
	if err != nil || o.sortField != "@timestamp" || o.sortOrder != "asc" {
		t.Fatalf("default time sort: %+v err=%v", o, err)
	}
	o, err = parseESLogQueryOpts(map[string]any{"sort": "log.time:DESC", "agg_size": 999}, "ts")
	if err != nil || o.sortField != "log.time" || o.sortOrder != "desc" || o.timeField != "ts" || o.aggSize != esLogMaxAggSize {
		t.Fatalf("field sort / cluster time field / agg cap: %+v err=%v", o, err)
	}
	if _, err := parseESLogQueryOpts(map[string]any{"sort": "newest"}, ""); err == nil {
		t.Fatal("invalid sort must fail")
	}
	o, _ = parseESLogQueryOpts(map[string]any{"fields": "message, level"}, "")
	if !reflect.DeepEqual(o.fields, []string{"message", "level"}) {
		t.Fatalf("fields from csv: %v", o.fields)
	}
	o, _ = parseESLogQueryOpts(map[string]any{"fields": []any{"message", " "}}, "")
	if !reflect.DeepEqual(o.fields, []string{"message"}) {
		t.Fatalf("fields from array: %v", o.fields)
	}
}

func TestESLogQueryOptsApply(t *testing.T) {
	o, _ := parseESLogQueryOpts(map[string]any{
		"sort": "asc", "time_from": "now-2h", "time_to": "now",
		"agg_field": "level", "agg_interval": "1m", "fields": []any{"message"},
	}, "")
	dsl := map[string]any{"size": 50, "query": map[string]any{"query_string": map[string]any{"query": "225781"}}}
	o.apply(dsl)
	raw, _ := json.Marshal(dsl)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)

	must := got["query"].(map[string]any)["bool"].(map[string]any)["must"].([]any)
	if must[0].(map[string]any)["query_string"] == nil {
		t.Fatalf("original query must be kept under bool.must: %s", raw)
	}
	rng := got["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)[0].(map[string]any)["range"].(map[string]any)["@timestamp"].(map[string]any)
	if rng["gte"] != "now-2h" || rng["lte"] != "now" {
		t.Fatalf("range: %v", rng)
	}
	if got["sort"].([]any)[0].(map[string]any)["@timestamp"].(map[string]any)["order"] != "asc" {
		t.Fatalf("sort: %s", raw)
	}
	aggs := got["aggs"].(map[string]any)
	if aggs[esLogAggByField] == nil || aggs[esLogAggTimeline] == nil {
		t.Fatalf("aggs: %s", raw)
	}
	if !reflect.DeepEqual(got["_source"], []any{"message"}) {
		t.Fatalf("_source: %v", got["_source"])
	}

	explicit := map[string]any{"sort": []any{"x"}, "aggs": map[string]any{"mine": map[string]any{}}}
	o.apply(explicit)
	if !reflect.DeepEqual(explicit["sort"], []any{"x"}) || explicit["aggs"].(map[string]any)["mine"] == nil {
		t.Fatalf("explicit body parts must not be overridden: %v", explicit)
	}
	if explicit["query"].(map[string]any)["bool"].(map[string]any)["must"].([]any)[0].(map[string]any)["match_all"] == nil {
		t.Fatalf("time range without query must wrap match_all: %v", explicit["query"])
	}
}

func TestSummarizeESAggregations(t *testing.T) {
	raw := json.RawMessage(`{
		"by_field": {"sum_other_doc_count": 7, "buckets": [{"key": "ERROR", "doc_count": 12}, {"key": "WARN", "doc_count": 3}]},
		"timeline": {"buckets": [{"key": 1758340800000, "key_as_string": "2026-09-20T09:58:00Z", "doc_count": 5}]},
		"max_ts": {"value": 1758340800000}
	}`)
	got := summarizeESAggregations(raw)
	byField := got["by_field"].(map[string]any)
	if byField["other_count"] != int64(7) || byField["buckets"].([]map[string]any)[0]["key"] != "ERROR" {
		t.Fatalf("by_field: %v", byField)
	}
	tl := got["timeline"].(map[string]any)["buckets"].([]map[string]any)
	if tl[0]["key"] != "2026-09-20T09:58:00Z" || tl[0]["count"] != int64(5) {
		t.Fatalf("timeline must prefer key_as_string: %v", tl)
	}
	if got["max_ts"].(map[string]any)["value"] == nil {
		t.Fatalf("metric aggs pass through: %v", got["max_ts"])
	}
	if summarizeESAggregations(nil) != nil {
		t.Fatal("empty aggregations must be nil")
	}
}

func TestQuoteLuceneSpecialTokens(t *testing.T) {
	cases := []struct{ in, want string; changed bool }{
		{"prestart AND word[123]", `prestart AND "word[123]"`, true},
		{"path:/var/log/x.log AND err", `path:"/var/log/x.log" AND err`, true},
		{"TS:[2026-09-20 TO 2026-09-21] AND vm-1", "TS:[2026-09-20 TO 2026-09-21] AND vm-1", false},
		{`"already [quoted]" AND (a OR b)`, `"already [quoted]" AND (a OR b)`, false},
		{"(slot[2])", `("slot[2]")`, true},
	}
	for _, c := range cases {
		got, changed := quoteLuceneSpecialTokens(c.in)
		if got != c.want || changed != c.changed {
			t.Errorf("%q => %q (%v), want %q (%v)", c.in, got, changed, c.want, c.changed)
		}
	}
}

func TestIsESQueryParseError(t *testing.T) {
	if !isESQueryParseError(errString("search_phase_execution_exception: [query_shard_exception] Failed to parse query [x]")) {
		t.Fatal("query_shard parse failure must be detected")
	}
	if isESQueryParseError(errString("connection refused")) {
		t.Fatal("network error is not a parse error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestQueryStringRangeHint(t *testing.T) {
	if queryStringRangeHint("TS:[2026-09-20T00:00 TO 2026-09-21T00:00] AND vm", "@timestamp") == "" {
		t.Fatal("time-like range field must produce a hint")
	}
	if queryStringRangeHint("port:[1 TO 100]", "@timestamp") != "" {
		t.Fatal("non-time range must not produce a hint")
	}
	if queryStringRangeHint("plain words", "@timestamp") != "" {
		t.Fatal("no range, no hint")
	}
}

func TestESLogQuery_InvestigationPrimitives(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns:        []string{"@timestamp", "message"},
		Rows:           [][]any{{"2026-09-20T09:58:10Z", "repair aborted"}},
		EstimatedTotal: 42,
		Aggregations:   json.RawMessage(`{"by_field":{"buckets":[{"key":"vm-225781","doc_count":40}]}}`),
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "logs-*"}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es", "query": "prestart", "sort": "asc", "time_from": "2026-09-20T00:00:00Z", "agg_field": "host.keyword",
	})
	if err != nil {
		t.Fatal(err)
	}
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatal(err)
	}
	if dsl["sort"] == nil || dsl["aggs"] == nil || dsl["query"].(map[string]any)["bool"] == nil {
		t.Fatalf("primitives must reach the DSL: %s", fr.gotDSL)
	}
	m := out.(map[string]any)
	aggs, ok := m["aggregations"].(map[string]any)
	if !ok || aggs["by_field"] == nil {
		t.Fatalf("aggregations must be summarized in payload: %v", m["aggregations"])
	}

	out, _ = tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "x", "sort": "sideways"})
	if out.(map[string]any)["ok"] != false {
		t.Fatalf("invalid sort must fail: %v", out)
	}
}
