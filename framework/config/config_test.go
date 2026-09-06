package config

import (
	"os"
	"path/filepath"
	"testing"

	yaml "go.yaml.in/yaml/v2"
)

func TestLoad_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
model: openai/gpt-4o
max_history: 5
middlewares:
  - logging
  - metrics
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.ModelName != "openai/gpt-4o" {
		t.Fatalf("expected model openai/gpt-4o, got %s", cfg.ModelName)
	}
	if cfg.MaxHistory != 5 {
		t.Fatalf("expected max_history 5, got %d", cfg.MaxHistory)
	}
	if len(cfg.Middlewares) != 2 || cfg.Middlewares[0] != "logging" {
		t.Fatalf("unexpected middlewares: %#v", cfg.Middlewares)
	}
}

func TestLoad_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"model":"openai/gpt-3.5-turbo","max_history":8}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.ModelName != "openai/gpt-3.5-turbo" || cfg.MaxHistory != 8 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := Config{ModelName: "openai/gpt-4", MaxHistory: 5}
	os.Setenv("OPENAI_MODEL", "openai/gpt-3.5-turbo")
	os.Setenv("AGENT_MAX_HISTORY", "20")
	os.Setenv("AGENT_WORKSPACE", "/ws/cli")
	defer func() {
		os.Unsetenv("OPENAI_MODEL")
		os.Unsetenv("AGENT_MAX_HISTORY")
		os.Unsetenv("AGENT_WORKSPACE")
	}()
	ApplyEnvOverrides(&cfg)
	if cfg.ModelName != "openai/gpt-3.5-turbo" || cfg.MaxHistory != 20 || cfg.Workspace != "/ws/cli" {
		t.Fatalf("expected overrides applied: %#v", cfg)
	}
}

func TestLoadWithEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("model: openai/gpt-4\nmax_history: 3"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("AGENT_MAX_HISTORY", "7")
	defer os.Unsetenv("AGENT_MAX_HISTORY")
	cfg, err := LoadWithEnv(path)
	if err != nil {
		t.Fatalf("LoadWithEnv: %v", err)
	}
	if cfg.MaxHistory != 7 {
		t.Fatalf("expected env override max_history 7, got %d", cfg.MaxHistory)
	}
}

func TestLoadForEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.dev.yaml")
	if err := os.WriteFile(path, []byte("model: openai/gpt-4o\nmax_history: 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForEnv("dev", dir)
	if err != nil {
		t.Fatalf("LoadForEnv: %v", err)
	}
	if cfg.ModelName != "openai/gpt-4o" || cfg.MaxHistory != 2 {
		t.Fatalf("unexpected: %#v", cfg)
	}
}

// TestLoad_Skills 验证 Skills 配置可正确解析（任务 1 验收）。
func TestLoad_Skills(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
model: openai/gpt-4o
max_history: 10
skills:
  skills_dirs: [skills, skills.d]
  enabled_skills: [a, b]
  disabled_skills: [c]
  allow_script_execution: true
  script_allowed_extensions: [.sh, .py]
  script_timeout_seconds: 60
  mcp_servers:
    - id: mcp-k8s
      transport: http
      endpoint: http://localhost:8080/mcp
      backend: metoro
    - id: mcp-local
      transport: stdio
      command: node
      args: [server.js, --stdio]
      env:
        NODE_ENV: test
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Skills.Dirs) != 2 || cfg.Skills.Dirs[0] != "skills" {
		t.Fatalf("unexpected skills_dirs: %v", cfg.Skills.Dirs)
	}
	if len(cfg.Skills.EnabledSkills) != 2 || cfg.Skills.EnabledSkills[0] != "a" {
		t.Fatalf("unexpected enabled_skills: %v", cfg.Skills.EnabledSkills)
	}
	if len(cfg.Skills.DisabledSkills) != 1 || cfg.Skills.DisabledSkills[0] != "c" {
		t.Fatalf("unexpected disabled_skills: %v", cfg.Skills.DisabledSkills)
	}
	if !cfg.Skills.AllowScriptExecution {
		t.Fatal("expected allow_script_execution true")
	}
	if len(cfg.Skills.ScriptAllowedExtensions) != 2 || cfg.Skills.ScriptAllowedExtensions[0] != ".sh" {
		t.Fatalf("unexpected script_allowed_extensions: %v", cfg.Skills.ScriptAllowedExtensions)
	}
	if cfg.Skills.ScriptTimeoutSeconds != 60 {
		t.Fatalf("expected script_timeout_seconds 60, got %d", cfg.Skills.ScriptTimeoutSeconds)
	}
	if len(cfg.Skills.MCPServers) != 2 || cfg.Skills.MCPServers[0].ID != "mcp-k8s" {
		t.Fatalf("unexpected mcp_servers: %v", cfg.Skills.MCPServers)
	}
	if cfg.Skills.MCPServers[0].Transport != "http" || cfg.Skills.MCPServers[0].Endpoint != "http://localhost:8080/mcp" {
		t.Fatalf("unexpected http mcp_server: %+v", cfg.Skills.MCPServers[0])
	}
	stdio := cfg.Skills.MCPServers[1]
	if stdio.ID != "mcp-local" || stdio.Transport != "stdio" || stdio.Command != "node" {
		t.Fatalf("unexpected stdio mcp_server: %+v", stdio)
	}
	if len(stdio.Args) != 2 || stdio.Args[0] != "server.js" {
		t.Fatalf("unexpected stdio args: %v", stdio.Args)
	}
	if stdio.Env["NODE_ENV"] != "test" {
		t.Fatalf("unexpected stdio env: %v", stdio.Env)
	}
}

