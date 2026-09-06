# S6 Whole-Repo Update Reject Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update 不能再把 workspace 写成整仓；空字符串落到 `{data_root}/agents/{id}`。Web 退役行保存发空路径。

**Architecture:** Create 已有 `WorkspaceUnderCodeRoots` 拒绝。Update 在写入前做同一检查；空字符串在 usecase 里与 Create 一样展开默认可写根。省略字段不改列。

**Tech Stack:** Go（portal service/biz）、React（`AgentForm.tsx`）

**规格:** [`2026-09-05-whole-repo-update-reject-design.md`](../specs/2026-09-05-whole-repo-update-reject-design.md)

**分支:** 从 `feature/s5-whole-repo-run-reject` 切 `feature/s6-whole-repo-update-reject`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 拒整仓 | `portal/internal/service/agent.go` `UpdateAgent` |
| 空→默认根 | `portal/internal/biz/agent_usecase.go` `Update` |
| fake 写入 workspace | `portal/internal/service/agent_update_hybrid_test.go` |
| 测 | `portal/internal/service/agent_create_workspace_test.go` |
| 表单 | `web/src/pages/AgentForm.tsx` |

禁止：改 ReAct / PromptBuilder / RCA；扫库迁移；自动 `LinkCode`。

---

### Task 1: Update API

**Files:** `agent.go`、`agent_usecase.go`、`agent_update_hybrid_test.go`、`agent_create_workspace_test.go`

- [x] **Step 1:** `hybridAgentRepo.Update` 应用 `workspace`。测试：`TestUpdateAgent_RejectsWholeRepoWorkspace`；`TestUpdateAgent_EmptyWorkspaceUsesDefault`（`dataRoot` 用 `t.TempDir()`）；`TestUpdateAgent_OmitsWorkspace_KeepsStored`。

- [x] **Step 2:** `UpdateAgent`：`req.Workspace != nil` 且 `WorkspaceUnderCodeRoots` → `ErrWorkspaceWholeRepoRetired`。usecase `Update`：workspace 空则 `{dataRoot}/agents/{id}` + `MkdirAll`。

- [x] **Step 3:** `cd portal && go test ./internal/service ./internal/biz -run "TestUpdateAgent_|TestCreateAgent_Rejects|TestChat_Rejects" -count=1`

- [x] **Step 4: Commit** `fix(portal): reject whole-repo workspace on agent update`

---

### Task 2: Web 退役行保存发空

**Files:** `web/src/pages/AgentForm.tsx`

- [x] `retiredWholeRepo` 时 submit `workspace: ''`；提示写明保存会改成默认可写根，可选再挂 `code/`。

- [x] **Commit** `fix(web): migrate retired whole-repo workspace on save`

---

### Task 3: 回归

- [x] `cd portal && go test ./internal/service ./internal/biz -count=1`（skip 预存 `TestSearchSessionsWithAgentFilterRequiresAgentUse`）
- [x] 不要 merge/push，除非用户明确要求。
