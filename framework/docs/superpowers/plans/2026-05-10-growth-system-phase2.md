# Growth System 二期：功能清单、评审与开发里程碑

**关联**：设计 [`../specs/2026-05-10-growth-system-design.md`](../specs/2026-05-10-growth-system-design.md) · 一期验收 [`../specs/2026-05-10-growth-system-acceptance.md`](../specs/2026-05-10-growth-system-acceptance.md) · 一期计划 [`2026-05-10-growth-system.md`](./2026-05-10-growth-system.md)（Task11/12）  
**日期**：2026-05-10  

本文档列出**相对一期已交付主线**仍属二期的能力，并给出**评审结论**与建议**开发顺序**；实施时按里程碑拆 PR，避免与一期回归耦合。

---

## 1. 二期功能清单（按来源合并去重）

### 1.1 技能环 A（一期验收表中「未闭环」项）

| ID | 功能 | 说明 / 验收要点 |
|----|------|-----------------|
| A1 | **复盘输入编排** | 拉取会话 transcript（与 portal `ChatTranscriptProvider` 对齐）；组装 workspace **技能索引快照**（路径 + 摘要，控制 token）。 |
| A2 | **结构化 patch 运行时路径** | LLM 或规则引擎输出 `SkillPatchBatch`；走已有 `ValidatePatchBatch` + `ApplyPatchBatch`；失败整批回滚、成功才清 `pending_skill_review`。 |
| A3 | **子 Run 隔离** | 复盘内若调 `ReAct`/`Run`，必须使用 `chat.MergeGrowthReviewMetadata`（`sixath.growth_review`），避免递归计数（§4.3）。 |
| A4 | **索引一致性** | 写盘后 bump `skills_index_generation` 或使 agent/workspace 技能缓存失效，保证下一轮 `BuildSkillsIndex` 读到新内容。 |
| A5 | **租约与失败策略** | 写盘中途失败：`RecordReviewRunFailure` 与 pending 保留已部分具备；需明确「是否重试同一 batch」「最大重试次数」。 |

### 1.2 记忆环 B 与触发 C 深化

| ID | 功能 | 说明 |
|----|------|------|
| B1 | **§5.2 输入扩展** | 复盘记忆分支可读「当前记忆子系统状态」（若 API 稳定），仍走现有 Manager/脏会话路径，**不**强制新用户画像表。 |
| C1 | **空闲轻检（Task12）** | 无 pending 时按「空闲 X 分钟」唤醒排队：`last_idle_check_at` 列 **或** 复用 `updated_at` + 策略文档；可选与 `cron_tasks` 共用调度器（spec §10）。 |
| C2 | **会话结束显式 trySchedule** | 与 C1 可合并：流式 `onDone`/保存 assistant 后，除现有 `growthwake` 外，是否要对「仅记忆待刷新」会话做轻量扫描（产品定）。 |

### 1.3 LLM 与成本（spec §5.3、计划 Task11）

| ID | 功能 | 说明 |
|----|------|------|
| L1 | **可选 LLM 复盘 Runner** | `framework/growth/runner_llm.go`（或 portal 编排 + framework 解析）；**配置开关** `growth.llm_review_enabled`（proto / YAML，默认 false）。 |
| L2 | **合并双 pending 单次 LLM** | 同 tick `pending_skill_review && pending_memory_review` 时单次往返产出 patch + 记忆提示；**失败降级**为两次独立调用或仅技能/仅记忆（计划需写死策略）。 |
| L3 | **模型选型** | 与主对话模型相同或独立 auxiliary（成本/质量，spec §10）。 |

### 1.4 观测、状态机与测试（spec §7、§8）

