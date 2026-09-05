# S19 Growth YAML Defaults Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 发货 `config.yaml` / `config.docker.yaml` 不再把 Growth LLM 复盘、C2s、learnings 打开。

**Architecture:** 改两份 yaml 布尔值。用 `internal/conf` 单测读磁盘文件锁定默认。不改 proto、不改 worker。

**Tech Stack:** YAML + Go 测试（`go.yaml.in/yaml/v2`）

**规格:** [`2026-09-05-growth-yaml-defaults-off-design.md`](../specs/2026-09-05-growth-yaml-defaults-off-design.md)

**分支:** 从 `feature/s18-nudge-default-off` 切 `feature/s19-growth-yaml-defaults-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测试 | `portal/internal/conf/shipped_growth_config_test.go` |
| 发货配置 | `portal/configs/config.yaml`、`portal/configs/config.docker.yaml` |

禁止：改 proto；删 Growth 包；合 assembler。

---

### Task 1: 失败测试

- [ ] `TestShippedConfig_growthReviewFlagsOff` 读 `../../configs/config.yaml` 与 `config.docker.yaml`
- [ ] 断言 `llm_review_enabled` / `session_end_skill_review_enabled` / `learnings_review_enabled` 为 false
- [ ] 同时锁定已关的 `worker_enabled` / `curator_enabled` / `session_end_memory_review_enabled` / `combined_review_enabled`
- [ ] 先跑应失败

---

### Task 2: 改 yaml

- [ ] 两份文件三项改为 false，更新注释说明 opt-in
- [ ] `cd portal && go test ./internal/conf -count=1`
- [ ] **Commit** `fix(portal): default shipped growth review flags off`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
