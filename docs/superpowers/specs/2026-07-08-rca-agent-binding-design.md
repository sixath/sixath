# RCA 工具接入 Agent 绑定 UI — 设计文档

- 日期:2026-07-08
- 状态:待评审
- 场景:让 RCA 工具链(已在 framework 实现)能通过 portal 的"新建工具 + Agent 绑定"UI 被创建、配置并绑定到指定 Agent,绑定后该 Agent 才拥有这些工具。

## 1. 目标与非目标

### 目标
在 portal 新增顶级工具类型 `rca`,使用户能在"新建工具"页选择 RCA、填写专属配置、保存为工具目录条目,再在 Agent 详情页绑定给某个 Agent;Agent 运行时按绑定构造出对应的 framework RCA 工具。

### 非目标
- 不改 framework 的 5 个 RCA 工具及其 `RegisterXxx`(已实现、已测,直接复用)。
- 不改 proto(`tool.type` 是 string 非 enum,新增值无需重生成 pb)。
- 不动 `.worktrees/page-confirmation/` 下的 proto/web 副本(在途分支);目标是主 `portal/`、`web/`。
- 不做 RCA 工具的自动 clone、时间窗查询等(framework 侧既有非目标不变)。

## 2. 背景架构(已查证)

portal 的工具与绑定机制:
- 工具存 DB `tool` 表:`{Name, Type: builtin|mcp|datasource, Config: JSON}`(`internal/data/model/tool.go`)。
- "新建工具"页 `web/src/pages/ToolForm.tsx` 按 `type` 条件渲染字段;datasource 类型即渲染一整套 DSN/Host/Port 字段,写入 `config.datasource.*`。RCA 照此范式。
- Agent 详情页绑定 = 选已存在工具的 id(`BindTools` 只传 `tool_ids`),配置在工具行上,不在绑定上。
- 运行时 `BuildRegistry(tools, reg)`(`internal/chat/agent_builder.go`)遍历 Agent 绑定的工具,按 `t.Type` switch:builtin → `registerBuiltinTool(reg, cfg)`(按 `cfg["func_path"]` 分发);datasource → 构建数据源工具。

## 3. 子工具与字段映射

顶级 `rca` 类型下,子工具下拉 3 项(`func_path`):

| func_path | 表单字段 | config(存 tool.Config.rca) | framework 构造 |
|---|---|---|---|
| `rca_code` | 仓库根 roots(多行文本,每行一个绝对路径) | `{func_path:"rca_code", roots:[]string}` | `tool.RegisterRCACodeTools(reg, roots)` → 一次注册 rca_grep + rca_glob + rca_read |
| `jaeger_trace` | Jaeger Query URL | `{func_path:"jaeger_trace", query_url:string}` | `tool.RegisterJaegerTool(reg, query_url)` |
| `es_log_query` | 已建 ES 数据源工具 id、默认索引、trace_id 字段 | `{func_path:"es_log_query", datasource_id, default_index, trace_id_field}` | 按 datasource_id 找到该 Agent 绑定的 datasource 工具连接 → 建 reader → `tool.RegisterESLogTool(reg, reader, tool.ESLogConfig{...})` |

**粒度决策:** 代码检索三工具合为一个 `rca_code` 子工具(与 `RegisterRCACodeTools` 一次注册三工具对齐,避免重复注册)。ES 数据源复用 portal 已建 datasource 工具(不在 RCA 表单里重复填连接)。

## 4. 运行时构造(portal/internal/chat/agent_builder.go)

- `BuildRegistry` 的 `switch t.Type` 增加:
  ```
  case biz.ToolTypeRCA:
      registerRCATool(reg, cfg, tools)   // tools = 该 Agent 绑定的全部工具,供 es 查 datasource
  ```
