# Growth 空闲轮询（Task12）

Growth worker 在消费 `pending_skill_review` / `pending_memory_review` 之外，会周期性对**无 pending** 的活动会话做轻量 **memory-only** 复盘（不产出技能 patch）。

## 行为

1. **Ticker**：默认每 **10 分钟** 触发一轮 `sweepIdle`（`SATH_GROWTH_IDLE_SWEEP` 或 `growth.idle_sweep_interval` 可改，范围 30s–24h）。
2. **入选条件**：`chat_growth_states` 中 `pending_skill_review=0` 且 `pending_memory_review=0`，且 `last_idle_check_at` 为空或早于 **10 分钟**（`growth.idle_check_interval` / `framework/growth.NewDefaults().IdleCheckInterval`）。
3. **执行**：抢 workspace 租约 → `Runner` 仅 `PendingMemory=true` → `NotifyMemorySessionDirty` → `MarkIdleCheckDone` 更新 `last_idle_check_at`（成功或失败均标记，避免热循环）。
4. **与 pending 轮询独立**：pending 队列默认 **45s** 轮询（`SATH_GROWTH_WORKER_POLL` / `growth.worker_poll_interval`）；置 pending 时仍会 `growthwake.Wake()` 立即多跑一轮。

## 会话结束轻检（C2，可选）

`growth.session_end_memory_review_enabled: true` 时，每次 assistant 消息落库后：

- 若 `OnAssistantTurn` **未**因达阈值而置 `pending_memory_review`；
- 且当前无 skill/memory pending；
- 且 `turns_since_memory_review > 0` 或 `tool_iters_since_review > 0`；

则置 `pending_memory_review` 并 `growthwake.Wake()`，由 worker 走与阈值路径相同的 memory-only 复盘。**默认 false**，避免每轮对话都触发记忆刷新。

## 配置示例（`config.yaml`）

```yaml
growth:
  worker_enabled: true
  worker_poll_interval: 45s
  idle_sweep_interval: 10m
  idle_check_interval: 10m
```

环境变量（YAML 未设置时作为 poll/idle sweep 的回退）：`SATH_GROWTH_WORKER_POLL`、`SATH_GROWTH_IDLE_SWEEP`。

## 相关代码

- `portal/internal/service/growth_worker.go` — `Loop` / `sweepIdle`
- `portal/internal/data/growth_mysql.go` — `ListIdleSessions`
- `framework/growth/config.go` — 默认阈值
