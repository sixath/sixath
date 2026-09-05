# S16 收口：C3 BackgroundReview 退出默认 Chat 装配

**日期**: 2026-09-05  
**状态**: 已确认（P4 leftover；S12/S15 非目标；2026-09-05 实施）  
**范围**: Portal ChatService 的 after-turn C3 钩子、`SATH_BACKGROUND_REVIEW` 默认值、GrowthWorker 是否默认构造。不删 `framework/growth`，不拆 worker poll Loop。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: P4（worker poll 默认 false）、[S12](./2026-09-05-unwire-growth-react-opts-design.md)（默认 Run 已不经 growth 选项）

**一句话**: 成长复盘是可选器官；默认对话回合不再 FinalizeTurn / spawn fork。Worker 只在 `growth.worker_enabled` 时构造并轮询。

---

## 1. 背景

P4：Growth 不进默认路径；`worker_enabled` 未配置时 **false**。现网 leftover：

| 事实 | 问题 |
|------|------|
| `SATH_BACKGROUND_REVIEW` 未设置 → **true** | 与 P4 相反 |
| `provideGrowthWorker` **总是** `NewGrowthWorker` | 注释写「给 C3 SpawnBackgroundReview」 |
| `chat.go` **从不**调用 `afterTurnBackgroundReview` | C3 Chat 钩子是死接线 |
| `SetBackgroundReviewer` 零调用 | `bgReviewer` 永远 nil |

Worker 的 `spawnReviewAgent` / `runForkReviewAgent` 仍给 **opt-in poll Loop** 用，要留。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `SATH_BACKGROUND_REVIEW` 未设置 | **false** |
| `provideGrowthWorker` | `!workerEnabled` → **nil** |
| ChatService `afterTurnBackgroundReview` / `SetBackgroundReviewer` / spawn-once | **删除** |
| `GrowthWorker.SpawnBackgroundReview`（C3 入口） | **删除**（Loop 走 `spawnReviewAgent`） |
| `runForkReviewAgent` | **保留**（worker fork） |
| `framework/growth` / GrowthUsecase / FinalizeTurn* | **不删** |
| ChatService 仍注入 `growthUC` | **保留字段**（避免本刀 regen wire） |
| CuratorWorker | **不改** |
| assembler | **不合** |

---

## 3. 行为

```text
默认进程:
  BackgroundReviewEnabled == false
  GrowthWorker == nil（除非 YAML/env worker_enabled）
  SendMessage / Stream 不 FinalizeTurn、不 spawn fork

growth.worker_enabled == true:
  构造 worker，newApp 启动 Loop（与现网一致）
```

---

## 4. 非目标

- 不删 `framework/growth`
- 不改 CuratorWorker
- 不把 `growthUC` 移出 `NewChatService` 签名（**S17 已拆**）
- 不改 `DefaultNudgeConfig`（framework 包默认）
- 不合 assembler

---

## 5. 成功标准

1. 未设 `SATH_BACKGROUND_REVIEW` 时 `backgroundReviewEnabledFromEnv() == false`。
2. `chat.go` 不含 `afterTurnBackgroundReview`。
3. `background_review.go` 不含 `afterTurnBackgroundReview` / `SetBackgroundReviewer`。
4. `provideGrowthWorker` 在 `!workerEnabled` 时返回 nil。
5. `cd portal && go test ./internal/biz ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY）。
