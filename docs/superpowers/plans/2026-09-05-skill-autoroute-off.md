# S22 Skill Auto-Route Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 发货 yaml 不再把 `skills.auto_route_enabled` 写成 true。

**Architecture:** 改两份 yaml。用 `internal/conf` 单测读磁盘锁定。不改 Channel、不 regen proto。

**Tech Stack:** YAML + Go 测试

**规格:** [`2026-09-05-skill-autoroute-off-design.md`](../specs/2026-09-05-skill-autoroute-off-design.md)

**分支:** 从 `feature/s21-sql-heal-off` 切 `feature/s22-skill-autoroute-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/conf/shipped_growth_config_test.go`（同文件加 Skills 断言） |
| 发货配置 | `portal/configs/config.yaml`、`config.docker.yaml` |

禁止：改 Channel auto_route；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestShippedConfig_skillAutoRouteOff`
- [ ] 先跑应失败

---

### Task 2: 改 yaml

- [ ] 两份文件 `auto_route_enabled: false`，注释写明 P3 已拆预注入
- [ ] `cd portal && go test ./internal/conf -count=1`
- [ ] **Commit** `fix(portal): default shipped skill auto_route off`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
