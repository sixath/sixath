package tool

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestOneOf(t *testing.T) {
	src := func(context.Context, map[string]any) ([]string, error) { return []string{"mysql1", "mysql2", "hive"}, nil }
	c := OneOf{Param: "datasource_id", Source: src}
	ctx := context.Background()

	if errs, err := c.Check(ctx, map[string]any{"datasource_id": "mysql1"}); err != nil || len(errs) != 0 {
		t.Fatalf("valid: %v %v", errs, err)
	}
	if errs, _ := c.Check(ctx, map[string]any{}); len(errs) != 0 {
		t.Fatalf("missing param must not be judged: %v", errs)
	}
	errs, err := c.Check(ctx, map[string]any{"datasource_id": "mysq1"})
	if err != nil || len(errs) != 1 || errs[0].Keyword != KeywordOneOf || errs[0].Candidates[0] != "mysql1" {
		t.Fatalf("invalid: %+v %v", errs, err)
	}
	errs, _ = c.Check(ctx, map[string]any{"datasource_id": "zzzzzzzz"})
	if len(errs) != 1 || len(errs[0].Candidates) != 3 {
		t.Fatalf("no fuzzy match must list candidates: %+v", errs)
	}

	norm := OneOf{Param: "datasource_id", Source: src, Normalize: func(_ map[string]any, v string) string {
		if v == "default" {
			return "mysql1"
		}
		return v
	}}
	if errs, _ := norm.Check(ctx, map[string]any{"datasource_id": "default"}); len(errs) != 0 {
		t.Fatalf("normalized: %v", errs)
	}
	rewrite := OneOf{Param: "datasource_id", Source: src, Normalize: func(_ map[string]any, v string) string { return "ds_" + v }}
	errs, _ = rewrite.Check(ctx, map[string]any{"datasource_id": "mysq1"})
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `"mysq1"`) || strings.Contains(errs[0].Message, "ds_") {
		t.Fatalf("message must quote the original value: %+v", errs)
	}
	blank := OneOf{Param: "datasource_id", Source: src, Normalize: func(map[string]any, string) string { return "" }}
	if errs, err := blank.Check(ctx, map[string]any{"datasource_id": "whatever"}); err != nil || len(errs) != 0 {
		t.Fatalf("empty normalized value must skip: %v %v", errs, err)
	}

	when := OneOf{Param: "name", Source: src, When: func(p map[string]any) bool { return p["action"] != "create" }}
	if errs, _ := when.Check(ctx, map[string]any{"name": "new", "action": "create"}); len(errs) != 0 {
		t.Fatalf("When=false must skip: %v", errs)
	}

	failing := OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) { return nil, errors.New("down") }}
	if _, err := failing.Check(ctx, map[string]any{"x": "a"}); err == nil {
		t.Fatal("source error must surface as check error (fail-open upstream)")
	}
	empty := OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) { return nil, nil }}
	if errs, err := empty.Check(ctx, map[string]any{"x": "a"}); err != nil || len(errs) != 0 {
		t.Fatal("empty candidate set means not applicable: no errors, no check error")
	}
}

func TestPattern(t *testing.T) {
	c := Pattern{Param: "trace_id", Regex: regexp.MustCompile(`^([0-9a-fA-F]{16}|[0-9a-fA-F]{32})$`), Hint: "16 or 32 hex chars"}
	ctx := context.Background()
	if errs, _ := c.Check(ctx, map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"}); len(errs) != 0 {
		t.Fatalf("valid: %v", errs)
	}
	errs, _ := c.Check(ctx, map[string]any{"trace_id": "req-123"})
	if len(errs) != 1 || errs[0].Keyword != KeywordPattern || errs[0].Hint == "" {
		t.Fatalf("invalid: %+v", errs)
	}
}

func TestReject(t *testing.T) {
	c := Reject{Label: "non_sql", Redirect: "use es_log_query for Elasticsearch", Detect: func(p map[string]any) (string, string, bool) {
		if s, _ := p["dsl"].(string); s == "level:ERROR" {
			return "dsl", "looks like a Lucene expression", true
		}
		return "", "", false
	}}
	ctx := context.Background()
	if errs, _ := c.Check(ctx, map[string]any{"dsl": "SELECT 1"}); len(errs) != 0 {
		t.Fatalf("sql: %v", errs)
	}
	errs, _ := c.Check(ctx, map[string]any{"dsl": "level:ERROR"})
	if len(errs) != 1 || errs[0].Keyword != KeywordReject || errs[0].Path != "dsl" || errs[0].Hint == "" {
		t.Fatalf("lucene: %+v", errs)
	}
}

func TestFieldRefs(t *testing.T) {
	calls := 0
	c := FieldRefs{
		Extract: func(map[string]any) []FieldRef {
			return []FieldRef{{Param: "query", Field: "servcie"}, {Param: "sort", Field: "@timestamp"}}
		},
		Fields: func(_ context.Context, _ map[string]any, refresh bool) ([]string, error) {
			calls++
			if refresh {
				return []string{"service", "@timestamp", "message"}, nil
			}
			return []string{"service", "@timestamp"}, nil
		},
		BodyHint: func(map[string]any) string { return "free text goes to field message" },
	}
	errs, err := c.Check(context.Background(), nil)
	if err != nil || len(errs) != 1 {
		t.Fatalf("got %+v %v", errs, err)
	}
	e := errs[0]
	if e.Keyword != KeywordUnknownField || e.Path != "query" || e.Candidates[0] != "service" || e.Hint == "" {
		t.Fatalf("got %+v", e)
	}
	if calls != 2 {
		t.Fatalf("unknown field must trigger exactly one forced refresh, calls=%d", calls)
	}

	calls = 0
	refreshed := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "message"}} },
		Fields: func(_ context.Context, _ map[string]any, refresh bool) ([]string, error) {
			calls++
			if refresh {
				return []string{"service", "message"}, nil
			}
			return []string{"service"}, nil
		},
	}
	if errs, err := refreshed.Check(context.Background(), nil); err != nil || errs != nil || calls != 2 {
		t.Fatalf("field only in refreshed catalog must pass: errs=%+v err=%v calls=%d", errs, err, calls)
	}

	refreshFails := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "message"}} },
		Fields: func(_ context.Context, _ map[string]any, refresh bool) ([]string, error) {
			if refresh {
				return nil, errors.New("timeout")
			}
			return []string{"service"}, nil
		},
	}
	if errs, err := refreshFails.Check(context.Background(), nil); err == nil || errs != nil {
		t.Fatalf("refresh error must surface as check error, not judge stale catalog: errs=%+v err=%v", errs, err)
	}

	noCatalog := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "x"}} },
		Fields:  func(context.Context, map[string]any, bool) ([]string, error) { return nil, nil },
	}
	if errs, err := noCatalog.Check(context.Background(), nil); err != nil || len(errs) != 0 {
		t.Fatal("empty catalog means not applicable (no mapper / mapping unavailable)")
	}
	broken := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "x"}} },
		Fields:  func(context.Context, map[string]any, bool) ([]string, error) { return nil, errors.New("timeout") },
	}
	if _, err := broken.Check(context.Background(), nil); err == nil {
		t.Fatal("fetch error must surface as check error (fail-open upstream)")
	}
}
