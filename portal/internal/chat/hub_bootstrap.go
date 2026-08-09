package chat

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/fake"
	"github.com/sixath/framework/memory/hub/local"
)

var (
	hubMu           sync.RWMutex
	hubCatalog      hub.Catalog
	hubDefaults     hub.Defaults
	hubReady        bool
	hubBindingStore local.BindingStore
	hubUnitWriter   local.UnitWriter
	hubSkillSources map[string]hub.SkillSource
	hubTrustGate    *hub.SkillTrustGate
	hubEnableFake   bool
)

// CatalogSnapshot is the read-only Catalog view for UI / diagnostics.
type CatalogSnapshot struct {
	Defaults   CatalogDefaults `json:"defaults"`
	Governance []string        `json:"governance"`
	Knowledge  []string        `json:"knowledge"`
}

// CatalogDefaults is JSON-friendly defaults.
type CatalogDefaults struct {
	Governance string `json:"governance"`
	Knowledge  string `json:"knowledge"`
}

// SetHubBindingStore installs the process BindingStore (MySQL in production).
// Resets readiness so the next InitLocalMemoryHub picks up the store.
func SetHubBindingStore(store local.BindingStore) {
	hubMu.Lock()
	defer hubMu.Unlock()
	hubBindingStore = store
	hubReady = false
}

// SetHubUnitWriter installs the optional memory-units write backend for LocalKnowledge.
// Resets readiness so the next InitLocalMemoryHub picks up UnitsWrite.
func SetHubUnitWriter(uw local.UnitWriter) {
	hubMu.Lock()
	defer hubMu.Unlock()
	hubUnitWriter = uw
	hubReady = false
}

// SetHubEnableFakeAdapter enables registering the in-process fake external Adapter (P2a).
func SetHubEnableFakeAdapter(enable bool) {
	hubMu.Lock()
	defer hubMu.Unlock()
	hubEnableFake = enable
	hubReady = false
}

// InitLocalMemoryHub registers the default local governance/knowledge providers.
// Idempotent until SetHubBindingStore / SetHubUnitWriter / SetHubEnableFakeAdapter / ResetLocalMemoryHubForTest.
//
// Optional knowledge backends (P3b/P3c):
//   - SATH_HUB_WIKI_ROOT      → DirWiki (Capabilities wiki)
//   - SATH_HUB_CODEGRAPH_ROOT → DirCodeGraph (Capabilities code_graph)
//   - memory_graph.enabled + neo4j → lazy GraphSearcher (source=graph; default sources include graph)
func InitLocalMemoryHub() {
	hubMu.Lock()
	defer hubMu.Unlock()
	if hubReady {
		return
	}
	store := hubBindingStore
	if store == nil {
		store = local.NewMemoryBindingStore()
		hubBindingStore = store
	}
	gov := local.NewLocalGovernance(store, nil, local.GovernanceConfig{})
	know := local.NewLocalKnowledge(buildKnowledgeBackendsLocked())
	hubCatalog = hub.Catalog{
		Gov:  map[string]hub.GovernanceProvider{gov.Name(): gov},
		Know: map[string]hub.KnowledgeProvider{know.Name(): know},
	}
	hubDefaults = hub.Defaults{Governance: gov.Name(), Knowledge: know.Name()}
	hubSkillSources = map[string]hub.SkillSource{}

	cacheRoot := filepath.Join(os.TempDir(), "sixath-hub-skills")
	if root := strings.TrimSpace(os.Getenv("SATH_HUB_SKILLS_CACHE")); root != "" {
		cacheRoot = root
	}
	fs := hub.NewFSMaterializer(cacheRoot)
	hubTrustGate = hub.NewSkillTrustGate(fs, fs, nil)

	if hubEnableFake || envTruthy("SATH_HUB_FAKE_ADAPTER") {
		fa := fake.New(store) // share BindingStore with local for Portal list/clear UX
		// Seed one unsigned skill so live E2E / UI can exercise draft→approve.
		fa.PutSkill(hub.SkillContent{
			SkillID: "demo-unsigned",
			Name:    "demo-unsigned",
			Version: "1",
			Body:    []byte("---\nname: demo-unsigned\ndescription: Memory Hub E2E demo skill\n---\n# demo-unsigned\n"),
			Signed:  false,
		})
		hubCatalog.Gov[fa.Name()] = fa
		hubCatalog.Know[fa.Name()] = fa
		hubSkillSources[fa.Name()] = fa
	}
	hubReady = true
}

// buildKnowledgeBackendsLocked reads optional wiki/codegraph roots from env and
// always attaches a lazy Neo4j GraphSearcher (no-ops until memory_graph enabled).
// Caller must hold hubMu (or be single-threaded at init).
func buildKnowledgeBackendsLocked() local.KnowledgeBackends {
	b := local.KnowledgeBackends{
		Graph: neo4jHubGraphSearcher{},
	}
	if root := strings.TrimSpace(os.Getenv("SATH_HUB_WIKI_ROOT")); root != "" {
		if w, err := local.NewDirWiki(root); err == nil {
			b.Wiki = w
		}
	}
	if root := strings.TrimSpace(os.Getenv("SATH_HUB_CODEGRAPH_ROOT")); root != "" {
		if cg, err := local.NewDirCodeGraph(root); err == nil {
			b.CodeGraph = cg
		}
	}
	if hubUnitWriter != nil {
		b.UnitsWrite = hubUnitWriter
	}
	return b
}

