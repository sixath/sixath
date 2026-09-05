# S14 收口：删除无调用者的 ChatStream 包装

**日期**: 2026-09-05  
**状态**: 已确认（S12 leftover；2026-09-05 实施）  
**范围**: `framework/templates/chat_stream.go`。不改 Portal SSE，不拆 GrowthWorker，不删 procedural。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S12](./2026-09-05-unwire-growth-react-opts-design.md)、[S13](./2026-09-05-drop-agent-alias-design.md)

**一句话**: Portal SSE 自己走 ReAct Stream，不经过 `templates.NewChatStreamHandler`；这两个包装零调用，删掉以免假装还有第二条 CLI 流式入口。

---

## 1. 背景

S12 明确 **不删** `NewChatStreamHandler`。现网磁盘事实：

- `framework/templates/chat_stream.go` 只有两个函数：`NewChatStreamHandler`、`NewChatAgentHandlerWithContext`
- `rg --glob '*.go'` 除定义外 **零调用**
- Portal 流式在 `portal/internal/service/chat_stream.go`，与本文件无关
- 仍活着的 CLI Chat 入口是 `NewChatAgentHandler` / `NewChatAgentHandlerWithWorkspace` / `NewChatAgentHandlerFromConfig`

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `chat_stream.go` | **删除**整文件 |
| `NewChatAgentHandler*`（`chat.go` / `from_config.go`） | **保留** |
| Portal SSE | **不改** |
| GrowthWorker / procedural / assembler | **不改** |

---

## 3. 行为

```text
templates.NewChatStreamHandler           → 不存在
templates.NewChatAgentHandlerWithContext → 不存在
templates.NewChatAgentHandler*           → 仍可从 config 装配 ChatAgent
Portal Stream SSE                        → 不变
```

---

## 4. 非目标

- 不拆 GrowthWorker / FinalizeTurnForBackgroundReview
- 不删 `portal/internal/chat/procedural_binding.go`（**S15 已拆**）
- 不合 assembler
- 不改 `middleware.StreamChain` / `StringStreamAdapter`（仍可被别处用）

---

## 5. 成功标准

1. `framework/templates/chat_stream.go` 不存在。
2. 现网 `*.go`（排除 `_neo4j_q`）不含 `NewChatStreamHandler` / `NewChatAgentHandlerWithContext`。
3. `cd framework && go test ./templates ./harness -count=1` 绿。
4. `NewChatAgentHandler` / `NewChatAgentHandlerWithWorkspace` 仍通过现有单测。
