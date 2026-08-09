# Turn Tool Surface

每轮按意图收窄 MCP/RCA/web/knowledge 工具面；`TurnIntentGate` 丢弃跨族调用。

- 关闭装配收窄：`SATH_TURN_TOOL_SURFACE=0`（`ActiveFamilies` 为 nil，全量绑定）
- 关闭门控：`SATH_TURN_INTENT_GATE=0`（仅装配收窄、无 PostModel 兜底）
- 规格：sixath 仓库 `docs/superpowers/specs/2026-08-09-turn-tool-surface-design.md`
