# Growth Curator R2b（workspace 清扫）

**范围**：周期性合并/整理 `workspace/skills/**`。技能 frontmatter `name` 改名后，由 **R2c** 自动反写同 workspace 下 agent 的 `cron_tasks.skill_execute` payload（见 `portal/docs/growth-cron-rewrite-r2c.md`）。

## 启用

```yaml
growth:
  curator_enabled: true
  curator_interval: 168h      # 同 workspace 最小间隔
  curator_poll_interval: 1h   # 扫描 agent workspace 列表
  curator_min_skills: 2
  # 二选一或同时（LLM 优先）：
  curator_llm_enabled: true
  growth:
    llm: { provider: ..., model: ..., api_key: ... }
  # curator_patch_file: "curator_patch.example.json"  # 假 Curator，默认 []
```

环境变量：`SATH_GROWTH_CURATOR_PATCH_FILE`（patch 文件路径）。

## 行为

1. `CuratorWorker` 按 `curator_poll_interval` 列出所有 agent 的 `workspace`（去重）。
2. 若距 `growth_curator_states.last_curator_at` 已超过 `curator_interval`，抢 `growth_workspace_leases` 租约。
3. `framework/growth.CuratorRunner`：技能索引 → LLM/文件 patch → `ApplyPatchBatch` → index generation bump。
4. 成功写 `last_curator_at`；失败记 `last_error` 且不推进时间（可重试）。

## 迁移

`portal/migrations/004_growth_curator_states.sql`（或 AutoMigrate `GrowthCuratorState`）。

## 指标

`GET /api/v1/growth/metrics` 增加 `curator_runs`、`curator_failed`。

## 关联

- 可行性：[framework/docs/superpowers/specs/2026-05-18-growth-r1-r3-feasibility.md](../../framework/docs/superpowers/specs/2026-05-18-growth-r1-r3-feasibility.md) §R2b
