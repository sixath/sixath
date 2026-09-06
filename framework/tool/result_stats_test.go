package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resultStatsHarness(t *testing.T) (context.Context, string, Tool) {
	t.Helper()
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-1")
	reg := NewRegistry()
	if err := RegisterResultStatsTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("result_stats")
	if !ok {
		t.Fatal("result_stats not registered")
	}
	return ctx, root, tl
}

func writeResultsJSONL(t *testing.T, root, rel string, lines ...string) string {
	t.Helper()
	full, relOut, err := resolveResultsPath(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return relOut
}

func execResultStats(t *testing.T, ctx context.Context, tl Tool, params map[string]any) any {
	t.Helper()
	res, err := tl.Execute(ctx, params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

func resultStatsErr(res any) string {
	m, ok := res.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["error"].(string)
	return s
}

func fixtureThreeLines() []string {
	return []string{
		`{"operation":"A","args":{"flowIds":["f1","f2"]}}`,
		`{"operation":"A","args":{"flowIds":["f1"]}}`,
		`{"operation":"B","args":{"flowIds":["f3"]}}`,
	}
}

func TestResultStats_GroupByInline(t *testing.T) {
	ctx, root, tl := resultStatsHarness(t)
	rel := writeResultsJSONL(t, root, "tmp/results/sess/a.jsonl", fixtureThreeLines()...)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":     rel,
		"group_by": "operation",
	})
	if _, ok := res.(*QuerySpillStub); ok {
		t.Fatal("small group_by must stay inline")
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", res)
	}
	if m["path"] != rel {
		t.Fatalf("path=%v want %s", m["path"], rel)
	}
	groups, ok := m["groups"].([]map[string]any)
	if !ok {
		t.Fatalf("groups type %T", m["groups"])
	}
	if len(groups) != 2 {
		t.Fatalf("groups=%v", groups)
	}
	if groups[0]["value"] != "A" || fmt.Sprint(groups[0]["count"]) != "2" {
		t.Fatalf("A: %#v", groups[0])
	}
	if groups[1]["value"] != "B" || fmt.Sprint(groups[1]["count"]) != "1" {
		t.Fatalf("B: %#v", groups[1])
	}
}

func TestResultStats_UniqueFlattenFirstSeen(t *testing.T) {
	ctx, root, tl := resultStatsHarness(t)
	rel := writeResultsJSONL(t, root, "tmp/results/sess/a.jsonl", fixtureThreeLines()...)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":   rel,
		"unique": "args.flowIds",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", res)
	}
	vals, ok := m["unique_values"].([]string)
	if !ok {
		t.Fatalf("unique_values type %T", m["unique_values"])
	}
	want := []string{"f1", "f2", "f3"}
	if len(vals) != len(want) {
		t.Fatalf("unique_values=%v", vals)
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("unique_values=%v want %v", vals, want)
		}
	}
}

func TestResultStats_GroupByAndUniqueErrorWithoutFile(t *testing.T) {
	ctx, _, tl := resultStatsHarness(t)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":     "tmp/results/sess/missing.jsonl",
		"group_by": "operation",
		"unique":   "args.flowIds",
	})
	errStr := resultStatsErr(res)
	if errStr == "" {
		t.Fatalf("want error payload, got %#v", res)
	}
	if strings.Contains(strings.ToLower(errStr), "re-query") || strings.Contains(strings.ToLower(errStr), "not found") {
		t.Fatalf("must not open file: %s", errStr)
	}
}

func TestResultStats_PathEscapeRejected(t *testing.T) {
	ctx, _, tl := resultStatsHarness(t)
	for _, p := range []string{"../x", "tmp/other/a.jsonl"} {
		res := execResultStats(t, ctx, tl, map[string]any{"path": p, "group_by": "operation"})
		if resultStatsErr(res) == "" {
			t.Fatalf("path %q must error, got %#v", p, res)
		}
	}
}

func TestResultStats_MissingFileReQuery(t *testing.T) {
	ctx, _, tl := resultStatsHarness(t)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":     "tmp/results/sess/nope.jsonl",
		"group_by": "operation",
	})
	errStr := resultStatsErr(res)
	if !strings.Contains(errStr, "re-query") {
		t.Fatalf("error %q must contain re-query", errStr)
	}
}

func TestResultStats_UniqueSpillStub(t *testing.T) {
	ctx, root, tl := resultStatsHarness(t)
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"id":"u%d"}`, i)
	}
	rel := writeResultsJSONL(t, root, "tmp/results/sess/ids.jsonl", lines...)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":   rel,
		"unique": "id",
	})
	stub, ok := res.(*QuerySpillStub)
	if !ok {
		t.Fatalf("want *QuerySpillStub, got %T %#v", res, res)
	}
	if stub.UniqueCount != 80 {
		t.Fatalf("unique_count=%d", stub.UniqueCount)
	}
	if stub.SourcePath != rel {
		t.Fatalf("source_path=%q want %q", stub.SourcePath, rel)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 80 {
		t.Fatalf("stats jsonl lines=%d", n)
	}
	raw, _ := json.Marshal(stub)
	if strings.Contains(string(raw), `"unique_values"`) {
		t.Fatalf("marshaled stub must not contain unique_values: %s", raw)
	}
}

func TestResultStats_GroupsTruncatedCap(t *testing.T) {
	ctx, root, tl := resultStatsHarness(t)
	lines := make([]string, 10001)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"operation":"op-%d"}`, i)
	}
	rel := writeResultsJSONL(t, root, "tmp/results/sess/ops.jsonl", lines...)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":     rel,
		"group_by": "operation",
	})
	stub, ok := res.(*QuerySpillStub)
	if !ok {
		t.Fatalf("want *QuerySpillStub, got %T", res)
	}
	if !stub.GroupsTruncated {
		t.Fatalf("groups_truncated=%v", stub.GroupsTruncated)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n > 10000 {
		t.Fatalf("stats jsonl lines=%d want ≤10000", n)
	}
}

func TestResultStats_SkipBadJSONLines(t *testing.T) {
	ctx, root, tl := resultStatsHarness(t)
	rel := writeResultsJSONL(t, root, "tmp/results/sess/mixed.jsonl",
		`{"operation":"A"}`,
		`not-json`,
		`{"operation":"B"}`,
	)
	res := execResultStats(t, ctx, tl, map[string]any{
		"path":     rel,
		"group_by": "operation",
	})
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", res)
	}
	if fmt.Sprint(m["skipped_bad_lines"]) != "1" {
		t.Fatalf("skipped_bad_lines=%v", m["skipped_bad_lines"])
	}
	groups, ok := m["groups"].([]map[string]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("groups=%v", m["groups"])
	}

	badRel := writeResultsJSONL(t, root, "tmp/results/sess/allbad.jsonl", "x", "y", "z")
	bad := execResultStats(t, ctx, tl, map[string]any{
		"path":     badRel,
		"group_by": "operation",
	})
	if resultStatsErr(bad) == "" {
		t.Fatalf("all-bad jsonl must error, got %#v", bad)
	}
}

func TestResultStatsDescriptionMentionsRunResultScript(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterResultStatsTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("result_stats")
	if !strings.Contains(tl.Description, "run_result_script") {
		t.Fatalf("%s", tl.Description)
	}
}