// TestLoad_SkillsEmpty 未配置 Skills 时零值不影响（任务 1 验收）。
func TestLoad_SkillsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("model: openai/gpt-4o\nmax_history: 5"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Skills.Dirs != nil || cfg.Skills.EnabledSkills != nil || cfg.Skills.AllowScriptExecution != false {
		t.Fatalf("expected zero value skills when not configured: %#v", cfg.Skills)
	}
}

func TestLoad_ToolGuardrails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
model: openai/gpt-4o
max_history: 10
tool_guardrails:
  enabled: true
  warnings_only: true
  same_args_failure_warn: 2
  idempotent_tools: [memory_search]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ToolGuardrails == nil || !cfg.ToolGuardrails.Enabled {
		t.Fatalf("expected tool_guardrails: %#v", cfg.ToolGuardrails)
	}
	if len(cfg.ToolGuardrails.IdempotentTools) != 1 || cfg.ToolGuardrails.IdempotentTools[0] != "memory_search" {
		t.Fatalf("unexpected idempotent_tools: %#v", cfg.ToolGuardrails.IdempotentTools)
	}
}

func TestLoadPortalAgentExtra(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
tool_guardrails:
  enabled: true
  warnings_only: true
  same_args_failure_warn: 2
portal:
  guardrail_halt:
    display: brief
    persist_system_message: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if !extra.ToolGuardrails.Enabled {
		t.Fatal("expected enabled")
	}
	if extra.Portal == nil || extra.Portal.GuardrailHalt == nil {
		t.Fatal("expected portal.guardrail_halt")
	}
	if extra.Portal.GuardrailHalt.Display != "brief" || !extra.Portal.GuardrailHalt.PersistSystemMessage {
		t.Fatalf("unexpected portal guardrail_halt: %#v", extra.Portal.GuardrailHalt)
	}
}

func TestLoadPortalAgentExtra_MemoryPrefetchOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_orchestrator_prefetch:
  enabled: true
  prefetch_timeout_ms: 1500
  prefetch_fail_closed: false
  max_snippets: 4
  max_total: 6
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryOrchestratorPrefetch == nil || !extra.MemoryOrchestratorPrefetch.Enabled {
		t.Fatalf("expected memory_orchestrator_prefetch: %#v", extra.MemoryOrchestratorPrefetch)
	}
	if extra.MemoryOrchestratorPrefetch.PrefetchTimeoutMS != 1500 {
		t.Fatalf("timeout: %d", extra.MemoryOrchestratorPrefetch.PrefetchTimeoutMS)
	}
	if extra.MemoryOrchestratorPrefetch.MaxSnippets != 4 {
		t.Fatalf("max_snippets: %d", extra.MemoryOrchestratorPrefetch.MaxSnippets)
	}
	if extra.MemoryOrchestratorPrefetch.MaxTotal == nil || *extra.MemoryOrchestratorPrefetch.MaxTotal != 6 {
		t.Fatalf("max_total: %#v", extra.MemoryOrchestratorPrefetch.MaxTotal)
	}
}

