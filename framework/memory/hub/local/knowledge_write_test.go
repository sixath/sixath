package local_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

type fakeUnitWriter struct {
	lastAgentID string
	lastID      string
	lastTitle   string
	lastContent string
	nextID      string
	writeErr    error
	approveErr  error
}

func (f *fakeUnitWriter) WriteDraft(_ context.Context, agentID, id, title, content string) (string, error) {
	f.lastAgentID = agentID
	f.lastID = id
	f.lastTitle = title
	f.lastContent = content
	if f.writeErr != nil {
		return "", f.writeErr
	}
	if f.nextID != "" {
		return f.nextID, nil
	}
	if id != "" {
		return id, nil
	}
	return "unit-new", nil
}

func (f *fakeUnitWriter) ApproveDraft(_ context.Context, agentID, id string) error {
	f.lastAgentID = agentID
	f.lastID = id
	return f.approveErr
}

func (f *fakeUnitWriter) ListDrafts(context.Context, string, int) ([]local.UnitDraftMeta, error) {
	return nil, nil
}

type searchOnlyWiki struct{}

func (searchOnlyWiki) Search(context.Context, string, int) ([]local.KnowledgeHit, error) {
	return nil, nil
}

func TestKnowledgeWrite_CapabilitiesAndDescribe(t *testing.T) {
	t.Parallel()
	none := local.NewLocalKnowledge(local.KnowledgeBackends{})
	if none.Capabilities().Write {
		t.Fatal("Write should be false with no wiki/units write")
	}
	if none.Capabilities().Has("knowledge_write") {
		t.Fatal("flag knowledge_write should be off")
	}
	for _, name := range toolNames(none.DescribeTools()) {
		if name == "knowledge_write" || name == "knowledge_approve" {
			t.Fatalf("unexpected tool %q without Write", name)
		}
	}

	wikiDir := t.TempDir()
	w, err := local.NewDirWiki(wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	withWiki := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: w})
	if !withWiki.Capabilities().Write || !withWiki.Capabilities().Has("knowledge_write") {
		t.Fatalf("Write/flag want true, got %#v", withWiki.Capabilities())
	}
	names := toolNames(withWiki.DescribeTools())
	if !contains(names, "knowledge_write") || !contains(names, "knowledge_approve") {
		t.Fatalf("missing write tools: %v", names)
	}
	readSchema := toolSchema(withWiki.DescribeTools(), "knowledge_read")
	props, _ := readSchema["properties"].(map[string]any)
	if _, ok := props["include_draft"]; !ok {
		t.Fatalf("knowledge_read schema missing include_draft: %#v", readSchema)
	}

	withUnits := local.NewLocalKnowledge(local.KnowledgeBackends{UnitsWrite: &fakeUnitWriter{}})
	if !withUnits.Capabilities().Write {
		t.Fatal("Write should be true with UnitsWrite")
	}
}

