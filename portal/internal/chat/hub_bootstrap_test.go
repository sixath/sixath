package chat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
	"github.com/sixath/framework/tool"
)

func TestInitLocalMemoryHub_ResolveDefaults(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	if !MemoryHubReady() {
		t.Fatal("not ready")
	}
	g, k, err := ResolveAgentHub(hub.AgentHubConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name() != "local" || k.Name() != "local" {
		t.Fatalf("gov=%s know=%s", g.Name(), k.Name())
	}
}

func TestResolveAgentHub_UnregisteredOverride(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	name := "tencent"
	_, _, err := ResolveAgentHub(hub.AgentHubConfig{Governance: &name})
	if err == nil {
		t.Fatal("expected error for unregistered override")
	}
}

func TestListCatalog_Local(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	snap, err := ListCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Defaults.Governance != "local" || snap.Defaults.Knowledge != "local" {
		t.Fatalf("%+v", snap.Defaults)
	}
	if len(snap.Governance) != 1 || snap.Governance[0] != "local" {
		t.Fatalf("gov=%v", snap.Governance)
	}
}

func TestValidateAgentHub_UnknownRejects(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	name := "tencent"
	err := ValidateAgentHub(biz.RuntimeToolsConfig{HubGovernance: &name})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAgentHub_LocalOK(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	name := "local"
	if err := ValidateAgentHub(biz.RuntimeToolsConfig{HubGovernance: &name, HubKnowledge: &name}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentHubConfigFromRuntime_TrimEmptyUsesDefaultSlot(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	empty := "  "
	cfg := AgentHubConfigFromRuntime(biz.RuntimeToolsConfig{HubGovernance: &empty})
	if cfg.Governance == nil || *cfg.Governance != "" {
		t.Fatalf("%+v", cfg)
	}
	_, _, err := ResolveForAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterKnowledgeHubTools_BadOverride(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	reg := tool.NewRegistry()
	bad := "nope"
	err := RegisterKnowledgeHubTools(reg, biz.RuntimeToolsConfig{HubKnowledge: &bad})
	if err == nil {
		t.Fatal("expected assembly error")
	}
}

func TestInitLocalMemoryHub_WikiCodeGraphEnv(t *testing.T) {
	ResetLocalMemoryHubForTest()
	wiki := t.TempDir()
	cg := t.TempDir()
	if err := os.WriteFile(filepath.Join(wiki, "note.md"), []byte("portal wiki index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cg, "main.go"), []byte("package main\nfunc BootHub() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_HUB_WIKI_ROOT", wiki)
	t.Setenv("SATH_HUB_CODEGRAPH_ROOT", cg)
	InitLocalMemoryHub()
	_, know, err := ResolveAgentHub(hub.AgentHubConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !know.Capabilities().Has("wiki") || !know.Capabilities().Has("code_graph") {
		t.Fatalf("%+v", know.Capabilities())
	}
	out, err := know.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{
		"query": "BootHub", "source": "codegraph",
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, _ := out.([]local.KnowledgeHit)
	if len(hits) != 1 {
		t.Fatalf("%#v", out)
	}
}
