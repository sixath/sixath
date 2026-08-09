# Growth G1：可配置阈值 Nudge

**范围**：`OnToolSuccess` / `OnAssistantTurn` 达阈后置 `pending_*` + `growthwake.Wake()`（异步 `GrowthWorker`）。

**非目标**：Hermes 式「主循环内 fork 复盘 agent、改对话 system prompt」。

## 与 Hermes 的差异

| | Sixath（G1） | Hermes 典型 nudge |
|--|--------------|-------------------|
| 触发 | 计数达阈 → pending + Wake | 主循环内注入 / fork 复盘 |
| 执行 | 后台 Worker 异步跑 Runner | 对话路径内同步或嵌套 agent |
| 关闭 | `Enabled=false` 只封顶计数、不 pending | 常把 interval 设 0 表示内层不 nudge |

G2（会话删除 `TrySessionEnd*`）与 G1 独立：关 nudge 后仍可经 session-end 置 pending。

## 配置

代码默认：`Enabled=true`，间隔 `0` → `growth.NewDefaults()`（技能 10 / 记忆 3）。**禁止**把 interval `0` 当成「每次都触发」。

当前未改 `conf.proto`（避免 Windows protobuf 再生）；用环境变量覆盖：

| 变量 | 含义 |
|------|------|
| `SATH_GROWTH_NUDGE_ENABLED` | `false`/`0`/`no`/`off` 关闭；默认开启 |
| `SATH_GROWTH_NUDGE_SKILL_TOOL_INTERVAL` | `>0` 覆盖技能工具成功间隔；`0`/未设用 Defaults |
| `SATH_GROWTH_NUDGE_MEMORY_TURN_INTERVAL` | 同上，assistant 回合间隔 |

测试/代码也可直接 `GrowthUsecase.SetNudgeConfig(growth.NudgeConfig{...})`。

后续若需 YAML，可在 `Growth` message 增加 `optional bool nudge_enabled` 与 interval 字段，再由 `ProvideGrowthUsecase` 优先读 conf。

## 相关

- Session-end 轻检：[`growth-session-end-skill-review.md`](./growth-session-end-skill-review.md)
- Gap spec G1 行：`docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md`
