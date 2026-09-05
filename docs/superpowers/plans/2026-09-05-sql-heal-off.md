# S21 SQL Heal Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无调用者的 `HealReadSQL` / `SchemaHealHint`，保留 execute_read「不自动自愈」单测。

**Architecture:** 删 `sql_heal.go` 与其单测。用 `os.Stat` 锁定文件不存在。

**Tech Stack:** Go（`framework/tool/data`）

**规格:** [`2026-09-05-sql-heal-off-design.md`](../specs/2026-09-05-sql-heal-off-design.md)

**分支:** 从 `feature/s20-unwire-hypertool` 切 `feature/s21-sql-heal-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `framework/tool/data/sql_heal_off_test.go` |
| 删除 | `framework/tool/data/sql_heal.go`、`sql_heal_test.go` |

禁止：改 `MaybeSpill`；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestSQLHealGoRemoved`：`os.Stat("sql_heal.go")` 必须失败
- [ ] 先跑应失败

---

### Task 2: 删文件

- [ ] 删除 `sql_heal.go` 与 `sql_heal_test.go`
- [ ] `cd framework && go test ./tool/data ./tool -count=1`
- [ ] **Commit** `fix(tool): drop unused SQL heal after default path unwired`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
