# S15 收口：procedural 退出 Portal 装配

**日期**: 2026-09-05  
**状态**: 已确认（S1 leftover；父规格 §6.2 / §6.3；2026-09-05 实施）  
**范围**: Portal `SetProceduralRepairConfig` 与 `procedural_*.go` 装配。不删 `framework/memory` procedural，不拆 GrowthWorker。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)（预取已不再注入）

**一句话**: 预取已经不注入 procedural；Portal 也不再从 `agent_extra` 装 catalog / auto-commit。five-gate 留在 framework 包里，不进默认外壳。

---

## 1. 背景

S1：默认预取不再设 `ProceduralBindings`；「无剩余引用时删 `procedural_binding.go`」。现网仍有引用：

- `SetPortalAgentExtra` 调 `SetProceduralRepairConfig`（`extra.yaml` 一旦 `enabled` 就进 catalog）
- `DefaultFailureSignalSink` 把 `ProceduralCatalogSink` 挂上 turnBus
- Portal 单测走 auto_commit 全路径

父规格 §6.2 CUT `procedural_*.go`；§6.3 five-gate **留包、移出默认装配**。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Portal catalog / auto-commit / Disable* | **删除** |
| `SetPortalAgentExtra` | **不再**调用 `SetProceduralRepairConfig` |
| `DefaultFailureSignalSink` | **保留** Logging + Ring；**不**挂 Catalog |
| 预取 | 维持 S1：不注入 `ProceduralBindings` / `LoadPersistedProcedural` |
| `framework/memory` procedural | **不删** |
| `config.MemoryProceduralRepair` | **保留解析**（旧 yaml 不炸）；运行时忽略 |
| GrowthWorker / assembler | **不改** |

---

## 3. 行为

```text
SetPortalAgentExtra(extra.MemoryProceduralRepair) → 忽略
DefaultFailureSignalSink → Logging + Ring（无 catalog）
failure signal → 只记日志/ring，不 activate、不 auto_commit
BuildPrefetchMemoryOrchestrator → ProceduralBindings 仍为空
framework/memory.CommitProceduralRepair → 仍可被测试/以后的器官直接调用
```

---

## 4. 非目标

- 不拆 GrowthWorker / FinalizeTurnForBackgroundReview
- 不删 `framework/memory` procedural 类型与 five-gate
- 不删 `config.MemoryProceduralRepair` 字段（避免旧配置反序列化失败）
- 不合 assembler

---

## 5. 成功标准

1. `portal/internal/chat/procedural_binding.go` 不存在。
2. `portal_agent_extra.go` 不含 `SetProceduralRepairConfig`。
3. 默认预取仍不注入 procedural。
4. `DefaultFailureSignalSink()` 非 nil；`cd portal && go test ./internal/chat ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY）。
