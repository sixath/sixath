# S9 收口：ChatAgent 接 `workspace`

**日期**: 2026-09-05  
**状态**: 已确认（S8 leftover；2026-09-05 实施）  
**范围**: `framework/harness.ChatAgent` 与 `templates` 的 Chat 装配。不改 ReAct、不删 `framework/agent` 别名、不改 Portal / Insights。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S8](./2026-09-05-cli-templates-workspace-design.md)

**一句话**: CLI 默认对话路径也是 `agent = model + workspace + harness`：非空 workspace 时 ChatAgent 用 PromptBuilder 读 `MEMORY.md` / `USER.md`。

---

## 1. 背景

S8 把 workspace 接到 skills / dataquery / MCP 的 **ReAct**。S8 明确 **不改** ChatAgent。

现网：`NewChatAgentHandlerFromConfig`（`sath serve` 的 `/chat`）构造无工具面的 `ChatAgent`，不读 workspace。yaml 写了 `workspace:` 也对对话路径无效。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| ChatAgent | `ChatConfig.Workspace` + `WithChatWorkspace`；Run / RunStream 经 PromptBuilder 注入 `MEMORY.md` / `USER.md` |
| 空 / 空白 | 不注入；消息面与现网一致（S7 锁定：CLI 空 workspace 仍可跑） |
| templates | `NewChatAgentHandlerFromConfig` 把 `cfg.Workspace` 交给 ChatAgent；`NewChatAgentHandler` 签名不变（无 workspace） |
| 不改成 ReAct | ChatAgent 仍无工具循环 |
| 别名 | `framework/agent` 转发 `WithChatWorkspace`；**不删**别名包 |
| Portal / Insights / RCA | **不改** |

---

## 3. 行为

```text
WithChatWorkspace(ws): TrimSpace 后写入 ChatConfig
ChatAgent.Run / RunStream:
  encoded = PromptBuilder(MEMORY.md, USER.md)
  非空 → replaceOrInsertFirstSystem
  空 → 消息原样
NewChatAgentHandlerFromConfig → WithChatWorkspace(cfg.Workspace)
```

可观测信号：与 S8 相同——根下有 `MEMORY.md` 时，模型看到的 system 含 `## MEMORY.md` 与正文。

---

## 4. 非目标

- 不把 ChatAgent 换成 ReAct
- 不强制 CLI 空 workspace 拒跑
- 不删 `framework/agent` 一季别名
- 不删 Insights 路由
- 不改 `NewChatStreamHandler`（Portal 走 ReAct）
- 不改 Portal、jaeger/ES、RCA

---

## 5. 成功标准

1. `NewChatAgent(..., WithChatWorkspace(dir))` 且根下有 `MEMORY.md` → 发给模型的 system 含该文件。
2. 空白 workspace 不注入。
3. 无 workspace 时 `TestChatAgent_Run_UsesHistoryAndStoresReply` 仍是历史+当前两条。
4. `cd framework && go test ./harness ./templates ./agent ./config -count=1` 绿。
