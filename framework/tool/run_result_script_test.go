package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func exitCodeOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return -999
	}
}

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := pythonInterpreter(); err != nil {
		t.Skip("python not on PATH")
	}
}

func TestRunResultScript_SmallInline(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "print('hello')",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("%T", out)
	}
	if exitCodeOf(m["exit_code"]) != 0 {
		t.Fatalf("%v", m["exit_code"])
	}
	if !strings.Contains(fmt.Sprint(m["output"]), "hello") {
		t.Fatalf("%v", m)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "tmp", "results", "sess-1", "*_run_result_script_*.jsonl"))
	if len(matches) != 0 {
		t.Fatalf("unexpected spill %v", matches)
	}
}

func TestRunResultScript_TextSpillSixtyLines(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "for i in range(60):\n print(i)",
	})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := out.(*QuerySpillStub)
	if !ok {
		t.Fatalf("%T %#v", out, out)
	}
	if stub.SourcePath != data {
		t.Fatalf("source=%s", stub.SourcePath)
	}
	if stub.Count != 60 {
		t.Fatalf("count=%d", stub.Count)
	}
	if stub.ExitCode == nil || *stub.ExitCode != 0 {
		t.Fatalf("exit=%v", stub.ExitCode)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if !strings.Contains(first, `"line"`) {
		t.Fatalf("want wrapped lines, got %s", first)
	}
}

func TestRunResultScript_JSONObjectsPassthrough(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	code := "for i in range(60):\n print('{\"a\":%d}'%i)"
	out, err := tl.Execute(ctx, map[string]any{"path": data, "code": code})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := out.(*QuerySpillStub)
	if !ok {
		t.Fatalf("%T", out)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(b)), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["line"]; ok {
		t.Fatalf("must not wrap: %v", row)
	}
	if _, ok := row["a"]; !ok {
		t.Fatalf("%v", row)
	}
}

func TestRunResultScript_ReadsArgv1(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	pyRel := "tmp/results/sess-1/count.py"
	py := "import sys\nprint(sum(1 for _ in open(sys.argv[1], encoding='utf-8')))\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(pyRel)), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{"path": data, "script_path": pyRel})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if !strings.Contains(fmt.Sprint(m["output"]), "1") {
		t.Fatalf("%v", m)
	}
}

func TestRunResultScript_ExitOneNotExecuteError(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "import sys\nprint('boom')\nsys.exit(1)",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if exitCodeOf(m["exit_code"]) != 1 {
		t.Fatalf("%v", m["exit_code"])
	}
}

func TestRunResultScript_TimeoutWithOutput(t *testing.T) {
	requirePython(t)
	old := scriptTimeout
	scriptTimeout = 200 * time.Millisecond
	defer func() { scriptTimeout = old }()
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "import time\nprint('before', flush=True)\ntime.sleep(5)\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	switch v := out.(type) {
	case map[string]any:
		if v["timed_out"] != true {
			t.Fatalf("%v", v)
		}
	case *QuerySpillStub:
		if !v.TimedOut {
			t.Fatal("want timed_out")
		}
	default:
		t.Fatalf("%T", out)
	}
}

func TestRunResultScript_ByteSpillFewHugeLines(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "print('x'*9000)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*QuerySpillStub); !ok {
		t.Fatalf("want stub, got %T %#v", out, out)
	}
}
