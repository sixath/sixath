package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/metadata"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/templates"
	"github.com/sixath/framework/tool"
	tooldata "github.com/sixath/framework/tool/data"
	toolskill "github.com/sixath/framework/tool/skillops"
	"google.golang.org/protobuf/types/known/structpb"
)

// DefaultMemoryConfig 默认记忆配置，可由 main 在加载 conf 后设置。
var DefaultMemoryConfig = config.MemoryConfig{
	Backend: "builtin",
	Defaults: config.MemorySearchConfig{
		Enabled: true,
		Sources: []string{"memory", "sessions"},
		Store:   config.MemoryStoreConfig{Path: ""},
		Sync: config.MemorySyncConfig{
			Sessions: &config.MemorySessionsConfig{
				DeltaBytes:    4096,
				DeltaMessages: 5,
			},
		},
	},
}

// BuildModel 根据 Agent 的 ModelConfig 创建模型实例
func BuildModel(provider, modelName, apiKey, baseURL string) (model.Model, error) {
	return model.NewModelFromConfig(model.ModelConfig{
		Provider: provider,
		Model:    modelName,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	})
}

// RegistryBuildResult BuildRegistry 的返回信息。
type RegistryBuildResult struct {
	McpServers       []toolskill.McpServerEntry
	DatasourcePrompt string
	DsBindings       []DatasourceBinding
}

// RegistryBuildOptions optional surface filter for BuildRegistry.
type RegistryBuildOptions struct {
	// ActiveFamilies nil => no filtering (legacy full bind).
	ActiveFamilies map[string]struct{}
	// Workspace is the agent writable root; rca_* uses workspace/code when present.
	Workspace string
}

// BuildRegistry 根据 Agent 绑定的工具与 MCP Server 列表构建 tool.Registry。
// tools 来自 ToolRepo.ListByAgent；servers 来自 McpServerRepo.ListByAgent。
func BuildRegistry(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, reg *tool.Registry, opts ...RegistryBuildOptions) (*RegistryBuildResult, error) {
	var o RegistryBuildOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	tools = filterToolsForSurface(tools, o.ActiveFamilies)
	servers = filterServersForSurface(servers, o.ActiveFamilies)

	reg.SetEventBus(events.DefaultBus())

	var mcpServers []toolskill.McpServerEntry
	var datasourceConfigs []datasource.Config
	var dsBindings []DatasourceBinding

	for _, t := range tools {
		cfg := toolConfigToMap(t.Config)
		switch t.Type {
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(cfg)
			if mc != nil {
				tool.RegisterMcpTool(reg, mc)
				mcpServers = append(mcpServers, mcpEntryFromConfig(mc))
			}
		case biz.ToolTypeBuiltin:
			registerBuiltinTool(reg, cfg)
		case biz.ToolTypeDatasource:
			dsMap := cfg
			if nested, ok := cfg["datasource"].(map[string]interface{}); ok {
				dsMap = nested
			}
			dsCfg := datasource.ConfigFromMap(dsMap)
			if dsCfg.Type == "" {
				return nil, fmt.Errorf("数据源工具 %q 配置缺少 type", t.Name)
			}
			dsCfg = canonicalDatasourceConfig(t.Name, dsCfg)
			datasourceConfigs = append(datasourceConfigs, dsCfg)
			b := bindingFromConfig(t.Name, dsCfg, nil)
			// purpose / default_index live on the tool config map, not datasource.Config.
			b.DefaultIndex = mapStringField(dsMap, "default_index", "defaultIndex")
			b.Purpose = mapStringField(dsMap, "purpose")
			dsBindings = append(dsBindings, b)
		case biz.ToolTypeRCA:
			registerRCATool(reg, cfg, o.Workspace)
		}
	}

	for _, s := range servers {
		mc := biz.McpServerToConfig(s)
		if mc == nil {
			continue
		}
		tool.RegisterMcpTool(reg, mc)
		if !reg.HasMcpServer(mc.Id) {
			return nil, fmt.Errorf("mcp server %q failed to register (check command/endpoint)", mc.Id)
		}
		mcpServers = append(mcpServers, mcpEntryFromConfig(mc))
	}

	var dsPrompt string
	if len(datasourceConfigs) > 0 {
		registered, prompt, err := registerDatasourceTools(reg, datasourceConfigs, dsBindings)
		if err != nil {
			return nil, err
		}
		dsBindings = registered
		dsPrompt = prompt
	}

	registerESLogFromAgentTools(reg, tools)

	return &RegistryBuildResult{McpServers: mcpServers, DatasourcePrompt: dsPrompt, DsBindings: dsBindings}, nil
}

