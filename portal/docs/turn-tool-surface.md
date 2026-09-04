# Turn Tool Surface

每轮按意图收窄 MCP/RCA/web/knowledge/code 工具面；`TurnIntentGate` 丢弃跨族调用。

- `code` 族：跨仓源码检索（`rca_grep` / `rca_glob` / `rca_read` / `rca_symbol`）。绑定 `rca.func_path` 为 `rca_code` / `rca_symbol` 时计入；意图含「源码 / 代码分析 / 调用链 / 流程梳理」等时激活。
- `rca` 族激活时单向并上 `code`（trace → 源码）；`code` 不自动打开 jaeger/ES。
- 总开关：`chat.investigation_gates`（默认 `off`）或 `SATH_INVESTIGATION_GATES`。`off` 时关闭工具面收窄、TurnIntentGate（含 HTTP 接地）、任务锁。非法值当 `off`。
- 单层覆盖（仅当该 env 已设置）：`SATH_TURN_TOOL_SURFACE`、`SATH_TURN_INTENT_GATE`、`SATH_TASK_LOCK`。
- `chat.turn_tool_surface_enabled` 只在总开关 `on` 时生效；总开关 `off` 时忽略，避免旧 YAML 把工具面重新打开。
- 规格：`docs/superpowers/specs/2026-09-04-investigation-gates-off-design.md`
- 发现未命中时热装载绑定 MCP：`list_tools` / `tool_search` 会按 Agent 已绑定 MCP 自动 Expand（可用 `SATH_MCP_EXPAND_ON_MISS=0` 关闭）
- 规格：sixath 仓库 `docs/superpowers/specs/2026-08-09-turn-tool-surface-design.md`；code 族见 `docs/superpowers/specs/2026-08-17-code-analysis-surface-design.md`
