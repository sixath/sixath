# Workspace First Class Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]` syntax for tracking.

**Goal:** 每个新建 Agent 必有默认可写 workspace；空 root 拒跑；新建不再把整仓当 workspace；`rca_code`/`rca_symbol` 在存在 `workspace/code` 时以该目录为根。

**Architecture:** 沿用模式 C：可写根 `{data_root}/agents/{id}/`，可选 `workspace/code` 挂代码。P2 **仅拦新建**整仓；已有整仓行仍可打开。空字符串在 Run 入口拒绝。不搬家 `framework/harness`，不删调查闸。

**Tech Stack:** Go（portal biz/service/chat）、React（`web/src/pages/AgentForm.tsx`）

**规格:** [`2026-09-05-agent-model-workspace-harness-design.md`](../specs/2026-09-05-agent-model-workspace-harness-design.md) §7.4 / §11 P2

---

## File map

| Path | 动作 |
|------|------|
| `portal/internal/biz/agent_usecase.go` | Create 后 `MkdirAll`；`RequireWorkspaceRoot` |
| `portal/internal/service/agent.go` | 新建拒绝整仓；Chat 拒空 root；BuildRegistry 传 workspace |
| `portal/internal/service/chat.go` | SendMessage / Stream 在写 user 消息前拒空 root；BuildRegistry 传 workspace |
| `portal/internal/chat/code_roots.go` | `ResolveWorkspaceCodeRoot` / `MergeRCARoots` |
| `portal/internal/chat/rca_builder.go` | `registerRCATool(reg, cfg, workspace)` 用 MergeRCARoots |
| `portal/internal/chat/agent_builder.go` | `RegistryBuildOptions.Workspace` |
| `web/src/pages/AgentForm.tsx` | 去掉整仓单选；新建不强制挂 code |
| `web/src/pages/ToolForm.tsx` | roots 文案：有 `workspace/code` 时以挂载为准 |

禁止：迁移已有整仓行；删 investigation/task_lock/turn_surface；改 `_neo4j_q/`。

**rca waiver:** 无 `workspace/code` 时仍用工具配置里的 `roots`（已有 RCA Agent 不挂载也能跑）。有 `workspace/code` 时 **只用** 该目录，忽略独立 roots。

---

### Task 1: `workspace/code` 解析与 RCA 根合并

**Files:**
- Modify: `portal/internal/chat/code_roots.go`
- Test: `portal/internal/chat/code_roots_test.go`

- [ ] **Step 1: 写失败测试** `TestResolveWorkspaceCodeRoot` / `TestMergeRCARoots_PrefersCodeMount`

- [ ] **Step 2: 实现** `ResolveWorkspaceCodeRoot`（`code` 为目录或指向目录的 symlink）与 `MergeRCARoots`

- [ ] **Step 3:** `cd portal && go test ./internal/chat -run "TestResolveWorkspaceCodeRoot|TestMergeRCARoots" -count=1` PASS

- [ ] **Step 4: Commit** `feat(chat): resolve rca roots from workspace/code`

---

### Task 2: 注册 RCA 时带上 workspace

**Files:**
- Modify: `rca_builder.go`、`agent_builder.go`、`service/chat.go`、`service/agent.go`、相关测试

- [ ] `registerRCATool(reg, cfg, workspace string)`；`RegistryBuildOptions.Workspace`
- [ ] 测试：有 `code/` 且配置 roots 为空仍注册；有 `code/` 时忽略配置 roots
- [ ] `cd portal && go test ./internal/chat -count=1 -skip TestNotifySessionMessageIndexed_WithDetachedCaller`

---

### Task 3: 空 root 拒跑

**Files:**
- Modify: `portal/internal/biz/agent_usecase.go`（导出 `ErrWorkspaceRequired` + `RequireWorkspaceRoot`）
- Modify: `chat.go` SendMessage（存 user 消息前）、SendMessageStream（GetForSession 后）、`agent.go` Chat

- [ ] 测试 `TestRequireWorkspaceRoot`
- [ ] `cd portal && go test ./internal/biz ./internal/service -count=1 -run RequireWorkspace`

---

### Task 4: 新建默认目录 + 拒绝整仓

**Files:**
- Modify: `agent_usecase.go` Create `MkdirAll`
- Modify: `agent.go` CreateAgent：`WorkspaceUnderCodeRoots` → `ErrWorkspaceWholeRepoRetired`
- Test: 扩展 `TestAgentCreateCreatesPrivateResourceForCaller`（TempDir + 目录存在）；`TestCreateAgent_RejectsWholeRepoWorkspace`

- [ ] 已有整仓 **Update 不拦**（仅拦 Create）
- [ ] `cd portal && go test ./internal/biz ./internal/service -count=1`

---

### Task 5: Web 去掉整仓选项

**Files:**
- Modify: `web/src/pages/AgentForm.tsx`
- Modify: `web/src/pages/ToolForm.tsx` 提示

- [ ] 删除 `WorkspaceMode` / 整仓 radio；浏览只设置 `selectedTarget`
- [ ] 新建不强制选择代码目录
- [ ] 编辑时若 workspace 落在 code_roots 下，显示退役说明，不改路径

---

### Task 6: 回归

- [ ] `cd portal && go test ./internal/chat/... ./internal/service/... ./internal/biz/... -count=1 -skip TestNotifySessionMessageIndexed_WithDetachedCaller`
- [ ] 对照：Create 空 workspace → `{data_root}/agents/{id}` 且目录存在；Create 整仓路径 400；Run 空字符串 400；`workspace/code` 存在时 rca 用该根

明确不做：P3 调查闸；强制迁移旧行；包搬家。
