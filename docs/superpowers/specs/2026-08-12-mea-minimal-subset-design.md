# Sixath MEA 最小子集（最大 ROI）

> 状态：**M0 计划已确认**（存储：Session 旁路文件/JSON）  
> 日期：2026-08-12  
> 分支：`feature/mea-minimal-subset`  
> 动机来源：LongHorizon-Harness（arXiv:2608.01964）对照 Sixath 状态管理 / 独立核验差距  
> 回链：[Harness 差距](./2026-07-11-harness-engineering-gap-design.md)、[Procedural Repair](./2026-07-30-procedural-repair-harness-design.md)、[Turn Tool Surface](./2026-08-09-turn-tool-surface-design.md)  
> 实现计划：[M0 plan](../plans/2026-08-12-mea-minimal-subset-m0.md)  
> Chat 接线：[M0.5 design](./2026-08-12-mea-m05-chat-wire-design.md)  
> M1 LLM Auditor：[M1 design](./2026-08-13-mea-m1-llm-auditor-design.md)  
> 端到端现状（入口分流 / ReAct / SSE / 排障）：[task-handling current](./2026-08-15-task-handling-current-design.md)

---

## 0. 已确认决策

| 项 | 选择 | 说明 |
|----|------|------|
| TaskState 存储 | **Session 旁路文件/JSON**（`data_root` 下按 session） | 最快落地；与 MemoryStore / Growth 表解耦；路径见 §5 |
| 范围 | **最小 MEA 子集** | TaskState + Contract + 只读 Audit；不先上三角色可互换后端 |
| Executor | **复用现有 ReAct + MCP** | 不替换主循环；MEA 包在外层编排 |
| 与 Procedural | **并置、不抢权** | FailureSignal 管工具失败→修复偏好；不得标任务 `completed` |
| 与 Growth | **并置、不抢权** | Growth 管技能/记忆沉淀；不得当事中 Auditor |
| 默认开关 | **默关 + feature flag / pilot** | 短任务不被拖慢；显式开启或阈值进入 |

---

## 1. 背景与问题

### 1.1 问题

长程工具任务（多步 MCP/CLI、跨依赖证据链）上，Sixath 仍把「做到哪、是否完成」主要放在同一段增长的 ReAct 上下文里：

| 已有 | 缺口（相对 LongHorizon-Harness MEA） |
|------|--------------------------------------|
| L0/L1/L2 压缩 | 压缩会丢细节；无外置任务板 |
| FailureSignal / Procedural | 管失败修复偏好，不验收任务完成 |
| Turn Tool Surface | 收窄工具面，不定义本轮子目标与验收 |
| Growth fork review | 事后沉淀技能，不是事中只读环境核验 |
| EpisodeLocalBuffer | episode scratch，非跨轮 audited state |

现有 harness 的两个结构性问题与论文一致：

1. **执行与任务状态同上下文** → context rot / 目标漂移  
2. **执行与完成判定耦合** → 假完成进入后续前提  

### 1.2 核心判断

1. **最大 ROI** 不在完整 Claude Code / Codex 三角色适配，而在：外置 TaskState + 有界 Contract + 只读核验写回。  
2. **可环境核对** 的任务（文件、exit code、配置、API 可断言字段）优先；验不了的 acceptance 不得假装 MEA。  
3. Agent 能力是 **model × harness**；MEA 抬的是失败地板与假完成，不补单步模型能力。

### 1.3 成功标准（本规格验收口径）

- Executor 自报成功 **不得** 直接把 requirement/artifact 标为 `completed`。  
- 仅 `audit.completion=complete` 且 `integrity=clean`（或 P0 机检通过）可推进对应状态。  
- 跨轮默认携带 TaskState + 被引用 audit；本轮原始工具轨迹可丢或仅留 summary。  
- Feature flag 关闭时，行为与现网 ReAct 路径一致。  
- Pilot 上 5～10 条「可机检」金样例：假完成率下降或 end-to-end pass 提升可观测。

---

## 2. 目标与非目标

### 2.1 目标（分期）

| 切片 | 名称 | 交付物 |
|------|------|--------|
| **M0** | TaskState + 门闩 | Session JSON 存储；Contract；**规则/脚本机检** 写回；feature flag |
| **M1** | LLM Auditor（只读） | 独立短上下文；只读工具白名单；变异 → `integrity=violation` |
| **M2** | 产品面 | SSE/UI 进度；`ask` 回用户；预算与 pilot 配置；观测事件 |

### 2.2 非目标

