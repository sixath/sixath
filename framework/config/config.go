package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sixath/framework/datasource"
	yaml "go.yaml.in/yaml/v2"
)

// MCPServerEntry 描述一个供 Skills 使用的 MCP 服务，与 tool.McpConfig 对齐。
type MCPServerEntry struct {
	Transport string            `json:"transport" yaml:"transport"` // ""|"http"|"stdio"
	Endpoint  string            `json:"endpoint" yaml:"endpoint"`
	ID        string            `json:"id" yaml:"id"`
	Backend   string            `json:"backend" yaml:"backend"`
	Command   string            `json:"command" yaml:"command"`
	Args      []string          `json:"args" yaml:"args"`
	Env       map[string]string `json:"env" yaml:"env"`
}

// SkillsConfig 定义 Skills 子系统相关配置。
type SkillsConfig struct {
	// Dirs 指定 Skills 搜索目录列表，例如 ["skills", "skills.d"]。
	Dirs []string `json:"skills_dirs" yaml:"skills_dirs"`
	// EnabledSkills 为可选白名单，若非空则只启用列表中的技能。
	EnabledSkills []string `json:"enabled_skills" yaml:"enabled_skills"`
	// DisabledSkills 为可选黑名单，可用于临时关闭部分技能。
	DisabledSkills []string `json:"disabled_skills" yaml:"disabled_skills"`
	// EnforceWriteAllowedFromSkills 控制是否仅在存在声明 execute_write 的 Skill 时才注册写/改工具。
	EnforceWriteAllowedFromSkills bool `json:"enforce_write_allowed_from_skills" yaml:"enforce_write_allowed_from_skills"`
	// MCPServers 为可选的 MCP 服务列表；当 Skill 声明 mcp_servers 时，将据此创建客户端并把工具注册到 Agent 上下文。
	MCPServers []MCPServerEntry `json:"mcp_servers" yaml:"mcp_servers"`
	// AllowScriptExecution 是否允许执行 Skill 目录下脚本（scripts/）；默认 false，仅读取不执行。
	AllowScriptExecution bool `json:"allow_script_execution" yaml:"allow_script_execution"`
	// ScriptAllowedExtensions 允许执行的脚本扩展名白名单，未配置时默认仅 [".sh"]（见需求 11.3.3、任务 15.1）。
	ScriptAllowedExtensions []string `json:"script_allowed_extensions" yaml:"script_allowed_extensions"`
	// ScriptTimeoutSeconds 单次脚本执行最大耗时（秒），未配置或 <=0 时默认 30；可设上限防止配置错误（见 11.3.9）。
	ScriptTimeoutSeconds int `json:"script_timeout_seconds" yaml:"script_timeout_seconds"`
}

// HyperToolConfig 控制 HyperTool meta-tool（论文 HyperTool 最小原型，见 docs/superpowers/specs/2026-06-12-hypertool-prototype-design.md）。
type HyperToolConfig struct {
	// Enabled 为 true 时注册 hypertool 工具；默认 false（动态 Python 执行需显式开启）。
	Enabled bool `json:"enabled" yaml:"enabled"`
	// TimeoutSeconds 单个代码块超时（秒）；<=0 时默认 30。
	TimeoutSeconds int `json:"timeout_seconds" yaml:"timeout_seconds"`
	// MaxInternalCalls 块内 call_tool 上限；<=0 时默认 20。
	MaxInternalCalls int `json:"max_internal_calls" yaml:"max_internal_calls"`
	// PythonCommand Python 解释器；空时默认 python。
	PythonCommand string `json:"python_command" yaml:"python_command"`
	// BlockedTools 块内禁止调用的工具名；空时使用内置默认黑名单。
	BlockedTools []string `json:"blocked_tools" yaml:"blocked_tools"`
}