| ID | 功能 | 说明 |
|----|------|------|
| O1 | **游标状态机显式化** | 「失败不前进技能游标」等与 `last_*`、`review_failed_at`、pending 的组合状态机文档 + 单测表驱动。 |
| O2 | **事件 payload 补全** | 如 `GrowthReviewScheduled` 带 `started_at`；与 `duration_ms` 字段命名统一。 |
| T1 | **集成测试** | 假 LLM 固定 JSON patch → 断言 FS +（若有）index generation（spec §8）。 |
| T2 | **租约集成测试** | MySQL docker 或 build tag `integration`（一期计划已提及）。 |

### 1.5 Spec 二期路线图 §9（远期）

| ID | 功能 | 说明 |
|----|------|------|
| R1 | **跨会话 FTS / 向量检索 + 工具** | 明确排除在一期；二期独立 spec 与数据面。 |
| R2 | **Curator 类合并、cron 技能引用反写** | 依赖当时是否存在引用链。 |
| R3 | **轨迹导出与压缩** | 对齐 Hermes trajectory，与成长触发解耦。 |

### 1.6 工程与运维

| ID | 功能 | 说明 |
|----|------|------|
| E1 | **独立 migration** | spec §10：`chat_growth_states` / lease 等在生产若禁用 AutoMigrate，提供版本化 SQL。 |
| E2 | **Worker 开关与可观测** | 如 `growth.worker_enabled`、进程指标（pending 队列深度、租约争抢失败次数）。 |
| E3 | **轮询间隔配置** | 已实现 **`SATH_GROWTH_WORKER_POLL`**（`time.ParseDuration`，如 `60s`），范围 5s–24h；后续可与 `growth.*` 配置合并进 Bootstrap。 |

---

## 2. 评审结论

### 2.1 优先级建议

| 层级 | 包含项 | 理由 |
|------|--------|------|
| **P0** | A1、A2、A3、L1（默认关）、T1 最小闭环 | 直接补齐 spec §1「技能环写回」与可验证回归；L1 关默认不影响现网。 |
| **P1** | A4、L2、O1、T2、E1 | 索引一致性与合并 LLM 降本；状态机防死循环；生产迁移与租约可信度。 |
| **P2** | B1、C1、C2、O2、E2、E3 | 体验与运维增强；可与 P0/P1 并行排期。 |
| **远期** | R1–R3 | 独立路线图与专项 spec，不宜与 growth v1 同里程碑混谈。 |

### 2.2 依赖关系（简图）

```text
L1(prompt+模型) ──► A2(patch 产出) ──► Validate + ApplyPatchBatch
       │
       └──► A1(transcript+索引摘要)
A4(缓存失效) ◄── A2 成功之后
MergeGrowthReviewMetadata ◄── 任意复盘内子 Agent（A3）
```

### 2.3 风险与缓解

| 风险 | 缓解 |
|------|------|
| LLM 输出非合法 JSON / 越权路径 | 强校验 `ValidatePatchBatch`；拒绝则 `RecordReviewRunFailure`、不落盘。 |
| 多实例与 LLM 长耗时 | 已有租约 TTL；Runner 内超时 + context cancel。 |
| 成本飙升 | L1 默认 false；L2 合并调用仅在双 pending 且 flag 开启时启用。 |

### 2.4 待产品 / 架构拍板

- 复盘 **是否与主对话同模型**（L3）。  
- **C1/C2** 是否要做「无 pending 也扫」及间隔默认值（建议默认 10m 与 Task12 文档一致）。  
- **失败重试**次数与是否自动降 pending（与 O1 状态机绑定）。

---

## 3. 开发里程碑（建议拆 PR）

| 里程碑 | 目标 | 主要交付物 |
|--------|------|------------|
| **2.1** | 可开关的「假 / 真」LLM 骨架 | `growth.llm_review_enabled`（或等价配置）；`Runner` 工厂：stub \| noop-llm stub；单测。 |
| **2.2** | 技能写回最小闭环 | A1+A2+A3：worker 调新 runner；patch 应用 + 清 pending；集成测试 T1（假 LLM）。 |
| **2.3** | 索引与观测 | A4 + O1 + O2；文档更新验收表二期段落。 |
| **2.4** | 合并 LLM 与空闲 | L2 + C1 + Task12 文档；可选 cron 接入。 |
| **2.5** | 生产化 | E1、E2、T2、R1 专项另立 epic。 |

