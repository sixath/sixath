# Procedural Repair Harness：过程式修复前置与试点

> 状态：草案（存储/注入/family/试点 Agent 已拍板）  
> 日期：2026-07-30  
> 动机来源：Living-Harness（arXiv:2607.26598）对照 Sixath MemoryStore / Skills / Guardrails 的差距分析  
> 回链：[门面 §8](./2026-07-25-memory-store-facade-design.md)、[Turn 提取](./2026-07-25-memory-store-turn-extract-design.md)、[LLM 冲突](./2026-07-26-memory-store-llm-conflict-design.md)、[Prefetch 配额](./2026-07-27-memory-store-prefetch-quota-design.md)、[Neo4j 图](./2026-07-27-memory-store-neo4j-graph-design.md)、[Harness 差距](./2026-07-11-harness-engineering-gap-design.md)  
> 切片策略：**先前置能力（P3-A…D），再过程态试点（P3-E）；禁止跳过前置直接全站自动 commit**

---

## 0. 已确认决策

| 项 | 选择 | 说明 |
|----|------|------|
| 存储 | **`memory_units.kind=procedural`** | 不新建过程表；既有事实行视为 `kind=fact`（或缺省等价 fact） |
| Prefetch / 注入 | **二者都要** | Orchestrator 围栏车道 **+** Skill router 提示/绑定 |
| Task family | **复用现有 Agent 标签/路由字段** | **不**新建 `task_families` 表或并行 family 列；解析规则见 §6.5 |
| 试点 Agent | **`zone-4100-agent`** | P3-E 唯一默认试点；其它 Agent 保持过程态关闭 |

---

## 1. 背景与问题

### 1.1 问题

LLM Agent 常能在本轮纠错或 retry，但同类执行失败会在后续任务复发：纠正停在 episode 内，没有写回「下次在什么条件下应执行什么动作」的持久程序。

Sixath 已具备较强的**陈述式记忆**（`memory_units`、向量 hybrid、冲突 supersede、可选实体图），弱在**过程式修复**：

| 已有 | 缺口 |
|------|------|
| 事实 / 偏好 / 实体关系 | trigger → 必调工具 / Skill / 转移 |
| Turn 提取 + Prefetch | 本轮反思 vs 全局 commit 边界不硬 |
| tool_guardrails | 失败信号未结构化接到「是否值得固化修复」 |
| Skills | 尚未作为「可挂接的修复动作槽」被自动强化 |

### 1.2 核心判断

1. **过程记错比事实记错更糟**：错误过程态会稳定塑造 tool call，形成负复利。  
2. **当前适合隔离试点，不适合默认全开**。  
3. **收益前置**不在「再做一个记忆模块」，而在：可判定失败、可挂接动作、可撤销写入。  
4. Neo4j 实体图（P2-I）**不是**本能力的前置；过程态优先挂 Skill/工具契约，图语义另议。

### 1.3 成功标准（本规格验收口径）

- 无结构化失败信号时，**不得**自动写入全局过程态。  
- 本轮 reflexion / retry 摘要**默认不进** MemoryStore。  
- 过程条目可禁用 / invalidate；Prefetch 可整车道关闭。  
- 单域试点能证明：同 `failure_code` 复发率下降，且可一键回退。

---

## 2. 目标与非目标

### 2.1 目标（分期）

| 切片 | 名称 | 交付物 |
|------|------|--------|
| **P3-A** | 结构化失败信号 | `FailureSignal` 契约 + 工具/护栏/任务结果接入点 |
| **P3-B** | Episode 写入边界 | task-local 缓冲；score/end-before-update；与 Turn 提取隔离 |
| **P3-C** | 可挂接动作槽 | trigger → Skill 或工具序列的绑定模型（先手写/半自动） |
| **P3-D** | 可撤销与观测 | 单条 disable、重复强化、Prefetch 命中与行为变化观测 |
| **P3-E** | 过程态试点 | 默关的 procedural repair 存储 + 双通道注入；单 Agent 域 |

### 2.2 非目标

