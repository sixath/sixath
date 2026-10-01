package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/biz"

	"log/slog"

	"github.com/sixath/framework/config"
	fwctx "github.com/sixath/framework/context"
	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/executor"
	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/investigate"
	"github.com/sixath/framework/investigate/cases"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/metadata"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/netx"
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

// RegistryBuildOptions optional inputs for BuildRegistry.
type RegistryBuildOptions struct {
	// Workspace is the agent writable root; rca_* uses workspace/code when present.
	Workspace string
	// AgentProxyID is agents.proxy_id (empty = direct inherit).
	AgentProxyID string
	// Proxies is the preloaded catalog (no ACL). A nonempty AgentProxyID
	// missing from Proxies is a permanent miss (fail-closed; no silent direct).
	Proxies map[string]netx.Spec
}

// BuildRegistry 根据 Agent 绑定的工具与 MCP Server 列表构建 tool.Registry。
// tools 来自 ToolRepo.ListByAgent；servers 来自 McpServerRepo.ListByAgent。
func BuildRegistry(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, reg *tool.Registry, opts ...RegistryBuildOptions) (*RegistryBuildResult, error) {
	var o RegistryBuildOptions
	if len(opts) > 0 {
		o = opts[0]
	}

	reg.SetEventBus(events.DefaultBus())

	var mcpServers []toolskill.McpServerEntry
	var datasourceConfigs []datasource.Config
	var dsBindings []DatasourceBinding
	var pendingVM []map[string]interface{}

	for _, t := range tools {
		cfg := toolConfigToMap(t.Config)
		switch t.Type {
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(cfg)
			if mc != nil {
				if err := applyMCPHTTPClient(mc, toolEgressBinding(cfg), o); err != nil {
					slog.Error("egress: skip mcp tool, proxy not in catalog", "tool", t.Name, "id", mc.Id, "err", err)
					break
				}
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
			b := bindingFromConfig(t.Name, dsCfg, nil)
			// purpose / default_index live on the tool config map, not datasource.Config.
			b.DefaultIndex = mapStringField(dsMap, "default_index", "defaultIndex")
			b.Purpose = mapStringField(dsMap, "purpose")
			if err := applyDatasourceEgress(&dsCfg, toolEgressBinding(cfg), o); err != nil {
				slog.Error("egress: datasource unavailable", "tool", t.Name, "id", dsCfg.ID, "err", err)
				b.Available = false
				b.Err = err.Error()
			}
			datasourceConfigs = append(datasourceConfigs, dsCfg)
			dsBindings = append(dsBindings, b)
		case biz.ToolTypeRCA:
			rcaMap, _ := cfg["rca"].(map[string]interface{})
			funcPath, _ := rcaMap["func_path"].(string)
			if funcPath == "vm_run_cmd" {
				pendingVM = append(pendingVM, cfg)
				break
			}
			registerRCATool(reg, cfg, o.Workspace, o)
		}
	}

	bound, err := registerBoundMCPServers(reg, servers, o)
	if err != nil {
		return nil, err
	}
	mcpServers = append(mcpServers, bound...)

	var dsPrompt string
	var mysqlExec executor.Executor
	var mysqlIDs []string
	if len(datasourceConfigs) > 0 {
		registered, prompt, exec, ids, err := registerDatasourceTools(reg, datasourceConfigs, dsBindings)
		if err != nil {
			return nil, err
		}
		dsBindings = registered
		dsPrompt = prompt
		mysqlExec = exec
		mysqlIDs = ids
	}
	for _, cfg := range pendingVM {
		registerVMRunCmdFromPortal(reg, cfg, o, mysqlExec, mysqlIDs)
	}

	registerESLogFromAgentTools(reg, tools, o)
	applyPortalToolDescriptions(reg, tools)
	applyHTTPRequestOverlay(reg, o)

	return &RegistryBuildResult{McpServers: mcpServers, DatasourcePrompt: dsPrompt, DsBindings: dsBindings}, nil
}

// registerBoundMCPServers registers ListByAgent MCP servers. Same-id rows already
// marked on the registry are skipped before applyMCPHTTPClient / RegisterMcpTool.
func registerBoundMCPServers(reg *tool.Registry, servers []*biz.McpServerMeta, o RegistryBuildOptions) ([]toolskill.McpServerEntry, error) {
	var mcpServers []toolskill.McpServerEntry
	for _, s := range servers {
		mc := biz.McpServerToConfig(s)
		if mc == nil {
			continue
		}
		if reg.HasMcpServer(mc.Id) {
			continue
		}
		if err := applyMCPHTTPClient(mc, netx.Binding{Mode: netx.ModeInherit}, o); err != nil {
			slog.Error("egress: skip mcp server, proxy not in catalog", "id", mc.Id, "err", err)
			return nil, fmt.Errorf("mcp server %q failed to register (check command/endpoint)", mc.Id)
		}
		tool.RegisterMcpTool(reg, mc)
		if !reg.HasMcpServer(mc.Id) {
			return nil, fmt.Errorf("mcp server %q failed to register (check command/endpoint)", mc.Id)
		}
		mcpServers = append(mcpServers, mcpEntryFromConfig(mc))
	}
	return mcpServers, nil
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
// 至少成功注册一个 mysql 时返回 NewMultiExecutor 与其 cfg.ID；否则 mysqlExec/mysqlIDs 为 nil。
func registerDatasourceTools(reg *tool.Registry, configs []datasource.Config, bindings []DatasourceBinding) ([]DatasourceBinding, string, executor.Executor, []string, error) {
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
		if strings.TrimSpace(b.Err) != "" {
			b.Available = false
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
		return outBindings, FormatDatasourcePrompt(outBindings, ""), nil, nil, fmt.Errorf("所有数据源均注册失败（请检查连接与账号）: %s", strings.Join(parts, "; "))
	}

	defaultDSID := ""
	if len(registered) > 0 {
		defaultDSID = registered[0].ID
	}

	prompt := FormatDatasourcePrompt(outBindings, defaultDSID)
	if len(registered) == 0 {
		// ES-only（或无可注册 data 源）：不注册三件套，仍返回路由提示。
		return outBindings, prompt, nil, nil, nil
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
	var mysqlIDs []string
	for _, cfg := range registered {
		if strings.EqualFold(strings.TrimSpace(cfg.Type), "mysql") {
			mysqlIDs = append(mysqlIDs, cfg.ID)
		}
	}
	if len(mysqlIDs) == 0 {
		return outBindings, prompt, nil, nil, nil
	}
	return outBindings, prompt, exec, mysqlIDs, nil
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

// BuildEffectiveSystemPrompt 返回装配进 Harness 的 Agent 文案。
// Skills 索引由 Harness PromptBuilder 负责，这里不再拼接。
func BuildEffectiveSystemPrompt(userPrompt string, skillsIdx *skills.Index) string {
	_ = skillsIdx
	return userPrompt
}

// HarnessReActOptions 把 workspace、额外 skills 目录与 workspace hooks 交给 Harness（结案审查只做规则级）。
func HarnessReActOptions(workspace string, extraSkillDirs []string) []agent.ReActOption {
	return HarnessReActOptionsFor(nil, workspace, extraSkillDirs)
}

// ResolveCriticModel 按 hooks.yaml critic.model 解析审查模型；nil 或解析失败时复用 agent 自身模型。
var ResolveCriticModel func(name string) (model.Model, error)

// CriticStopHook 按 workspace 的 critic 配置构造结案审查；未启用时返回 nil。m 为 agent 模型（可为 nil，仅规则级）。
func CriticStopHook(m model.Model, workspace string) agent.StopHook {
	cfg, ok := agent.WorkspaceCriticConfig(strings.TrimSpace(workspace))
	if !ok {
		return nil
	}
	cm := m
	if name := strings.TrimSpace(cfg.Model); name != "" && ResolveCriticModel != nil {
		if resolved, err := ResolveCriticModel(name); err == nil && resolved != nil {
			cm = resolved
		} else {
			slog.Warn("harness critic: fall back to agent model", "model", name, "err", err)
		}
	}
	return agent.NewCriticHook(cfg, tool.DefaultInvestigationStore, cm)
}

// HarnessReActOptionsFor 同 HarnessReActOptions；m 非 nil 时结案审查可做模型级复核。
func HarnessReActOptionsFor(m model.Model, workspace string, extraSkillDirs []string) []agent.ReActOption {
	opts := []agent.ReActOption{agent.WithReActWorkspace(workspace)}
	if len(extraSkillDirs) > 0 {
		opts = append(opts, agent.WithReActSkillsDirs(extraSkillDirs))
	}
	var hooks []agent.ToolHook
	if ws := strings.TrimSpace(workspace); ws != "" {
		if loaded, err := agent.LoadWorkspaceHarnessHooks(ws); err == nil {
			hooks = append(hooks, loaded...)
		}
		stopHooks, maxNudges, err := agent.LoadWorkspaceStopHooks(ws, workspaceStopHookOptions())
		if err != nil {
			slog.Warn("harness hooks: skip stop_rules", "workspace", ws, "err", err)
			stopHooks = nil
		}
		if critic := CriticStopHook(m, ws); critic != nil {
			stopHooks = append(stopHooks, critic)
		}
		if len(stopHooks) > 0 {
			opts = append(opts, agent.WithReActStopHooks(stopHooks...), agent.WithReActMaxStopNudges(maxNudges))
		}
		if agent.WorkspaceInvestigationLedgerEnabled(ws) {
			opts = append(opts, agent.WithReActToolSuccessHook(agent.InvestigationObserver(tool.DefaultInvestigationStore)))
		}
	}
	if len(hooks) > 0 {
		opts = append(opts, agent.WithReActToolHooks(hooks...))
	}
	return opts
}

// DefaultMaxOutputTokens Portal 对话默认单次回复 token 上限（框架 CallConfig 默认为 1024）。
// 8192 会把「完整映射表（468 条）」这类长表截在约 350 行；RCA 明细需要更高上限。
const DefaultMaxOutputTokens = 32768

// ReActOptionsFromAgent 按 Agent 模型配置追加 ReAct 选项：
//   - token 计数器（按 provider/model 复用，随真实 usage 自校准，见 token_counter.go）；
//   - max_output_tokens（<=0 时用 BuildReActAgent 的默认值）。
func ReActOptionsFromAgent(meta biz.AgentMeta) []agent.ReActOption {
	opts := []agent.ReActOption{
		agent.WithReActTokenCounter(TokenCounterFor(meta.ModelConfig.Provider, meta.ModelConfig.Model)),
	}
	if n := meta.ModelConfig.MaxOutputTokens; n > 0 {
		opts = append(opts, agent.WithReActMaxOutputTokens(n))
	}
	return opts
}

func workspaceStopHookOptions() agent.StopHookOptions {
	return agent.StopHookOptions{Todos: tool.DefaultTodoStore, Investigations: tool.DefaultInvestigationStore}
}

// InvestigateConfig 组装 deep_investigate 配置：子 agent 复用父 agent 工作区的 stop_rules 与台账观测。
func InvestigateConfig(m model.Model, workspace string) investigate.Config {
	cfg := investigate.Config{Model: m}
	if ws := strings.TrimSpace(workspace); ws != "" {
		if hooks, n, err := agent.LoadWorkspaceStopHooks(ws, workspaceStopHookOptions()); err == nil {
			cfg.StopHooks, cfg.MaxStopNudges = hooks, n
		}
		if critic := CriticStopHook(m, ws); critic != nil {
			cfg.StopHooks = append(cfg.StopHooks, critic)
		}
		if agent.WorkspaceInvestigationLedgerEnabled(ws) {
			cfg.ExtraOptions = append(cfg.ExtraOptions,
				agent.WithReActToolSuccessHook(agent.InvestigationObserver(tool.DefaultInvestigationStore)))
		}
	}
	return cfg
}

// WorkspaceCaseResolver 返回 agent 工作区下 cases/ 案例库的解析函数；工作区为空时返回 nil。
func WorkspaceCaseResolver(workspace string) tool.CaseStoreResolver {
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		return nil
	}
	store := cases.ForWorkspace(ws)
	return func(context.Context) cases.Store { return store }
}

// RegisterInvestigationTools 注册调查类工具：工作区开启 investigation_ledger 时注册台账，
// 门控可见的 compare（有探测类工具才进 schema）、timeline（有日志类工具才进 schema），以及 deep_investigate（代码+日志工具都配置才进 schema）。
func RegisterInvestigationTools(reg *tool.Registry, m model.Model, workspace string) error {
	if agent.WorkspaceInvestigationLedgerEnabled(workspace) {
		resolve := WorkspaceCaseResolver(workspace)
		if err := tool.RegisterInvestigationToolWithOptions(reg, tool.DefaultInvestigationStore, tool.InvestigationToolOptions{Cases: resolve}); err != nil {
			return err
		}
		if resolve != nil {
			if _, exists := reg.Get(tool.CaseLibraryToolName); !exists {
				if err := tool.RegisterCaseLibraryTool(reg, tool.DefaultInvestigationStore, resolve); err != nil {
					return err
				}
			}
		}
	}
	if _, exists := reg.Get(tool.CompareToolName); !exists {
		if err := tool.RegisterCompareTool(reg); err != nil {
			return err
		}
	}
	if _, exists := reg.Get(tool.TimelineToolName); !exists {
		if err := tool.RegisterTimelineToolWithOptions(reg, tool.TimelineOptions{ChangeSources: agent.WorkspaceChangeSources(workspace)}); err != nil {
			return err
		}
	}
	if _, exists := reg.Get(tool.FieldHistoryToolName); !exists {
		if err := tool.RegisterFieldHistoryTool(reg); err != nil {
			return err
		}
	}
	if _, exists := reg.Get(tool.FsCompareToolName); !exists {
		if err := tool.RegisterFsCompareTool(reg); err != nil {
			return err
		}
	}
	return investigate.Register(reg, InvestigateConfig(m, workspace))
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
		agent.WithReActMaxContextRunes(fwctx.DefaultMaxContextRunes),
		agent.WithReActMaxOutputTokens(DefaultMaxOutputTokens),
		agent.WithReActEventBus(events.DefaultBus()),
	}
	if globalToolGuardrails != nil {
		cp := *globalToolGuardrails
		opts = append(opts, agent.WithReActToolGuardrails(&cp))
	}
	if ShouldEnableParallelTools(reg) {
		opts = append(opts, agent.WithReActParallelTools(true))
	}
	opts = append(opts, extra...)
	return agent.NewReActAgent(m, mem, reg, opts...)
}

// IsPlanMode 判断 Agent 是否启用 Plan-Execute 模式。
func IsPlanMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "plan", "plan_execute", "plan-execute":
		return true
	default:
		return false
	}
}

// BuildAgent 按 mode 构建 Agent：
//   - react（默认）→ ReActAgent；
//   - plan / plan_execute → PlanExecuteAgent（planner 复用同一模型，worker 为 ReActAgent）。
//
// 规划失败 / 步骤失败重规划耗尽时，PlanExecuteAgent 内部自动回退 ReAct，保证可用性。
func BuildAgent(m model.Model, reg *tool.Registry, systemPrompt string, maxHistory int, mode string, extra ...agent.ReActOption) agent.Agent {
	worker := BuildReActAgent(m, reg, systemPrompt, maxHistory, extra...)
	if IsPlanMode(mode) {
		return agent.NewPlanExecuteAgent(m, worker)
	}
	return worker
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