// Config 保存框架在 V0.2 阶段的核心配置。
type Config struct {
	// 默认模型标识，如 "openai/gpt-4o"。
	ModelName string `json:"model" yaml:"model"`
	// 会话短期记忆窗口大小。
	MaxHistory int `json:"max_history" yaml:"max_history"`
	// 启用的中间件名称列表，例如 ["logging","metrics","tracing","cache"]。
	Middlewares []string `json:"middlewares" yaml:"middlewares"`

	// DataSources 定义可用于数据查询 Agent 的数据源列表（可为空）。
	DataSources []datasource.Config `json:"data_sources" yaml:"data_sources"`
	// DefaultDatasourceID 为数据查询 Agent 默认使用的数据源 ID（可被请求覆盖）。
	DefaultDatasourceID string `json:"default_datasource_id" yaml:"default_datasource_id"`
	// DataAllowWrite 控制数据查询 Agent 是否允许写/改；为 false 时仅启用只读工具。
	DataAllowWrite bool `json:"data_allow_write" yaml:"data_allow_write"`

	// Skills 为 Skills 子系统的全局配置。
	Skills SkillsConfig `json:"skills" yaml:"skills"`

	// Memory 为工作区记忆（Memory Search）子系统的配置。
	Memory MemoryConfig `json:"memory" yaml:"memory"`

	// SessionSearch 为跨会话 FTS 检索（R1 session_search 工具）配置。
	SessionSearch SessionSearchConfig `json:"session_search" yaml:"session_search"`

	// ToolGuardrails 可选；与 Portal agent_extra 中 tool_guardrails 节同形（设计 §6.3）。
	ToolGuardrails *ToolGuardrails `json:"tool_guardrails" yaml:"tool_guardrails"`

	// ContextCompression 可选；L2 与 token 粗估参数（设计 §5）；auxiliary 模型实例由上层根据 L2AuxiliaryModel 字段装配。
	ContextCompression ContextCompression `json:"context_compression" yaml:"context_compression"`

	// HyperTool 可选；启用可执行代码块 meta-tool，将确定性工具子流程折叠为单次外层调用。
	HyperTool HyperToolConfig `json:"hypertool" yaml:"hypertool"`

	// RCA 可选;线上根因分析工具链(Jaeger + ELK + 多仓库代码检索)。
	RCA RCAConfig `json:"rca" yaml:"rca"`
}

// RCAConfig 配置线上问题排查(RCA)工具链。各子节缺省时对应工具不注册。
type RCAConfig struct {
	Jaeger RCAJaegerConfig `json:"jaeger" yaml:"jaeger"`
	ES     RCAESConfig     `json:"es" yaml:"es"`
	Repos  RCAReposConfig  `json:"repos" yaml:"repos"`
}

// RCAJaegerConfig Jaeger Query 无鉴权访问配置。
type RCAJaegerConfig struct {
	QueryURL string `json:"query_url" yaml:"query_url"`
}

// RCAESConfig ELK 日志查询配置;复用已注册的 ES datasource(密钥走 datasource 的 env)。
type RCAESConfig struct {
	DatasourceID string `json:"datasource_id" yaml:"datasource_id"`
	DefaultIndex string `json:"default_index" yaml:"default_index"`
	TraceIDField string `json:"trace_id_field" yaml:"trace_id_field"`
}

// RCAReposConfig 多仓库代码检索的仓库根白名单。
type RCAReposConfig struct {
	Roots []string `json:"roots" yaml:"roots"`
}

// MemoryConfig 工作区记忆配置。
type MemoryConfig struct {
	Backend  string             `json:"backend" yaml:"backend"` // "builtin" | "qmd"
	Defaults MemorySearchConfig `json:"defaults" yaml:"defaults"`
	Qmd      *QmdConfig         `json:"qmd" yaml:"qmd"` // backend 为 qmd 时
}

// QmdConfig QMD 后端配置（Phase 3）。
type QmdConfig struct {
	Command string `json:"command" yaml:"command"`
}

// MemorySearchConfig 工作区记忆检索配置。
type MemorySearchConfig struct {
	Enabled    bool                 `json:"enabled" yaml:"enabled"`
	Sources    []string             `json:"sources" yaml:"sources"` // ["memory"] 或 ["memory","sessions"]
	ExtraPaths []string             `json:"extra_paths" yaml:"extra_paths"`
	Provider   string               `json:"provider" yaml:"provider"` // "openai" | "ollama" | ""
	Model      string               `json:"model" yaml:"model"`
	Store      MemoryStoreConfig    `json:"store" yaml:"store"`
	Chunking   MemoryChunkingConfig `json:"chunking" yaml:"chunking"`
	Sync       MemorySyncConfig     `json:"sync" yaml:"sync"`
	Query      MemoryQueryConfig    `json:"query" yaml:"query"`
	Cache      MemoryCacheConfig    `json:"cache" yaml:"cache"`
}

// MemoryStoreConfig 存储配置。
type MemoryStoreConfig struct {
	Path string `json:"path" yaml:"path"` // SQLite 索引文件路径
}

// MemoryChunkingConfig 分块配置。
type MemoryChunkingConfig struct {
	Tokens  int `json:"tokens" yaml:"tokens"`   // 每块约 token 数，默认 512
	Overlap int `json:"overlap" yaml:"overlap"` // 块间重叠，默认 64
}

// MemorySyncConfig 同步触发配置。
type MemorySyncConfig struct {
	OnSearch        bool                  `json:"on_search" yaml:"on_search"`
	Watch           bool                  `json:"watch" yaml:"watch"`
	WatchDebounceMs int                   `json:"watch_debounce_ms" yaml:"watch_debounce_ms"`
	IntervalMinutes int                   `json:"interval_minutes" yaml:"interval_minutes"`
	Sessions        *MemorySessionsConfig `json:"sessions" yaml:"sessions"` // Phase 2: 会话源增量触发
}