func filterServersForSurface(servers []*biz.McpServerMeta, active map[string]struct{}) []*biz.McpServerMeta {
	if active == nil {
		return servers
	}
	var out []*biz.McpServerMeta
	for _, s := range servers {
		if s == nil {
			continue
		}
		if FamilyActive(active, MCPFamilyID(s.ID)) {
			out = append(out, s)
		}
	}
	return out
}

func filterToolsForSurface(tools []*biz.ToolMeta, active map[string]struct{}) []*biz.ToolMeta {
	if active == nil {
		return tools
	}
	var out []*biz.ToolMeta
	for _, t := range tools {
		if t == nil {
			continue
		}
		switch t.Type {
		case biz.ToolTypeRCA:
			if FamilyActive(active, familyForRCATool(t)) {
				out = append(out, t)
			}
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(toolConfigToMap(t.Config))
			fid := LegacyMCPFamilyID(t.Name)
			if mc != nil && mc.Id != "" {
				fid = MCPFamilyID(mc.Id)
			}
			if FamilyActive(active, fid) {
				out = append(out, t)
			}
		default:
			fam := FamilyCore
			if t.Type == biz.ToolTypeDatasource {
				if isElasticsearchType(datasourceTypeFromMeta(t)) {
					fam = FamilyRCA
				} else if ToolFamilySplitEnabled() {
					fam = FamilyData
				}
			}
			if FamilyActive(active, fam) {
				out = append(out, t)
			}
		}
	}
	return out
}

func mcpEntryFromConfig(mc *tool.McpConfig) toolskill.McpServerEntry {
	if mc == nil {
		return toolskill.McpServerEntry{}
	}
	env := mc.Env
	if env != nil {
		env = make(map[string]string, len(mc.Env))
		for k, v := range mc.Env {
			env[k] = v
		}
	}
	args := mc.Args
	if args != nil {
		args = append([]string(nil), mc.Args...)
	}
	return toolskill.McpServerEntry{
		Transport: mc.Transport,
		Endpoint:  mc.Endpoint,
		Id:        mc.Id,
		Backend:   mc.Backend,
		Command:   mc.Command,
		Args:      args,
		Env:       env,
	}
}

// canonicalDatasourceConfig 将运行时数据源 ID 对齐为工具名，避免界面名称与 datasource_id 不一致。
func canonicalDatasourceConfig(toolName string, cfg datasource.Config) datasource.Config {
	if toolName != "" {
		cfg.ID = toolName
	}
	return cfg
}

// registerDatasourceTools 注册 list_tables、describe_table、execute_read。
// Elasticsearch 绑定不进入 data 三件套（仍可作为 RCA es_log_query 的连接配置存在于工具列表）。
// 单个非 ES 数据源注册失败时降级为不可用（其余仍可用）；全部非 ES 均失败才返回错误。
func registerDatasourceTools(reg *tool.Registry, configs []datasource.Config, bindings []DatasourceBinding) ([]DatasourceBinding, string, error) {
	dsReg := datasource.NewRegistry()
	datasource.RegisterMySQL(dsReg)
	datasource.RegisterHive(dsReg)
	datasource.RegisterElasticsearch(dsReg)
	datasource.RegisterMongoDB(dsReg)

	var registered []datasource.Config
	outBindings := make([]DatasourceBinding, 0, len(bindings))
	nonESAttempts := 0
	for i, cfg := range configs {
		b := bindings[i]
		if isElasticsearchType(cfg.Type) {
			b.SkipDataTools = true
			b.Available = false
			b.Err = "elasticsearch 不走 list_tables/describe_table/execute_read；请用 es_log_query(cluster=…) 或 http_request"
			outBindings = append(outBindings, b)
			continue
		}
		nonESAttempts++
		if _, err := dsReg.Register(cfg); err != nil {
			b.Available = false
			b.Err = err.Error()
			outBindings = append(outBindings, b)
			continue
		}
		b.Available = true
		b.Err = ""
		b.SkipDataTools = false
		outBindings = append(outBindings, b)
		registered = append(registered, cfg)
	}
	if len(registered) == 0 && nonESAttempts > 0 {
		var parts []string
		for _, b := range outBindings {
			if b.SkipDataTools {
				continue
			}
			name := b.ID
			if name == "" {
				name = b.ToolName
			}
			if b.Err != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", name, b.Err))
			} else {
				parts = append(parts, name+": unknown error")
			}
		}
		return outBindings, FormatDatasourcePrompt(outBindings, ""), fmt.Errorf("所有数据源均注册失败（请检查连接与账号）: %s", strings.Join(parts, "; "))
	}

	defaultDSID := ""
	if len(registered) > 0 {
		defaultDSID = registered[0].ID
	}

	prompt := FormatDatasourcePrompt(outBindings, defaultDSID)
	if len(registered) == 0 {
		// ES-only（或无可注册 data 源）：不注册三件套，仍返回路由提示。
		return outBindings, prompt, nil
	}

	store := metadata.NewInMemoryStore(nil)
	desc := templates.GetDescriptor("mysql")
	if registered[0].Type != "" {
		desc = templates.GetDescriptor(registered[0].Type)
	}

	exec := executor.NewMultiExecutor(
		dsReg,
		executor.NewMySQLExecutor(dsReg),
		executor.NewESExecutor(dsReg),
		executor.NewMongoExecutor(dsReg),
	)

	for _, td := range desc.Tools {
		if td.Name == templates.ToolExecuteWrite {
			continue
		}
		var opts *tool.RegisterToolOptions
		if td.Description != "" {
			opts = &tool.RegisterToolOptions{Description: td.Description}
		}
		switch td.Name {
		case templates.ToolListTables:
			_ = tooldata.RegisterListTablesTool(reg, &tooldata.ListTablesConfig{
				Store:               store,
				Registry:            dsReg,
				DefaultDatasourceID: defaultDSID,
			}, opts)
		case templates.ToolDescribeTable:
			_ = tooldata.RegisterDescribeTableTool(reg, &tooldata.DescribeTableConfig{
				Store:               store,
				Registry:            dsReg,
				DefaultDatasourceID: defaultDSID,
			}, opts)
		case templates.ToolExecuteRead:
			_ = tooldata.RegisterExecuteReadTool(reg, &tooldata.ExecuteReadConfig{
				Exec:                exec,
				Registry:            dsReg,
				Store:               store,
				DefaultDatasourceID: defaultDSID,
			}, opts)
		}
	}
	return outBindings, prompt, nil
}

