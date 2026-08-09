package chat

import (
	"testing"

	"github.com/sixath/framework/config"
)

func TestMemoryGraphEnabled_DefaultOff(t *testing.T) {
	t.Setenv("SATH_MEMORY_GRAPH_ENABLED", "")
	SetMemoryGraphConfig(nil)
	if memoryGraphEnabled() {
		t.Fatal("expected default off")
	}
}

func TestMemoryGraphEnabled_EnvOverride(t *testing.T) {
	SetMemoryGraphConfig(&config.MemoryGraph{Enabled: false})
	t.Setenv("SATH_MEMORY_GRAPH_ENABLED", "true")
	if !memoryGraphEnabled() {
		t.Fatal("env should enable")
	}
	t.Setenv("SATH_MEMORY_GRAPH_ENABLED", "false")
	SetMemoryGraphConfig(&config.MemoryGraph{Enabled: true})
	if memoryGraphEnabled() {
		t.Fatal("env false should disable")
	}
}

func TestApplyMemoryGraphOptions_MissingURI(t *testing.T) {
	t.Setenv("SATH_MEMORY_GRAPH_ENABLED", "")
	SetMemoryGraphConfig(&config.MemoryGraph{
		Enabled:  true,
		Provider: "neo4j",
		Neo4j:    &config.MemoryNeo4j{Username: "neo4j"},
	})
	opts := MemoryStoreOptions{}
	applyMemoryGraphOptions(&opts)
	if opts.Graph != nil {
		t.Fatal("expected no injection without uri")
	}
	SetMemoryGraphConfig(nil)
}

func TestApplyMemoryGraphOptions_ProviderNone(t *testing.T) {
	t.Setenv("SATH_MEMORY_GRAPH_ENABLED", "")
	SetMemoryGraphConfig(&config.MemoryGraph{
		Enabled:  true,
		Provider: "none",
		Neo4j:    &config.MemoryNeo4j{URI: "bolt://localhost:7687"},
	})
	opts := MemoryStoreOptions{}
	applyMemoryGraphOptions(&opts)
	if opts.Graph != nil {
		t.Fatal("provider none should not inject")
	}
	SetMemoryGraphConfig(nil)
}
