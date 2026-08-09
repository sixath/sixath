package config

import (
	"errors"
	"os"
	"path/filepath"

	yaml "go.yaml.in/yaml/v2"
)

// ToolGuardrails 与 design-memory-tools §6.3 YAML 对齐，可由框架或 Portal 的 agent_extra.yaml 加载。
type ToolGuardrails struct {
	Enabled                   bool     `json:"enabled" yaml:"enabled"`
	WarningsOnly              bool     `json:"warnings_only" yaml:"warnings_only"`
	SameArgsFailureWarn       int      `json:"same_args_failure_warn" yaml:"same_args_failure_warn"`
	SameArgsFailureHalt       int      `json:"same_args_failure_halt" yaml:"same_args_failure_halt"`
	SameToolFailureWarn       int      `json:"same_tool_failure_warn" yaml:"same_tool_failure_warn"`
	SameToolFailureHalt       int      `json:"same_tool_failure_halt" yaml:"same_tool_failure_halt"`
	IdempotentTools           []string `json:"idempotent_tools" yaml:"idempotent_tools"`
	MutatingTools             []string `json:"mutating_tools" yaml:"mutating_tools"`
	IdempotentRelaxMultiplier int      `json:"idempotent_relax_multiplier" yaml:"idempotent_relax_multiplier"`
	// NoProgressToolOnlyWarn R3：连续多轮模型仍选择仅调工具（无最终 assistant 文本）；0 关闭。
	NoProgressToolOnlyWarn int `json:"no_progress_tool_only_warn" yaml:"no_progress_tool_only_warn"`
	// NoProgressToolOnlyHalt R3 硬停阈值；0 表示不因 R3 硬停。
	NoProgressToolOnlyHalt int `json:"no_progress_tool_only_halt" yaml:"no_progress_tool_only_halt"`
}

// PortalGuardrailHaltUI Portal 对 guardrail_halt 的展示策略（与 agent trace 中 sixath.origin 配合）。
type PortalGuardrailHaltUI struct {
	// Display: none | banner | brief | full。默认 banner：用户可见一句说明，不暴露内部 system 注入全文。
	Display string `json:"display" yaml:"display"`
	// PersistSystemMessage 为 true 时将用户可见文案以 assistant 消息落库；false 时仅 HTTP/流式返回不落库。
	PersistSystemMessage bool `json:"persist_system_message" yaml:"persist_system_message"`
}

// MemoryOrchestratorPrefetch Turn 前记忆围栏注入（设计 §4）；与 Portal agent_extra 同文件可选加载。
type MemoryOrchestratorPrefetch struct {
	Enabled            bool   `json:"enabled" yaml:"enabled"`
	PrefetchTimeoutMS  int    `json:"prefetch_timeout_ms" yaml:"prefetch_timeout_ms"`
	PrefetchFailClosed bool   `json:"prefetch_fail_closed" yaml:"prefetch_fail_closed"`
	FenceTag           string `json:"fence_tag" yaml:"fence_tag"`
	// StreamScrub 为 true 时 Portal SSE 在 chunk 上剔除记忆围栏内字节，且 EOF 未闭合时不向 UI/落库泄露缓冲（设计 §4.3、§4.6）。
	StreamScrub bool `json:"stream_scrub" yaml:"stream_scrub"`
	// MaxSnippets 每路 Recall limit；省略或 <=0 → 5（P2-F）。
	MaxSnippets int `json:"max_snippets" yaml:"max_snippets"`
	// MaxTotal 去重后全局条数顶；省略(nil) → 8；显式 <=0 → 不截断（P2-F）。
	MaxTotal *int `json:"max_total" yaml:"max_total"`
}

// MemoryExtractionModel is an optional auxiliary chat model for turn extraction.
type MemoryExtractionModel struct {
	Provider string `json:"provider" yaml:"provider"`
	Model    string `json:"model" yaml:"model"`
	APIKey   string `json:"api_key" yaml:"api_key"`
	BaseURL  string `json:"base_url" yaml:"base_url"`
}

// MemoryExtraction configures optional post-turn LLM fact extraction (P2-C).
type MemoryExtraction struct {
	Enabled         bool                   `json:"enabled" yaml:"enabled"`
	MaxFactsPerTurn int                    `json:"max_facts_per_turn" yaml:"max_facts_per_turn"`
	Auxiliary       *MemoryExtractionModel `json:"auxiliary" yaml:"auxiliary"`
}

// MemoryConflict configures optional semantic conflict resolution on Remember add (P2-D2).
type MemoryConflict struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	K       int  `yaml:"k" json:"k"`
}

// MemoryQdrant configures the Qdrant units vector sidecar (P2-H).
type MemoryQdrant struct {
	URL        string `json:"url" yaml:"url"`
	Collection string `json:"collection" yaml:"collection"`
	APIKey     string `json:"api_key" yaml:"api_key"`
	GRPCPort   int    `json:"grpc_port" yaml:"grpc_port"` // 0 → derive from URL (6333→6334)
}