| 项 | 说明 |
|----|------|
| 全站默认开启自动过程 commit | 明确不做 |
| 新建过程专用表 | 使用 `memory_units.kind`（见 §0） |
| 新建 TaskFamily 体系 | 复用 Agent 标签/路由；见 §6.5 |
| 重开/扩大 Neo4j 实体图作为本能力依赖 | P2-I 保持可关；本规格不要求 |
| 用过程态替代事实 MemoryStore / D2 冲突 | 并置；D2 **仅**作用于 `kind=fact` |
| 通用 Evolution-SOP 零样本跨域 | 域规则先手写；不承诺自动迁移 |
| 完整 rollback / 历史回归测试平台 | P3-D 仅要求 disable + 观测；完整回归属后续 |
| Trajectory → RL / 改底座权重 | 不做 |
| 强制改模型 routing 或发明新工具 | Constraint 门：只能建议/绑定已有工具与 Skill |

---

## 3. 架构总览

```text
┌─────────────────────────────────────────────────────────────┐
| AgentRun / ChatSession                                      |
|  tools + Skills + C_act（冻结：工具集/策略/权限）            |
└───────────────┬─────────────────────────────────────────────┘
                │ emit
                ▼
┌───────────────────────┐     episode-local only
│ FailureSignal bus     │─────────────────────────────────────► task-local buffer
│ (P3-A)                │                                         (P3-B)
└───────────┬───────────┘
            │ after score / episode end
            ▼
┌───────────────────────┐
│ Commit gates          │  schema · scope · evidence · constraint · merge
│ (P3-E uses; P3-A/B/D  │  无证据 → 不写全局
│  提供输入)            │
└───────────┬───────────┘
            │ accepted repair → memory_units kind=procedural
            ▼
┌───────────────────────┐
│ Dual inject (P3-E)    │──► Orchestrator Prefetch procedural lane
│                       │──► Skill router suggest/prefer binding
└───────────┬───────────┘
            │
            ▼
     disable / invalidate / metrics (P3-D)
```

**两时间尺度**（对齐 Living-Harness 思想，实现不绑定其 POMDP 公式）：

- **Run 内**：环境与对话变；全局过程态只读。  
- **Episode 结束后**：才允许门控 commit。

---

## 4. P3-A：结构化失败信号

### 4.1 目标

把「好像失败了」变成可机读的 `FailureSignal`，作为一切全局过程写入的**唯一证据入口**。

### 4.2 数据契约（草稿）

```go
type FailureSignal struct {
    Code       string            // 稳定枚举，如 tool_not_called, tool_failed, policy_violation, user_reject, task_failed
    AgentID    string
    SessionID  string
    TaskFamily string            // 由 §6.5 从 Agent 标签/路由解析；缺省 = agent_id
    ToolName   string            // 可选
    SkillID    string            // 可选
    Message    string            // 短人类可读；非唯一证据
    Evidence   map[string]string // 结构化附件：expected_tool, guardrail_id, http_status, ...
    At         time.Time
}
```

### 4.3 信号源（首批）

| 源 | Code 示例 | 说明 |
|----|-----------|------|
| ToolGuardrails halt/warn 阈值 | `tool_repeat_fail` | 复用现有 guardrail 计数 |
| 工具执行错误 | `tool_failed` | 执行器已有错误时打点 |
| 策略 / confirm 拒绝 | `policy_violation` / `user_reject` | 与 HITL 对齐处接入 |
| 显式任务失败（若域有 evaluator） | `task_failed` | 无 evaluator 的域可不发此码 |

**禁止**：仅凭模型自然语言「我错了」生成可 commit 的全局信号（可进 task-local，不可作 Evidence 门唯一依据）。

### 4.4 验收

1. 至少 2 类源能稳定发出带 `Code` 的信号（单测 + 可选集成）。  
2. 无 `FailureSignal` 时，任何「过程 commit」路径 no-op。  
3. 信号结构化字段可被日志/metrics 检索（`code`, `agent_id`, `tool_name`）。

---

## 5. P3-B：Episode 写入边界

### 5.1 目标

硬分离：

| 通道 | 寿命 | 可进 MemoryStore |
|------|------|------------------|
| task-local reflexion / retry 摘要 | 当前任务实例结束即弃 | **否** |
| Turn 事实提取（既有 P2-C） | 持久 | 仅 `kind=fact` units；**不**写 procedural |
| 过程 commit（P3-E） | 持久 | 仅 `kind=procedural`，经门控且 episode 已结束 |

### 5.2 规则

