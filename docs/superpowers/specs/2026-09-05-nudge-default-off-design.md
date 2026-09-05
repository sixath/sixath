# S18 收口：Growth nudge 默认关闭

**日期**: 2026-09-05  
**状态**: 已确认（S17 leftover；P4 默认路径；2026-09-05 实施）  
**范围**: `framework/growth.DefaultNudgeConfig` 与 Portal `nudgeConfigFromEnv` 缺省。不删 OnToolSuccess API，不改 worker Loop。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S17](./2026-09-05-chat-growthuc-off-design.md)（Chat 已不注入 growthUC）

**一句话**: 阈值 nudge 是可选器官；未设 `SATH_GROWTH_NUDGE_ENABLED` 时不置 pending、不 Wake。

---

## 1. 背景

S12–S17 已把默认 Chat 从 growth 钩子拆开。`OnToolSuccess` / `OnAssistantTurn` 生产路径已无调用者，但：

- `DefaultNudgeConfig()` 仍 `Enabled=true`
- `NewGrowthUsecase` / `nudgeConfigFromEnv` 继承该默认

以后若误接回 Chat，会再次默认点火。P4：Growth 不进默认路径。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `DefaultNudgeConfig().Enabled` | **false** |
| `SATH_GROWTH_NUDGE_ENABLED=1/true/yes/on` | 仍可打开 |
| `OnToolSuccess` / `OnAssistantTurn` API | **保留**（opt-in 调用） |
| 测 pending 的单测 | 显式 `SetNudgeConfig(Enabled: true)` 或 env=true |
| worker / curator / assembler | **不改** |

---

## 3. 行为

```text
DefaultNudgeConfig().Enabled == false
nudgeConfigFromEnv() 未设 env → Enabled false
SATH_GROWTH_NUDGE_ENABLED=true → Enabled true（与现网 env 覆盖一致）
Enabled=false：计数仍前进，不置 pending、不 Wake
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 CuratorWorker / GrowthWorker Loop
- 不合 assembler

---

## 5. 成功标准

1. `DefaultNudgeConfig().Enabled == false`。
2. 未设 `SATH_GROWTH_NUDGE_ENABLED` 时 `nudgeConfigFromEnv().Enabled == false`。
3. `cd framework && go test ./growth -count=1` 绿。
4. `cd portal && go test ./internal/biz ./internal/service -count=1` 绿（skip 预存 `TestSearchSessionsWithAgentFilterRequiresAgentUse` / SQLITE_BUSY）。
