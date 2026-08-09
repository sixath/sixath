# Growth System 首期验收对照表

**关联设计**：[`2026-05-10-growth-system-design.md`](./2026-05-10-growth-system-design.md)  
**关联计划**：[`../plans/2026-05-10-growth-system.md`](../plans/2026-05-10-growth-system.md)（Task1–10 已按该计划主线落地；Task11/12 可选） · **二期清单与评审**：[phase2 计划](../plans/2026-05-10-growth-system-phase2.md)  
**范围**：对照 spec 条文标注 **已实现 / 部分实现 / 未实现**，供 release 签字与排期缺口用。  
**日期**：2026-05-10（随实现迭代更新本表「状态」列即可）

---

## 1. 摘要

| 视角 | 结论 |
|------|------|
| 对照 **实现计划 Task1–10** | 计数、pending、租约、worker、Bus 事件、记忆 stub、`patch`/`applier` 库、Agent hook 等 **主线已交付**。 |
| 对照 **spec §1「首期必须闭环」字面** | **二期 P0 已补齐**：`llm_review_enabled` + `review_patch_file` / 真 LLM 走 `SkillReviewRunner` 写盘；**§4.2** 含 `growthwake` + `last_idle_check_at` 空闲扫描（见 [Task12 文档](../../../portal/docs/growth-idle-polling.md)）；**§4.3** metadata + `MergeGrowthReviewMetadata`；**§7** 状态机见 [2026-05-18-growth-state-machine.md](./2026-05-18-growth-state-machine.md)。 |

---

## 2. §1 背景与目标

| 条款 | 状态 | 说明 |
|------|------|------|
| **A 技能环**：阈值 → 执行体 → `SKILL.md` 及附属文件受控写回 | **已实现（feature flag）** | `SkillReviewRunner`：transcript + 索引快照 → `ProposeSkillPatches` → `ValidatePatchBatch` + `ApplyPatchBatch`；默认 `llm_review_enabled=false` 仍走 Stub。 |
| **B 记忆环**：阈值与会话边界 → 现有记忆管线 | **部分** | `NotifyMemorySessionDirty` 已在 stub 路径触发；与 **§4.2** 一并见下（无单独 onDone SQL 轻检表）。 |
| 首期不做 FTS、`session_search` 等价 | **已实现** | 无相关实现。 |
| 首期不做 Hermes Curator 等路线图项 | **已实现** | 未纳入。 |

---

## 3. §2 已锁定决策摘要

| 决策 | 状态 | 说明 |
|------|------|------|
| A+B 同首期 | **部分** | B 有完整触发链；A **缺真写盘复盘**。 |
| 触发 **C**（计数 + 会话结束/空闲轻检） | **已实现** | 计数 + pending 轮询 + `growthwake`；**空闲轻检**：`last_idle_check_at` + 默认 10m 间隔（[Task12](../../../portal/docs/growth-idle-polling.md)）。 |
| 技能权威 **A**（工作区为真） | **已实现（能力）** | Applier 以 workspace 为根；**当前默认 runner 不写文件**。 |
| 架构 **2**（`framework/growth` + portal 编排） | **已实现** | 与决策一致。 |
| 多实例 **租约** | **已实现** | `growth_workspace_leases` + 事务内抢占/续期。 |

---

## 4. §3 架构总览

| 条款 | 状态 | 说明 |
|------|------|------|
| portal：游标、租约、workspace | **已实现** | `ChatGrowthState`、lease、`ListPendingReviewSessions` join `agents.workspace`。 |
| framework：阈值、Runner、Applier、计数钩子 | **部分** | 阈值/hook/applier/Runner 接口与 **StubRunner** 具备；**瘦 Agent + 受限工具集**式真复盘未实现。 |
| 观测：Bus 三类 growth 事件 | **已实现** | 同上；**Completed/Failed** payload 含 **`duration_ms`**（自本轮 handle 起算）。 |

---

## 5. §4 触发模型

| 条款 | 状态 | 说明 |
|------|------|------|
| **4.1** 工具成功 → `tool_iters_since_review` | **已实现** | `ToolSuccessHook` → `GrowthUsecase.OnToolSuccess`。 |
| **4.1** assistant 持久化 → `turns_since_memory_review` | **已实现** | `OnAssistantTurn` 与落库路径挂钩。 |
| **4.1** 达阈值置 pending、主路径不阻塞 | **已实现** | 异步写库；worker 消费 pending。 |
| **4.2** 会话结束轻检并入队（租约约束下） | **已实现（可选）** | 阈值 + `growthwake`；**C2** `session_end_memory_review_enabled` 可在未达阈值但有计数时置 `pending_memory`（默认关）。 |
| **4.2** 空闲 X 分钟唤醒 | **已实现** | `sweepIdle` ticker（默认 10m）+ `ListIdleSessions` / `MarkIdleCheckDone`；配置见 Task12 文档。 |
| **4.3** 递归防护：子 Run Metadata 禁止再计数/再入队 | **部分** | **`sixath.growth_review`**（`growth.MetaGrowthReview`）+ `ChatService` hook **已忽略**计数；**子 Agent** 须在构造 `agent.Request` 时自行合并 metadata（可用 **`chat.MergeGrowthReviewMetadata`**）；当前 Stub 不调 ReAct，无运行时子 Run。 |