| 项 | 说明 |
|----|------|
| Manager/Executor/Auditor 可互换外部后端（Claude Code/Codex） | 不做 |
| 用 MEA 重写 Growth / Procedural | 不做 |
| TaskState 进 MySQL / `memory_units`（本阶段） | 明确选用旁路文件；日后可迁，接口先稳定 |
| GUI 专用计算机使用审计器 | 不做（可后续） |
| 默全开 MEA | 不做 |
| 无权威环境状态的主观任务强制机检 | 降级单轮 ReAct 或不进入 MEA |

---

## 3. 架构总览

```text
┌──────────────────────────────────────────────────────────────┐
| Chat / long-task entry (flag or threshold)                   |
└────────────────────────┬─────────────────────────────────────┘
                         ▼
              ┌────────────────────┐
              │ TaskState store    │  data_root/mea/<session_id>.json
              │ S_i                │
              └─────────┬──────────┘
                        │
         ┌──────────────┼──────────────┐
         ▼              ▼              ▼
   ┌──────────┐   ┌──────────┐   ┌──────────┐
   │ Manager  │   │ Executor │   │ Auditor  │
   │ (短预算) │   │ ReAct+MCP│   │ 只读     │
   │ → c_i    │   │ → o_i    │   │ → v_i    │
   └────┬─────┘   └────┬─────┘   └────┬─────┘
        │              │              │
        │              │ 环境可变     │ 环境只读
        └──────────────┴──────┬───────┘
                              ▼
                    Manager 合入 v_i → S_{i+1}
                    丢弃本轮原始轨迹（可选保留 o_i）
```

**硬规则**

- Manager **不**直接调会改环境的工具。  
- Auditor **不**获得写/突变工具；P0 机检同只读。  
- 只有合入 `v_i` 的路径可把记录标 `completed`。

---

## 4. 数据模型

### 4.1 TaskState

```text
TaskState {
  version: 1
  session_id, agent_id
  goal: string                 // 原始用户目标
  records: []TaskRecord
  audits: []AuditReport        // 或分文件；至少可按 id 引用
  updated_at
}

TaskRecord {
  id, kind: requirement | artifact | fact
  status: pending | completed | blocked | untrusted
  summary: string
  evidence_refs: []string      // audit ids
}
```

### 4.2 Contract `c_i`

```text
Contract {
  round: i
  goal: string
  acceptance: []string         // 至少一条可观察标准
  boundaries: []string
  relevant_state_ids: []string
  prior_audit_ids: []string
  tool_hint?: string           // 可选，对接 Turn Surface family
}
```

无任何可观察 `acceptance` → Manager 不得发 `execute`；应 `ask` 或降级非 MEA。

### 4.3 Execution report `o_i` / Audit `v_i`

```text
ExecutionReport {
  round, summary, artifacts_touched[], issues[]
  // 不可直接写 TaskState
}

AuditReport {
  id, round
  completion: complete | incomplete | blocked
  integrity: clean | suspect | violation   // M0 机检：pass→clean / fail→suspect|violation
  proposed_updates: []{ record_id?, kind, status, summary }
  evidence: []{ type, ref, excerpt }
}
```

---

## 5. 存储（已拍板）

### 5.1 路径

- 根：`{data_root}/mea/`  
- 文件：`{session_id}.json`（session_id 需做路径安全清洗，禁止 `/` `..`）  
- 可选：同目录 `audits/{session_id}/{round}.json` 若单文件过大再拆（M0 可先单文件）

`data_root` 与 Portal 现有配置一致（含 Docker `/data/portal`）。

### 5.2 并发与完整性

- 单 session 串行 MEA 轮次（与单用户流式对话一致）。  
- 写盘：临时文件 + rename，避免半写。  
- 不进 MemoryStore Prefetch；需要注入时由 Manager/编排器显式把 **摘要后的 S_i** 放进 Executor/Auditor 输入。

### 5.3 生命周期

- 会话结束可保留文件供排查；清理策略后续（TTL / 管理 API）不阻塞 M0。  
- Feature flag 关闭不创建文件。

---

## 6. 单轮流程与控制决策

Manager 输出 `(S', q, c?)`，其中 `q ∈ {execute, done, blocked, ask}`：

| q | 含义 |
|---|------|
| `execute` | 附带 `c_i`，进入 Executor |
| `done` | 审计后状态满足原 goal |
| `blocked` | 无允许动作可推进 |
| `ask` | 需用户信息/授权 |

循环结束：`done` / `blocked` / `ask` / `max_rounds`。

建议默认：`max_rounds=25`；Executor 预算与现网 turn 对齐或略紧；Manager/Auditor 短预算（论文参考 300s 量级，实现用 token/步数更合适）。

---

## 7. M0：规则/脚本审计（优先）