// RegisterHubProvider adds an external provider pair into the Catalog (must Init first or will Init).
func RegisterHubProvider(gov hub.GovernanceProvider, know hub.KnowledgeProvider, source hub.SkillSource) {
	if !MemoryHubReady() {
		InitLocalMemoryHub()
	}
	hubMu.Lock()
	defer hubMu.Unlock()
	if hubCatalog.Gov == nil {
		hubCatalog.Gov = map[string]hub.GovernanceProvider{}
	}
	if hubCatalog.Know == nil {
		hubCatalog.Know = map[string]hub.KnowledgeProvider{}
	}
	if hubSkillSources == nil {
		hubSkillSources = map[string]hub.SkillSource{}
	}
	if gov != nil {
		hubCatalog.Gov[gov.Name()] = gov
	}
	if know != nil {
		hubCatalog.Know[know.Name()] = know
	}
	if source != nil {
		name := ""
		if gov != nil {
			name = gov.Name()
		} else if know != nil {
			name = know.Name()
		}
		if name != "" {
			hubSkillSources[name] = source
		}
	}
}

// ResetLocalMemoryHubForTest clears Catalog state (tests only).
func ResetLocalMemoryHubForTest() {
	hubMu.Lock()
	defer hubMu.Unlock()
	hubReady = false
	hubBindingStore = nil
	hubUnitWriter = nil
	hubCatalog = hub.Catalog{}
	hubDefaults = hub.Defaults{}
	hubSkillSources = nil
	hubTrustGate = nil
	hubEnableFake = false
}

// ResolveForAgent is the P0 entry for tools/UI to resolve providers (alias of ResolveAgentHub).
func ResolveForAgent(agent hub.AgentHubConfig) (hub.GovernanceProvider, hub.KnowledgeProvider, error) {
	return ResolveAgentHub(agent)
}

// ResolveAgentHub resolves governance/knowledge for an agent override config.
func ResolveAgentHub(agent hub.AgentHubConfig) (hub.GovernanceProvider, hub.KnowledgeProvider, error) {
	hubMu.RLock()
	defer hubMu.RUnlock()
	if !hubReady {
		return nil, nil, hub.ErrNotSupported
	}
	return hub.Resolve(hubCatalog, hubDefaults, agent)
}

// MemoryHubReady reports whether InitLocalMemoryHub has run.
func MemoryHubReady() bool {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hubReady
}

// ListCatalog returns registered provider names and defaults.
func ListCatalog() (CatalogSnapshot, error) {
	hubMu.RLock()
	defer hubMu.RUnlock()
	if !hubReady {
		return CatalogSnapshot{}, hub.ErrNotSupported
	}
	snap := CatalogSnapshot{
		Defaults: CatalogDefaults{
			Governance: hubDefaults.Governance,
			Knowledge:  hubDefaults.Knowledge,
		},
	}
	for name := range hubCatalog.Gov {
		snap.Governance = append(snap.Governance, name)
	}
	for name := range hubCatalog.Know {
		snap.Knowledge = append(snap.Knowledge, name)
	}
	sort.Strings(snap.Governance)
	sort.Strings(snap.Knowledge)
	return snap, nil
}

// AgentHubConfigFromRuntime maps persisted runtime_tools hub_* to hub.AgentHubConfig.
func AgentHubConfigFromRuntime(c biz.RuntimeToolsConfig) hub.AgentHubConfig {
	out := hub.AgentHubConfig{}
	if c.HubGovernance != nil {
		s := strings.TrimSpace(*c.HubGovernance)
		out.Governance = &s
	}
	if c.HubKnowledge != nil {
		s := strings.TrimSpace(*c.HubKnowledge)
		out.Knowledge = &s
	}
	if c.HubFallbackToDefaultOnReadError != nil {
		out.FallbackToDefaultOnReadError = *c.HubFallbackToDefaultOnReadError
	}
	return out
}

// ValidateAgentHub dry-runs Resolve for Create/Update save-time checks (§3.5.1 assembly).
func ValidateAgentHub(c biz.RuntimeToolsConfig) error {
	if !MemoryHubReady() {
		InitLocalMemoryHub()
	}
	_, _, err := ResolveForAgent(AgentHubConfigFromRuntime(c))
	return err
}

// ResolveForRuntimeTools resolves providers from an agent's RuntimeToolsConfig.
func ResolveForRuntimeTools(c biz.RuntimeToolsConfig) (hub.GovernanceProvider, hub.KnowledgeProvider, error) {
	if !MemoryHubReady() {
		InitLocalMemoryHub()
	}
	return ResolveForAgent(AgentHubConfigFromRuntime(c))
}

// HubBindingStore returns the process BindingStore (nil before init).
func HubBindingStore() local.BindingStore {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hubBindingStore
}

// HubTrustGate returns the process SkillTrustGate (nil before init).
func HubTrustGate() *hub.SkillTrustGate {
	hubMu.RLock()
	defer hubMu.RUnlock()
	return hubTrustGate
}

// HubSkillSource returns SkillSource for a hub name.
func HubSkillSource(name string) hub.SkillSource {
	hubMu.RLock()
	defer hubMu.RUnlock()
	if hubSkillSources == nil {
		return nil
	}
	return hubSkillSources[name]
}