1. **End-before-update**：全局过程写入不得影响产生该信号的同一 scored 回合的行为（与 Living-Harness score-before-update 同精神）。  
2. Portal Turn 提取（`NotifyMemoryExtractFromTurn`）**不得**把过程修复字段写入 units；若写入则必须 `kind=fact` 且不含 binding。  
3. 若存在本轮 retry 缓冲，缓冲键与全局 store 键空间隔离；实例结束 `Clear`。

### 5.3 验收

1. 单测：task-local 写入后结束实例 → 全局 Recall/Prefetch 不可见。  
2. 文档：`portal/docs/memory-integration.md` 增补「过程态 vs 事实态 / 本轮 vs 全局」一小节（随 P3-B 落地改，不提前空写大段）。

---

## 6. P3-C：可挂接动作槽

### 6.1 目标

过程修复的价值在「下次改行为」。先具备**动作槽**，再自动写记忆。

### 6.2 绑定模型（最小）

```text
ProceduralBinding:
  trigger_code | trigger_query   # 与 FailureSignal.Code 或自然语言 trigger 对齐
  action_kind: skill | tool_sequence
  skill_id? 
  tool_names?                    # 有序；必须 ⊆ 当前 Agent 已注册工具
  mode: suggest | prefer         # P3-E 试点只用 suggest；prefer 需显式开
```

### 6.3 约束

- **Constraint 门**：禁止引用未注册工具；禁止绕过 Permission / confirm。  
- 首期允许**纯手写** binding（配置或 Skill frontmatter），不要求 LLM 生成。  
- 自动生成 binding 属 P3-E，且必须经 Evidence 门。

### 6.4 双通道露出（与 §0 一致）

| 通道 | 行为 |
|------|------|
| Orchestrator Prefetch | procedural lane 注入短「条件 → 建议动作」文本 |
| Skill router | 命中 trigger 时 suggest/prefer 对应 `skill_id`（或附带 tool_sequence 提示） |

两通道共用同一 `kind=procedural` 条目与 binding；任一侧可单独观测开关，但产品默认**两侧都接**。

### 6.5 Task family 解析（复用 Agent 字段）

**不**新增 `task_families` 表，**不**在 `memory_units` 上新增并行 `task_family` 列。

解析顺序（实现 plan 钉死具体字段名，以当时 Agent / 路由模型为准）：

1. Agent 上已有的**标签 / 路由分类字段**（若存在且非空）→ 作为 `TaskFamily` 字符串。  
2. 否则 → **`agent_id`** 作为 family 主键。  

写入 procedural unit 时：family 写入 **`metadata`**（如 `metadata.task_family`），便于过滤；scope 仍用既有 `scope_type` / `scope_id` / `agent_id`。  
跨 family 检索默认关闭；显式配置才允许。

### 6.6 验收

1. 配置/手写一条 `suggest` binding → Prefetch **与** Skill router 均可露出（至少单测覆盖两侧）。  
2. 引用未知工具 → 拒绝装载并打日志。  
3. 无独立标签时 family 回落 `agent_id`，行为稳定可测。

---

## 7. P3-D：可撤销与观测

### 7.1 目标

没有「能关能清」，就不得开启自动 commit。

### 7.2 能力

| 能力 | 要求 |
|------|------|
| Disable / Invalidate | 按 id 或 `failure_code`+`agent_id` 使条目不再被 Prefetch / router 使用（`status` 或 metadata 标记；与 fact 的 superseded/deleted 语义区分清楚） |
| Strengthen-on-repeat | 同 `Code`+scope 默认 N≥2（可配）次才从 candidate → active |
| 观测 | 记录：条目被 Prefetch / router 命中次数；关联 run 是否出现目标 tool/skill |

### 7.3 非目标（本切片）

全量历史回归套件、自动 rollback 到某 checkpoint、陈旧条目 GC 策略（可列为后续 backlog）。

### 7.4 验收

1. disable 后 Prefetch 与 Skill router 均不再返回/选用该条。  
2. 单次信号只产生 candidate（或根本不写）；达 N 次才 active。  
3. 至少一项 metrics/log 字段证明「命中可观测」。

---

## 8. P3-E：过程态试点（依赖 P3-A…D）

### 8.1 前置门闩

P3-A～D 未验收前，**禁止**合并默认开启的自动过程 commit。

### 8.2 存储：`memory_units.kind`

**决议**：过程态落在既有 `memory_units`，通过 **`kind`** 区分。

