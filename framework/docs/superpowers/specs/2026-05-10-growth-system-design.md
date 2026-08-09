# Growth System（技能环 A + 记忆环 B）设计说明

**状态**：已评审（与产品方对齐：触发策略 C、技能权威 A、架构路线 2、B 首期不含 FTS）。  
**日期**：2026-05-10  
**范围**：首期交付与二期边界；实现前需再拆 `writing-plans` 任务单。  
**验收对照**（实现进度 vs 本 spec）：[`2026-05-10-growth-system-acceptance.md`](./2026-05-10-growth-system-acceptance.md)

---

## 1. 背景与目标

将 sixath 的「成长」做成与仓库根目录 [Hermes Agent 成长架构剖析](../../../../hermes-growth-architecture.md) 思路一致的**系统工程**：**触发 → 执行 → 持久化 → 观测**，而不是单一功能开关。

**首期必须闭环**：

- **A 技能环**：在阈值触发下，由受约束的复盘执行体读取会话与 workspace 技能，产出对 **`SKILL.md` 及附属文件** 的受控写回（与现有 `BuildSkillsIndex` / workspace 扫盘一致）。
- **B 记忆环**：在阈值与会话边界触发下，驱动**现有**记忆管线（如 `NotifyMemorySessionDirty`、`memory.Manager` 的短期/向量/摘要路径）更新；**不包含**跨会话 FTS 检索（明确二期）。

**首期明确不做**：

- 跨会话全文检索（FTS5 或等价）与「`session_search` 式」工具（二期）。
- Hermes 级 Curator 大合并、反写 cron 引用、Skills Hub 多源安装、轨迹 RL 训练闭环（路线图可引用，不纳入本 spec 验收）。

---

## 2. 已锁定决策摘要

| 维度 | 决策 |
|------|------|
| 范围 | A + B 同时第一期 |
| 触发 | **C**：主循环计数 + 会话结束/空闲轻量补检 |
| 技能权威存储 | **A**：仅工作区文件为真相源 |
| 总体架构 | **方案 2**：`framework` 内成长子系统 + `portal` 编排（游标、队列、workspace 路径） |
| B 检索 | 首期不做 FTS；二期再做跨会话搜索 |
| 多实例 | 接受**多 portal 写同一 workspace** 风险；首期用**租约/单写者**约束保证 at-most-once 写技能文件 |

---

## 3. 架构总览

```text
[portal]                         [framework]
  │                                   │
  │  会话/游标持久化                   │  growth（新建包名可议）
  │  入队 / 抢租约                    │  - 阈值策略
  │  workspace 根路径                │  - ReviewRunner（瘦 Agent / 受限工具集）
  │                                   │  - PatchApplier（原子写文件）
  │                                   │  - 计数钩子（由 ReAct/tool 注入）
  ▼                                   ▼
chat_sessions / growth_state   agent + tool + memory（现有）
  │                                   │
  └────────── events.Bus（观测）──────┘
```

- **编排与真相游标**：`portal` 负责将「每会话/每 agent」计数与**复盘租约**持久化，避免多副本重复触发；调用 `framework/growth` 的 API 执行实际复盘。
- **领域逻辑**：`framework/growth`（包名实现时可定为 `growth` 或 `review`）实现阈值、递归防护、结构化 patch 解析、原子落盘、与记忆更新接口对接。
- **观测**：通过现有 `events.Bus` 发布 `GrowthReviewScheduled` / `Completed` / `Failed`（事件名以代码为准），不依赖 Bus 做跨进程任务可靠性。

---

## 4. 触发模型（C）

### 4.1 计数锚点

- **工具锚点**：每次工具执行成功返回后（或按业务定义「仅失败不计数」——**默认：成功才 +1**），`tool_iters_since_review` 自增。
- **回合锚点**：每次 assistant 完成一轮可见输出并持久化后，`turns_since_memory_review`（或与技能共用统一游标，**默认：分字段**便于独立阈值）自增。

阈值到达时置位 `pending_skill_review` / `pending_memory_review`（布尔或位图），**不在用户关键路径上阻塞**；由异步 worker 消费。

### 4.2 会话边界与空闲

- **会话结束**：流式 `onDone`、或非流式保存 assistant 消息后，触发**轻量检查**：若存在 pending 标志或「上次轻检距今」超时，则尝试入队（仍受租约约束）。
- **空闲 X 分钟**：依赖后台 ticker（可与现有 `cron` 基础设施对齐，或 portal 内 `time.After` 类调度——**实现计划阶段二选一**）；仅唤醒排队，不重复计数。

### 4.3 递归防护

