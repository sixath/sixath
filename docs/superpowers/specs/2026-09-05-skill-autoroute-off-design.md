# S22 收口：发货 yaml 关掉 Skill 预注入 auto_route

**日期**: 2026-09-05  
**状态**: 已确认（S19 leftover；P3 Skill 预注入已拆；2026-09-05 实施）  
**范围**: `portal/configs/config.yaml` 与 `config.docker.yaml` 的 `skills.auto_route_enabled`。不改 Channel `auto_route_*`，不 regen proto。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S19](./2026-09-05-growth-yaml-defaults-off-design.md)（当时明确不改此键）；[S21](./2026-09-05-sql-heal-off-design.md)

**一句话**: P3 已拆 SKILL 全文预注入；发货 yaml 不得再把 `auto_route_enabled` 写成 true。

---

## 1. 背景

`conf.Skills.auto_route_enabled` 注释仍是「按用户消息关键词匹配 Top-1 Skill 并预注入 SKILL.md 正文」。磁盘事实：

- `skill_router.go` 不存在（P3）
- `main` 只读 `GetAllowScriptExecution`；`GetAutoRouteEnabled` / `GetRouteMinScore` / `GetRouteMaxBodyRunes` **零调用**
- 发货 yaml 仍 `auto_route_enabled: true`

Channel / Gateway 的 `auto_route_*` 是另一套：把消息路由到哪个 Agent，**不是** SKILL 正文预注入。本切片不碰。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 发货 `skills.auto_route_enabled` | **false** |
| `allow_script_execution` | **不改**（仍 true） |
| `route_min_score` / `route_max_body_runes` | **保留死键**（本切片不删 proto 字段） |
| Channel `auto_route_*` | **不改** |
| proto regen | **不做** |

---

## 3. 行为

```text
portal/configs/config.yaml 与 config.docker.yaml：
  skills.auto_route_enabled: false
GetAutoRouteEnabled 仍无调用者；yaml 与 P3 口径对齐
```

---

## 4. 非目标

- 不删 proto `Skills.auto_route_*` 字段
- 不改 Gateway 渠道自动路由
- 不改 `MaybeSpill`
- 不合 assembler

---

## 5. 成功标准

1. 两份发货 yaml `auto_route_enabled: false`。
2. 单测锁定该键。
3. `cd portal && go test ./internal/conf -count=1` 绿。