// MemoryVector configures optional units vector sidecar (P2-E1 sqlite-only hybrid-recall
// sidecar / P2-E sqlite / P2-H qdrant D2-peer + Recall(source=units) sidecar).
type MemoryVector struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Provider string `json:"provider" yaml:"provider"` // none | sqlite | qdrant
	StoreDir string `json:"store_dir" yaml:"store_dir"`
	// Path is the legacy P2-E1 hybrid-recall sqlite sidecar filename override
	// (relative to Portal's data_root; "" → memory_unit_vectors.db).
	Path      string                 `json:"path" yaml:"path"`
	Qdrant    *MemoryQdrant          `json:"qdrant" yaml:"qdrant"`
	Embedding *MemoryExtractionModel `json:"embedding" yaml:"embedding"`
}

// MemoryNeo4j configures the Neo4j graph sidecar (P2-I).
type MemoryNeo4j struct {
	URI      string `json:"uri" yaml:"uri"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	Database string `json:"database" yaml:"database"`
}

// MemoryGraph configures optional units graph sidecar (P2-I).
type MemoryGraph struct {
	Enabled               bool                   `json:"enabled" yaml:"enabled"`
	Provider              string                 `json:"provider" yaml:"provider"` // none | neo4j
	MinRelationConfidence float64                `json:"min_relation_confidence" yaml:"min_relation_confidence"`
	MaxEntities           int                    `json:"max_entities" yaml:"max_entities"`
	MaxHops               int                    `json:"max_hops" yaml:"max_hops"`
	RRFK                  int                    `json:"rrf_k" yaml:"rrf_k"`
	Neo4j                 *MemoryNeo4j           `json:"neo4j" yaml:"neo4j"`
	Auxiliary             *MemoryExtractionModel `json:"auxiliary" yaml:"auxiliary"`
}

// MemoryStoreAgentWorkspace is the agent file-memory write gate under memory_store (P2-G).
type MemoryStoreAgentWorkspace struct {
	WriteEnabled bool `json:"write_enabled" yaml:"write_enabled"`
}

// MemoryProceduralRepair configures hand-written procedural bindings (P3-C) and future auto-commit (P3-E).
type MemoryProceduralRepair struct {
	Enabled        bool                        `json:"enabled" yaml:"enabled"`
	AutoCommit     bool                        `json:"auto_commit" yaml:"auto_commit"` // P3-E; ignore if true in P3-C
	MinSupport     int                         `json:"min_support" yaml:"min_support"`
	MaxProcedural  int                         `json:"max_procedural" yaml:"max_procedural"`
	Mode           string                      `json:"mode" yaml:"mode"` // default suggest
	PilotAgents    []string                    `json:"pilot_agents" yaml:"pilot_agents"`
	Bindings       []MemoryProceduralBindingYAML `json:"bindings" yaml:"bindings"`
	Inject         *MemoryProceduralInject     `json:"inject" yaml:"inject"`
}

// MemoryProceduralBindingYAML is the config shape for one hand-written binding.
type MemoryProceduralBindingYAML struct {
	TriggerCode  string   `json:"trigger_code" yaml:"trigger_code"`
	TriggerQuery string   `json:"trigger_query" yaml:"trigger_query"`
	ActionKind   string   `json:"action_kind" yaml:"action_kind"`
	SkillID      string   `json:"skill_id" yaml:"skill_id"`
	ToolNames    []string `json:"tool_names" yaml:"tool_names"`
	Mode         string   `json:"mode" yaml:"mode"`
	AgentID      string   `json:"agent_id" yaml:"agent_id"`
}

// MemoryProceduralInject toggles dual-channel exposure.
type MemoryProceduralInject struct {
	Prefetch    *bool `json:"prefetch" yaml:"prefetch"`
	SkillRouter *bool `json:"skill_router" yaml:"skill_router"`
}

// MemoryStoreBlock is the nested memory_store shell in agent_extra (P2-G).
// Nested fields override legacy top-level keys when non-nil.
type MemoryStoreBlock struct {
	AgentWorkspace    *MemoryStoreAgentWorkspace  `json:"agent_workspace" yaml:"agent_workspace"`
	Prefetch          *MemoryOrchestratorPrefetch `json:"prefetch" yaml:"prefetch"`
	Extraction        *MemoryExtraction           `json:"extraction" yaml:"extraction"`
	Conflict          *MemoryConflict             `json:"conflict" yaml:"conflict"`
	Vector            *MemoryVector               `json:"vector" yaml:"vector"`
	Graph             *MemoryGraph                `json:"graph" yaml:"graph"`
	ProceduralRepair  *MemoryProceduralRepair     `json:"procedural_repair" yaml:"procedural_repair"`
}

// PortalAgentExtra 可选叠加配置（与 Kratos Bootstrap 分离，避免改 proto 即可迭代）。
type PortalAgentExtra struct {
	ToolGuardrails *ToolGuardrails         `json:"tool_guardrails" yaml:"tool_guardrails"`
	Portal         *PortalAgentExtraPortal `json:"portal" yaml:"portal"`
	// MemoryStore nested shell (P2-G); overrides top-level memory_* when set.
	MemoryStore *MemoryStoreBlock `json:"memory_store" yaml:"memory_store"`
	// MemoryOrchestratorPrefetch 启用时由 Portal 在 ReAct 上挂 memory.Orchestrator（依赖 memory.defaults.enabled 等）。
	// Deprecated: prefer memory_store.prefetch.
	MemoryOrchestratorPrefetch *MemoryOrchestratorPrefetch `json:"memory_orchestrator_prefetch" yaml:"memory_orchestrator_prefetch"`
	// MemoryExtraction 启用时由 Portal 在 assistant 落库后异步 AddFromTurn（默认关）。
	// Deprecated: prefer memory_store.extraction.
	MemoryExtraction *MemoryExtraction `json:"memory_extraction" yaml:"memory_extraction"`
	// MemoryConflict 启用时由 Portal 装配 SemanticConflictResolver（peer Recall limit = K）。
	// Deprecated: prefer memory_store.conflict.
	MemoryConflict *MemoryConflict `json:"memory_conflict" yaml:"memory_conflict"`
	// MemoryVector 启用时为 units 装配向量 Sidecar（sqlite / qdrant；默认关）；
	// 亦用于 hybrid_recall（P2-E1）的 UnitVectorIndex 侧车。
	// Deprecated: prefer memory_store.vector.
	MemoryVector *MemoryVector `json:"memory_vector" yaml:"memory_vector"`
	// MemoryGraph 启用时为 units 装配 Neo4j 图 Sidecar（默认关）。
	// Deprecated: prefer memory_store.graph.
	MemoryGraph *MemoryGraph `json:"memory_graph" yaml:"memory_graph"`
	// MemoryProceduralRepair hand-written bindings (P3-C); prefer memory_store.procedural_repair.
	MemoryProceduralRepair *MemoryProceduralRepair `json:"memory_procedural_repair" yaml:"memory_procedural_repair"`
	// Web 联网搜索（web_search / web_extract）；与 config.yaml 中 web 节同形。
	Web *WebTools `json:"web" yaml:"web"`
}

// PortalAgentExtraPortal Portal 侧 UI 与落库策略。
type PortalAgentExtraPortal struct {
	GuardrailHalt *PortalGuardrailHaltUI `json:"guardrail_halt" yaml:"guardrail_halt"`
}

// NormalizePortalAgentExtra applies memory_store nested overrides onto top-level fields (nested wins).
func NormalizePortalAgentExtra(extra *PortalAgentExtra) {
	if extra == nil || extra.MemoryStore == nil {
		return
	}
	ms := extra.MemoryStore
	if ms.Prefetch != nil {
		extra.MemoryOrchestratorPrefetch = ms.Prefetch
	}
	if ms.Extraction != nil {
		extra.MemoryExtraction = ms.Extraction
	}
	if ms.Conflict != nil {
		extra.MemoryConflict = ms.Conflict
	}
	if ms.Vector != nil {
		extra.MemoryVector = ms.Vector
	}
	if ms.Graph != nil {
		extra.MemoryGraph = ms.Graph
	}
	if ms.ProceduralRepair != nil {
		extra.MemoryProceduralRepair = ms.ProceduralRepair
	}
}

// LoadPortalAgentExtra 从 path 读取 YAML；文件不存在时返回 (nil, nil)。
func LoadPortalAgentExtra(path string) (*PortalAgentExtra, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var extra PortalAgentExtra
	if err := yaml.Unmarshal(data, &extra); err != nil {
		return nil, err
	}
	NormalizePortalAgentExtra(&extra)
	// NOTE: every new PortalAgentExtra field must be added here, or a config
	// containing only that section is silently discarded as (nil, nil).
	if extra.ToolGuardrails == nil && extra.Portal == nil && extra.MemoryStore == nil &&
		extra.MemoryOrchestratorPrefetch == nil &&
		extra.MemoryExtraction == nil && extra.MemoryConflict == nil && extra.MemoryVector == nil &&
		extra.MemoryGraph == nil && extra.MemoryProceduralRepair == nil && extra.Web == nil {
		return nil, nil
	}
	return &extra, nil
}

// ResolvePortalAgentExtraPath 在 dir 下尝试 agent_extra.yaml / agent_extra.yml。
func ResolvePortalAgentExtraPath(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("empty config dir")
	}
	for _, name := range []string{"agent_extra.yaml", "agent_extra.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}
