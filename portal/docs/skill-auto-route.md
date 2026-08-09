# Skill 自动路由（P1）

**配置**（`config.yaml` → `skills`）：

| 字段 | 默认 | 说明 |
|------|------|------|
| `auto_route_enabled` | `true`（代码默认） | 关闭则不预注入 |
| `route_min_score` | `5` | 关键词路由最低分 |
| `route_max_body_runes` | `12000` | 注入 SKILL 正文上限 |

## 行为

每轮用户消息发送时（`SendMessage` / `SendMessageStream` / Agent `Chat`）：

1. 扫描 workspace `skills/**/SKILL.md` 元数据；
2. `framework/skills.RouteBest` 对用户问题做关键词打分（name / description / tags）；
3. 达阈值则将 **Top-1** 的 SKILL.md 正文追加到 system prompt，并注明无需再 `load_skill`。

实现：`portal/internal/chat/skill_router.go` · `framework/skills/route.go`

## 打分规则（摘要）

- 问题包含 skill `name`（含 `-` 转空格）：+12
- name 分段词命中：+4/词
- description 含问题词：+2/词
- tag 命中：+5

## 与 Growth 闭环

1. 用户排障 → C2s 会话结束触发 skill review → 写入/更新 `SKILL.md`
2. 下次同类问题 → **auto_route** 预注入该 Skill → 少依赖模型自觉 `load_skill`

## 验收

1. `skills/auto-route-demo/SKILL.md`，`name: auto-route-demo`
2. 发消息：`请按 auto-route-demo 处理`
3. 抓包或日志：system prompt 含 `【已自动匹配 Skill: auto-route-demo` 与正文

## 后续（未做）

- embedding / 向量路由（R1c）
- Top-K 多技能注入
- 强制工具白名单（仅允许 skill 声明的 allowed_tools）
