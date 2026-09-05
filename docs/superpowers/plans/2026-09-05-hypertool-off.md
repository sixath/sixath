# S26 HyperTool Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无调用者的 HyperTool 实现，保留 skills handler「不接线」单测与 config 死键。

**Architecture:** `os.Stat` 锁定文件不存在。删 `hypertool.go`、测试与 runner。

**Tech Stack:** Go（`framework/tool`、`framework/templates`）

**规格:** [`2026-09-05-hypertool-off-design.md`](../specs/2026-09-05-hypertool-off-design.md)

**分支:** 从 `feature/s25-shelf-family-code-model-off` 切 `feature/s26-hypertool-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/tool/hypertool_off_test.go` |
| 删除 | `framework/tool/hypertool.go`、`hypertool_test.go`、`hypertool_runner.py` |
| 保留 | `framework/templates/hypertool_off_test.go`、`config.HyperTool` |

禁止：删 growth/mea/hub；合 assembler。

---

### Task 1: 失败测试

- [x] `TestHyperToolGoRemoved`：`os.Stat("hypertool.go")` 必须失败
- [x] 先跑应失败

---

### Task 2: 删文件

- [x] 删除 hypertool 实现与 runner
- [x] `cd framework && go test ./tool ./templates -count=1`
- [x] **Commit** `fix(tool): drop unused hypertool after default path unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
