package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/chat"

	"github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func TestCodeRootsList_ExistingOnly(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")

	srv := khttp.NewServer()
	r := srv.Route("/")
	r.GET("/api/v1/code-roots", CodeRootsListHandler([]string{root, missing}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-roots", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roots []string `json:"roots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	want := chat.NormalizeCodeRoots([]string{root})
	if len(body.Roots) != 1 || body.Roots[0] != want[0] {
		t.Fatalf("roots = %#v, want %#v", body.Roots, want)
	}
}

func TestCodeRootsBrowse_HappyPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "d1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f1"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	norm := chat.NormalizeCodeRoots([]string{root})[0]

	srv := khttp.NewServer()
	r := srv.Route("/")
	r.GET("/api/v1/code-roots/browse", CodeRootsBrowseHandler([]string{root}))

	q := url.Values{}
	q.Set("root", norm)
	q.Set("path", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-roots/browse?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Root    string `json:"root"`
		Path    string `json:"path"`
		Entries []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.Root != norm {
		t.Fatalf("root = %q, want %q", body.Root, norm)
	}
	if len(body.Entries) != 1 || body.Entries[0].Name != "d1" || body.Entries[0].Type != "dir" {
		t.Fatalf("entries = %+v", body.Entries)
	}
}

func TestCodeRootsBrowse_EscapeRejected(t *testing.T) {
	root := t.TempDir()
	norm := chat.NormalizeCodeRoots([]string{root})[0]

	srv := khttp.NewServer()
	r := srv.Route("/")
	r.GET("/api/v1/code-roots/browse", CodeRootsBrowseHandler([]string{root}))

	q := url.Values{}
	q.Set("root", norm)
	q.Set("path", "../outside")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-roots/browse?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestBrowseCodeRoots_NotFound(t *testing.T) {
	root := t.TempDir()
	norm := chat.NormalizeCodeRoots([]string{root})[0]
	_, err := browseCodeRoots([]string{root}, norm, "missing-dir")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.IsNotFound(err) {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestWorkspaceLink_Success(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "repo")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "agent-ws") // must not exist yet

	out, err := linkWorkspaceCode(workspace, target, []string{root})
	if err != nil {
		if strings.Contains(err.Error(), "symlink") || strings.Contains(err.Error(), "privilege") {
			t.Skip("symlink not permitted:", err)
		}
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("out type %T", out)
	}
	link := filepath.Join(workspace, chat.WorkspaceCodeLink)
	if m["link"] != link {
		t.Fatalf("link = %v, want %v", m["link"], link)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink")
	}
	// Idempotent: same target → 200 noop
	if _, err := linkWorkspaceCode(workspace, target, []string{root}); err != nil {
		t.Fatalf("noop same target: %v", err)
	}
}

func TestWorkspaceLink_EscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	workspace := t.TempDir()
	_, err := linkWorkspaceCode(workspace, outside, []string{root})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.IsBadRequest(err) {
		t.Fatalf("err = %v, want BadRequest", err)
	}
}

func TestWorkspaceLink_Conflict(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if _, err := linkWorkspaceCode(workspace, a, []string{root}); err != nil {
		if strings.Contains(err.Error(), "symlink") || strings.Contains(err.Error(), "privilege") {
			t.Skip("symlink not permitted:", err)
		}
		t.Fatal(err)
	}
	_, err := linkWorkspaceCode(workspace, b, []string{root})
	if err == nil {
		t.Fatal("expected conflict")
	}
	se := errors.FromError(err)
	if se.Code != 409 {
		t.Fatalf("code = %d, want 409; err=%v", se.Code, err)
	}
}
