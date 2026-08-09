package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/fake"
)

func TestFake_TransportOnKnowledgeCall(t *testing.T) {
	a := fake.New(nil)
	a.TransportErr = errors.New("down")
	_, err := a.Call(context.Background(), hub.Identity{}, "knowledge_search", map[string]any{"query": "x"})
	if !hub.IsTransport(err) {
		t.Fatalf("%v", err)
	}
}

func TestFake_EnforceHubOnBind(t *testing.T) {
	a := fake.New(nil)
	err := a.BindAssets(context.Background(), "a1", []hub.AssetRef{{Kind: hub.AssetKindSkill, ID: "s", Hub: "local"}})
	if !errors.Is(err, hub.ErrHubMismatch) {
		t.Fatalf("%v", err)
	}
}
