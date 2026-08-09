package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/memory/hub/local"
)

func TestWikiDraftPaths(t *testing.T) {
	got, err := local.CanonicalWikiID("docs/foo.draft.md")
	if err != nil || got != "docs/foo.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = local.CanonicalWikiID("docs/foo")
	if err != nil || got != "docs/foo.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := local.CanonicalWikiID("docs/foo.txt"); err == nil {
		t.Fatal("expected error for non-.md")
	}
	if got := local.DraftPathForWikiID("docs/foo.md"); got != "docs/foo.draft.md" {
		t.Fatalf("got %q", got)
	}
	if !local.IsWikiDraftFile("foo.draft.md") {
		t.Fatal("expected draft")
	}
	if local.IsWikiDraftFile("foo.md") {
		t.Fatal("not draft")
	}
}

func TestDirWiki_WriteApprove_SearchSkipsDraft(t *testing.T) {
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := w.WriteDraft(ctx, "note.md", "# secret-token-xyz\n")
	if err != nil {
		t.Fatal(err)
	}
	if id != "note.md" {
		t.Fatalf("id=%q", id)
	}
	hits, err := w.Search(ctx, "secret-token-xyz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("search should skip draft, got %#v", hits)
	}
	if err := w.ApproveDraft(ctx, "note.md", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.md")); err != nil {
		t.Fatalf("formal missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.draft.md")); !os.IsNotExist(err) {
		t.Fatalf("draft should be gone, err=%v", err)
	}
	hits, err = w.Search(ctx, "secret-token-xyz", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("after approve %#v err=%v", hits, err)
	}
}

func TestDirWiki_ApproveRequiresOverwrite(t *testing.T) {
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustWrite(t, filepath.Join(dir, "note.md"), "content-A\n")
	if _, err := w.WriteDraft(ctx, "note.md", "content-B\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.ApproveDraft(ctx, "note.md", false); err == nil {
		t.Fatal("expected overwrite required error")
	}
	if err := w.ApproveDraft(ctx, "note.md", true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "content-B\n" {
		t.Fatalf("got %q", body)
	}
}

func TestDirWiki_WriteRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.WriteDraft(context.Background(), "../x.md", "no")
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestDirWiki_ListDraftsAndReadPreferDraft(t *testing.T) {
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustWrite(t, filepath.Join(dir, "note.md"), "formal-body\n")
	if _, err := w.WriteDraft(ctx, "note.md", "draft-body-unique\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteDraft(ctx, "other.md", "other-draft\n"); err != nil {
		t.Fatal(err)
	}
	drafts, err := w.ListDrafts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts=%#v", drafts)
	}
	ids := map[string]bool{}
	for _, d := range drafts {
		ids[d.ID] = true
		if d.ID == "note.md" && !strings.Contains(d.Preview, "draft-body-unique") {
			t.Fatalf("preview=%q", d.Preview)
		}
	}
	if !ids["note.md"] || !ids["other.md"] {
		t.Fatalf("ids=%v", ids)
	}
	hit, err := w.ReadPreferDraft(ctx, "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID != "note.md" {
		t.Fatalf("id=%q", hit.ID)
	}
	if !strings.Contains(hit.Content, "draft-body-unique") {
		t.Fatalf("content=%q", hit.Content)
	}
	if err := w.ApproveDraft(ctx, "note.md", true); err != nil {
		t.Fatal(err)
	}
	hit, err = w.ReadPreferDraft(ctx, "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hit.Content, "draft-body-unique") {
		t.Fatalf("after approve content=%q", hit.Content)
	}
	// no draft for other after only note approved — other still draft
	hit, err = w.ReadPreferDraft(ctx, "missing.md")
	if err == nil {
		t.Fatal("expected missing error")
	}
	_ = hit
}