在上 LLM Auditor 前，用 **可机检 acceptance** 关门闩：

| 类型（示例） | 检查 |
|--------------|------|
| `path_exists` | 工作区/约定路径存在 |
| `file_contains` | 文件含模式 |
| `command_exit` | 只读或约定校验命令 exit 0（须在白名单） |
| `json_path` | 文件/工具结果 JSON 路径断言 |

- 机检失败 → 不得 `completed`；记录 `incomplete` + evidence。  
- 无法映射到机检的 acceptance → 该条不得单独支撑 `complete`（M0）；M1 再交 LLM Auditor。

---

## 8. M1：LLM Auditor

- 新鲜上下文：原 goal、`S_i`、`c_i`、`o_i`、引用 audit；**无** Executor 原始思维链。  
- 工具：只读子集（read/list/stat、约定只读 MCP；禁 write/exec 突变）。  
- 若检测到任务相关变异 → `integrity=violation`，本轮不得推进 `completed`。  
- `proposed_updates` 仅建议；Manager 合入。

---

## 9. 与现有子系统边界

| 子系统 | 允许 | 禁止 |
|--------|------|------|
| Turn Tool Surface | Contract.`tool_hint` / 本轮 family 收窄 | 用 Surface 代替 acceptance |
| FailureSignal | 观测执行失败；可提示 Manager 标 blocked | 用 signal 标任务 completed |
| Procedural Prefetch | 照常注入修复偏好 | 改写 TaskState |
| Growth | 会话后照常复盘 | 充当 Auditor 或清 TaskState 完成态 |
| Context 压缩 | 压缩 Executor 上下文 | 把 TaskState 「只活在压缩后历史里」 |

---

## 10. 进入条件（避免短任务税）

满足任一才进入 MEA（可配置）：

1. `SATH_MEA=1` / Agent 配置 `mea.enabled`  
2. Pilot agent 列表命中  
3. （可选）本 turn 预计多步：工具族 ≥ N 或用户显式「分步执行/验收」类意图  

未进入 → 现网路径，零行为变化。

---

## 11. 观测

- turnBus / 日志事件（建议）：`mea.round.start` / `mea.contract` / `mea.audit` / `mea.state.updated` / `mea.done|blocked|ask`  
- 不与 `FailureSignal.Code` 混用同一枚举；可并行挂 sink 便于排障。  
- M2：SSE 推送 TaskState 摘要供 UI。

---

## 12. 配置面（建议）

```yaml
# 示意；具体挂 Portal conf / env
mea:
  enabled: false
  pilot_agents: []
  max_rounds: 25
  data_subdir: mea          # under data_root
  manager_max_tokens: ...
  executor_budget: ...      # 复用或覆盖
  auditor_mode: rules       # M0: rules；M1: rules+llm
```

---

## 13. 风险与规避

| 风险 | 规避 |
|------|------|
| 旁路 JSON 与多实例 Portal 不同步 | M0 假定单 writer；多实例需会话粘滞或日后迁共享存储 |
| 路径穿越 | session_id 净化；限制在 `data_root/mea` |
| Auditor/机检可写 | 白名单；校验命令只读约定 |
| 三套状态混乱 | §9 禁止表 + code review 清单 |
| 短任务变慢 | §10 默关 + 进入条件 |

---

## 14. 测试与验收

### 14.1 M0

- 单元：TaskState 读写、rename 落盘、非法 session_id  
- 单元：Executor `o_i` 不能完成状态；仅 audit/机检可  
- 集成：flag 关 → 无 `mea/` 文件、行为不变  
- 金样例：至少 3 条「假完成」被机检拦住；至少 2 条多步依赖正确推进  

### 14.2 M1

- Auditor 无写工具；强制写尝试 → violation  
- 与 M0 机检并置：机检 fail 不得被 LLM 单独改写为 complete（策略：机检一票否决）

---

## 15. 实现落点（预告，非本规格强制文件名）

| 层 | 方向 |
|----|------|
| `framework/` | `mea` 包或 `memory/mea`：类型、Store、Orchestrator、rules auditor |
| `portal/` | flag 接线、chat 长任务入口、data_root 路径、事件 |
| `web/` | M2 再做进度展示 |

详细任务拆解见 [M0 实现计划](../plans/2026-08-12-mea-minimal-subset-m0.md)。

---

## 16. 开放问题（不阻塞 M0）

1. 多 Portal 副本时会话粘滞是否已保证（影响旁路文件正确性）。  
2. `ask` 与现有 HITL / confirm 工具如何统一 UX。  
3. 金样例清单与 pilot agent 名单（实现前选定）。
