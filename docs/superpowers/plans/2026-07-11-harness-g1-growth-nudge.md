# G1：可配置 Growth Nudge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把已有 `OnToolSuccess` / `OnAssistantTurn` 阈值复盘触发做成**可配置**（开关 + 间隔），并文档化与 Hermes「主循环 fork nudge」的差异；不在本计划实现 Hermes 式对话内注入复盘 agent。

**Architecture:** Sixath 已用计数器达阈 → `pending_*` + `growthwake.Wake()`（异步 Worker）。G1 = Portal conf / env 覆盖 `growth.Defaults` 间隔，并增加 `nudge_enabled`（false 时计数可保留但不置 pending）。默认保持当前行为（enabled=true，间隔用现有 Defaults）。

**Tech Stack:** Go；`portal/internal/conf`；`portal/internal/biz/growth.go`；`framework/growth` Defaults。

**Spec:** gap design G1；在 CDP Phase 2 **之后**执行（用户选择 C：先 A 后 B）。

> **Git：** 无仓库则跳过 Commit。  
> **非目标：** 主循环内 fork ReAct 复盘、改 Agent system prompt 注入、Hermes 完全对齐。

---

## 现状锚点

| 代码 | 行为 |
|------|------|
| `GrowthUsecase.OnToolSuccess` | `ToolItersSinceReview >= SkillToolInterval` → pending_skill |
| `GrowthUsecase.OnAssistantTurn` | `TurnsSinceMemoryReview >= MemoryTurnInterval` → pending_memory |
| `framework/growth.NewDefaults()` | 默认间隔常量 |

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Modify `framework/growth/config.go`（Defaults 实际位置） | 导出可覆盖字段；配合 NudgeConfig |
| Modify `portal/internal/conf/conf.proto` + pb | `nudge_enabled`, `skill_tool_interval`, `memory_turn_interval` |
| Modify `portal/internal/biz/growth.go` | 读 conf；`!nudgeEnabled` 只计数不置 pending |
| Modify `portal/internal/biz/growth_test.go` | 关闭 nudge / 自定义间隔 |
| Modify `portal/docs/growth-session-end-skill-review.md` 或新建短文 | G1 说明 |
| Modify gap spec G1 行 | 已落地 |

---

### Task 1: Defaults + Usecase 开关

**Files:**
- `framework/growth` Defaults
- `portal/internal/biz/growth.go`

```go
type NudgeConfig struct {
	Enabled            bool // default true
	SkillToolInterval  int  // default from NewDefaults()
	MemoryTurnInterval int
}
```

`OnToolSuccess` / `OnAssistantTurn`：达阈时若 `!Enabled` → **不置 pending、不 Wake**；计数封顶在 `interval`（`count = min(count, interval)`）。

**Conf 默认（写死，避开 proto3 bool 零值陷阱）：**
- Usecase 构造默认 `NudgeConfig{Enabled: true, …}`（代码默认，**不是**依赖 proto 零值）
- proto 用 `optional bool nudge_enabled` 或 `nudge_disabled`；若用普通 `bool`，接线逻辑必须是：`if conf 显式设置了字段则覆盖，否则保持 Enabled=true`
- `skill_tool_interval` / `memory_turn_interval`：`0` = 使用 `NewDefaults()`；**禁止**把 0 当合法 interval（单测锁死：interval=0 不导致每次都 pending）

- [ ] **Step 1:** 测 Enabled=false 时连调超过 interval 不置 `PendingSkillReview`
- [ ] **Step 2:** FAIL
- [ ] **Step 3:** 实现 `SetNudgeConfig` / 构造注入
- [ ] **Step 4:** PASS
- [ ] **Step 5:** Commit `feat(growth): make skill/memory nudge threshold configurable`

---

### Task 2: Portal conf 接线

**Files:**
- `conf.proto` Growth 段新字段（默认：enabled=true，interval=0 表示用 framework Defaults）
- GrowthWorker / NewGrowthUsecase 读 conf 调用 SetNudgeConfig
- 测试：conf 覆盖 interval=2 时第 2 次 tool success 置 pending

- [ ] **Step 1–5:** TDD + Commit `feat(portal): wire growth nudge conf`

---

### Task 3: 文档

- [ ] gap spec G1 → 已落地（可配置阈值；非 Hermes fork）
- [ ] 短文档：与 Hermes 差异表（异步 Worker vs 主循环 fork）
- [ ] `go test` portal biz + framework growth PASS

---

## 完成定义

| 项 | 验收 |
|----|------|
| 开关 | `nudge_enabled=false` 永不因阈值置 pending |
| 间隔 | conf 可改 skill/memory interval |
| 默认 | 不改 conf 时行为与今日一致 |
| 文档 | 写明非 Hermes 主循环 fork |

## 风险

| 风险 | 缓解 |
|------|------|
| 误关 nudge 导致永不复盘 | 默认 true；session-end G2 仍可置 pending |
| 与 G2 混淆 | 文档：G1=阈值 pending；G2=删会话 light review |