func TestLoadPortalAgentExtra_MemoryExtractionOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_extraction:
  enabled: true
  max_facts_per_turn: 3
  auxiliary:
    provider: openai
    model: gpt-4o-mini
    api_key: sk-test
    base_url: https://example.com/v1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryExtraction == nil || !extra.MemoryExtraction.Enabled {
		t.Fatalf("expected memory_extraction: %#v", extra.MemoryExtraction)
	}
	if extra.MemoryExtraction.MaxFactsPerTurn != 3 {
		t.Fatalf("max_facts: %d", extra.MemoryExtraction.MaxFactsPerTurn)
	}
	aux := extra.MemoryExtraction.Auxiliary
	if aux == nil || aux.Provider != "openai" || aux.Model != "gpt-4o-mini" || aux.APIKey != "sk-test" {
		t.Fatalf("auxiliary: %#v", aux)
	}
}

func TestLoadPortalAgentExtra_StreamScrubYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_orchestrator_prefetch:
  enabled: false
  stream_scrub: true
  fence_tag: "sixath-memory-context"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryOrchestratorPrefetch == nil || !extra.MemoryOrchestratorPrefetch.StreamScrub {
		t.Fatalf("expected stream_scrub: %#v", extra.MemoryOrchestratorPrefetch)
	}
}

func TestLoadPortalAgentExtra_MemoryConflictOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_conflict:
  enabled: true
  k: 12
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryConflict == nil || !extra.MemoryConflict.Enabled {
		t.Fatalf("expected memory_conflict: %#v", extra.MemoryConflict)
	}
	if extra.MemoryConflict.K != 12 {
		t.Fatalf("k: %d", extra.MemoryConflict.K)
	}
}

func TestLoadPortalAgentExtra_MemoryVectorOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_vector:
  enabled: true
  provider: sqlite
  store_dir: data/mem_vec
  embedding:
    provider: openai
    model: text-embedding-3-small
    api_key: sk-emb
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryVector == nil || !extra.MemoryVector.Enabled {
		t.Fatalf("expected memory_vector: %#v", extra.MemoryVector)
	}
	if extra.MemoryVector.Provider != "sqlite" || extra.MemoryVector.StoreDir != "data/mem_vec" {
		t.Fatalf("vector cfg: %#v", extra.MemoryVector)
	}
	emb := extra.MemoryVector.Embedding
	if emb == nil || emb.Model != "text-embedding-3-small" || emb.APIKey != "sk-emb" {
		t.Fatalf("embedding: %#v", emb)
	}
}

// TestLoadPortalAgentExtra_MemoryVectorLegacyPath covers the P2-E1 hybrid-recall
// sqlite sidecar's legacy provider/path shape (superseded by provider/store_dir
// but still parsed for backward compatibility).
func TestLoadPortalAgentExtra_MemoryVectorLegacyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_vector:
  provider: none
  path: custom.db
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryVector == nil {
		t.Fatalf("expected memory_vector: %#v", extra.MemoryVector)
	}
	if extra.MemoryVector.Provider != "none" {
		t.Fatalf("provider: %q", extra.MemoryVector.Provider)
	}
	if extra.MemoryVector.Path != "custom.db" {
		t.Fatalf("path: %q", extra.MemoryVector.Path)
	}
}

func TestLoadPortalAgentExtra_MemoryVectorQdrant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_store:
  vector:
    enabled: true
    provider: qdrant
    qdrant:
      url: http://localhost:6333
      collection: sixath_memory_units
      api_key: secret
      grpc_port: 6334
    embedding:
      provider: openai
      model: text-embedding-3-small
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	v := extra.MemoryVector
	if v == nil || !v.Enabled || v.Provider != "qdrant" || v.Qdrant == nil {
		t.Fatalf("vector: %#v", v)
	}
	if v.Qdrant.URL != "http://localhost:6333" || v.Qdrant.Collection != "sixath_memory_units" {
		t.Fatalf("qdrant: %#v", v.Qdrant)
	}
	if v.Qdrant.APIKey != "secret" || v.Qdrant.GRPCPort != 6334 {
		t.Fatalf("qdrant auth/port: %#v", v.Qdrant)
	}
}

func TestLoadPortalAgentExtra_MemoryGraph(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_store:
  graph:
    enabled: true
    provider: neo4j
    min_relation_confidence: 0.8
    max_hops: 2
    rrf_k: 40
    neo4j:
      uri: bolt://localhost:7687
      username: neo4j
      password: secret
      database: sixath
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	g := extra.MemoryGraph
	if g == nil || !g.Enabled || g.Provider != "neo4j" || g.Neo4j == nil {
		t.Fatalf("graph: %#v", g)
	}
	if g.Neo4j.URI != "bolt://localhost:7687" || g.MinRelationConfidence != 0.8 || g.MaxHops != 2 || g.RRFK != 40 {
		t.Fatalf("graph fields: %#v", g)
	}
}

