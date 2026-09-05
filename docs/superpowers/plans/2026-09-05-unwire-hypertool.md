# S20 Unwire HyperTool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 默认 CLI skills handler 不再注册 HyperTool、不再注入 HyperTool prompt。

**Architecture:** 删 `skills_handler.go` 里的装配调用与 `buildSkillsAwareSystemPrompt`。保留 `tool.RegisterHyperTool`。源码锁定测试。

**Tech Stack:** Go（`framework/templates`、`framework/tool`）

**规格:** [`2026-09-05-unwire-hypertool-design.md`](../specs/2026-09-05-unwire-hypertool-design.md)

**分支:** 从 `feature/s19-growth-yaml-defaults-off` 切 `feature/s20-unwire-hypertool`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/templates/hypertool_off_test.go` |
| 装配 | `framework/templates/skills_handler.go` |

禁止：删 `hypertool.go`；改 Portal Chat；合 assembler。

---

### Task 1: 失败测试

- [x] `TestSkillsHandlerGo_doesNotWireHyperTool`：读 `skills_handler.go`，不得含 `RegisterHyperTool` / `HyperToolPromptSnippet`
- [x] 先跑应失败

---

### Task 2: 拆装配

- [x] 去掉 `RegisterHyperTool`；system prompt 走 `BuildSkillsAwarePrompt`；删除 `buildSkillsAwareSystemPrompt`
- [x] `cd framework && go test ./templates ./tool -count=1`
- [x] **Commit** `fix(templates): unwire hypertool from default skills handler`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
