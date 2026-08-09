package local_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

func TestMapUnitToAssetStatus_DraftInMetadata(t *testing.T) {
	st := local.MapUnitToAssetStatus(local.UnitDBActive, map[string]any{"hub_status": "draft"})
	if st != hub.AssetDraft {
		t.Fatalf("%s", st)
	}
}

func TestMapUnitToAssetStatus_Superseded(t *testing.T) {
	st := local.MapUnitToAssetStatus(local.UnitDBSuperseded, nil)
	if st != hub.AssetSuperseded {
		t.Fatalf("%s", st)
	}
}

func TestLoadoutEligible_DraftExcluded(t *testing.T) {
	if local.LoadoutEligible(hub.AssetDraft) {
		t.Fatal("draft must not be loadout-eligible")
	}
	if !local.LoadoutEligible(hub.AssetActive) {
		t.Fatal("active must be eligible")
	}
}

type countingUnits struct{ calls int }

func (c *countingUnits) Search(context.Context, string, int) ([]local.KnowledgeHit, error) {
	c.calls++
	return []local.KnowledgeHit{{ID: "u1", Source: "units", Content: "fact"}}, nil
}

type stubSearch struct {
	src string
	hit local.KnowledgeHit
}

func (s stubSearch) Search(context.Context, string, int) ([]local.KnowledgeHit, error) {
	h := s.hit
	h.Source = s.src
	return []local.KnowledgeHit{h}, nil
}

func TestResolveLoadout_DefaultNoUnitsWithoutBinding(t *testing.T) {
	store := local.NewMemoryBindingStore()
	skills := local.StaticSkills{
		{ID: "sk-a", Kind: hub.AssetKindSkill, Name: "A", Status: hub.AssetActive},
	}
	g := local.NewLocalGovernance(store, skills, local.GovernanceConfig{})
	refs, err := g.ResolveLoadout(context.Background(), hub.Identity{AgentID: "agt1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if r.Kind == hub.AssetKindUnit {
			t.Fatalf("unexpected unit in default loadout: %#v", r)
		}
	}
	if len(refs) != 1 || refs[0].ID != "sk-a" {
		t.Fatalf("%#v", refs)
	}
}

func TestResolveLoadout_ExplicitBindingIncluded(t *testing.T) {
	store := local.NewMemoryBindingStore()
	g := local.NewLocalGovernance(store, nil, local.GovernanceConfig{})
	err := g.BindAssets(context.Background(), "agt1", []hub.AssetRef{
		{ID: "sk-b", Kind: hub.AssetKindSkill, Hub: "local", Name: "B", Status: hub.AssetActive},
	})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := g.ResolveLoadout(context.Background(), hub.Identity{AgentID: "agt1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != "sk-b" || refs[0].Hub != "local" {
		t.Fatalf("%#v", refs)
	}
}

func TestLoadoutFilter_ExcludesDraftBinding(t *testing.T) {
	store := local.NewMemoryBindingStore()
	g := local.NewLocalGovernance(store, nil, local.GovernanceConfig{})
	if err := g.BindAssets(context.Background(), "agt1", []hub.AssetRef{
		{ID: "sk-d", Kind: hub.AssetKindSkill, Hub: "local", Status: hub.AssetDraft},
	}); err != nil {
		t.Fatal(err)
	}
	refs, err := g.ResolveLoadout(context.Background(), hub.Identity{AgentID: "agt1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("draft should be excluded: %#v", refs)
	}
}

func TestBindAssets_EnforceHub(t *testing.T) {
	g := local.NewLocalGovernance(local.NewMemoryBindingStore(), nil, local.GovernanceConfig{})
	err := g.BindAssets(context.Background(), "agt1", []hub.AssetRef{
		{ID: "x", Kind: hub.AssetKindSkill, Hub: "tencent"},
	})
	if !errors.Is(err, hub.ErrHubMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestKnowledgeSearch_DefaultSourcesExcludeUnits(t *testing.T) {
	units := &countingUnits{}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{
		Transcript: stubSearch{src: "transcript", hit: local.KnowledgeHit{ID: "t1", Content: "hi"}},
		Units:      units,
	})
	out, err := k.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{"query": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if units.calls != 0 {
		t.Fatalf("units called %d", units.calls)
	}
	hits, _ := out.([]local.KnowledgeHit)
	if len(hits) != 1 || hits[0].ID != "t1" {
		t.Fatalf("%#v", out)
	}
}

func TestKnowledgeSearch_ExplicitUnits(t *testing.T) {
	units := &countingUnits{}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{Units: units})
	_, err := k.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{
		"query":  "q",
		"source": "units",
	})
	if err != nil {
		t.Fatal(err)
	}
	if units.calls != 1 {
		t.Fatalf("calls=%d", units.calls)
	}
}

func TestKnowledgeCall_UnknownTool(t *testing.T) {
	k := local.NewLocalKnowledge(local.KnowledgeBackends{})
	_, err := k.Call(context.Background(), hub.Identity{}, "nope", nil)
	if !errors.Is(err, hub.ErrNotSupported) {
		t.Fatalf("%v", err)
	}
}

func TestKnowledgeCapabilities_WikiCodeGraphFlags(t *testing.T) {
	k := local.NewLocalKnowledge(local.KnowledgeBackends{})
	if k.Capabilities().Has("wiki") || k.Capabilities().Has("code_graph") {
		t.Fatal("expected no wiki/code_graph without backends")
	}
	wiki := stubSearch{src: "wiki", hit: local.KnowledgeHit{ID: "w1", Source: "wiki", Content: "page"}}
	cg := stubSearch{src: "codegraph", hit: local.KnowledgeHit{ID: "c1", Source: "codegraph", Content: "sym"}}
	k2 := local.NewLocalKnowledge(local.KnowledgeBackends{Wiki: wiki, CodeGraph: cg})
	if !k2.Capabilities().Has("wiki") || !k2.Capabilities().Has("code_graph") {
		t.Fatalf("%+v", k2.Capabilities())
	}
	out, err := k2.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{
		"query": "q", "source": "wiki,codegraph",
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, _ := out.([]local.KnowledgeHit)
	if len(hits) != 2 {
		t.Fatalf("%#v", hits)
	}
}

// default sources still exclude wiki/codegraph (opt-in via source=)
func TestKnowledgeSearch_DefaultExcludesWiki(t *testing.T) {
	w := &countingWiki{}
	k := local.NewLocalKnowledge(local.KnowledgeBackends{
		Transcript: stubSearch{src: "transcript", hit: local.KnowledgeHit{ID: "t1", Content: "hi"}},
		Wiki:       w,
	})
	_, err := k.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{"query": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if w.calls != 0 {
		t.Fatalf("wiki called on default sources: %d", w.calls)
	}
}

type countingWiki struct{ calls int }

func (c *countingWiki) Search(context.Context, string, int) ([]local.KnowledgeHit, error) {
	c.calls++
	return []local.KnowledgeHit{{ID: "w", Source: "wiki"}}, nil
}
