# Turn Tool Surface

每轮按意图收窄 MCP/RCA/web/knowledge 工具面；`TurnIntentGate` 丢弃跨族调用。

- 关闭装配收窄：`SATH_TURN_TOOL_SURFACE=0`（`ActiveFamilies` 为 nil，全量绑定）
- 关闭门控：`SATH_TURN_INTENT_GATE=0`（仅装配收窄、无 PostModel 兜底）
- 发现未命中时热装载绑定 MCP：`list_tools` / `tool_search` 会按 Agent 已绑定 MCP 自动 Expand（可用 `SATH_MCP_EXPAND_ON_MISS=0` 关闭）
- 规格：sixath 仓库 `docs/superpowers/specs/2026-08-09-turn-tool-surface-design.md`
