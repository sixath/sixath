package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSessionResultAbs_OK(t *testing.T) {
	root := t.TempDir()
	sid := "7d069a05-0699-42ea-a3af-2407e0a1aa20"
	rel := "tmp/results/" + sid + "/1787986133618_run_result_script_28.jsonl"
	abs, err := ResolveSessionResultAbs(root, sid, rel)
	if err != nil {
		t.Fatalf("ResolveSessionResultAbs: %v", err)
	}
	want := filepath.Join(root, "tmp", "results", sid, "1787986133618_run_result_script_28.jsonl")
	if abs != want {
		t.Fatalf("abs=%q want %q", abs, want)
	}
}

func TestResolveSessionResultAbs_RejectsTraversalAndOtherSession(t *testing.T) {
	root := t.TempDir()
	sid := "sess-1"
	cases := []string{
		"tmp/results/sess-1/../sess-2/a.jsonl",
		"tmp/results/other/a.jsonl",
		"tmp/results/sess-1/a.json",
		"../tmp/results/sess-1/a.jsonl",
		"",
	}
	for _, rel := range cases {
		if _, err := ResolveSessionResultAbs(root, sid, rel); err == nil {
			t.Fatalf("rel %q: want error", rel)
		}
	}
}

func TestReadJSONLResultFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.jsonl")
	body := "{\"line\":\"4103_abc vmid=1, gid=2\"}\n{\"flowId\":\"x\",\"vmid\":3}\nnot-json\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadJSONLResultFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d want 3", len(rows))
	}
	if rows[0]["line"] != "4103_abc vmid=1, gid=2" {
		t.Fatalf("row0=%v", rows[0])
	}
	if rows[1]["flowId"] != "x" {
		t.Fatalf("row1=%v", rows[1])
	}
	if !strings.Contains(rows[2]["line"].(string), "not-json") {
		t.Fatalf("row2=%v", rows[2])
	}
}
