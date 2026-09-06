# S4 RCA Code Mount Only Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rca_code` / `rca_symbol` 只使用 Agent 的 `workspace/code`；关掉 P2 无挂载时回退到工具 `roots` 的 waiver。

**Architecture:** `MergeRCARoots` 忽略 configured；无 mount 返回 nil，现有 `registerRCATool` 空 roots skip 逻辑保持。jaeger / es_log 不动。整仓 workspace 仍不强制迁移。

**Tech Stack:** Go（portal chat）、React（`ToolForm.tsx` 文案）

**规格:** [`2026-09-05-rca-code-mount-only-design.md`](../specs/2026-09-05-rca-code-mount-only-design.md)

**分支:** 从 `feature/s3-harness-workspace-rename` 切 `feature/s4-rca-code-mount-only`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 关掉 waiver | `portal/internal/chat/code_roots.go` `MergeRCARoots` |
| 测 Merge | `code_roots_test.go`：无挂载忽略 configured |
| 测注册 | `rca_builder_test.go`、`rca_binding_acceptance_test.go`、`agent_builder_test.go`：无 mount + 有 roots → 不注册；有 `code/` 才注册 |
| 表单 | `web/src/pages/ToolForm.tsx`：rca_code/symbol 不再写「无挂载后备」 |

禁止：改 ReAct / PromptBuilder / 包名；自动 `LinkCode`；改 jaeger/ES；删 `framework/tool` rca 实现；改 `framework/templates` CLI 装配（非 Portal Agent workspace）。

---

### Task 1: `MergeRCARoots` 忽略 configured

**Files:** `portal/internal/chat/code_roots.go`、`code_roots_test.go`

- [ ] **Step 1:** 把 `TestMergeRCARoots_WaiverWithoutMount` 改成断言 `nil`/空（有配置 roots 也一样）。保留 PrefersCodeMount。

- [ ] **Step 2:** `MergeRCARoots` 有 mount 返回 `[]string{code}`，否则 `nil`。第二参数保留但忽略。

- [ ] **Step 3:** `cd portal && go test ./internal/chat -run "TestMergeRCARoots" -count=1`

- [ ] **Step 4: Commit** `fix(chat): stop using tool rca.roots as workspace/code fallback`

---

### Task 2: 注册路径与表单

**Files:** `rca_builder_test.go`、`rca_binding_acceptance_test.go`、`agent_builder_test.go`、`ToolForm.tsx`

- [ ] **Step 1:** 无 workspace/code、仅配置 roots 的 `registerRCATool` / `BuildRegistry` 用例改为 expect skip。成功用例改为 `Mkdir(workspace/code)` 并传 `RegistryBuildOptions{Workspace}`。

- [ ] **Step 2:** ToolForm：`rca_code`/`rca_symbol` 不再提示 roots 后备；说明未挂载则不注册。不必清库里的 `roots` 字段。

- [ ] **Step 3:** `cd portal && go test ./internal/chat -count=1`

- [ ] **Step 4: Commit** `fix(portal): register rca_code only from workspace/code`

---

### Task 3: 回归

- [ ] `cd portal && go test ./internal/chat ./internal/service -count=1`（skip 预存 SQLITE_BUSY）
- [ ] 不要开始下一切片。不要 merge/push，除非用户明确要求。
