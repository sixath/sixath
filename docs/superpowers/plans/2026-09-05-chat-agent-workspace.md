# S9 ChatAgent Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ChatAgent 非空 workspace 时用 PromptBuilder 读 `MEMORY.md` / `USER.md`；CLI FromConfig 把 `cfg.Workspace` 交进去。

**Architecture:** 不改成 ReAct。`WithChatWorkspace` + Run/RunStream 共用 `replaceOrInsertFirstSystem`。templates 装配层接线。别名只转发。

**Tech Stack:** Go（`framework/harness`、`framework/templates`）

**规格:** [`2026-09-05-chat-agent-workspace-design.md`](../specs/2026-09-05-chat-agent-workspace-design.md)

**分支:** 从 `feature/s8-cli-templates-workspace` 切 `feature/s9-chat-agent-workspace`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| ChatAgent | `framework/harness/agent.go`、`prompt_prepare.go`、`agent_test.go` |
| 别名转发 | `framework/agent/alias.go` |
| templates | `framework/templates/chat.go`、`from_config.go`、`templates_test.go` |
| CLI 无 ModelName 回退 | `framework/cli/serve.go`（`NewChatAgentHandlerWithWorkspace`） |

禁止：改 Portal；删别名包；ChatAgent→ReAct；Insights。

---

### Task 1: 失败测试

- [x] `TestChatAgent_Run_InjectsWorkspaceMemoryMD`
- [x] `TestChatAgent_Run_BlankWorkspaceSkipsMemoryMD`
- [x] `TestNewChatAgentHandlerWithWorkspace_InjectsMemoryMD`
- [x] 跑测试确认失败

---

### Task 2: 接线

- [x] `WithChatWorkspace`；Run/RunStream 注入 PromptBuilder
- [x] FromConfig / `NewChatAgentHandlerWithWorkspace` / serve 回退
- [x] alias 转发
- [x] `cd framework && go test ./harness ./templates ./agent ./config -count=1`

- [x] **Commit** `fix(harness): wire workspace into ChatAgent prompt`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
