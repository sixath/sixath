package hub_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/memory/hub"
)

type fakeGov struct {
	name string
}

func (f fakeGov) Name() string                  { return f.name }
func (f fakeGov) Capabilities() hub.Capabilities { return hub.Capabilities{} }
func (f fakeGov) ResolveLoadout(context.Context, hub.Identity) ([]hub.AssetRef, error) {
	return nil, nil
}
func (f fakeGov) CheckAccess(context.Context, hub.Identity, hub.AssetRef, string) (bool, error) {
	return false, nil
}
func (f fakeGov) ListAccessible(context.Context, hub.Identity, hub.AssetFilter) (hub.Page[hub.AssetRef], error) {
	return hub.Page[hub.AssetRef]{}, nil
}

type fakeKnow struct {
	name string
}

func (f fakeKnow) Name() string                  { return f.name }
func (f fakeKnow) Capabilities() hub.Capabilities { return hub.Capabilities{} }
func (f fakeKnow) DescribeTools() []hub.ToolDesc { return nil }
func (f fakeKnow) Call(context.Context, hub.Identity, string, map[string]any) (any, error) {
	return nil, hub.ErrNotSupported
}

func TestResolve_UnregisteredGovernance(t *testing.T) {
	cat := hub.Catalog{
		Gov:  map[string]hub.GovernanceProvider{},
		Know: map[string]hub.KnowledgeProvider{"local": fakeKnow{name: "local"}},
	}
	_, _, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolve_UnregisteredKnowledge(t *testing.T) {
	cat := hub.Catalog{
		Gov:  map[string]hub.GovernanceProvider{"local": fakeGov{name: "local"}},
		Know: map[string]hub.KnowledgeProvider{},
	}
	_, _, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolve_Defaults(t *testing.T) {
	cat := hub.Catalog{
		Gov:  map[string]hub.GovernanceProvider{"local": fakeGov{name: "local"}},
		Know: map[string]hub.KnowledgeProvider{"local": fakeKnow{name: "local"}},
	}
	g, k, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name() != "local" || k.Name() != "local" {
		t.Fatalf("got gov=%s know=%s", g.Name(), k.Name())
	}
}

func TestResolve_AgentOverridesOneFace(t *testing.T) {
	cat := hub.Catalog{
		Gov: map[string]hub.GovernanceProvider{
			"local":   fakeGov{name: "local"},
			"tencent": fakeGov{name: "tencent"},
		},
		Know: map[string]hub.KnowledgeProvider{
			"local":   fakeKnow{name: "local"},
			"tencent": fakeKnow{name: "tencent"},
		},
	}
	govName := "tencent"
	g, k, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{
		Governance: &govName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name() != "tencent" {
		t.Fatalf("gov=%s", g.Name())
	}
	if k.Name() != "local" {
		t.Fatalf("know=%s want local default", k.Name())
	}
}

func TestResolve_CrossConfig(t *testing.T) {
	cat := hub.Catalog{
		Gov: map[string]hub.GovernanceProvider{
			"local": fakeGov{name: "local"},
			"fake":  fakeGov{name: "fake"},
		},
		Know: map[string]hub.KnowledgeProvider{
			"local": fakeKnow{name: "local"},
			"fake":  fakeKnow{name: "fake"},
		},
	}
	govName, knowName := "fake", "local"
	g, k, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{
		Governance: &govName,
		Knowledge:  &knowName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name() != "fake" || k.Name() != "local" {
		t.Fatalf("gov=%s know=%s", g.Name(), k.Name())
	}

	govName, knowName = "local", "fake"
	g, k, err = hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{
		Governance: &govName,
		Knowledge:  &knowName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name() != "local" || k.Name() != "fake" {
		t.Fatalf("gov=%s know=%s", g.Name(), k.Name())
	}
}

func TestEnforceHub_Mismatch(t *testing.T) {
	err := hub.EnforceHub(fakeGov{name: "local"}, hub.AssetRef{ID: "1", Hub: "tencent"})
	if !errors.Is(err, hub.ErrHubMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestEnforceHub_OK(t *testing.T) {
	err := hub.EnforceHub(fakeGov{name: "local"}, hub.AssetRef{ID: "1", Hub: "local"})
	if err != nil {
		t.Fatal(err)
	}
}