复盘执行体内 **禁止**再次递增成长计数或再次入队同源复盘（等价 Hermes：`nudge_interval = 0`）。由 `framework` 在构造子 `Run` 时注入 `Metadata` 标志位实现。

---

## 5. 执行与持久化

### 5.1 技能（A）

- **输入**：当前会话 transcript；workspace 技能索引快照（路径 + 摘要，避免整仓读入上下文过大）。
- **输出**：结构化 patch 列表（`path`、`op` ∈ {create,patch,delete}、`content` 或 unified diff 子集——**格式在实现计划中固定 schema**）。
- **落盘**：一律 `tmp` + `rename` 原子提交；失败则整 patch 批次回滚（不写半套）。
- **索引一致性**：写成功后 bump `skills_index_generation` 或使该 agent/workspace 缓存失效，下一轮 `BuildSkillsIndex` 或热重载读取新内容。

### 5.2 记忆（B）

- **输入**：同 transcript + 当前记忆子系统可读状态（若有）。
- **输出**：调用现有「脏会话通知 / Manager 写入」路径；**不**在本期引入新的「用户画像 DB 表」除非实现计划证明必要。
- **首期不做**：跨会话检索与 `session_search` 等价工具。

### 5.3 合并 LLM 调用（可选优化）

若同一 tick 内 `pending_skill_review` 与 `pending_memory_review` 同时为真，**允许**单次 LLM 往返生成两类产出（Hermes `_COMBINED_REVIEW_PROMPT` 思路），以省成本；失败时拆分为两次重试——**实现计划需写清降级策略**。

---

## 6. 多实例与 workspace 租约

**问题**：技能权威在磁盘，多 `portal` 副本可能并发复盘同一 workspace。

**首期约束**：

- 使用 **租约表**（建议 `growth_workspace_lease`：`workspace_key`、`holder_id`、`expires_at`）或基于 DB 的 `GET_LOCK`（若 MySQL）实现 **at-most-one** 复盘执行者。
- 租约 TTL 大于单次复盘 P99 时间；丢失租约仅导致跳过本轮，**不得**半写文件（依赖原子写）。
- 无租约 winner：将任务重新入队或延迟重试，**不向用户报错**。

---

## 7. 错误处理与可观测性

- 复盘失败：写 `last_error`、`failed_at`；游标策略为 **「不前进 tool_iters 阈值」或「部分前进避免死循环」**——**默认：失败不前进技能游标，记忆游标独立配置**（实现计划落地为明确状态机）。
- 事件：`GrowthReviewScheduled` / `GrowthReviewCompleted` / `GrowthReviewFailed` + payload（session_id、agent_id、workspace、duration、error 摘要），供已有 debug 流与日志系统消费。

---

## 8. 测试策略

- **单元**：阈值边界、递归标志、patch 应用原子性、租约抢锁/过期。
- **集成**：假 LLM 返回固定 JSON patch；断言文件系统与 index generation。
- **不测**：首期不包含 FTS 的检索正确性（二期 spec 覆盖）。

---

## 9. 二期路线图（非验收）

- B：跨会话 FTS / 向量检索 + 工具化召回。
- A：Curator 类合并、与 `cron_tasks` 或任务配置中技能引用的**一致性反写**（若届时存在引用链）。
- 训练：轨迹导出与压缩（对齐 Hermes trajectory 管线）。

可行性调研与立项 checklist：[2026-05-18-growth-r1-r3-feasibility.md](./2026-05-18-growth-r1-r3-feasibility.md)。

**实施清单与评审/里程碑**（含一期缺口补全、Task11/12）：[`../plans/2026-05-10-growth-system-phase2.md`](../plans/2026-05-10-growth-system-phase2.md)。

---

## 10. 开放项（由 implementation plan 收口，不留 TBD 语义）

- 空闲检测与现有 `portal` `cron_tasks` 的**复用程度**（共用调度器 vs 独立 goroutine）。
- 复盘 LLM **模型选型**（是否与对话主模型相同；成本与质量权衡）。
- `growth_workspace_lease` 与 `chat_sessions` 扩列的**表结构**在 data 层单独立 migration。

---

## 11. 自检记录（spec self-review）

- **占位符**：无 TBD/TODO 残留；开放项已列为明确决策点而非模糊需求。
- **一致性**：触发 C、存储 A、架构 2、多实例租约与「首期无 FTS」无冲突。
- **范围**：单 spec 仍偏大，**实现时必须**按 `writing-plans` 拆为可并行子任务（portal 迁移 / framework 包 / 事件契约）。
- **歧义**：「工具成功才计数」已在 4.1 标注默认；若产品改为「失败也计数」需改本节一句即可。
