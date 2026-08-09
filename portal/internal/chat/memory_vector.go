package chat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

var (
	vectorMu         sync.Mutex
	storedVectorYAML *config.MemoryVector

	// P2-E1 UnitVectors sidecar state — backs the LIKE∪vector hybrid_recall path.
	vectorDataRoot   = "./data"
	vectorIndex      memory.UnitVectorIndex
	vectorIndexBuilt bool

	// P2-E / P2-H Vectors sidecar state (sqlite/qdrant) — backs D2 peer discovery
	// and the newer Recall(source=units) semantic search + graph expand.
	vectorIndexMu       sync.Mutex
	sharedVectorIndex   memory.VectorIndex
	sharedVectorKey     string
	errEmbedUnavailable = errors.New("memory: embedding model unavailable")
)

// SetMemoryVectorConfig stores agent_extra memory_vector settings and drops any
// index opened under the previous configuration (both the P2-E1 UnitVectors
// hybrid-recall sidecar and the newer P2-E/P2-H Vectors sqlite/qdrant sidecar).
func SetMemoryVectorConfig(cfg *config.MemoryVector) {
	vectorMu.Lock()
	if cfg == nil {
		storedVectorYAML = nil
	} else {
		cp := *cfg
		if cfg.Qdrant != nil {
			q := *cfg.Qdrant
			cp.Qdrant = &q
		}
		storedVectorYAML = &cp
	}
	closeVectorIndexLocked()
	vectorMu.Unlock()
	closeSharedVectorIndex()
}

// SetMemoryVectorDataRoot supplies Portal's data.data_root; call once at startup
// before any BuildMemoryStore. Defaults to ./data. Governs the P2-E1 UnitVectors
// sidecar only; the newer Vectors sidecar uses memory_vector.store_dir instead.
func SetMemoryVectorDataRoot(root string) {
	vectorMu.Lock()
	defer vectorMu.Unlock()
	if r := strings.TrimSpace(root); r != "" {
		vectorDataRoot = r
	}
	closeVectorIndexLocked()
}

func closeVectorIndexLocked() {
	if vectorIndex != nil {
		_ = vectorIndex.Close()
	}
	vectorIndex = nil
	vectorIndexBuilt = false
}

// memoryVectorEnabled reports whether the newer P2-E/P2-H Vectors sidecar
// (sqlite/qdrant; D2 peer discovery + Recall(source=units) semantic search) is
// enabled. Default off; SATH_MEMORY_VECTOR_ENABLED overrides.
func memoryVectorEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_MEMORY_VECTOR_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	vectorMu.Lock()
	defer vectorMu.Unlock()
	return storedVectorYAML != nil && storedVectorYAML.Enabled
}

// unitVectorSidecarEnabled reports whether the P2-E1 UnitVectors sidecar backing
// hybrid_recall is enabled. Default on (sqlite); provider=none disables it.
// Callers must hold vectorMu, or be single-goroutine test code.
// Go's Mutex is not reentrant — do not lock again here.
func unitVectorSidecarEnabled() bool {
	if storedVectorYAML == nil {
		return true // default sqlite; still requires an embedder to take effect
	}
	return !strings.EqualFold(strings.TrimSpace(storedVectorYAML.Provider), "none")
}

func memoryVectorProvider() string {
	if storedVectorYAML == nil {
		return "sqlite"
	}
	p := strings.ToLower(strings.TrimSpace(storedVectorYAML.Provider))
	if p == "" {
		return "sqlite"
	}
	return p
}

func memoryVectorStorePath() string {
	dir := "data/memory_units_vectors"
	if storedVectorYAML != nil && strings.TrimSpace(storedVectorYAML.StoreDir) != "" {
		dir = strings.TrimSpace(storedVectorYAML.StoreDir)
	}
	return filepath.Join(dir, "units_vectors.db")
}

func memoryVectorQdrantConfig() (memory.QdrantConfig, bool) {
	if storedVectorYAML == nil || storedVectorYAML.Qdrant == nil {
		return memory.QdrantConfig{}, false
	}
	q := storedVectorYAML.Qdrant
	url := strings.TrimSpace(q.URL)
	if url == "" {
		return memory.QdrantConfig{}, false
	}
	return memory.QdrantConfig{
		URL:        url,
		Collection: strings.TrimSpace(q.Collection),
		APIKey:     strings.TrimSpace(q.APIKey),
		GRPCPort:   q.GRPCPort,
	}, true
}

func memoryVectorEmbedFactoryAvailable() bool {
	if storedVectorYAML != nil && storedVectorYAML.Embedding != nil {
		if strings.TrimSpace(storedVectorYAML.Embedding.Model) != "" {
			return true
		}
	}
	if storedExtractionYAML != nil && storedExtractionYAML.Auxiliary != nil {
		if strings.TrimSpace(storedExtractionYAML.Auxiliary.Model) != "" {
			return true
		}
	}
	return globalMemoryAgentGetter != nil
}

// memoryVectorPath resolves the P2-E1 UnitVectors sidecar file path.
// Callers must hold vectorMu, or be single-goroutine test code.
func memoryVectorPath(dataRoot string) string {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	name := "memory_unit_vectors.db"
	if storedVectorYAML != nil && strings.TrimSpace(storedVectorYAML.Path) != "" {
		name = strings.TrimSpace(storedVectorYAML.Path)
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dataRoot, name)
}