| kind | 含义 | 默认 |
|------|------|------|
| `fact` | 陈述式事实 / 偏好（既有 Turn 提取与工具 remember） | 缺省或既有行视为 fact |
| `procedural` | 过程修复（trigger / repair / binding） | 仅 P3-E 路径写入 |

实现要点：

1. **迁移**：为 `memory_units` 增加 `kind` 列（或等价；推荐独立列便于索引），默认 `fact`；存量行 backfill 为 `fact`。  
2. **内容**：`content` 存人类可读「条件 → 修复」摘要；binding / failure_code / support_count / mode 等放 **`metadata` JSON**（字段名实现 plan 钉死）。  
3. **D2**：`SemanticConflictResolver` **只**对 `kind=fact` 的 add 生效；procedural 走本规格五门，不走事实 LLM 冲突。  
4. **Recall / Prefetch**：事实车道默认 `kind IN (fact)` 或 `kind IS NULL` 兼容；procedural 车道显式 `kind=procedural`。  
5. **向量 sidecar**：P3-E 试点可不强制索引 procedural；若索引，须可按 kind 过滤，避免事实 hybrid 被过程摘要污染。

最小逻辑字段（映射到 content + metadata，非新表）：

```text
id, scope_*, agent_id, kind=procedural, status(candidate|active|disabled|…),
failure_code, trigger, repair_summary (→ content),
binding (skill_id / tool_names / mode),
support_count, content_hash, metadata.task_family, created_at, updated_at
```

`status`：优先复用/扩展既有 ENUM；若 `candidate`/`disabled` 不宜塞进现有 `active|superseded|deleted`，则用 **`metadata.procedural_status`** + 仅 `active` 行可被双通道注入（disable → 软删或 metadata 标记且召回过滤）。实现 plan 二选一写死。

### 8.3 Commit 五门（对齐 Living-Harness，落地为显式检查）

| 门 | 规则 |
|----|------|
| Schema | 严格 JSON；失败则丢弃（可一次 repair pass） |
| Scope | 默认按 `agent_id` + §6.5 family；跨 family 需显式允许 |
| Evidence | 必须关联 ≥1 条 `FailureSignal`（或域 evaluator） |
| Constraint | binding ⊆ 已注册工具/Skill；不改 `C_act` |
| Merge | 同 hash/同 code 合并 strengthen；冲突 → 保持旧或 disable 待审（试点选保守：不覆盖 active） |

### 8.4 双通道注入

- **Orchestrator Prefetch**：独立 procedural lane；可整车道 `enabled: false`。  
- **Skill router**：读取同批 `kind=procedural` active 条目，按 trigger 匹配后 suggest/prefer Skill。  
- 配额：procedural Prefetch **计入**全局 `max_total`，并设 `max_procedural` 分顶（扩展 P2-F；若改配额语义另开小切片）。  
- 注入形态：短「条件 → 建议动作」；`mode=suggest` 不得用强制系统指令改写工具权限。

### 8.5 配置（示意）

```yaml
memory_store:
  procedural_repair:
    enabled: false                 # 默认关
    auto_commit: false             # 默认关；仅 candidate 或人工
    min_support: 2
    max_procedural: 3
    mode: suggest                  # suggest | prefer
    pilot_agents: ["zone-4100-agent"]  # 仅列表内 Agent 可启用过程态
    inject:
      prefetch: true               # Orchestrator 围栏
      skill_router: true           # Skill router
```

Env 覆盖（命名待实现 plan 定）：`SATH_MEMORY_PROCEDURAL_ENABLED` 等。

### 8.6 试点范围

- **首个试点 Agent：`zone-4100-agent`**（按 Agent `name` / 路由标识解析；实现 plan 钉死与 Portal `agents` 表的对应字段，一般为 `name` 或 `id`）。  
- 过程态 `enabled` / `auto_commit` 仅对该 Agent（或其 §6.5 family）放开；其它 Agent 保持关闭。  
- 手写域监控规则（Evolution-SOP 精神）：针对该 Agent 的工具/Skill，白名单哪些 `FailureSignal.Code` 允许 commit。  
- 对比：在该 Agent 上开/关 procedural 双通道的同坑复发率（人工或小回归集）。

### 8.7 验收

