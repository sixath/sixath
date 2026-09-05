# S36 Remaining Dead Shell Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 清掉 HyperTool config、Web `code_*`、发货 Growth worker yaml、Portal code 模型 settings 死壳。

**Architecture:** 源码锁定先行。proto / Channel / MaybeSpill / `growth.llm` 不动。

**Tech Stack:** Go、TypeScript、发货 yaml

**规格:** [`2026-09-05-remaining-dead-shell-off-design.md`](../specs/2026-09-05-remaining-dead-shell-off-design.md)

**分支:** 从 `feature/s35-mea-shell-off` 切 `feature/s36-remaining-dead-shell-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 改 | `web/src/api/client.ts`、`framework/config/config.go`、`portal/configs/config.yaml`、`portal/configs/config.docker.yaml`、`portal/internal/conf/shipped_growth_config_test.go` |
| 删 | `portal/internal/data/portal_settings.go`、`portal/internal/data/portal_settings_test.go` |
| 测 | `portal/internal/service/dead_shell_off_test.go`、`framework/config/hypertool_cfg_off_test.go`、`portal/internal/data/portal_settings_off_test.go` |

禁止：regen proto；改 Channel；改 MaybeSpill；合 assembler。

---

### Task 1: 失败锁定测试

- [ ] `TestWebClientTs_omitsCodeModelFields`
- [ ] `TestConfigGo_omitsHyperTool`
- [ ] `TestShippedConfig_omitsDeadGrowthWorkerKeys`
- [ ] `TestPortalSettingsGoRemoved`
- [ ] 先跑必须红

---

### Task 2: 拆死壳

- [ ] 去掉 Web `code_*`；删 `HyperToolConfig`；发货 yaml 只留 `growth.llm` 注释；删 `portal_settings.go`
- [ ] 跑锁定测试与包测试
- [ ] **Commit** `fix(shelf): drop leftover hypertool config, code-model shell, and growth worker yaml`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
