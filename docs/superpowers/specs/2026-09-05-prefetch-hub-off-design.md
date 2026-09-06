# S28 收口：记忆预取不再依赖 Hub

**日期**: 2026-09-05  
**状态**: 已确认（用户继续清货架；器官包第三刀接线；2026-09-05 实施）  
**范围**: `framework/memory/store_prefetch_backend.go` 对 `memory/hub` 的 import。不删 `framework/memory/hub`，不拆 Portal `hub_*.go`。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S24](./2026-09-05-remaining-default-path-off-design.md)（默认 Run 不再注入 prefetch）；[S27](./2026-09-05-mea-off-design.md)

**一句话**: 默认循环已不跑 prefetch；`StorePrefetchBackend` 仍用 Hub loadout 过滤草稿，把 `framework/memory` 焊在器官包上。拆掉这条边。

---

## 1. 背景

S24：`BuildReActAgent` 不再注入 MemoryOrchestrator。现网磁盘：

- `framework/memory` 包根只有 `store_prefetch_backend.go` import `memory/hub/local`
- 用途仅 `prefetchUnitLoadoutEligible`：按 DB `status` + `hub_status` 跳过 draft/stale/superseded/deleted
- Portal `hub_*.go`、HTTP `memory_hub*.go` 仍在（S1 管理面未清干净）；growth 仍 opt-in。二者**不在本刀**。

父规格 P4：默认路径不再接线 hub。本刀只切断 **memory 包 → hub 包** 这条边，不假装能删整个 hub。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `store_prefetch_backend.go` | **不再 import hub**；loadout 过滤改成本地字符串判断，行为与现网一致 |
| `framework/memory/hub` | **保留** |
| Portal Hub HTTP / `hub_*.go` / growth / assembler | **不改 / 不合** |
| `config.MemoryOrchestratorPrefetch` | **保留死键** |

---

## 3. 行为

```text
framework/memory（包根 *.go）→ 不含 github.com/sixath/framework/memory/hub
StorePrefetchBackend 仍跳过 draft / stale / superseded / deleted
默认 Chat 仍不注入 prefetch Orchestrator
```

---

## 4. 非目标

- 不删 `framework/memory/hub` 或 Portal `hub_*.go`
- 不删 `StorePrefetchBackend` / `BuildPrefetchMemoryOrchestrator`（测试与脚本仍引用）
- 不改 `MaybeSpill`、Channel
- 不合 assembler

---

## 5. 成功标准

1. `framework/memory` 包根 `*.go`（不含 `hub/` 子目录）不含 `github.com/sixath/framework/memory/hub`。
2. `TestStorePrefetchBackend_Prefetch_SkipsDraftUnits` 仍通过。
3. `cd framework && go test ./memory -count=1` 绿。
4. `cd portal && go test ./internal/chat ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY）。
