# S14 Dead Chat Stream Wrappers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无调用者的 `NewChatStreamHandler` / `NewChatAgentHandlerWithContext`。

**Architecture:** Portal SSE 不走 templates 这对包装。删文件，不改 ChatAgent / ReAct / Portal Stream。

**Tech Stack:** Go（`framework/templates`）

**规格:** [`2026-09-05-dead-chat-stream-off-design.md`](../specs/2026-09-05-dead-chat-stream-off-design.md)

**分支:** 从 `feature/s13-drop-agent-alias` 切 `feature/s14-dead-chat-stream-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 失败测试 | `framework/templates/chat_stream_off_test.go` |
| 删除 | `framework/templates/chat_stream.go` |

禁止：改 Portal `chat_stream.go`；拆 GrowthWorker；删 procedural。

---

### Task 1: 失败测试

- [ ] `TestChatStreamGoRemoved`：`chat_stream.go` 必须不存在
- [ ] 先跑应失败

---

### Task 2: 删文件

- [ ] `git rm framework/templates/chat_stream.go`
- [ ] `cd framework && go test ./templates ./harness -count=1`
- [ ] **Commit** `fix(templates): remove unused ChatStream handler wrappers`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
