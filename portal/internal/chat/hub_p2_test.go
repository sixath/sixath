package chat

import (
	"context"
	"errors"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/fake"
	"github.com/sixath/framework/memory/hub/local"
)

func TestFakeAdapter_CatalogAndUnsignedBindDraft(t *testing.T) {
	ResetLocalMemoryHubForTest()
	t.Setenv("SATH_HUB_SKILLS_CACHE", t.TempDir())
	SetHubEnableFakeAdapter(true)
	InitLocalMemoryHub()

	snap, err := ListCatalog()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range snap.Governance {
		if n == fake.Name {
			found = true
		}
	}
	if !found {
		t.Fatalf("fake not in catalog: %+v", snap)
	}

	src := HubSkillSource(fake.Name)
	fa, ok := src.(*fake.Adapter)
	if !ok || fa == nil {
		t.Fatal("expected *fake.Adapter as skill source")
	}
	fa.PutSkill(hub.SkillContent{
		SkillID: "ext-1", Name: "ext-1", Version: "1",
		Body:   []byte("---\nname: ext-1\ndescription: x\n---\n"),
		Signed: false,
	})

	name := fake.Name
	rt := biz.RuntimeToolsConfig{HubGovernance: &name, HubKnowledge: &name}
	err = BindAgentAssets(context.Background(), rt, "agent-1", []AssetJSON{
		{Kind: "skill", ID: "ext-1", Hub: fake.Name},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := ListAgentBindings(context.Background(), rt, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 1 || view.Items[0].Status != string(hub.AssetDraft) {
		t.Fatalf("want draft binding, got %+v", view)
	}
	loadout, err := ListAgentLoadout(context.Background(), rt, "agent-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range loadout.Items {
		if it.ID == "ext-1" {
			t.Fatal("draft skill must not enter loadout")
		}
	}

	// P2b: approve draft → active → enters loadout
	if err := SetAgentAssetStatus(context.Background(), rt, AssetJSON{Kind: "skill", ID: "ext-1", Hub: fake.Name}, string(hub.AssetActive)); err != nil {
		t.Fatal(err)
	}
	view, err = ListAgentBindings(context.Background(), rt, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Items[0].Status != string(hub.AssetActive) {
		t.Fatalf("want active after approve, got %+v", view.Items[0])
	}
	loadout, err = ListAgentLoadout(context.Background(), rt, "agent-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	okLoad := false
	for _, it := range loadout.Items {
		if it.ID == "ext-1" {
			okLoad = true
		}
	}
	if !okLoad {
		t.Fatalf("approved skill missing from loadout: %+v", loadout.Items)
	}
}

func TestFakeAdapter_TransportReadFailOpen(t *testing.T) {
	ResetLocalMemoryHubForTest()
	InitLocalMemoryHub()
	fa := fake.New(local.NewMemoryBindingStore())
	fa.TransportErr = errors.New("down")
	RegisterHubProvider(fa, fa, fa)

	refs, err := hub.ReadLoadout(context.Background(), fa, hub.Identity{AgentID: "a"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if refs != nil && len(refs) != 0 {
		t.Fatalf("%#v", refs)
	}
	ok, err := hub.CheckAccessSafe(context.Background(), fa, hub.Identity{}, hub.AssetRef{ID: "1", Hub: fake.Name}, "read", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("transport must deny CheckAccess")
	}
}
