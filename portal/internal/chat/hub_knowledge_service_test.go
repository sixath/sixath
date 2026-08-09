package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

func TestListApproveKnowledgeDrafts_Wiki(t *testing.T) {
	ResetLocalMemoryHubForTest()
	t.Cleanup(ResetLocalMemoryHubForTest)

	wikiRoot := t.TempDir()
	t.Setenv("SATH_HUB_WIKI_ROOT", wikiRoot)
	InitLocalMemoryHub()

	rt := biz.RuntimeToolsConfig{}
	ctx := context.Background()
	agentID := "agent-kd-wiki"

	_, know, err := ResolveForRuntimeTools(rt)
	if err != nil {
		t.Fatal(err)
	}
	lk, ok := know.(*local.LocalKnowledge)
	if !ok {
		t.Fatalf("know type %T", know)
	}
	ww := lk.WikiWriter()
	if ww == nil {
		t.Fatal("WikiWriter nil")
	}
	canonical, err := ww.WriteDraft(ctx, "notes/hello.md", "# Hello\n\ndraft body")
	if err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}
	if canonical != "notes/hello.md" {
		t.Fatalf("canonical=%q", canonical)
	}

	drafts, err := ListKnowledgeDrafts(ctx, rt, agentID, "wiki", 10)
	if err != nil {
		t.Fatalf("ListKnowledgeDrafts: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Source != "wiki" || drafts[0].ID != canonical {
		t.Fatalf("drafts=%+v", drafts)
	}
	if !strings.Contains(drafts[0].Preview, "draft body") {
		t.Fatalf("preview=%q", drafts[0].Preview)
	}

	if err := ApproveKnowledgeDraft(ctx, rt, agentID, "wiki", canonical, false); err != nil {
		t.Fatalf("ApproveKnowledgeDraft: %v", err)
	}

	formal := filepath.Join(wikiRoot, filepath.FromSlash(canonical))
	body, err := os.ReadFile(formal)
	if err != nil {
		t.Fatalf("formal missing: %v", err)
	}
	if !strings.Contains(string(body), "draft body") {
		t.Fatalf("formal body=%q", body)
	}
	draftPath := filepath.Join(wikiRoot, filepath.FromSlash(local.DraftPathForWikiID(canonical)))
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("draft should be removed: err=%v", err)
	}

	drafts, err = ListKnowledgeDrafts(ctx, rt, agentID, "wiki", 10)
	if err != nil {
		t.Fatalf("List after approve: %v", err)
	}
	if len(drafts) != 0 {
		t.Fatalf("want empty, got %+v", drafts)
	}
}

func TestListApproveKnowledgeDrafts_Units(t *testing.T) {
	ResetLocalMemoryHubForTest()
	t.Cleanup(ResetLocalMemoryHubForTest)

	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	SetHubUnitWriter(NewMemoryUnitWriter(store))
	InitLocalMemoryHub()

	rt := biz.RuntimeToolsConfig{}
	ctx := context.Background()
	agentID := "agent-kd-units"

	_, know, err := ResolveForRuntimeTools(rt)
	if err != nil {
		t.Fatal(err)
	}
	lk := know.(*local.LocalKnowledge)
	uw := lk.UnitWriter()
	if uw == nil {
		t.Fatal("UnitWriter nil")
	}
	id, err := uw.WriteDraft(ctx, agentID, "", "Unit Title", "unit draft content")
	if err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	drafts, err := ListKnowledgeDrafts(ctx, rt, agentID, "units", 10)
	if err != nil {
		t.Fatalf("ListKnowledgeDrafts: %v", err)
	}
	if len(drafts) != 1 || drafts[0].Source != "units" || drafts[0].ID != id {
		t.Fatalf("drafts=%+v", drafts)
	}
	if drafts[0].Title != "Unit Title" || !strings.Contains(drafts[0].Preview, "unit draft") {
		t.Fatalf("meta=%+v", drafts[0])
	}

	if err := ApproveKnowledgeDraft(ctx, rt, agentID, "units", id, false); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	drafts, err = ListKnowledgeDrafts(ctx, rt, agentID, "units", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 0 {
		t.Fatalf("want empty, got %+v", drafts)
	}

	hit, err := store.Get(ctx, memory.GetRef{
		Scope: memory.ScopeUser, ScopeID: agentID, ID: id, AgentID: agentID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if local.MapUnitToAssetStatus(unitDBStatus(hit.Metadata), hit.Metadata) != hub.AssetActive {
		t.Fatalf("meta=%+v", hit.Metadata)
	}
}

func TestListKnowledgeDrafts_EmptyAgentID(t *testing.T) {
	_, err := ListKnowledgeDrafts(context.Background(), biz.RuntimeToolsConfig{}, "", "", 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApproveKnowledgeDraft_MissingWriter(t *testing.T) {
	ResetLocalMemoryHubForTest()
	t.Cleanup(ResetLocalMemoryHubForTest)
	t.Setenv("SATH_HUB_WIKI_ROOT", "")
	t.Setenv("SATH_HUB_CODEGRAPH_ROOT", "")
	InitLocalMemoryHub() // no wiki root, no unit writer

	rt := biz.RuntimeToolsConfig{}
	ctx := context.Background()
	if err := ApproveKnowledgeDraft(ctx, rt, "a1", "wiki", "x.md", false); err == nil {
		t.Fatal("expected wiki writer error")
	}
	if err := ApproveKnowledgeDraft(ctx, rt, "a1", "units", "u1", false); err == nil {
		t.Fatal("expected units writer error")
	}
	if _, err := ListKnowledgeDrafts(ctx, rt, "a1", "wiki", 5); err == nil {
		t.Fatal("expected wiki list error")
	}
}
