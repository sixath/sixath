package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fileToolsCtx(root string) context.Context {
	return context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
}

func registerFileToolsForTest(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileTools(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestReadFile_LineNumbersAndLimit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("read_file")
	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"path":   "docs/note.txt",
		"offset": 2,
		"limit":  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	content := m["content"].(string)
	if content != "2|line2" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestWriteFile_HarnessHooksYAMLIsDangerPath(t *testing.T) {
	root := t.TempDir()
	store := NewInMemoryWorkspaceFilePendingStore()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-hooks"},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "sess-hooks")

	res, err := tl.Execute(ctx, map[string]any{
		"path":    "harness/hooks.yaml",
		"content": "version: 1\nrules: []\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "pending" {
		t.Fatalf("harness/hooks.yaml must require confirm, got %#v", m)
	}
}

func TestWriteFile_DangerPathProposeAndConfirm(t *testing.T) {
	root := t.TempDir()
	store := NewInMemoryWorkspaceFilePendingStore()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-env"},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "sess-file")

	res, err := tl.Execute(ctx, map[string]any{
		"path":    ".env",
		"content": "SECRET=1",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "pending" || m["token"] != "tok-env" {
		t.Fatalf("propose: %#v", m)
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); !os.IsNotExist(err) {
		t.Fatal("must not write before confirm")
	}

	res2, err := tl.Execute(ctx, map[string]any{
		"path":          "ignored",
		"content":       "ignored",
		"confirm_token": "tok-env",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.(map[string]any)["status"] != "ok" {
		t.Fatalf("confirm: %#v", res2)
	}
	b, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "SECRET=1" {
		t.Fatalf("got %q", string(b))
	}
}

func TestWriteFile_SafePathDirectWhenStoreConfigured(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore: NewInMemoryWorkspaceFilePendingStore(),
		TokenGen:     &fakeTokenGen{next: "unused"},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "s")
	res, err := tl.Execute(ctx, map[string]any{
		"path":    "main.go",
		"content": "package main\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["status"] != "ok" {
		t.Fatalf("%#v", res)
	}
}

func TestWriteFile_DangerWithoutStoreStillWrites(t *testing.T) {
	root := t.TempDir()
	reg := registerFileToolsForTest(t) // no store → legacy direct write
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "s")
	res, err := tl.Execute(ctx, map[string]any{"path": ".env", "content": "x=1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["status"] != "ok" {
		t.Fatalf("%#v", res)
	}
	b, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil || string(b) != "x=1" {
		t.Fatalf("got %q err=%v", string(b), err)
	}
}

func TestPatch_DangerPathRequiresConfirm(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "id_rsa"), []byte("old-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewInMemoryWorkspaceFilePendingStore()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore: store,
		TokenGen:     &fakeTokenGen{next: "tok-key"},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("patch")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "sess")
	res, err := tl.Execute(ctx, map[string]any{
		"path":       "id_rsa",
		"old_string": "old-key",
		"new_string": "new-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["status"] != "pending" {
		t.Fatalf("%#v", res)
	}
	res2, err := tl.Execute(ctx, map[string]any{"confirm_token": "tok-key", "path": "x", "old_string": "a", "new_string": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if res2.(map[string]any)["status"] != "ok" {
		t.Fatalf("%#v", res2)
	}
	b, _ := os.ReadFile(filepath.Join(root, "id_rsa"))
	if !strings.Contains(string(b), "new-key") {
		t.Fatalf("got %q", string(b))
	}
}

func TestWriteFile_CreatesParents(t *testing.T) {
	root := t.TempDir()
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("write_file")
	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"path":    "nested/dir/file.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["status"] != "ok" {
		t.Fatalf("%#v", res)
	}
	b, err := os.ReadFile(filepath.Join(root, "nested", "dir", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("got %q", string(b))
	}
}

func TestPatch_ExactAndTrim(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("patch")

	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"path":       "main.go",
		"old_string": "println(\"hi\")",
		"new_string": "println(\"bye\")",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["strategy"] != "exact" {
		t.Fatalf("%#v", res)
	}

	res, err = tl.Execute(fileToolsCtx(root), map[string]any{
		"path":       "main.go",
		"old_string": "  println(\"bye\")  ",
		"new_string": "println(\"ok\")",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["strategy"] != "trim" {
		t.Fatalf("expected trim strategy, got %#v", res)
	}
}

func TestPatch_LineNormalized(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	content := "alpha   beta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("patch")
	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"path":       "a.txt",
		"old_string": "alpha beta",
		"new_string": "alpha OK",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["strategy"] != "line-normalized" {
		t.Fatalf("%#v", res)
	}
}

func TestFileTools_RejectPathEscape(t *testing.T) {
	root := t.TempDir()
	reg := registerFileToolsForTest(t)
	readTool, _ := reg.Get("read_file")
	res, err := readTool.Execute(fileToolsCtx(root), map[string]any{"path": "../secret.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.(map[string]any)["error"] == nil {
		t.Fatal("expected path error")
	}
}

func TestSearchFiles_ContentFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.go"), []byte("package one\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.go"), []byte("package two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("search_files")
	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"pattern":   "func Foo",
		"target":    "content",
		"file_glob": "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches := res.(map[string]any)["matches"].([]contentMatch)
	if len(matches) != 1 || matches[0].Path != "one.go" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestSearchFiles_FilesGlob(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registerFileToolsForTest(t)
	tl, _ := reg.Get("search_files")
	res, err := tl.Execute(fileToolsCtx(root), map[string]any{
		"pattern": "*.py",
		"target":  "files",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches := res.(map[string]any)["matches"].([]fileMatch)
	if len(matches) != 1 || !strings.HasSuffix(matches[0].Path, "a.py") {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestApplyFilePatch_AmbiguousExact(t *testing.T) {
	_, _, _, err := applyFilePatch("foo foo", "foo", "bar", false)
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
}

func TestWorkspaceFile_ConfirmErrorCode_NotFound(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore: NewInMemoryWorkspaceFilePendingStore(),
		TokenGen:     &fakeTokenGen{next: "tok"},
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "sess")

	res, err := tl.Execute(ctx, map[string]any{
		"path":          ".env",
		"content":       "x=1",
		"confirm_token": "no-such-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "not_found" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已失效（可能已被替换、已使用或服务重启），请重新发起" {
		t.Fatalf("error: %#v", m)
	}
}

func TestWorkspaceFile_ConfirmErrorCode_Expired(t *testing.T) {
	root := t.TempDir()
	store := NewInMemoryWorkspaceFilePendingStore()
	reg := NewRegistry()
	if err := RegisterWorkspaceFileToolsWithConfig(reg, &WorkspaceFileConfig{
		PendingStore:      store,
		TokenGen:          &fakeTokenGen{next: "tok-exp"},
		ConfirmTTLSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("write_file")
	ctx := context.WithValue(fileToolsCtx(root), ContextKeySessionID, "sess-exp")

	_, err := tl.Execute(ctx, map[string]any{"path": ".env", "content": "SECRET=1"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := store.GetPending(ctx, "sess-exp", "tok-exp")
	if p == nil {
		t.Fatal("pending missing")
	}
	p.CreatedAt = time.Now().Add(-10 * time.Minute)
	_ = store.SavePending(ctx, "sess-exp", *p)

	res, err := tl.Execute(ctx, map[string]any{
		"path":          "ignored",
		"content":       "ignored",
		"confirm_token": "tok-exp",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["error_code"] != "expired" {
		t.Fatalf("error_code: %#v", m)
	}
	if m["error"] != "确认已过期，请让助手重新发起操作" {
		t.Fatalf("error: %#v", m)
	}
}