func TestLoadPortalAgentExtra_MemoryStoreNestedWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_orchestrator_prefetch:
  enabled: false
  max_snippets: 1
memory_extraction:
  enabled: false
memory_store:
  agent_workspace:
    write_enabled: true
  prefetch:
    enabled: true
    max_snippets: 7
    max_total: 9
  extraction:
    enabled: true
    max_facts_per_turn: 2
  conflict:
    enabled: true
    k: 4
  vector:
    enabled: true
    provider: sqlite
    store_dir: data/v
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryStore == nil || extra.MemoryStore.AgentWorkspace == nil || !extra.MemoryStore.AgentWorkspace.WriteEnabled {
		t.Fatalf("agent_workspace: %#v", extra.MemoryStore)
	}
	if extra.MemoryOrchestratorPrefetch == nil || !extra.MemoryOrchestratorPrefetch.Enabled {
		t.Fatalf("prefetch should win from memory_store: %#v", extra.MemoryOrchestratorPrefetch)
	}
	if extra.MemoryOrchestratorPrefetch.MaxSnippets != 7 {
		t.Fatalf("max_snippets=%d", extra.MemoryOrchestratorPrefetch.MaxSnippets)
	}
	if extra.MemoryOrchestratorPrefetch.MaxTotal == nil || *extra.MemoryOrchestratorPrefetch.MaxTotal != 9 {
		t.Fatalf("max_total=%v", extra.MemoryOrchestratorPrefetch.MaxTotal)
	}
	if extra.MemoryExtraction == nil || !extra.MemoryExtraction.Enabled || extra.MemoryExtraction.MaxFactsPerTurn != 2 {
		t.Fatalf("extraction: %#v", extra.MemoryExtraction)
	}
	if extra.MemoryConflict == nil || !extra.MemoryConflict.Enabled || extra.MemoryConflict.K != 4 {
		t.Fatalf("conflict: %#v", extra.MemoryConflict)
	}
	if extra.MemoryVector == nil || !extra.MemoryVector.Enabled || extra.MemoryVector.StoreDir != "data/v" {
		t.Fatalf("vector: %#v", extra.MemoryVector)
	}
}

func TestLoadPortalAgentExtra_MemoryStoreOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent_extra.yaml")
	content := `
memory_store:
  prefetch:
    enabled: true
    prefetch_timeout_ms: 900
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil || extra == nil {
		t.Fatalf("LoadPortalAgentExtra: %v %#v", err, extra)
	}
	if extra.MemoryOrchestratorPrefetch == nil || !extra.MemoryOrchestratorPrefetch.Enabled {
		t.Fatalf("expected nested prefetch promoted: %#v", extra.MemoryOrchestratorPrefetch)
	}
	if extra.MemoryOrchestratorPrefetch.PrefetchTimeoutMS != 900 {
		t.Fatalf("timeout=%d", extra.MemoryOrchestratorPrefetch.PrefetchTimeoutMS)
	}
}

func TestRCAConfig_YAML(t *testing.T) {
	yml := []byte(`
model: openai/gpt-4o
workspace: /ws/agent
rca:
  jaeger:
    query_url: http://jaeger:16686
  es:
    datasource_id: es-logs
    default_index: app-logs-*
    trace_id_field: trace_id
  repos:
    roots:
      - /repos/service-a
      - /repos/service-b
`)
	var cfg Config
	if err := yaml.Unmarshal(yml, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Workspace != "/ws/agent" {
		t.Fatalf("workspace = %q", cfg.Workspace)
	}
	if cfg.RCA.Jaeger.QueryURL != "http://jaeger:16686" {
		t.Fatalf("jaeger url = %q", cfg.RCA.Jaeger.QueryURL)
	}
	if cfg.RCA.ES.DatasourceID != "es-logs" || cfg.RCA.ES.DefaultIndex != "app-logs-*" || cfg.RCA.ES.TraceIDField != "trace_id" {
		t.Fatalf("es cfg = %+v", cfg.RCA.ES)
	}
	if len(cfg.RCA.Repos.Roots) != 2 || cfg.RCA.Repos.Roots[0] != "/repos/service-a" {
		t.Fatalf("repos = %+v", cfg.RCA.Repos)
	}
}
