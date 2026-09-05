# S25 Shelf Family Code-Model Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除零调用货架函数，并拆掉 FamilyCode 切模与对应外壳。

**Architecture:** 源码锁定测试先行。删文件与接线。proto/DB 死键留下。器官包不删。

**Tech Stack:** Go（portal chat/service/server）+ Web 设置/Agent 表单

**规格:** [`2026-09-05-shelf-family-code-model-off-design.md`](../specs/2026-09-05-shelf-family-code-model-off-design.md)

**分支:** 从 `feature/portal-assembler` 切 `feature/s25-shelf-family-code-model-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/chat/shelf_off_test.go`、`portal/internal/server/http_code_model_off_test.go` |
| 删除 | `tool_families.go`、`code_model.go`、`code_model_settings.go` 及对应单测 |
| 接线 | `agent_builder.go`、`chat.go`、`portal_agent_extra.go`、`memory_extract.go`、`memory_graph.go`、`http.go` |
| Web | `SettingsPage.tsx`、`AgentForm.tsx`、`AgentDetail.tsx`、`client.ts` |

禁止：删 growth/mea/hub/hypertool；合 assembler。

---

### Task 1: 失败测试

- [x] 源码锁定：上述文件/字符串不得存在
- [x] 先跑应失败

---

### Task 2: 实现

- [x] 删文件与接线
- [x] `cd portal && go test ./internal/chat ./internal/service ./internal/server -count=1`
- [x] **Commit** `fix(portal): drop unused shelf family and code-model switch`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
