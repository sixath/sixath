package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/config"
)

func TestMemoryVectorEnabled_DefaultOff(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	SetMemoryVectorConfig(nil)
	if memoryVectorEnabled() {
		t.Fatal("expected default off")
	}
}

func TestMemoryVectorEnabled_EnvOverride(t *testing.T) {
	SetMemoryVectorConfig(&config.MemoryVector{Enabled: false})
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "true")
	if !memoryVectorEnabled() {
		t.Fatal("env should enable")
	}
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "false")
	SetMemoryVectorConfig(&config.MemoryVector{Enabled: true})
	if memoryVectorEnabled() {
		t.Fatal("env false should disable")
	}
}

func TestApplyMemoryVectorOptions_NoEmbedFactory(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	dir := t.TempDir()
	SetMemoryVectorConfig(&config.MemoryVector{
		Enabled: true, Provider: "sqlite", StoreDir: dir,
	})
	opts := MemoryStoreOptions{}
	applyMemoryVectorOptions(&opts)
	if opts.Vectors != nil || opts.Embed != nil {
		t.Fatalf("expected no injection without embed factory, got vectors=%v embed=%v", opts.Vectors != nil, opts.Embed != nil)
	}
}

func TestApplyMemoryVectorOptions_WithEmbeddingConfig(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	dir := t.TempDir()
	SetMemoryVectorConfig(&config.MemoryVector{
		Enabled:  true,
		Provider: "sqlite",
		StoreDir: dir,
		Embedding: &config.MemoryExtractionModel{
			Provider: "openai",
			Model:    "text-embedding-3-small",
			APIKey:   "sk-test",
		},
	})
	opts := MemoryStoreOptions{}
	applyMemoryVectorOptions(&opts)
	if opts.Vectors == nil || opts.Embed == nil {
		t.Fatal("expected Vectors and Embed injected")
	}
	if _, err := os.Stat(filepath.Join(dir, "units_vectors.db")); err != nil {
		t.Fatalf("expected sqlite file: %v", err)
	}
	closeSharedVectorIndex()
	SetMemoryVectorConfig(nil)
}

func TestApplyMemoryVectorOptions_QdrantMissingURL(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	SetMemoryVectorConfig(&config.MemoryVector{
		Enabled:  true,
		Provider: "qdrant",
		Qdrant:   &config.MemoryQdrant{Collection: "c"},
		Embedding: &config.MemoryExtractionModel{
			Provider: "openai",
			Model:    "text-embedding-3-small",
			APIKey:   "sk-test",
		},
	})
	opts := MemoryStoreOptions{}
	applyMemoryVectorOptions(&opts)
	if opts.Vectors != nil || opts.Embed != nil {
		t.Fatalf("expected no injection without qdrant url, got vectors=%v embed=%v", opts.Vectors != nil, opts.Embed != nil)
	}
	SetMemoryVectorConfig(nil)
}

func TestApplyMemoryVectorOptions_UnknownProvider(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	SetMemoryVectorConfig(&config.MemoryVector{
		Enabled:  true,
		Provider: "none",
		Embedding: &config.MemoryExtractionModel{
			Provider: "openai", Model: "text-embedding-3-small", APIKey: "k",
		},
	})
	opts := MemoryStoreOptions{}
	applyMemoryVectorOptions(&opts)
	if opts.Vectors != nil {
		t.Fatal("provider none should not inject")
	}
	SetMemoryVectorConfig(nil)
}

func TestDefaultMemoryStoreOptions_IncludesVectorWhenConfigured(t *testing.T) {
	t.Setenv("SATH_MEMORY_VECTOR_ENABLED", "")
	t.Setenv("SATH_MEMORY_CONFLICT_ENABLED", "")
	SetMemoryAgentGetter(nil)
	SetMemoryExtractionConfig(nil)
	SetMemoryConflictConfig(nil)
	dir := t.TempDir()
	SetMemoryVectorConfig(&config.MemoryVector{
		Enabled: true, Provider: "sqlite", StoreDir: dir,
		Embedding: &config.MemoryExtractionModel{Provider: "openai", Model: "text-embedding-3-small", APIKey: "k"},
	})
	opts := DefaultMemoryStoreOptions()
	if opts.Vectors == nil || opts.Embed == nil {
		t.Fatal("DefaultMemoryStoreOptions should apply vector options")
	}
	closeSharedVectorIndex()
	SetMemoryVectorConfig(nil)
}

// restoreTestVectorDefaults resets the package-level sidecar knobs to the
// TestMain baseline (provider=none, data root ./data). Prefer this over
// SetMemoryVectorConfig(nil), which would re-enable the production sqlite default
// and let later tests open a real DB under the package cwd.
func restoreTestVectorDefaults(t *testing.T) {
	t.Helper()
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	SetMemoryVectorDataRoot("./data")
}

func TestMemoryVectorProviderNone_DisablesIndex(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	if unitVectorSidecarEnabled() {
		t.Fatal("provider=none must disable the sidecar")
	}
	if idx := sharedUnitVectorIndex(); idx != nil {
		t.Fatalf("provider=none must not open an index, got %T", idx)
	}
}

// Production default (nil config) means sqlite. TestMain pins provider=none for the
// package, so this case explicitly clears to nil and restores afterwards.
func TestMemoryVectorPath(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })

	SetMemoryVectorConfig(nil)
	if !unitVectorSidecarEnabled() {
		t.Fatal("omitted config should default to sqlite")
	}
	if got := memoryVectorPath("data"); got != filepath.Join("data", "memory_unit_vectors.db") {
		t.Fatalf("default path wrong: %s", got)
	}

	SetMemoryVectorConfig(&config.MemoryVector{Path: "custom.db"})
	if got := memoryVectorPath("data"); got != filepath.Join("data", "custom.db") {
		t.Fatalf("relative path must join data root: %s", got)
	}
}

// SetMemoryVectorConfig / SetMemoryVectorDataRoot must force the next
// sharedUnitVectorIndex() call to re-resolve (hot reload + test isolation).
func TestSharedUnitVectorIndex_CachesAndResets(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })
	SetMemoryVectorDataRoot(t.TempDir())
	SetMemoryVectorConfig(nil) // enable production-default sqlite under TempDir

	first := sharedUnitVectorIndex()
	if first == nil {
		t.Fatal("default provider should open an index")
	}
	if second := sharedUnitVectorIndex(); second != first {
		t.Fatal("index must be a cached singleton across calls")
	}
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	if sharedUnitVectorIndex() != nil {
		t.Fatal("reconfigure must drop the cached index")
	}
}

// 无 aux 且无 AgentGetter → 不注入 embedder（与 semantic resolver 判定一致）。
func TestDefaultMemoryStoreOptions_NoModelFactory_NoEmbedder(t *testing.T) {
	t.Cleanup(func() {
		restoreTestVectorDefaults(t)
		SetMemoryAgentGetter(nil)
	})
	SetMemoryAgentGetter(nil)
	SetMemoryConflictConfig(nil)

	opts := DefaultMemoryStoreOptions()
	if opts.UnitEmbedder != nil {
		t.Fatal("embedder must be nil without a model factory")
	}
	if opts.UnitVectors != nil {
		t.Fatal("TestMain baseline (provider=none) must not inject an index")
	}
}