- 新增 `registerRCATool(reg *tool.Registry, cfg map[string]any, agentTools []*biz.ToolMeta)`:
  - 读 `cfg["func_path"]`,分发:
    - `rca_code`:读 `roots []string`;空则跳过+warn;否则 `RegisterRCACodeTools(reg, roots)`。
    - `jaeger_trace`:读 `query_url`;空则跳过+warn;否则 `RegisterJaegerTool(reg, query_url)`。
    - `es_log_query`:读 `datasource_id/default_index/trace_id_field`;在 `agentTools` 中找 `Type==datasource 且其 config.datasource.id==datasource_id` 的工具,取其连接构建 `datasource.Registry`+`executor.NewBundle().Reader`;找不到 → warn+跳过;否则 `RegisterESLogTool`。
    - 未知 func_path → 跳过。
  - 缺配置一律跳过+warn,绝不阻断整个 Agent 构建。
- 复用现有 datasource 构建逻辑(与 `registerDatasourceTools` 同款 `datasource.NewRegistry`+`RegisterElasticsearch`+`Register`+`executor.NewBundle`);可抽一个内部 helper 供两处共用,避免重复。

## 5. 后端校验(portal/internal/biz/tool.go)

- `ToolType` 增加常量 `ToolTypeRCA ToolType = "rca"`。
- `ToolUsecase.Create/Update`:允许 `type=="rca"`(当前对非 builtin/mcp/datasource 会回退 builtin —— 需把 rca 纳入合法集,不回退)。
- 校验 RCA 工具的 `func_path` ∈ {rca_code, jaeger_trace, es_log_query},否则返回错误(避免存下无法构造的工具)。

## 6. 前端(web/src/pages/ToolForm.tsx + web/src/api/client.ts)

- `client.ts`:工具 `type` 联合类型加 `'rca'`;`ToolConfig` 增加可选 `rca?: { func_path; roots?; query_url?; datasource_id?; default_index?; trace_id_field? }`。
- `ToolForm.tsx`:
  - type `useState` 联合加 `'rca'`;类型下拉加 `<option value="rca">RCA</option>`。
  - 新增 `{type === 'rca' && (...)}` 条件块:
    - 子工具下拉(rca_code / jaeger_trace / es_log_query)→ 写 `config.rca.func_path`。
    - 按 func_path 展开对应字段(roots 多行 / query_url / datasource_id+default_index+trace_id_field),写入 `config.rca.*`。
  - 提交映射:`type==='rca'` 时 `submitConfig = { rca: {...} }`(与 datasource 分支同款处理)。
  - 编辑回填:加载已有 rca 工具时从 `t.config.rca` 回填表单。

## 7. 测试

- **portal 单测**(`internal/chat/agent_builder_test.go` 或新增):
  - `registerRCATool` 三分支:rca_code 注册出 grep/glob/read;jaeger_trace 注册;es_log_query 在 agentTools 含匹配 datasource 时注册、缺失时跳过不报错;未知 func_path 跳过;缺配置跳过。
  - `internal/biz/tool_test`:Create/Update 接受 type=rca;非法 func_path 报错。
- **framework**:不改,不新增。
- **前端**:手动验证——新建 RCA 工具(各子工具)→ 在 Agent 详情绑定 → Agent 工具列表出现对应工具。
- **回归**:`cd portal && go build ./... && go test ./...`;`cd web && npm run build`(或既有构建命令)。

## 8. 数据流

```
用户 → ToolForm 选 RCA + 子工具 + 填字段
  → 保存(portal biz 校验 type/func_path)→ tool 表新行(Config.rca)
用户 → Agent 详情 → 绑定该工具(agent_tool)
Agent 运行 → BuildRegistry 遍历绑定工具
  → case rca → registerRCATool 按 func_path 调 framework RegisterXxx
  → Agent 的 registry 拥有对应 RCA 工具 → 模型可调用
```

## 9. 后续(本期不做)
- RCA 专属字段的强校验(URL 格式、roots 路径存在性)可后续增强。
- 若将来要"一个 agent 绑定即得全套 RCA",可加一个聚合子工具,不影响本设计。