1. 默认配置下行为与未实现本切片时一致（全关）。  
2. 试点 Agent：同 code 复发下降可观测，或文档记录负结果与回退。  
3. 关掉 `enabled` 或 disable 条目后，行为回到基线。  
4. `kind=fact` 路径（提取 / D2 / hybrid）回归仍绿。

---

## 9. 与既有能力的关系

| 能力 | 关系 |
|------|------|
| P2-C Turn 提取 | 继续只写 `kind=fact`；过程字段禁止混入 |
| P2-D2 语义冲突 | **仅** `kind=fact`；procedural 另门控 |
| P2-E/F Prefetch | P3-E 增加 procedural 车道 + `max_procedural`；双通道含 Skill router |
| P2-I Neo4j | 不阻塞；若未来做 repair graph，另开规格，语义≠实体 REL |
| Skills / skill router | P3-C/E 主挂载与注入通道之一 |
| ToolGuardrails | P3-A 主信号源之一 |
| Harness 证据面（2026-07-11） | 同方向；本规格是记忆/修复侧落地，不替代 Hook 脊柱 |

---

## 10. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 错误过程态负复利 | 默认关；suggest；min_support；disable |
| 与事实提取抢上下文 | 分车道 + max_procedural；Recall 按 kind 过滤 |
| fact/procedural 同表误伤 D2 | D2 显式排除 procedural |
| 无 evaluator 域乱写 | Evidence 门拒绝；域 SOP 白名单 code |
| 范围膨胀成「自进化平台」 | 非目标表 + 切片门闩 |

---

## 11. 实施顺序（强制）

```text
P3-A 失败信号
  → P3-B 本轮/全局边界
  → P3-C 动作槽（可先手写 binding）+ 双通道接口契约
  → P3-D disable + 重复强化 + 观测
  → P3-E kind=procedural 试点（默关；含迁移）
```

允许 **P3-C 手写 binding** 与 **P3-A** 并行，但 **P3-E auto_commit** 不得先于 A/B/D。

每切片另开 `docs/superpowers/plans/YYYY-MM-DD-….md`；本文件为伞规格。

---

## 12. 开放问题

1. ~~Procedural 存新表 vs `kind=procedural`？~~ → **已拍板：`memory_units.kind=procedural`**  
2. ~~Prefetch 注入点？~~ → **已拍板：Orchestrator 围栏 + Skill router 都要**  
3. ~~TaskFamily 新建？~~ → **已拍板：复用 Agent 标签/路由；缺省 `agent_id`**  
4. ~~试点首个 Agent？~~ → **已拍板：`zone-4100-agent`**

（伞规格级开放问题已清空；字段名级细节留给各切片 implementation plan。）

---

## 13. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-07-30 | 初稿：由 Living-Harness 对照讨论收敛为 P3-A…E 伞规格 |
| 2026-07-30 | 拍板：kind=procedural；双通道注入；family 复用 Agent 字段 |
| 2026-07-30 | 拍板试点 Agent：`zone-4100-agent` |
| 2026-07-30 | P3-A plan：`docs/superpowers/plans/2026-07-30-procedural-repair-p3a-failure-signal.md` |
| 2026-07-30 | **P3-A delivered**：FailureSignal mapper + turnBus bridge（framework `memory` + portal stream path） |
| 2026-07-30 | P3-B plan：`docs/superpowers/plans/2026-07-30-procedural-repair-p3b-episode-boundary.md` |
| 2026-07-30 | **P3-B delivered**：EpisodeLocalBuffer + Facade 拒 procedural + extract `kind=fact` |
| 2026-07-30 | P3-C plan：`docs/superpowers/plans/2026-07-30-procedural-repair-p3c-bindings.md` |
| 2026-07-30 | **P3-C delivered**：手写 ProceduralBinding + Prefetch/Skill router 双通道 suggest |
| 2026-07-30 | P3-D plan：`docs/superpowers/plans/2026-07-30-procedural-repair-p3d-catalog.md` |
| 2026-07-30 | **P3-D delivered**：ProceduralCatalog candidate→active、Disable、hit 观测 |
| 2026-07-30 | P3-E plan：`docs/superpowers/plans/2026-07-30-procedural-repair-p3e-persist.md` |
| 2026-07-30 | **P3-E delivered**：`kind` 列 + `CommitProceduralRepair` + fact-only Recall + pilot `auto_commit` |