func TestKnowledgeWrite_WikiDraftApproveSearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: w})
	ctx := context.Background()
	id := hub.Identity{AgentID: "agent-1"}

	out, err := k.Call(ctx, id, "knowledge_write", map[string]any{
		"source":  "wiki",
		"id":      "note.md",
		"content": "# secret-token-xyz\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, out)
	if m["status"] != "draft" || m["id"] != "note.md" || m["source"] != "wiki" {
		t.Fatalf("write result %#v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.draft.md")); err != nil {
		t.Fatalf("draft missing: %v", err)
	}

	hits, err := k.Call(ctx, id, "knowledge_search", map[string]any{
		"query":  "secret-token-xyz",
		"source": "wiki",
	})
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := hits.([]local.KnowledgeHit); !ok || len(list) != 0 {
		t.Fatalf("search should miss draft, got %#v", hits)
	}

	out, err = k.Call(ctx, id, "knowledge_approve", map[string]any{
		"source": "wiki",
		"id":     "note.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	m = asMap(t, out)
	if m["status"] != "active" || m["id"] != "note.md" {
		t.Fatalf("approve result %#v", m)
	}

	hits, err = k.Call(ctx, id, "knowledge_search", map[string]any{
		"query":  "secret-token-xyz",
		"source": "wiki",
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := hits.([]local.KnowledgeHit)
	if !ok || len(list) != 1 {
		t.Fatalf("after approve %#v", hits)
	}
}

func TestKnowledgeWrite_UnitsRequiresWriterAndAgentID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k := local.NewLocalKnowledge(local.KnowledgeBackends{})
	_, err := k.Call(ctx, hub.Identity{AgentID: "a1"}, "knowledge_write", map[string]any{
		"source":  "units",
		"content": "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "units") {
		t.Fatalf("want clear units write error, got %v", err)
	}

	fake := &fakeUnitWriter{nextID: "u-1"}
	k = local.NewLocalKnowledge(local.KnowledgeBackends{UnitsWrite: fake})
	_, err = k.Call(ctx, hub.Identity{}, "knowledge_write", map[string]any{
		"source":  "units",
		"content": "hello",
		"title":   "t",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "agent") {
		t.Fatalf("empty AgentID should error, got %v", err)
	}

	out, err := k.Call(ctx, hub.Identity{AgentID: "agent-42"}, "knowledge_write", map[string]any{
		"source":  "units",
		"id":      "keep-me",
		"title":   "Title",
		"content": "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastAgentID != "agent-42" || fake.lastID != "keep-me" || fake.lastTitle != "Title" || fake.lastContent != "body" {
		t.Fatalf("fake got agent=%q id=%q title=%q content=%q", fake.lastAgentID, fake.lastID, fake.lastTitle, fake.lastContent)
	}
	m := asMap(t, out)
	if m["id"] != "u-1" || m["status"] != "draft" || m["source"] != "units" {
		t.Fatalf("units write result %#v", m)
	}
}

func TestKnowledgeWrite_ReadIncludeDraftAndExplicitDraftID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := local.NewDirWiki(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: w})
	ctx := context.Background()
	id := hub.Identity{AgentID: "a"}
	mustWrite(t, filepath.Join(dir, "foo.md"), "formal-body\n")
	if _, err := w.WriteDraft(ctx, "foo.md", "draft-body\n"); err != nil {
		t.Fatal(err)
	}

	got, err := k.Call(ctx, id, "knowledge_read", map[string]any{
		"source":        "wiki",
		"id":            "foo.md",
		"include_draft": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hit := asHit(t, got)
	if hit.ID != "foo.md" || hit.Content != "draft-body\n" {
		t.Fatalf("include_draft prefer draft: %#v", hit)
	}

	got, err = k.Call(ctx, id, "knowledge_read", map[string]any{
		"source": "wiki",
		"id":     "foo.draft.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	hit = asHit(t, got)
	if hit.ID != "foo.md" || !strings.Contains(hit.Content, "draft-body") {
		t.Fatalf("explicit draft id: %#v", hit)
	}
}

func TestKnowledgeWrite_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wikiDir := t.TempDir()
	w, err := local.NewDirWiki(wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: w, UnitsWrite: &fakeUnitWriter{}})
	id := hub.Identity{AgentID: "a"}

	_, err = k.Call(ctx, id, "knowledge_write", map[string]any{
		"source":  "wiki",
		"id":      "x.md",
		"content": "",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "content") {
		t.Fatalf("empty content: %v", err)
	}

	_, err = k.Call(ctx, id, "knowledge_write", map[string]any{
		"source":  "mystery",
		"content": "x",
	})
	if err == nil {
		t.Fatal("unknown source should error")
	}

	kSearchOnly := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: searchOnlyWiki{}})
	_, err = kSearchOnly.Call(ctx, id, "knowledge_write", map[string]any{
		"source":  "wiki",
		"id":      "x.md",
		"content": "hi",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "wiki") {
		t.Fatalf("wiki without WikiWriter: %v", err)
	}

	_, err = k.Call(ctx, hub.Identity{}, "knowledge_approve", map[string]any{
		"source": "units",
		"id":     "u1",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "agent") {
		t.Fatalf("units approve empty AgentID: %v", err)
	}
}

func toolNames(descs []hub.ToolDesc) []string {
	out := make([]string, 0, len(descs))
	for _, d := range descs {
		out = append(out, d.Name)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func toolSchema(descs []hub.ToolDesc, name string) map[string]any {
	for _, d := range descs {
		if d.Name == name {
			return d.InputSchema
		}
	}
	return nil
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	if m, ok := v.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v %#v", err, v)
	}
	return m
}

func asHit(t *testing.T, v any) local.KnowledgeHit {
	t.Helper()
	if h, ok := v.(local.KnowledgeHit); ok {
		return h
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var h local.KnowledgeHit
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("hit: %v %#v", err, v)
	}
	return h
}