---

## 4. 修订记录

| 日期 | 变更 |
|------|------|
| 2026-05-10 | 初版：二期清单 + 评审 + 里程碑。 |
| 2026-05-10 | 标注 `SATH_GROWTH_WORKER_POLL` 已落地；设计/验收文档增加二期计划链接。 |
| 2026-05-18 | **P0 主体落地**：A4 进程内 `growth.DefaultSkillsIndexTracker.Bump` 在 patch apply 后调用；portal worker 注入 `InvalidateSkillsCache` 回调。L1 新增 `framework/growth/runner_llm.go`（`LLMClient` + `NewLLMSkillProposer` + `NewLLMCombinedProposer` + prompt 模板），portal `service.NewGrowthWorker` 适配 `framework/model.Model` 为 `growth.LLMClient` 并按 `growth.llm` / `growth.combined_review_enabled` 配置 wire 真 LLM。L2 合并 proposer 在 portal 层注入完成。A5：迁移 003 增加 `review_retry_count`；`RecordReviewRunFailure` 递增、`ClearGrowthPending` 在两者全清时复位；`DropPendingAfterMaxRetry` + worker 指数退避（base 30s，上限 10min）。conf 增加 `Growth.combined_review_enabled` / `Growth.llm` / `Growth.max_retry` 与 `GrowthLLM` 子消息。补单测：`framework/growth/skills_index_tracker_test.go`、`runner_llm_test.go`、`portal/internal/service/growth_llm_wiring_test.go`、`portal/internal/biz/growth_test.go` 新增重试/退避用例。 |
| 2026-05-18 | **P1/P2 补齐**：B1 新增 `RunnerDeps.MemoryState` + portal `chat.GetMemoryStateSummary`，复盘 prompt 注入记忆 backend/files/chunks/cache 摘要。L3 `GrowthLLM.auxiliary` 子消息支持独立 cheap 模型；portal `newGrowthModelClient` 优先使用 auxiliary。E2 `framework/growth/metrics.go` 暴露 reviews_scheduled/completed/failed、lease_contention、lease_acquire_err、idle_sweep_runs、pending_dropped、pending_depth；portal `/api/v1/growth/metrics` 端点输出 JSON。T2 `growth_lease_integration_test.go`（`//go:build integration`）+ `scripts/integration_growth_lease.sh`（docker MySQL 自动化）。O1 新增独立 spec [`../specs/2026-05-18-growth-state-machine.md`](../specs/2026-05-18-growth-state-machine.md) —— 抽象 IDLE/PENDING/BACKOFF/DROPPED 四态 + mermaid + 单测映射。 |
| 2026-05-18 | **收尾**：E3 `growth.worker_poll_interval` / `idle_sweep_interval` / `idle_check_interval` 并入 Bootstrap（仍兼容 env）；T1 `TestSkillReviewRunner_T1FakeLLMPatchToFSAndIndexGen`；Task12 [`portal/docs/growth-idle-polling.md`](../../../../portal/docs/growth-idle-polling.md)；验收表同步；`IdleCheckInterval` 默认改为 10m。 |
| 2026-05-18 | **C2/L2/A3**：`session_end_memory_review_enabled` + `TrySessionEndMemoryReview`；合并 LLM 失败降级 `runSequential`；`growth.MergeReviewMetadata`（portal `chat.MergeGrowthReviewMetadata` 委托）。 |
| 2026-05-18 | 新增远期可行性调研 [`../specs/2026-05-18-growth-r1-r3-feasibility.md`](../specs/2026-05-18-growth-r1-r3-feasibility.md)（R1–R3 依赖、风险、引用链 checklist）。 |
| 2026-05-18 | R2 §4.3 引用链表按代码库预填（cron `skill_execute` 路径契约、生产 SQL 待执行）。 |
