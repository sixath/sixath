# Turn Tool Surface

每轮按意图收窄 MCP/RCA/web/knowledge/code 工具面；`TurnIntentGate` 丢弃跨族调用。

- `code` 族：跨仓源码检索（`rca_grep` / `rca_glob` / `rca_read` / `rca_symbol`）。绑定 `rca.func_path` 为 `rca_code` / `rca_symbol` 时计入；意图含「源码 / 代码分析 / 调用链 / 流程梳理」等时激活。
- `rca` 族激活时单向并上 `code`（trace → 源码）；`code` 不自动打开 jaeger/ES。
- 关闭装配收窄：`chat.turn_tool_surface_enabled: false` 或 `SATH_TURN_TOOL_SURFACE=0`（`ActiveFamilies` 为 nil，全量绑定）；源码分析系统提示仍追加（全量工具里已有 `rca_*`，需要同一纪律）。环境变量优先于 YAML。
- 关闭门控：`SATH_TURN_INTENT_GATE=0`（仅装配收窄、无 PostModel 兜底）
- 发现未命中时热装载绑定 MCP：`list_tools` / `tool_search` 会按 Agent 已绑定 MCP 自动 Expand（可用 `SATH_MCP_EXPAND_ON_MISS=0` 关闭）
- 规格：sixath 仓库 `docs/superpowers/specs/2026-08-09-turn-tool-surface-design.md`；code 族见 `docs/superpowers/specs/2026-08-17-code-analysis-surface-design.md`
