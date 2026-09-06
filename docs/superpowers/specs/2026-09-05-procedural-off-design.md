# S40 收口：删除 procedural five-gate

**日期**: 2026-09-05  
**状态**: 已确认（用户在 S39 后要求开刀；废止 S15「留包」）  
**范围**: `framework/memory` 的 binding / catalog / commit；prefetch 注入；`config.MemoryProceduralRepair`。保留 `KindProcedural` 历史过滤、`Remember` 拒写、FailureSignal。不合 assembler（本刀从 assembler 切出）。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S15](./2026-09-05-procedural-portal-off-design.md)；[S39](./2026-09-05-credential-solicitation-off-design.md)

**一句话**: Portal 已不装配 procedural；five-gate 只被自己的单测和 prefetch 可选字段调用。删掉，以免假装还能 auto-commit 修复槽。

---

## 1. 背景

S15 锁定：Portal catalog / auto-commit 删除；**`framework/memory` procedural 不删**；yaml 字段留下以免旧配置炸。磁盘（`rg`，排除 `_neo4j_q`）：

| leftover | 现网 |
|----------|------|
| Portal `procedural_*.go` / `SetProceduralRepairConfig` | **不存在** |
| `CommitProceduralRepair` / `ProceduralCatalog` / `MatchProceduralBindings` | 只活在 `framework/memory` 与 e2e 单测 |
| prefetch `ProceduralBindings` | 默认空；字段仍可 opt-in 注入 |
| `config.MemoryProceduralRepair` | 解析 yaml，运行时忽略 |
| `KindProcedural` 默认 recall 排除 / `ErrProceduralRememberBlocked` | **仍**挡住历史行与裸 Remember |
| FailureSignal / Logging+Ring | **仍**给失败信号，不是 five-gate |

父规格 §6.3：five-gate 移出默认、不重写。S15 留包的 waiver 本刀关掉。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `procedural_commit.go` / `procedural_binding.go` / `procedural_catalog.go` 及单测 | **删除** |
| prefetch 的 `ProceduralBindings` / `LoadPersistedProcedural` / 匹配注入 | **删除** |
| `config.MemoryProceduralRepair` 及 extra yaml 死键 | **删除**（未知 yaml 键忽略） |
| `IsPilotAgent` / `ErrProceduralCommitRejected` / procedural meta 常量 | **删除** |
| `KindProcedural` + 默认 recall 排除 + `ErrProceduralRememberBlocked` | **保留**（历史 `memory_units` 不 DROP、不进默认 Context） |
| FailureSignal / episode buffer / Logging+Ring | **保留** |
| Channel / `MaybeSpill` / assembler 合入 | **不改**（本刀单独分支） |

---

## 3. 行为

```text
CommitProceduralRepair / ProceduralCatalog / MatchProceduralBindings → 不存在
prefetch 不再产出 label=procedural 的围栏块
agent_extra.yaml 的 procedural_repair 键被忽略
Remember(kind=procedural) 仍失败
默认 Recall 仍排除 kind=procedural 历史行
FailureSignal 仍可记日志/ring
```

---

## 4. 非目标

- 不 DROP `memory_units`、不擦历史 procedural 行
- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill` / `hybrid_recall`
- 不删 FailureSignal
- 不合回 assembler，除非用户明确要求

---

## 5. 成功标准

1. `framework/memory/procedural_commit.go`、`procedural_binding.go`、`procedural_catalog.go` 不存在。
2. `StorePrefetchBackend` 不含 `ProceduralBindings`。
3. `framework/config` 不含 `MemoryProceduralRepair`。
4. `cd framework && go test ./memory ./config ./harness -count=1` 绿。
5. `cd portal && go test ./internal/chat ./internal/data -count=1` 绿（skip 预存 SQLITE_BUSY）。
