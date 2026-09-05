# S15 Procedural Portal Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Portal 不再装配 procedural catalog / auto-commit；failure sink 只留 Logging+Ring。

**Architecture:** five-gate 留在 `framework/memory`。Portal 只拆接线。旧 `MemoryProceduralRepair` yaml 仍能解析，运行时忽略。

**Tech Stack:** Go（portal `internal/chat`）

**规格:** [`2026-09-05-procedural-portal-off-design.md`](../specs/2026-09-05-procedural-portal-off-design.md)

**分支:** 从 `feature/s14-dead-chat-stream-off` 切 `feature/s15-procedural-portal-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 失败测试 | `portal/internal/chat/procedural_portal_off_test.go` |
| sink 搬家 | 新建 `portal/internal/chat/failure_signal_sink.go`（Logging+Ring，无 catalog） |
| 拆接线 | `portal/internal/chat/portal_agent_extra.go` |
| 删除 | `procedural_binding.go`、`procedural_auto_commit_test.go`、`procedural_e2e_test.go`；`procedural_binding_test.go` 的 Disable 用例 |
| 预取断言 | 保留「默认不注入」到 `procedural_portal_off_test.go` 或 prefetch 测试 |
| sink 测试 | `failure_signal_sink_test.go` 去掉 `SetProceduralRepairConfig` |

禁止：删 `framework/memory` procedural；改 GrowthWorker；合 assembler。

---

### Task 1: 失败测试

- [x] `TestProceduralBindingGoRemoved`
- [x] `TestPortalAgentExtraGo_DoesNotCallSetProceduralRepairConfig`
- [x] 先跑应失败

---

### Task 2: 拆接线

- [x] sink 无 catalog；extra 忽略 `MemoryProceduralRepair`
- [x] 删 Portal procedural 装配与只测该路径的测试
- [x] `cd portal && go test ./internal/chat ./internal/service -count=1`
- [x] **Commit** `fix(portal): unwire procedural catalog from agent extra`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