// MemorySessionsConfig 会话源同步配置（Phase 2）。
type MemorySessionsConfig struct {
	DeltaBytes    int `json:"delta_bytes" yaml:"delta_bytes"`       // 累积字节超过此值触发 sync
	DeltaMessages int `json:"delta_messages" yaml:"delta_messages"` // 累积消息数超过此值触发 sync
}

// MemoryQueryConfig 检索配置。
type MemoryQueryConfig struct {
	MaxResults int                `json:"max_results" yaml:"max_results"`
	MinScore   float64            `json:"min_score" yaml:"min_score"`
	TimeoutSec int                `json:"timeout_sec" yaml:"timeout_sec"`
	Hybrid     MemoryHybridConfig `json:"hybrid" yaml:"hybrid"`
}

// MemoryHybridConfig 混合检索配置。
type MemoryHybridConfig struct {
	Enabled             bool    `json:"enabled" yaml:"enabled"`
	VectorWeight        float64 `json:"vector_weight" yaml:"vector_weight"`
	TextWeight          float64 `json:"text_weight" yaml:"text_weight"`
	CandidateMultiplier int     `json:"candidate_multiplier" yaml:"candidate_multiplier"`
}

// MemoryCacheConfig embedding 缓存配置。
type MemoryCacheConfig struct {
	Enabled    bool `json:"enabled" yaml:"enabled"`
	MaxEntries int  `json:"max_entries" yaml:"max_entries"`
}

// SessionSearchConfig 跨会话 FTS（R1）配置。
type SessionSearchConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	StoreDir string `json:"store_dir" yaml:"store_dir"` // 每 agent 一个 {agent_id}.db
}

// FromEnv 从环境变量加载核心配置。
// 目前支持：
// - OPENAI_MODEL: 默认模型名称
// - AGENT_MAX_HISTORY: 会话记忆窗口大小
func FromEnv() Config {
	cfg := Config{
		ModelName:   os.Getenv("OPENAI_MODEL"),
		MaxHistory:  10,
		Middlewares: []string{},
	}
	if v := os.Getenv("AGENT_MAX_HISTORY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxHistory = n
		}
	}
	return cfg
}

// Load 从给定路径加载配置文件，支持 YAML（.yaml/.yml）与 JSON。
// 若文件不存在或解析失败，将返回错误。
func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	default:
		return cfg, errors.New("unsupported config file extension: " + ext)
	}

	// 合理默认值补齐。
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 10
	}
	return cfg, nil
}

// ApplyEnvOverrides 用环境变量覆盖 cfg 中对应字段（未设置的环境变量不覆盖）。
// 用于在 Load 之后叠加环境变量，实现 B.2.1 环境变量覆盖。
func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("OPENAI_MODEL"); v != "" {
		cfg.ModelName = v
	}
	if v := os.Getenv("AGENT_MAX_HISTORY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxHistory = n
		}
	}
	// 可选：AGENT_MIDDLEWARES 逗号分隔，覆盖中间件列表
	if v := os.Getenv("AGENT_MIDDLEWARES"); v != "" {
		var list []string
		for _, s := range splitAndTrim(v, ",") {
			if s != "" {
				list = append(list, s)
			}
		}
		if len(list) > 0 {
			cfg.Middlewares = list
		}
	}
	if v := os.Getenv("DEFAULT_DATASOURCE_ID"); v != "" {
		cfg.DefaultDatasourceID = v
	}
	if v := os.Getenv("DATA_ALLOW_WRITE"); v != "" {
		lower := strings.ToLower(strings.TrimSpace(v))
		cfg.DataAllowWrite = lower == "1" || lower == "true" || lower == "yes"
	}
}

func splitAndTrim(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// LoadWithEnv 加载配置文件并用环境变量覆盖部分字段（B.2.1）。
func LoadWithEnv(path string) (Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return cfg, err
	}
	ApplyEnvOverrides(&cfg)
	return cfg, nil
}

// LoadForEnv 按环境名加载多配置文件（B.2.2）。
// 若 env 为空则使用 "dev"。路径规则：dir 为目录时加载 dir/config.<env>.<ext>，否则将 dir 视为前缀，加载 <dir>.<env>.<ext>。
// 例如 LoadForEnv("dev", "config") 尝试 config.dev.yaml / config.dev.yml / config.dev.json。
func LoadForEnv(env, dir string) (Config, error) {
	if env == "" {
		env = "dev"
	}
	exts := []string{".yaml", ".yml", ".json"}
	for _, ext := range exts {
		path := filepath.Join(dir, "config."+env+ext)
		cfg, err := Load(path)
		if err == nil {
			ApplyEnvOverrides(&cfg)
			return cfg, nil
		}
		if !os.IsNotExist(err) {
			return cfg, err
		}
	}
	return Config{}, errors.New("no config file found for env: " + env)
}
