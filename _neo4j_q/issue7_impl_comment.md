## 实现进展（治本 L1 + L2 v0）

已在本地落地（待合入）：

**P0 治本**
- framework: `PostModelPolicy` 钩子（`Run` / sync stream / tools stream 三路径，工具执行前）
- portal: `TurnIntentGate`
  - 正文已像最终答复仍带 tool_calls → 丢弃工具并收束
  - `web_search` 等漂移敏感工具与本轮 user 无 token 重叠 → 丢弃/收束
- portal: `AppendTurnIntentPrompt` 任务边界约束；收敛 `web_prompt`「无条件写完整」

**P1 配套**
- `WebToolsEnabled=false` 时不再 fallback `WebToolsShouldRegister()`（fail-closed）
- web system prompt 仅在 Agent 显式启用 web 时追加

关闭闸门：`SATH_TURN_INTENT_GATE=0`