---

## 6. §5 执行与持久化

| 条款 | 状态 | 说明 |
|------|------|------|
| **5.1 A 输入** transcript + 技能索引快照 | **已实现（flag 开）** | `SkillReviewRunner` + portal `Transcript` 注入。 |
| **5.1 A 输出** 结构化 patch | **已实现（flag 开）** | file-stub / `NewLLMSkillProposer` / combined proposer。 |
| **5.1 A 落盘** tmp+rename、失败整批回滚 | **已实现** | worker 路径调用 `ApplyPatchBatch`。 |
| **5.1** 索引 generation / 缓存失效 | **已实现** | `DefaultSkillsIndexTracker.Bump` + portal `InvalidateSkillsCache` 回调。 |
| **5.2 B** 现有脏会话 / Manager 路径 | **已实现（stub）** | `NotifyMemorySessionDirty`。 |
| **5.2** 不新增用户画像类表 | **已实现** | 无新表。 |
| **5.3** 同 tick 双 pending 合并 LLM | **已实现（flag）** | `combined_review_enabled` + `NewLLMCombinedProposer`。 |

---

## 7. §6 多实例与租约

| 条款 | 状态 | 说明 |
|------|------|------|
| 租约表、单写者语义 | **已实现** | 与 spec 对齐。 |
| TTL、丢租约不半写 | **部分** | TTL 有默认；**半写**风险由未来真写盘路径依赖 applier（当前不写）。 |
| 未抢到租约：重试/延迟、对用户不可见 | **部分** | 不向用户报错 **满足**；**显式重入队**依赖 pending 仍存在 + 下轮 poll（与 stub 清 pending 的交互需产品确认是否足够）。 |

---

## 8. §7 错误处理与可观测性

| 条款 | 状态 | 说明 |
|------|------|------|
| 失败写 `last_*` / `failed_at`、游标状态机 | **部分** | **`RecordReviewRunFailure`** 回写 `last_skill_error`/`last_memory_error` 与 **`review_failed_at`**；pending **保留**以便重试；**完整「失败不前进游标」状态机**仍可单独建模。 |
| 事件 payload 含 duration 等 | **已实现** | **Scheduled** 含 `started_at`；**Completed/Failed** 含 **`duration_ms`**。 |

---

## 9. §8 测试策略（对照 spec 期望）

| 条款 | 状态 | 说明 |
|------|------|------|
| 单测：阈值、patch 原子、租约逻辑 | **部分** | 阈值、applier、租约 CAS 等有覆盖；**租约无 docker 级集成测试**（计划曾允许以编译/手工为主）。 |
| 集成：假 LLM + JSON patch + FS/index | **部分** | `TestSkillReviewRunner_T1FakeLLMPatchToFSAndIndexGen`（framework 单测）；租约见 `//go:build integration`。 |

---

## 10. §9–11（非首期或开放项）

| 条款 | 状态 |
|------|------|
| §9 二期路线图 | **不适用**（非首期验收） |
| §10 开放项（cron 复用、模型选型、迁移策略） | **未收口**（worker 采用进程内 goroutine；其余待产品/运维决策） |
| §11 spec 自检 | 本文件即对「实现 vs spec」的显式追踪 |

---

## 11. 建议后续排期（对齐 spec 签字）

1. **真技能复盘路径**：读 transcript + 技能索引摘要 → 产出 patch → `ApplyPatchBatch`（可先 feature flag）；子 Run 使用 **`chat.MergeGrowthReviewMetadata`**。  
2. **§7 游标状态机**：是否在失败时显式「不前进」计数器、与 `last_*` 的展示策略（产品收口）。  
3. **§4.2 深化（可选）**：无 pending 的会话结束轻检、`last_idle_check_at` 或与 `updated_at` 策略；文档见计划 Task12。  
4. **Task11/12**：合并 LLM、空闲行为文档（仍为可选）。

---

## 12. 修订记录

| 日期 | 变更 |
|------|------|
| 2026-05-10 | 初版：对照 `2026-05-10-growth-system-design.md` 与 Task1–10 实现现状。 |
| 2026-05-10 | 收口：§4.2 唤醒、§4.3 metadata + hook、§7 回写与 `duration_ms`、验收表同步。 |
| 2026-05-10 | 增加二期计划文档链接（`2026-05-10-growth-system-phase2.md`）。 |
| 2026-05-18 | 同步二期 P0–P2 落地：技能写回、空闲扫描、合并 LLM、状态机 spec、metrics、E3 配置字段与 Task12 文档。 |