func sharedVectorIndexForKey(key string, open func() (memory.VectorIndex, error)) memory.VectorIndex {
	vectorIndexMu.Lock()
	defer vectorIndexMu.Unlock()
	if sharedVectorIndex != nil && sharedVectorKey == key {
		return sharedVectorIndex
	}
	if sharedVectorIndex != nil {
		_ = sharedVectorIndex.Close()
		sharedVectorIndex = nil
		sharedVectorKey = ""
	}
	idx, err := open()
	if err != nil {
		log.Printf("memory vector: open index failed: %v", err)
		return nil
	}
	sharedVectorIndex = idx
	sharedVectorKey = key
	return sharedVectorIndex
}

func sharedSQLiteVectorIndex() memory.VectorIndex {
	path := memoryVectorStorePath()
	return sharedVectorIndexForKey("sqlite:"+path, func() (memory.VectorIndex, error) {
		return memory.NewSQLiteVectorIndex(path)
	})
}

func sharedQdrantVectorIndex() memory.VectorIndex {
	cfg, ok := memoryVectorQdrantConfig()
	if !ok {
		return nil
	}
	key := "qdrant:" + cfg.URL + "|" + cfg.Collection + "|" + strings.TrimSpace(cfg.APIKey)
	return sharedVectorIndexForKey(key, func() (memory.VectorIndex, error) {
		return memory.NewQdrantVectorIndex(cfg)
	})
}

func closeSharedVectorIndex() {
	vectorIndexMu.Lock()
	defer vectorIndexMu.Unlock()
	if sharedVectorIndex != nil {
		_ = sharedVectorIndex.Close()
		sharedVectorIndex = nil
		sharedVectorKey = ""
	}
}

// resolveVectorEmbedModel prefers memory_vector.embedding, then extraction auxiliary / agent chat.
func resolveVectorEmbedModel(agentMeta *biz.AgentMeta) (model.Model, error) {
	if storedVectorYAML != nil && storedVectorYAML.Embedding != nil {
		emb := storedVectorYAML.Embedding
		if strings.TrimSpace(emb.Model) != "" {
			return BuildModel(emb.Provider, emb.Model, emb.APIKey, emb.BaseURL)
		}
	}
	return resolveMemoryAuxModel(agentMeta)
}

// dynamicMemoryEmbed backs the newer P2-E/P2-H Vectors sqlite/qdrant sidecar.
func dynamicMemoryEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	var meta *biz.AgentMeta
	if globalMemoryAgentGetter != nil {
		if aid, _ := ctx.Value(tool.ContextKeyAgentID).(string); strings.TrimSpace(aid) != "" {
			got, err := globalMemoryAgentGetter.Get(ctx, aid)
			if err == nil {
				meta = got
			}
		}
	}
	m, err := resolveVectorEmbedModel(meta)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errEmbedUnavailable
	}
	embs, err := m.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(embs))
	for i, e := range embs {
		out[i] = e.Vector
	}
	return out, nil
}

// dynamicUnitEmbedder resolves the embedding model per call, mirroring
// dynamicSemanticConflictResolver (auxiliary → agent chat model). Backs the
// P2-E1 UnitVectors hybrid-recall sidecar (distinct from dynamicMemoryEmbed,
// which backs the newer Vectors sqlite/qdrant sidecar).
type dynamicUnitEmbedder struct {
	agents AgentGetter
}

var _ memory.UnitEmbedder = (*dynamicUnitEmbedder)(nil)

func (e *dynamicUnitEmbedder) Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error) {
	var meta *biz.AgentMeta
	getter := globalMemoryAgentGetter
	if e != nil && e.agents != nil {
		getter = e.agents
	}
	if getter != nil && strings.TrimSpace(agentID) != "" {
		got, err := getter.Get(ctx, agentID)
		if err != nil {
			return nil, err
		}
		meta = got
	}
	m, err := resolveMemoryAuxModel(meta)
	if err != nil {
		return nil, fmt.Errorf("%w: unit embed model unavailable: %v", memory.ErrEmbedModelUnavailable, err)
	}
	if m == nil {
		return nil, fmt.Errorf("%w: unit embed model unavailable", memory.ErrEmbedModelUnavailable)
	}
	embs, err := m.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(embs))
	for _, e := range embs {
		out = append(out, e.Vector)
	}
	return out, nil
}

// applyMemoryVectorOptions fills Vectors/Embed when enabled and embed factory is available.
func applyMemoryVectorOptions(opts *MemoryStoreOptions) {
	if opts == nil || !memoryVectorEnabled() {
		return
	}
	if !memoryVectorEmbedFactoryAvailable() {
		return
	}
	var idx memory.VectorIndex
	switch memoryVectorProvider() {
	case "sqlite":
		idx = sharedSQLiteVectorIndex()
	case "qdrant":
		if _, ok := memoryVectorQdrantConfig(); !ok {
			return
		}
		idx = sharedQdrantVectorIndex()
	default:
		return
	}
	if idx == nil {
		return
	}
	opts.Vectors = idx
	opts.Embed = dynamicMemoryEmbed
}

// sharedUnitVectorIndex lazily opens one sqlite sidecar per process. BuildMemoryStore
// runs at several call sites (chat service, prefetch bootstrap, runtime tools); each
// must reuse the same handle rather than opening the db again.
// Returns a nil interface when disabled or unopenable (fail-open: D2 keeps using LIKE).
func sharedUnitVectorIndex() memory.UnitVectorIndex {
	vectorMu.Lock()
	defer vectorMu.Unlock()
	if vectorIndexBuilt {
		return vectorIndex
	}
	vectorIndexBuilt = true
	if !unitVectorSidecarEnabled() {
		return nil
	}
	idx, err := memory.NewSQLiteUnitVectorIndex(memoryVectorPath(vectorDataRoot))
	if err != nil {
		return nil
	}
	vectorIndex = idx // assign only on success so the interface stays truly nil otherwise
	return vectorIndex
}
