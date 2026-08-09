package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/fake"
	"github.com/sixath/framework/memory/hub/local"
	"github.com/sixath/framework/tool"
)

// TestMemoryHub_GovernanceAndKnowledgeE2E exercises the full in-process path:
// catalog → fake unsigned bind (draft) → approve → loadout → wiki/codegraph knowledge tools.
func TestMemoryHub_GovernanceAndKnowledgeE2E(t *testing.T) {
	ResetLocalMemoryHubForTest()
	wikiRoot := t.TempDir()
	cgRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(wikiRoot, "hub-e2e.md"), "# Hub E2E\nToken MEMORY_HUB_WIKI_MARKER for search.\n")
	mustWriteFile(t, filepath.Join(cgRoot, "boot.go"), "package main\n\nfunc HubE2ESymbol() {}\n")
	t.Setenv("SATH_HUB_SKILLS_CACHE", t.TempDir())
	t.Setenv("SATH_HUB_WIKI_ROOT", wikiRoot)
	t.Setenv("SATH_HUB_CODEGRAPH_ROOT", cgRoot)
	SetHubEnableFakeAdapter(true)
	InitLocalMemoryHub()

	snap, err := ListCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(snap.Governance, "local") || !containsStr(snap.Governance, fake.Name) {
		t.Fatalf("catalog gov=%v", snap.Governance)
	}

	// --- Governance: unsigned bind stays out of loadout until approve ---
	name := fake.Name
	rt := biz.RuntimeToolsConfig{HubGovernance: &name, HubKnowledge: &name}
	agentID := "e2e-hub-agent"
	if err := BindAgentAssets(context.Background(), rt, agentID, []AssetJSON{
		{Kind: "skill", ID: "demo-unsigned", Hub: fake.Name},
	}); err != nil {
		t.Fatal(err)
	}
	binds, err := ListAgentBindings(context.Background(), rt, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if binds.Total != 1 || binds.Items[0].Status != string(hub.AssetDraft) {
		t.Fatalf("want draft binding: %+v", binds)
	}
	loadout, err := ListAgentLoadout(context.Background(), rt, agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range loadout.Items {
		if it.ID == "demo-unsigned" {
			t.Fatal("draft must not be in loadout")
		}
	}
	if err := SetAgentAssetStatus(context.Background(), rt, AssetJSON{Kind: "skill", ID: "demo-unsigned", Hub: fake.Name}, string(hub.AssetActive)); err != nil {
		t.Fatal(err)
	}
	loadout, err = ListAgentLoadout(context.Background(), rt, agentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAsset(loadout.Items, "demo-unsigned") {
		t.Fatalf("approved skill missing: %+v", loadout.Items)
	}

	// --- Knowledge: tools registered + wiki/codegraph opt-in search ---
	localName := "local"
	rtLocal := biz.RuntimeToolsConfig{HubGovernance: &localName, HubKnowledge: &localName}
	_, know, err := ResolveForRuntimeTools(rtLocal)
	if err != nil {
		t.Fatal(err)
	}
	if !know.Capabilities().Has("wiki") || !know.Capabilities().Has("code_graph") {
		t.Fatalf("caps=%+v", know.Capabilities())
	}
	reg := tool.NewRegistry()
	if err := RegisterKnowledgeHubTools(reg, rtLocal); err != nil {
		t.Fatal(err)
	}
	wikiTool, ok := reg.Get("knowledge_search")
	if !ok {
		t.Fatal("knowledge_search not registered")
	}
	out, err := wikiTool.Execute(context.Background(), map[string]any{
		"query": "MEMORY_HUB_WIKI_MARKER", "source": "wiki", "limit": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, ok := out.([]local.KnowledgeHit)
	if !ok || len(hits) == 0 || !strings.Contains(hits[0].Content, "MEMORY_HUB_WIKI_MARKER") {
		t.Fatalf("wiki hits=%#v", out)
	}
	out, err = wikiTool.Execute(context.Background(), map[string]any{
		"query": "HubE2ESymbol", "source": "codegraph", "limit": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, ok = out.([]local.KnowledgeHit)
	if !ok || len(hits) == 0 || !strings.Contains(hits[0].Content, "HubE2ESymbol") {
		t.Fatalf("codegraph hits=%#v", out)
	}
	// default sources must not include wiki
	out, err = wikiTool.Execute(context.Background(), map[string]any{"query": "MEMORY_HUB_WIKI_MARKER"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _ = out.([]local.KnowledgeHit)
	for _, h := range hits {
		if h.Source == "wiki" {
			t.Fatal("default search must not hit wiki")
		}
	}

	readTool, ok := reg.Get("knowledge_read")
	if !ok {
		t.Fatal("knowledge_read not registered")
	}
	readOut, err := readTool.Execute(context.Background(), map[string]any{"id": "hub-e2e.md", "source": "wiki"})
	if err != nil {
		t.Fatal(err)
	}
	hit, ok := readOut.(local.KnowledgeHit)
	if !ok || !strings.Contains(hit.Content, "Hub E2E") {
		t.Fatalf("read=%#v", readOut)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func containsAsset(items []AssetJSON, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}
