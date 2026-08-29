package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitScriptOutput_DropsBlankAndTrailing(t *testing.T) {
	lines := splitScriptOutput("a\n\nb\n")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("%q", lines)
	}
}

func TestRowsFromScriptLines_AllJSONObjects(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, `{"b":2}`})
	if len(rows) != 2 {
		t.Fatalf("%v", rows)
	}
	if _, ok := rows[0]["line"]; ok {
		t.Fatal("must not wrap pure json objects")
	}
	if rows[0]["a"] != float64(1) {
		t.Fatalf("%v", rows[0])
	}
}

func TestRowsFromScriptLines_WrapsText(t *testing.T) {
	rows := rowsFromScriptLines([]string{"hello", "world"})
	if rows[0]["line"] != "hello" || rows[1]["line"] != "world" {
		t.Fatalf("%v", rows)
	}
}

func TestRowsFromScriptLines_MixedGoesWrap(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, "not-json"})
	if rows[0]["line"] != `{"a":1}` || rows[1]["line"] != "not-json" {
		t.Fatalf("%v", rows)
	}
}

func TestTruncateUTF8Bytes(t *testing.T) {
	s := strings.Repeat("x", 9000)
	got := truncateUTF8Bytes(s, 8192)
	if len(got) != 8192 {
		t.Fatalf("len=%d", len(got))
	}
	s2 := strings.Repeat("你", 100)
	got2 := truncateUTF8Bytes(s2, 4)
	if got2 != "你" {
		t.Fatalf("%q", got2)
	}
}

func scriptTool(t *testing.T) Tool {
	t.Helper()
	reg := NewRegistry()
	if err := RegisterRunResultScriptTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("run_result_script")
	if !ok {
		t.Fatal("tool not registered")
	}
	return tl
}

func TestRunResultScript_NoWorkspace(t *testing.T) {
	tl := scriptTool(t)
	_, err := tl.Execute(context.Background(), map[string]any{
		"path": "tmp/results/s/a.jsonl",
		"code": "print(1)",
	})
	if err == nil || !strings.Contains(err.Error(), "workspace_root_missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunResultScript_PathOutsideResults(t *testing.T) {
	ctx, root := spillCtx(t)
	if err := os.MkdirAll(filepath.Join(root, "tmp", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "other", "x.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": "tmp/other/x.jsonl", "code": "print(1)"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_MissingFile(t *testing.T) {
	ctx, _ := spillCtx(t)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": "tmp/results/sess-1/missing.jsonl", "code": "print(1)"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_CodeAndScriptPath(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path":        data,
		"code":        "print(1)",
		"script_path": data,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_NeitherCodeNorScript(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": data})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_CodeTooLong(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": strings.Repeat("a", 65537),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_ScriptNotPy(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	js := filepath.Join(root, "tmp", "results", "sess-1", "x.js")
	if err := os.WriteFile(js, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path":        data,
		"script_path": "tmp/results/sess-1/x.js",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func writeFixtureJSONL(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "tmp", "results", "sess-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "tmp/results/sess-1/in.jsonl"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("{\"x\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}
