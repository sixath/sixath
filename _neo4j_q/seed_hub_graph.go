//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sixath/framework/memory"
)

// Seeds a tiny session-scope graph for Hub knowledge_search E2E.
// Usage: go run seed_hub_graph.go <session_id>
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run seed_hub_graph.go <session_id>")
		os.Exit(2)
	}
	sessionID := os.Args[1]
	pass := os.Getenv("NEO4J_PASSWORD")
	if pass == "" {
		pass = "jw123456"
	}
	uri := os.Getenv("NEO4J_URI")
	if uri == "" {
		uri = "bolt://127.0.0.1:7687"
	}
	store, err := memory.NewNeo4jGraphStore(memory.Neo4jConfig{
		URI: uri, Username: "neo4j", Password: pass,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	alpha := memory.Entity{
		ID: memory.StableEntityID(memory.ScopeSession, sessionID, "KnowledgeE2EAlpha"),
		Name: "KnowledgeE2EAlpha", Type: "concept",
		Scope: memory.ScopeSession, ScopeID: sessionID,
		SourceMemoryID: "know-e2e-alpha", Confidence: 0.95,
	}
	beta := memory.Entity{
		ID: memory.StableEntityID(memory.ScopeSession, sessionID, "KnowledgeE2EBeta"),
		Name: "KnowledgeE2EBeta", Type: "concept",
		Scope: memory.ScopeSession, ScopeID: sessionID,
		SourceMemoryID: "know-e2e-beta", Confidence: 0.95,
	}
	if err := store.UpsertEntity(ctx, alpha); err != nil {
		fmt.Fprintf(os.Stderr, "upsert alpha: %v\n", err)
		os.Exit(1)
	}
	if err := store.UpsertEntity(ctx, beta); err != nil {
		fmt.Fprintf(os.Stderr, "upsert beta: %v\n", err)
		os.Exit(1)
	}
	if err := store.UpsertRelation(ctx, memory.Relation{
		SubjectID: alpha.ID, Predicate: "related_to", ObjectID: beta.ID,
		Scope: memory.ScopeSession, ScopeID: sessionID, Confidence: 0.9,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "upsert rel: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("SEEDED session=%s alpha=%s beta=%s\n", sessionID, alpha.ID, beta.ID)
}
