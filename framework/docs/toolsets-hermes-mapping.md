# Sixath 工具集（Toolset）与 Hermes 对照

本仓库在 `framework/tool/toolset.go` 中定义与 [Hermes `toolsets.py`](https://github.com/NousResearch/hermes-agent/blob/main/toolsets.py) 对齐的标签，并在 `Registry.Register` 时为内置工具自动写入 `Tool.Toolset`，供 `ListByToolsets` 与后续 Portal 配置使用。

## 标签语义（核心 + MCP + P0 扩展）

| 标签常量 | Hermes 典型工具 | Sixath 当前工具 |
|----------|-----------------|-----------------|
| `web` | `web_search`, `web_extract` | `http_request`；P0 opt-in：`web_search`, `web_extract` |
| `file` | `read_file`, `write_file`, `patch`, `search_files` | `execute_read`, `execute_write`, `list_tables`, `describe_table`；P0 opt-in：`read_file`, `write_file`, `patch`, `search_files` |
| `skills` | `skills_list`, `skill_view`, `skill_manage` | `load_skill`, `read_skill_file`, `execute_skill_script`；P0 opt-in：`skills_list`, `skill_view`, `skill_manage` |
| `memory` | `memory` | `memory_remember`, `memory_recall`, `memory_get`（MemoryStore facade） |
| `terminal` | `terminal`, `process` | `ssh_exec`；P0 opt-in 本地：`terminal` |
| `browser` | Hermes browser / CDP 族（navigate、snapshot、click、type 等） | Phase 2 S2 最小集 opt-in：`browser_navigate`, `browser_snapshot`, `browser_click`, `browser_type`（`ToolsetBrowser`）；全栈其余工具仍 backlog |
| `todo` | `todo` | P0 opt-in：`todo`（`ToolsetTodo`） |
| `cronjob` | `cronjob` | P0 opt-in：`cronjob`（`ToolsetCronjob`） |
| `core` | — | `ask_user` 等 |
| `mcp` | 动态 MCP | 由 `RegisterMcpTool` 注册的所有 MCP 工具 |

## P0 运行时工具与 Portal opt-in

Hermes P0 工具默认 **不注册**（NFR-1），由 `portal/internal/chat/runtime_tools.go` 的 `RegisterAgentRuntimeTools` 按 `HermesP0ToolFlags` 启用：

| 工具 | Framework 注册 | Feature flag（环境变量） |
|------|----------------|------------------------|
| `memory_remember` / `memory_recall` / `memory_get` | `RegisterMemoryStoreTools` | session scope 默认启用；`SATH_AGENT_MEMORY_WRITE_ENABLED` 或 Agent `runtime_tools.memory_write_enabled` 仅控制 agent 文件写入；**仅模型主动 tool_call 写文件，无后台自动写** |
| `skills_list` / `skill_view` / `skill_manage` | `RegisterSkillsListViewTools` / `RegisterSkillManageTool` | `SATH_SKILL_RUNTIME_MANAGE_ENABLED` |
| `todo` | `RegisterTodoTool` | `SATH_TODO_ENABLED` |
| `read_file` 等 | `RegisterWorkspaceFileTools` | `SATH_WORKSPACE_FILES_ENABLED` |
| `web_search` / `web_extract` | `RegisterWebTools` | `SATH_WEB_TOOLS_ENABLED` |
| `terminal` | `RegisterTerminalTool` | `SATH_TERMINAL` 同义：`TERMINAL_LOCAL_ENABLED` |
| `browser_*`（四工具） | `RegisterBrowserTools`（Portal：`RegisterBrowserRuntimeTools`） | `SATH_BROWSER_ENABLED`（`BROWSER_ENABLED` deprecate 同义）；CDP：`BROWSER_CDP_URL`；CheckFn Healthy 失败则不出 schema |
| `cronjob` | `RegisterCronjobTool` | `CRONJOB_TOOL_ENABLED` |

基线工具（始终注册）：`memory_remember`、`memory_recall`、`memory_get`、`load_skill`、`read_skill_file`、`execute_skill_script`。

## Web 搜索后端（P0 默认博查）

- 环境变量 `WEB_SEARCH_BACKEND`：默认 `bocha`；可选 `tavily`
- 博查：`BOCHA_API_KEY`
- Tavily：`TAVILY_API_KEY`
- `CheckFn` 在后端未配置时隐藏 `web_search` schema

## 与 Hermes 的差异说明

- **browser**：Hermes 侧为完整 CDP 工具族；Sixath Phase 2 仅四工具最小集（navigate/snapshot/click/type），confirm/下载/全栈仍 backlog。
- **file**：Hermes 侧重本地/工作区文件；Sixath 将数据源只读/只写与表结构工具归入 `file`，P0 工作区四件套同名对齐 Hermes。
- **calculator_add**：Hermes 核心列表无直接对应，暂挂在 `skills` 便于教学 Demo。
- **跨会话转录**：使用 `memory_recall(scope=session, source=transcript)`，不再注册独立 `session_search` 工具。
- **ask_user / append_learning**：Sixath 基线能力，非 Hermes P0 对照项。

## API

- `tool.PresetHermesCoreTags()`：返回 `[]string{web,file,skills,memory,terminal}`，与常见 Hermes 五类预设一致（不含 `mcp`、`todo`、`cronjob`）。
- `(*Registry).ListByToolsets(tags []string)`：`tags` 为空时等价 `List()`。
- `tool.ListForAPI(ctx, reg, toolsets)`：结合 `CheckFn` 与 toolset 白名单过滤模型可见 schema。

## 自定义工具

`Register` 时若显式设置 `Tool.Toolset`，将覆盖内置名默认映射；未设置且工具名不在内置表时，`Toolset` 为空——**不会**出现在 `ListByToolsets` 结果中，但仍出现在 `List()` 中。
