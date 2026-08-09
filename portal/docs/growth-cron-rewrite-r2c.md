# Growth R2c：cron skill_execute 引用反写

**关联**：`framework/growth/skill_rename.go`、`portal/internal/biz/cron_ref_rewrite.go`、可行性文档 §4.3。

## 触发时机

在以下路径 **成功 `ApplyPatchBatch` 且 patch 含 SKILL.md frontmatter `name` 变更** 后执行：

1. **Growth 技能复盘**（`SkillReviewRunner`）
2. **Curator 清扫**（`CuratorRunner`）

仅更新 `payload_kind = skill_execute` 的任务；`agent_turn` 自然语言任务不处理。

## payload 契约

```
{name}/scripts/{filename}
```

首段 `{name}` 须与 `skills.Index` 的 frontmatter `name` 一致（非目录名）。

## 范围

按 **workspace** 查找全部 `agents.id`，再更新这些 agent 的 cron 任务。

## 指标

`GET /api/v1/growth/metrics` → `cron_refs_rewritten`（每更新一条任务 +1）。

## 限制

- 仅从 patch 的 `OpPatch` + SKILL.md 推断 `old→new` 映射；纯删除/新建不产生反写。
- 合并删技能但未在 patch 中显式改名时，cron 仍可能指向已删技能名，需 Curator/复盘 prompt 产出 rename patch 或人工修 cron。
