# S5 Whole-Repo Run Reject Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Chat / Stream / 快捷 Chat / ExecuteSkill 拒绝整仓 workspace，与 Create 同一错误码。

**Architecture:** 抽出 `requireRunWorkspace`：先 `RequireWorkspaceRoot`，再 `WorkspaceUnderCodeRoots`。`ChatService` 注入 `codeRoots`。不自动 `LinkCode`、不改库、不拦 Update。

**Tech Stack:** Go（portal service + wire）、React（`AgentForm.tsx` 文案）

**规格:** [`2026-09-05-whole-repo-run-reject-design.md`](../specs/2026-09-05-whole-repo-run-reject-design.md)

**分支:** 从 `feature/portal-assembler` 切 `feature/s5-whole-repo-run-reject`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 共享检查 | `portal/internal/service/workspace_run.go`（新） |
| Agent Run | `portal/internal/service/agent.go` Chat + ExecuteSkill |
| 会话 Run | `portal/internal/service/chat.go` SendMessage + SendMessageStream；`ProvideChatServiceWithTurnTrace` 注入 `codeRoots` |
| wire | `portal/cmd/backend/wire_gen.go` |
| 测 | `portal/internal/service/agent_create_workspace_test.go`、`chat.go` 配套测试 |
| 文案 | `web/src/pages/AgentForm.tsx` |

禁止：改 ReAct / PromptBuilder / RCA；自动 `LinkCode`；拦 Update；改 `framework/templates` CLI。

---

### Task 1: `requireRunWorkspace` + Agent Chat/ExecuteSkill

**Files:** `workspace_run.go`、`agent.go`、`agent_create_workspace_test.go`

- [x] **Step 1:** 新增 `TestChat_RejectsWholeRepoWorkspace`（仿 `TestChat_RejectsEmptyWorkspace`，workspace 在 `codeRoots` 下，reason=`WORKSPACE_WHOLE_REPO_RETIRED`）。`TestExecuteSkill_RejectsWholeRepoWorkspace` 同判据。

- [x] **Step 2:** 实现 `requireRunWorkspace`；Chat / ExecuteSkill 改用它。

- [x] **Step 3:** `cd portal && go test ./internal/service -run "TestChat_Rejects|TestExecuteSkill_Rejects|TestCreateAgent_Rejects" -count=1`

- [x] **Step 4: Commit** `fix(portal): reject whole-repo workspace on agent run`

---

### Task 2: ChatService + wire + UI

**Files:** `chat.go`、`wire_gen.go`、`AgentForm.tsx`、SendMessage 测试

- [x] **Step 1:** `ChatService.codeRoots` + `SetCodeRoots`；`ProvideChatServiceWithTurnTrace` 增加 `codeRoots []string`。SendMessage / Stream 用 `requireRunWorkspace`。

- [x] **Step 2:** `TestSendMessage_RejectsWholeRepoWorkspace`：会话绑定整仓 Agent，在写消息前失败。

- [x] **Step 3:** 更新 `wire_gen.go`（`go generate` 或手改：把已有 `v` 传入 Provide）。AgentForm 退役文案改为不能再跑对话。

- [x] **Step 4:** `cd portal && go test ./internal/service -count=1`；`cd portal/cmd/backend && go build -o NUL .`（确认 wire 编译）

- [x] **Step 5: Commit** `fix(portal): reject whole-repo workspace on chat run`

---

### Task 3: 回归

- [x] `cd portal && go test ./internal/chat ./internal/service -count=1`（skip 预存 SQLITE_BUSY）
- [x] 不要开始下一切片。不要 merge/push，除非用户明确要求。
