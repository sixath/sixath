# S35 MEA Shell Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从 Agent 表单/详情拆掉已无运行时的 MEA 勾选。

**Architecture:** `RUNTIME_TOOL_FIELDS` 不再列出 `mea_enabled`；proto/biz/DB 死键留下。

**Tech Stack:** TypeScript（`web/src`）、Playwright e2e、Go 源码锁定

**规格:** [`2026-09-05-mea-shell-off-design.md`](../specs/2026-09-05-mea-shell-off-design.md)

**分支:** 从 `feature/s34-remaining-growth-off` 切 `feature/s35-mea-shell-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 改 | `web/src/api/client.ts` |
| 测 | `web/e2e/agent-runtime-tools.spec.ts`、`portal/internal/service/mea_off_test.go` |

禁止：regen proto；改 Channel；合 assembler。

---

### Task 1: 失败锁定测试

- [ ] e2e：`runtime-tool-mea_enabled` 必须不可见
- [ ] `TestChatGo_DoesNotStreamMEA`
- [ ] 先跑 e2e 应失败（勾选仍在）

---

### Task 2: 拆 Web 字段

- [ ] 从 `RuntimeToolsConfig` / `RuntimeToolFlagKey` / `RUNTIME_TOOL_FIELDS` / `normalizeRuntimeTools` 去掉 `mea_enabled`
- [ ] 跑 e2e 与 portal 锁定测试
- [ ] **Commit** `fix(web): drop mea_enabled agent toggle after MEA package removed`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
