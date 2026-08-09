package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/tool"
)

func TestFailureCaptureHook_hardFail(t *testing.T) {
	root := t.TempDir()
	h := NewFailureCaptureHook(FailureCaptureConfig{})
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)
	_, err := h.After(ctx, "demo_tool", nil, errors.New("boom"))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("must preserve exec err, got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".learnings", "ERRORS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "demo_tool") || !strings.Contains(string(b), "boom") {
		t.Fatalf("ERRORS.md missing capture: %s", b)
	}
}

func TestFailureCaptureHook_softFail(t *testing.T) {
	root := t.TempDir()
	h := NewFailureCaptureHook(FailureCaptureConfig{})
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)
	res, err := h.After(ctx, "es_log_query", map[string]any{"ok": false, "error": "timeout"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["ok"] != false {
		t.Fatalf("result mutated: %#v", res)
	}
	b, err := os.ReadFile(filepath.Join(root, ".learnings", "ERRORS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "soft-fail") {
		t.Fatalf("expected soft-fail entry: %s", b)
	}
}

func TestFailureCaptureHook_dedup(t *testing.T) {
	root := t.TempDir()
	h := NewFailureCaptureHook(FailureCaptureConfig{DedupTTL: time.Minute})
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)
	_, _ = h.After(ctx, "t", nil, errors.New("x"))
	_, _ = h.After(ctx, "t", nil, errors.New("x"))
	b, err := os.ReadFile(filepath.Join(root, ".learnings", "ERRORS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "### Summary") != 1 {
		t.Fatalf("expected 1 summary, got:\n%s", b)
	}
}

func TestFailureCaptureHook_skipsSkillManageAndGrowthReview(t *testing.T) {
	root := t.TempDir()
	h := NewFailureCaptureHook(FailureCaptureConfig{})
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)

	_, _ = h.After(ctx, "skill_manage", nil, errors.New("nope"))
	if _, err := os.Stat(filepath.Join(root, ".learnings", "ERRORS.md")); !os.IsNotExist(err) {
		t.Fatal("skill_manage failures must not be captured")
	}

	md := growth.MergeReviewMetadata(nil)
	ctx2 := WithRequestMetadata(ctx, md)
	_, _ = h.After(ctx2, "demo", nil, errors.New("nope"))
	if _, err := os.Stat(filepath.Join(root, ".learnings", "ERRORS.md")); !os.IsNotExist(err) {
		t.Fatal("growth review runs must not capture")
	}
}

func TestFailureCaptureHook_successNoWrite(t *testing.T) {
	root := t.TempDir()
	h := NewFailureCaptureHook(FailureCaptureConfig{})
	ctx := context.WithValue(context.Background(), tool.ContextKeyWorkspaceRoot, root)
	_, _ = h.After(ctx, "ok_tool", map[string]any{"ok": true}, nil)
	if _, err := os.Stat(filepath.Join(root, ".learnings", "ERRORS.md")); !os.IsNotExist(err) {
		t.Fatal("success must not write ERRORS")
	}
}
