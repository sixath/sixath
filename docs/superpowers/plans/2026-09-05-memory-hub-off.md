# S30 Memory Hub Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无外部调用者的 `framework/memory/hub` 包。

**Architecture:** 在 `framework/memory/hub_off_test.go` 用 `os.Stat("hub")` 锁定目录不存在，然后 `git rm -r framework/memory/hub`。

**Tech Stack:** Go（`framework/memory`）

**规格:** [`2026-09-05-memory-hub-off-design.md`](../specs/2026-09-05-memory-hub-off-design.md)

**分支:** 从 `feature/s29-portal-hub-shell-off` 切 `feature/s30-memory-hub-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/memory/hub_off_test.go` |
| 删除 | `framework/memory/hub/` |

禁止：删 growth；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestHubPackageRemoved`：`hub` 目录必须不存在
- [ ] 先跑应失败

---

### Task 2: 删目录

- [ ] `git rm -r framework/memory/hub`
- [ ] `cd framework && go test ./memory ./harness ./tool ./templates -count=1`
- [ ] **Commit** `fix(memory): drop unused hub package after portal shell unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
