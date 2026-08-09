package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

func TestBindUnbindClear_RoundTrip(t *testing.T) {
	ResetLocalMemoryHubForTest()
	store := local.NewMemoryBindingStore()
	SetHubBindingStore(store)
	InitLocalMemoryHub()

	agentID := "agent-hub-1"
	rt := biz.RuntimeToolsConfig{}
	err := BindAgentAssets(context.Background(), rt, agentID, []AssetJSON{
		{Kind: "skill", ID: "demo", Name: "demo", Hub: "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := ListAgentBindings(context.Background(), rt, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 1 || view.Items[0].ID != "demo" {
		t.Fatalf("%+v", view)
	}

	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "ws-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: ws-skill\ndescription: workspace\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := BuildSkillsIndex(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	loadout, err := ListAgentLoadout(context.Background(), rt, agentID, idx)
	if err != nil {
		t.Fatal(err)
	}
	foundBound, foundWS := false, false
	for _, it := range loadout.Items {
		if it.ID == "demo" {
			foundBound = true
		}
		if it.ID == "ws-skill" {
			foundWS = true
		}
	}
	if !foundBound || !foundWS {
		t.Fatalf("loadout missing items: %+v", loadout.Items)
	}

	n, err := ClearAgentBindings(context.Background(), rt, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cleared=%d", n)
	}
	view, err = ListAgentBindings(context.Background(), rt, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 0 {
		t.Fatalf("%+v", view)
	}
}

func TestBind_EnforceHubMismatch(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	// unit kind skips materialize; Hub mismatch hits EnforceHub on local writer
	err := BindAgentAssets(context.Background(), biz.RuntimeToolsConfig{}, "a1", []AssetJSON{
		{Kind: "unit", ID: "x", Hub: "tencent"},
	})
	if err == nil {
		t.Fatal("expected enforceHub error")
	}
	if !errors.Is(err, hub.ErrHubMismatch) {
		t.Fatalf("want ErrHubMismatch, got %v", err)
	}
}
