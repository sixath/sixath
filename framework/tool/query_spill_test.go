package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spillCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-1")
	return ctx, root
}

func TestMaybeSpill_SmallKeepsHits(t *testing.T) {
	ctx, root := spillCtx(t)
	rows := []map[string]any{{"operation": "a"}}
	payload := map[string]any{"hits": rows, "count": 1, "total": 1, "hit_status": HitStatusHits}
	stub, out := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub != nil {
		t.Fatal("small result must not spill")
	}
	if _, err := os.Stat(filepath.Join(root, "tmp")); !os.IsNotExist(err) && err != nil {
		t.Fatal(err)
	}
	if out["hits"] == nil {
		t.Fatal("hits must remain")
	}
}

func TestMaybeSpill_RowCountWritesJSONL(t *testing.T) {
	ctx, root := spillCtx(t)
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	rows[6] = map[string]any{"args": map[string]any{"flowIds": []any{"flow-late"}}}
	payload := map[string]any{
		"hits": rows, "count": 51, "total": 51, "hit_status": HitStatusHits,
		"queried_index": "idx", "has_more": true, "continue_from": 51,
		"extracted_ids": []string{"flow-late"},
	}
	refs := deriveESLogRefs(payload)
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, refs)
	if stub == nil || !stub.Spilled || stub.Count != 51 {
		t.Fatalf("stub=%#v", stub)
	}
	if stub.HitStatus != HitStatusHits || stub.HasMore != true || stub.ContinueFrom != 51 {
		t.Fatalf("meta=%#v", stub)
	}
	b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if n := strings.Count(string(b), "\n"); n != 51 {
		t.Fatalf("jsonl lines=%d", n)
	}
	found := false
	for _, id := range stub.ExtractedIDs {
		if id == "flow-late" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extracted_ids=%v", stub.ExtractedIDs)
	}
	raw, _ := json.Marshal(stub)
	head := string(raw[:min(2048, len(raw))])
	if !strings.Contains(head, `"spilled"`) || !strings.Contains(head, `"path"`) || !strings.Contains(head, `"count"`) {
		t.Fatalf("key order head=%s", head)
	}
	if strings.Contains(string(raw), `"hits":`) {
		t.Fatal("stub must not contain hits")
	}
}

func TestMaybeSpill_ByteThreshold(t *testing.T) {
	ctx, _ := spillCtx(t)
	fat := strings.Repeat("x", 9000)
	rows := []map[string]any{{"message": fat}}
	payload := map[string]any{"hits": rows, "count": 1, "total": 1, "hit_status": HitStatusHits}
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub == nil {
		t.Fatal("fat single row must spill")
	}
}

func TestMaybeSpill_NoWorkspaceFallback(t *testing.T) {
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	stub, out := MaybeSpill(context.Background(), "es_log_query", rows, payload, nil)
	if stub != nil {
		t.Fatal("must not spill")
	}
	if out["spill_error"] != "workspace_root_missing" || out["hits"] == nil {
		t.Fatalf("%#v", out)
	}
}

func TestMaybeSpill_FileCap(t *testing.T) {
	old := spillFileMaxBytes
	spillFileMaxBytes = 64
	t.Cleanup(func() { spillFileMaxBytes = old })
	ctx, _ := spillCtx(t)
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"message": strings.Repeat("y", 20)}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub == nil || !stub.FileTruncated || stub.Count <= 0 || stub.Count >= 51 {
		t.Fatalf("file cap stub=%#v", stub)
	}
}

func TestMaybeSpill_TTLOnlySessionDir(t *testing.T) {
	ctx, root := spillCtx(t)
	other := filepath.Join(root, "tmp", "results", "other-sess")
	_ = os.MkdirAll(other, 0o755)
	oldf := filepath.Join(other, "old.jsonl")
	if err := os.WriteFile(oldf, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(oldf, past, past)

	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	if stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil); stub == nil {
		t.Fatal("expected spill")
	}
	if _, err := os.Stat(oldf); err != nil {
		t.Fatalf("must not delete other session: %v", err)
	}
}

func TestResolveResultsPath_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, _, err := resolveResultsPath(root, "../secret.json"); err == nil {
		t.Fatal("expected reject")
	}
	if _, _, err := resolveResultsPath(root, "tmp/other/a.jsonl"); err == nil {
		t.Fatal("expected reject")
	}
}
