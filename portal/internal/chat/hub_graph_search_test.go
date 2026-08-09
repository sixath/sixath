package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
	"github.com/sixath/framework/tool"
)

func TestNeo4jHubGraphSearcher_SearchLive(t *testing.T) {
	cfg := &config.MemoryGraph{
		Enabled:  true,
		Provider: "neo4j",
		MaxHops:  1,
		Neo4j: &config.MemoryNeo4j{
			URI:      "bolt://127.0.0.1:7687",
			Username: "neo4j",
			Password: "jw123456",
		},
	}
	SetMemoryGraphConfig(cfg)
	t.Cleanup(func() {
		closeSharedGraphStore()
		SetMemoryGraphConfig(nil)
	})

	g := sharedNeo4jGraphStore()
	if g == nil {
		t.Skip("neo4j not reachable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	scopeID := "hub-graph-e2e-" + time.Now().Format("150405")
	alpha := memory.Entity{
		ID: memory.StableEntityID(memory.ScopeSession, scopeID, "HubGraphAlpha"),
		Name: "HubGraphAlpha", Type: "concept", Scope: memory.ScopeSession, ScopeID: scopeID,
		SourceMemoryID: "unit-alpha", Confidence: 0.9,
	}
	beta := memory.Entity{
		ID: memory.StableEntityID(memory.ScopeSession, scopeID, "HubGraphBeta"),
		Name: "HubGraphBeta", Type: "concept", Scope: memory.ScopeSession, ScopeID: scopeID,
		SourceMemoryID: "unit-beta", Confidence: 0.9,
	}
	if err := g.UpsertEntity(ctx, alpha); err != nil {
		t.Fatalf("upsert alpha: %v", err)
	}
	if err := g.UpsertEntity(ctx, beta); err != nil {
		t.Fatalf("upsert beta: %v", err)
	}
	if err := g.UpsertRelation(ctx, memory.Relation{
		SubjectID: alpha.ID, Predicate: "related_to", ObjectID: beta.ID,
		Scope: memory.ScopeSession, ScopeID: scopeID, Confidence: 0.9,
	}); err != nil {
		t.Fatalf("upsert rel: %v", err)
	}

	ctx = context.WithValue(ctx, tool.ContextKeySessionID, scopeID)
	hits, err := neo4jHubGraphSearcher{}.Search(ctx, "HubGraphAlpha", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected graph hits")
	}
	found := false
	for _, h := range hits {
		if h.Source != "graph" {
			t.Fatalf("source=%s", h.Source)
		}
		if h.ID == beta.ID || h.ID == alpha.ID ||
			strings.Contains(h.Content, "HubGraphBeta") ||
			strings.Contains(h.Content, "HubGraphAlpha") {
			found = true
		}
	}
	if !found {
		t.Fatalf("hits=%#v", hits)
	}

	k := local.NewLocalKnowledge(local.KnowledgeBackends{Graph: neo4jHubGraphSearcher{}})
	out, err := k.Call(ctx, hub.Identity{SessionID: scopeID}, "knowledge_search", map[string]any{
		"query": "HubGraphAlpha", "source": "graph", "limit": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// LocalKnowledge.Call does not put SessionID into ctx; searcher needs ctx value.
	// Re-call searcher path is enough; Call with graph backend uses same Search.
	got, _ := out.([]local.KnowledgeHit)
	if len(got) == 0 {
		t.Fatalf("knowledge_search graph empty: %#v", out)
	}
}