func toolConfigToMap(s interface{}) map[string]interface{} {
	if s == nil {
		return nil
	}
	st, ok := s.(*structpb.Struct)
	if !ok || st.Fields == nil {
		return nil
	}
	m := make(map[string]interface{})
	for k, v := range st.Fields {
		m[k] = v.AsInterface()
	}
	return m
}

func registerBuiltinTool(reg *tool.Registry, cfg map[string]interface{}) {
	funcPath := ""
	if v, ok := cfg["func_path"].(string); ok {
		funcPath = v
	}
	switch funcPath {
	case "calculator_add", "calculator":
		_ = tool.RegisterCalculatorTool(reg)
	case "ssh_exec":
		_ = tool.RegisterSSHExecTool(reg, tool.SSHExecConfigFromMap(cfg))
	default:
		// 未知 builtin 跳过
	}
}

// BuildSkillsIndex merges workspace/skills with extra shared Skill directories.
func BuildSkillsIndex(workspace string, extraSkillDirs []string) (*skills.Index, error) {
	dirs := make([]string, 0, len(extraSkillDirs)+1)
	if workspace != "" {
		skillsDir := filepath.Join(workspace, "skills")
		if st, err := os.Stat(skillsDir); err == nil && st.IsDir() {
			dirs = append(dirs, skillsDir)
		}
	}
	for _, dir := range extraSkillDirs {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	return skills.NewIndex(dirs, nil, nil)
}

// AllowScriptExecution 是否允许 execute_skill_script，默认 true。由 main 根据 config 或环境变量设置。
var AllowScriptExecution = true

// SetAllowScriptExecution 由 main 在加载 config 后调用，用于设置脚本执行开关。
func SetAllowScriptExecution(allow bool) {
	AllowScriptExecution = allow
}

// RegisterLearningTools 注册 append_learning（写入 .learnings，供 Growth 复盘消费）。
func RegisterLearningTools(reg *tool.Registry) error {
	if reg == nil {
		return nil
	}
	return toolskill.RegisterAppendLearningTool(reg)
}

// ExecuteSkillScript 直接执行技能脚本，供 Agent.ExecuteSkill API 使用
func ExecuteSkillScript(ctx context.Context, workspace string, extraSkillDirs []string, skillName, relPath, input string) (string, error) {
	if workspace == "" || skillName == "" || relPath == "" {
		return "", errors.New("workspace, skillName and relPath are required")
	}
	idx, err := BuildSkillsIndex(workspace, extraSkillDirs)
	if err != nil {
		return "", err
	}
	reg := tool.NewRegistry()
	if err := RegisterSkillTools(reg, idx, nil, true); err != nil {
		return "", err
	}
	t, ok := reg.Get("execute_skill_script")
	if !ok {
		return "", errors.New("execute_skill_script not registered")
	}
	result, err := t.Execute(ctx, map[string]any{"name": skillName, "path": relPath, "input": input})
	if err != nil {
		return "", err
	}
	if s, ok := result.(string); ok {
		return s, nil
	}
	return fmt.Sprint(result), nil
}

// BuildEffectiveSystemPrompt 根据 Agent 的 systemPrompt 与 skills 构建最终注入的系统提示。
// 当 skillsIdx 非空时，若用户未配置 systemPrompt，则使用完整的技能说明；若已配置，则在其后追加技能摘要。
func BuildEffectiveSystemPrompt(userPrompt string, skillsIdx *skills.Index) string {
	if skillsIdx == nil {
		return userPrompt
	}
	skillsPrompt := templates.BuildSkillsAwarePrompt(skillsIdx)
	if userPrompt == "" {
		return skillsPrompt
	}
	return userPrompt + "\n\n---\n\n" + skillsPrompt
}

// DefaultMaxOutputTokens Portal 对话默认单次回复 token 上限（框架 CallConfig 默认为 1024）。
// 8192 会把「完整映射表（468 条）」这类长表截在约 350 行；RCA 明细需要更高上限。
const DefaultMaxOutputTokens = 32768

// ReActOptionsFromAgent 按 Agent 模型配置追加 ReAct 选项（如 max_output_tokens）。
func ReActOptionsFromAgent(meta biz.AgentMeta) []agent.ReActOption {
	n := meta.ModelConfig.MaxOutputTokens
	if n <= 0 {
		return nil
	}
	return []agent.ReActOption{agent.WithReActMaxOutputTokens(n)}
}

// BuildReActAgent 构建 ReActAgent。extra 在默认选项之后应用，可覆盖例如 MaxSteps、EventBus，或注入 WithReActToolSuccessHook。
func BuildReActAgent(m model.Model, reg *tool.Registry, systemPrompt string, maxHistory int, extra ...agent.ReActOption) agent.Agent {
	mem := memory.NewBufferMemory(maxHistory)
	if maxHistory <= 0 {
		maxHistory = 20
	}
	opts := []agent.ReActOption{
		agent.WithReActMaxSteps(80),
		agent.WithReActMaxHistory(maxHistory),
		agent.WithReActMaxContextRunes(model.DefaultMaxContextRunes),
		agent.WithReActMaxOutputTokens(DefaultMaxOutputTokens),
		agent.WithReActEventBus(events.DefaultBus()),
	}
	if globalToolGuardrails != nil {
		cp := *globalToolGuardrails
		opts = append(opts, agent.WithReActToolGuardrails(&cp))
	}
	if orch := prefetchOrchestratorForReAct(); orch != nil {
		opts = append(opts, agent.WithReActMemoryOrchestrator(orch))
	}
	if ShouldEnableParallelTools(reg) {
		opts = append(opts, agent.WithReActParallelTools(true))
	}
	if gate := NewTurnIntentGate(); gate != nil {
		opts = append(opts, agent.WithReActPostModelPolicy(gate))
	}
	opts = append(opts, extra...)
	return agent.NewReActAgent(m, mem, reg, opts...)
}

// ShouldEnableEvidenceGate reports whether the registry has RCA evidence tools
// (jaeger_trace or es_log_query) that warrant Soft EvidenceGate on ReAct.
func ShouldEnableEvidenceGate(reg *tool.Registry) bool {
	if reg == nil {
		return false
	}
	if _, ok := reg.Get("jaeger_trace"); ok {
		return true
	}
	if _, ok := reg.Get("es_log_query"); ok {
		return true
	}
	return false
}

// ShouldEnableParallelTools is true when the registry has code-root tools that
// are safe to run together in one ReAct step (grep/read/symbol).
func ShouldEnableParallelTools(reg *tool.Registry) bool {
	if reg == nil {
		return false
	}
	for _, name := range []string{"rca_read", "rca_grep", "rca_glob", "rca_symbol"} {
		if _, ok := reg.Get(name); ok {
			return true
		}
	}
	return false
}

// ShouldApplyEvidenceGate is true only when this turn is an RCA investigation.
// Bound ES/Jaeger tools must not force log evidence on unrelated lookups (e.g. Mongo).
func ShouldApplyEvidenceGate(active map[string]struct{}, userText string) bool {
	if active != nil {
		_, ok := active[FamilyRCA]
		return ok
	}
	scores := scoreFamilies(strings.TrimSpace(userText), familySet([]string{FamilyRCA}), nil)
	return scores[FamilyRCA] > 0
}
