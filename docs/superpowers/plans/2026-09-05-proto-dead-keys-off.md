# S37 Proto Dead Keys Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** regen proto，去掉 Agent / conf 死字段；保留 `growth.llm` 与 Channel 路由。

**Architecture:** 先锁 proto 源文件不含死键，再改 `.proto`、`make config && make api`，修 biz/data/service 编译与测试。

**Tech Stack:** protobuf、Go、发货 yaml

**规格:** [`2026-09-05-proto-dead-keys-off-design.md`](../specs/2026-09-05-proto-dead-keys-off-design.md)

**分支:** 从 `feature/s36-remaining-dead-shell-off` 切 `feature/s37-proto-dead-keys-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 改 | `portal/api/agent/v1/agent.proto`、`portal/internal/conf/conf.proto`、生成的 `*.pb.go`、`biz`/`data`/`service` 映射、发货 yaml、`shipped_growth_config_test.go` |
| 测 | `portal/internal/conf/proto_dead_keys_off_test.go` |

禁止：改 Channel proto；改 MaybeSpill；合 assembler。

---

### Task 1: 失败锁定测试

- [ ] `TestAgentProto_omitsDeadKeys`
- [ ] `TestConfProto_omitsDeadGrowthAndSkillRouteKeys`
- [ ] 先跑必须红

---

### Task 2: 改 proto 并修好调用方

- [ ] 删字段 + reserved；`make config` / `make api`
- [ ] 去掉 biz/data/service 死映射；Update 只保留 `hybrid_recall` presence
- [ ] 发货 yaml 去掉 skills 预注入死键
- [ ] **Commit** `fix(proto): drop leftover mea/hub/code-model and growth worker fields`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
