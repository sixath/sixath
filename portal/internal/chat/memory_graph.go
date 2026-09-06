package chat

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
)

var (
	storedGraphYAML  *config.MemoryGraph
	graphStoreMu     sync.Mutex
	sharedGraphStore memory.GraphStore
	sharedGraphKey   string
)

// SetMemoryGraphConfig stores agent_extra memory_graph / memory_store.graph settings.
func SetMemoryGraphConfig(cfg *config.MemoryGraph) {
	if cfg == nil {
		storedGraphYAML = nil
		return
	}
	cp := *cfg
	if cfg.Neo4j != nil {
		n := *cfg.Neo4j
		cp.Neo4j = &n
	}
	if cfg.Auxiliary != nil {
		a := *cfg.Auxiliary
		cp.Auxiliary = &a
	}
	storedGraphYAML = &cp
}

func memoryGraphEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_MEMORY_GRAPH_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return storedGraphYAML != nil && storedGraphYAML.Enabled
}

func memoryGraphProvider() string {
	if storedGraphYAML == nil {
		return "none"
	}
	p := strings.ToLower(strings.TrimSpace(storedGraphYAML.Provider))
	if p == "" {
		return "neo4j"
	}
	return p
}

func memoryGraphNeo4jConfig() (memory.Neo4jConfig, bool) {
	if storedGraphYAML == nil || storedGraphYAML.Neo4j == nil {
		return memory.Neo4jConfig{}, false
	}
	n := storedGraphYAML.Neo4j
	uri := strings.TrimSpace(n.URI)
	if uri == "" {
		return memory.Neo4jConfig{}, false
	}
	return memory.Neo4jConfig{
		URI:      uri,
		Username: strings.TrimSpace(n.Username),
		Password: n.Password,
		Database: strings.TrimSpace(n.Database),
	}, true
}

func sharedNeo4jGraphStore() memory.GraphStore {
	cfg, ok := memoryGraphNeo4jConfig()
	if !ok {
		return nil
	}
	key := "neo4j:" + cfg.URI + "|" + cfg.Username + "|" + cfg.Database
	graphStoreMu.Lock()
	defer graphStoreMu.Unlock()
	if sharedGraphStore != nil && sharedGraphKey == key {
		return sharedGraphStore
	}
	if sharedGraphStore != nil {
		_ = sharedGraphStore.Close()
		sharedGraphStore = nil
		sharedGraphKey = ""
	}
	idx, err := memory.NewNeo4jGraphStore(cfg)
	if err != nil {
		log.Printf("memory graph: open neo4j failed: %v", err)
		return nil
	}
	sharedGraphStore = idx
	sharedGraphKey = key
	return sharedGraphStore
}

func closeSharedGraphStore() {
	graphStoreMu.Lock()
	defer graphStoreMu.Unlock()
	if sharedGraphStore != nil {
		_ = sharedGraphStore.Close()
		sharedGraphStore = nil
		sharedGraphKey = ""
	}
}

func resolveGraphModel(agentMeta *biz.AgentMeta) (model.Model, error) {
	if storedGraphYAML != nil && storedGraphYAML.Auxiliary != nil {
		aux := storedGraphYAML.Auxiliary
		if strings.TrimSpace(aux.Model) != "" {
			return BuildModel(aux.Provider, aux.Model, aux.APIKey, aux.BaseURL)
		}
	}
	return resolveMemoryAuxModel(agentMeta)
}

func memoryGraphMinConfidence() float64 {
	if storedGraphYAML != nil && storedGraphYAML.MinRelationConfidence > 0 {
		return storedGraphYAML.MinRelationConfidence
	}
	return 0.7
}

func memoryGraphMaxEntities() int {
	if storedGraphYAML != nil && storedGraphYAML.MaxEntities > 0 {
		return storedGraphYAML.MaxEntities
	}
	return 32
}

func formatGraphDrops(drops map[string]int) string {
	if len(drops) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(drops))
	for k, v := range drops {
		if v <= 0 {
			continue
		}
		parts = append(parts, k+":"+strconv.Itoa(v))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func memoryGraphMaxHops() int {
	if storedGraphYAML != nil && storedGraphYAML.MaxHops > 0 {
		return storedGraphYAML.MaxHops
	}
	return 1
}

func memoryGraphRRFK() int {
	if storedGraphYAML != nil && storedGraphYAML.RRFK > 0 {
		return storedGraphYAML.RRFK
	}
	return 60
}

// applyMemoryGraphOptions fills Graph / hop / RRF when enabled and Neo4j is reachable.
func applyMemoryGraphOptions(opts *MemoryStoreOptions) {
	if opts == nil || !memoryGraphEnabled() {
		return
	}
	if memoryGraphProvider() != "neo4j" {
		return
	}
	if _, ok := memoryGraphNeo4jConfig(); !ok {
		return
	}
	g := sharedNeo4jGraphStore()
	if g == nil {
		return
	}
	opts.Graph = g
	opts.GraphMaxHops = memoryGraphMaxHops()
	opts.GraphRRFK = memoryGraphRRFK()
}
