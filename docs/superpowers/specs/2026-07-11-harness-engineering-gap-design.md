# Sixath Harness Engineering 差距与改造设计

**日期**: 2026-07-11  
**状态**: 已评审；**Phase 0–2 + G4 + S2 浏览器 12 工具 + process notify 唤醒 + 真 pty + vision LLM 已落地**；S4 仍 backlog  
**口径**: A（成熟度雷达）+ B（Hermes 式成长运行时）+ D（企业运维/RCA）  
**成功标准（近 1–2 季度）**: 先把 harness 做成可产品化运行时平台（B）；用 A 度量；D（RCA/运维）为第一垂直客户  
**改造路径**: 方案 2 — Harness 脊柱优先，RCA 作证明场；Hermes 能力作目录不按原 P0 字面堆工具  
**关联**:  
- [hermes-capability-gap-requirements](../../../framework/docs/superpowers/specs/2026-05-25-hermes-capability-gap-requirements.md)  
- [design-agent-runtime-hermes-inspired](../../../framework/docs/design-agent-runtime-hermes-inspired.md)  
- [rca-toolchain-design](./2026-07-07-rca-toolchain-design.md)  
- [rca-agent-binding-design](./2026-07-08-rca-agent-binding-design.md)  
- [harness-phase2-browser-cdp](../plans/2026-07-11-harness-phase2-browser-cdp.md)

---

## 1. 背景与目标

### 1.1 什么是 Harness Engineering

Agent = Model + Harness。Harness 是模型之外的一切：上下文管道、工具编排、执行环境、验证回压（sensors）、护栏、可观测、多 Agent 编排与成长闭环。原则：**Agent 犯错一次，就工程化修复 harness，使同类错误不可再犯。**

### 1.2 本设计要解决什么

Sixath 已具备可用运行时（ReAct、L0–L2、toolset/`CheckFn`、护栏、HITL、Growth、RCA 工具等），但相对 2026 harness 成熟度：

- **控制面**（可拦截 Hook、生命周期、预算隔离、prompt 分层）尚未成为一等公民  
- **证据面**（RCA 结案验证回压）薄弱  
- Hermes 对齐计划偏「表面工具清单」，未按平台脊柱重排  

本设计给出差距清单与分期改造，使外部集成方能「注册 Hook + 挂 RCA/CDP 工具 + 配置证据门」，而无需 fork `react_agent.go`。

### 1.3 非目标（近季）

| 项 | 说明 |
|----|------|
| 全量 IM Gateway / OpenClaw 复刻 | 不做 |
| Trajectory → RL 训练管线 | 不做 |
| 纯编码 IDE harness（对标 Claude Code 全套） | 不做（口径 C 仅旁注） |
| Kanban / 17 渠道一次到位 | 不做 |
| **浏览器 CDP** | **S2 12 工具已落地**（含 cdp/dialog）；**vision LLM 已落地**；多 tab CDP 路由仍 waiver；见 §4.5 S2 |

---

## 2. 现状成熟度（口径 A）

评分 1–5；对照代码与现有设计文档（2026-07-11）。

| 层 | 分 | 现状锚点 | 主要短板 |
|----|----|----------|----------|
| 上下文 / 记忆 | 4 | L0–L2、memory_search/get、session_search、memory 写（opt-in） | Stable/Ephemeral prompt 未产品化；跨会话 LLM 摘要仍弱 |
| 工具编排 | 3.5 | toolset、`CheckFn`/`ListForAPI`、defer catalog、MCP | 工具严格串行；缺可 block 的 `ToolHook` 矩阵 |
| 执行环境 | 3 | pathguard、workspace file、terminal、ssh_exec、execute_write 确认 | 沙箱/危险命令审批不完整；无统一失败→结构化回馈契约 |
| 验证回压 | 2 | ToolGuardrails、forced summary | 无 RCA 领域证据门；无通用 verify 步骤 |
| 护栏 / 权限 | 3.5 | PermissionPolicy、confirm/ask_user、injection 扫描雏形 | Hook 与 Permission 顺序未统一；无插件式 shell-hook |
| 可观测 | 3 | events Bus、middleware metrics/tracing、TraceSink | 轨迹→成长/策略回流弱 |
| 多 Agent / 编排 | 2.5 | PlanExecute、Growth fork-agent | 通用 subagent spawn + 预算隔离未产品化 |
| 成长闭环（B） | 3.5 | GrowthWorker、skill_manage、append_learning、AgentReviewRunner | 主循环 nudge 与 Hook 未焊死；fork 路径 cron 反写有 gap |

**总判**：不是「缺 Agent」，而是控制面与证据面未成为一等公民。

---

## 3. 改造架构：三层脊柱（方案 2）

```text
┌─────────────────────────────────────────────────────────┐
│ 控制面 Control   ToolHook(可 block) · Session hooks     │
│                  Permission 统一顺序 · 预算/子 Agent     │
│                  Stable/Ephemeral prompt · toolset API  │
├─────────────────────────────────────────────────────────┤
│ 证据面 Evidence  RCA 结案门 · 工具结果契约 · 可重试语义  │
│  (第一客户 D)    观测字段 · 「缺证据则回压」传感器       │
├─────────────────────────────────────────────────────────┤
│ 成长面 Growth    on_chat_session_end → 复盘/learnings   │
│                  （BrowserSession 清理仅 Phase 2）       │
└─────────────────────────────────────────────────────────┘
          ↑ 表面工具（Hermes 目录 + CDP）按脊柱依赖插入
```

**原则**

- **补齐**：控制面 Hook、证据门、预算隔离  
- **适配**：现有 Permission/Guardrails/Growth 接到 Hook，不另起炉灶  
- **Hermes 文档角色**：能力目录与验收用例来源；优先级以本设计脊柱为准  

### 3.1 生命周期术语（避免混用）

| 术语 | 含义 | 典型钩子 |
|------|------|----------|
| **ChatSession** | Portal/用户侧一次对话会话（多轮 message） | `on_chat_session_end`（成长回流、轨迹落盘） |
| **AgentRun** | 单次 `ReAct.Run` / `RunStream`（一轮用户消息触发） | `on_run_end`（可选；指标、本轮摘要） |
| **BrowserSession** | CDP 浏览器自动化会话（与 ChatSession 1:N 或 1:1 可配） | `on_browser_session_end`（关页、断 CDP；**仅 Phase 2**） |

**默认（Phase 0）**：C2 只落地 **`on_chat_session_end`**（Portal 会话关闭/过期/显式结束时触发）。**不**在每次 AgentRun 结束触发成长复盘，避免「每 turn 焊 Growth」。BrowserSession 钩子随 S2 引入，不占用 Phase 0。

### 3.2 目标调用顺序（单次工具）

```text
Model tool_calls
  → ListForAPI(CheckFn)
  → Hook.Before[] ──block──→ tool result(error, blocked=true) → Model
  → PermissionPolicy
  → Execute
  → Hook.After[]
  → ToolGuardrails（重复/无进展等）
  → EventBus + Trace
```

**C1 block 语义（写死）**：`Before` 返回 error（或显式 `Block(reason)`）→ **不调用** `Execute` → 向模型写入 tool 结果，含 `blocked=true` 与可操作 `reason`。Hook **不是**参数变换链的唯一用途，但 Before 允许改 args；与 runtime 设计 §6.3 对齐时以「可 block」为准。

### 3.3 EvidenceGate 触发点（与工具管线分离）

E1 **不**挂在「每次工具 Execute 之后」。根因宣称发生在**最终答复 / 结案路径**。

| 触发点 | Phase 1 默认 | 说明 |
|--------|--------------|------|
| **Final-answer gate** | **采用** | 在以下任一结案路径触发：① 模型本步不再发 tool_calls、准备输出最终回复；② **MaxSteps / forced summary** 强制结案。检查本 Run 的 `evidence_refs` 累积；不足则 Soft 回压（注入提示，要求补证据或显式写「证据不足」），可配 HardHalt。**作用域**：仅对绑定了 RCA/证据工具、或 Agent 显式开启 evidence 策略的实例生效——**不是**全局 final-answer 拦截 |
| 专用结案工具 | 不做 | 避免强迫模型多学一个工具 |
| ChatSession end | 不做 E1 | 会话结束太晚，用户已看到答复 |

工具管线只负责把 RCA 工具输出归一成 `evidence_refs`（E2），供 Final-answer gate 读取。

---

## 4. 缺口清单

### 4.1 控制面 — 补齐

| ID | 缺口 | 为何必须 | 现状 |
|----|------|----------|------|
| C1 | `ToolHook` 最小集（pre/post_tool_call，Before 可 block） | HaaS 扩展点；CDP/RCA/审批挂载点 | 仅 `ToolSuccessHook` + EventBus；设计见 runtime §6.3；语义见 §3.2 |
| C2 | **`on_chat_session_end`**（见 §3.1） | 成长回流、轨迹落盘 | Growth 靠 Worker；未在 ChatSession 结束挂钩 |
| C3 | Permission ↔ Hook 统一顺序 | 见 §3.2 | Permission 在 `executeOneToolCall`；无 Hook 链 |
| C4 | 子 Agent 预算隔离 | fork/RCA 子任务不耗尽父 `MaxSteps` | fork 有瘦身 toolset；通用 spawn+budget 未产品化 |
| C5 | Stable / Ephemeral prompt 契约 | 垂直 prompt 可插拔、缓存稳定 | 多处拼 system；无稳定哈希 API |

### 4.2 控制面 — 适配

| ID | 项 | 改法 |
|----|-----|------|
| A1 | PermissionPolicy / ToolGuardrails / confirm_required | 明确顺序或实现为 Hook；不重写业务 |
| A2 | CheckFn + ListForAPI + defer catalog | 保持；CDP/RCA 新工具一律走同一门控 |
| A3 | ask_user / execute_write / skill_manage pending | 统一 confirmation kind 注册表，供 CDP 复用 |
| A4 | EventBus / TraceSink | Hook block reason 写入事件 |

### 4.3 证据面 — 补齐（RCA）

| ID | 缺口 | 说明 |
|----|------|------|
| E1 | 结案证据门（sensor） | **已落地**：Final-answer Soft inject ≤1；forceFinal 仅 Metadata；「证据不足」放行；默认关闭、RCA 启用 |
| E2 | **RCA/证据相关工具**结果契约 | **已落地**：`ok` / `error_code` / `evidence_refs`（限定五工具） |
| E3 | 失败可重试语义 | **已落地**：暂态 vs 永久（`transient` / `permanent`） |
| E4 | RCA Skill 薄封装（可选） | **已落地**：`skills_examples/skills/rca-investigation`（trace→log→code）；不硬编码进 ReAct |
| E5 | Portal RCA 绑定完备性 | **验收 passed**（`portal/internal/chat/rca_binding_acceptance_test.go`）：type=rca、func_path 白名单、BuildRegistry→List/ListForAPI 可见；对照 [rca-agent-binding-design](./2026-07-08-rca-agent-binding-design.md) |

### 4.4 成长面

| ID | 项 | 类型 |
|----|-----|------|
| G1 | 主循环 nudge（技能/记忆阈值）→ 复盘 | **已落地（可配置）**：`NudgeConfig` + `SetNudgeConfig` / `SATH_GROWTH_NUDGE_*`；异步 Worker pending，**非** Hermes 主循环 fork（见 `portal/docs/growth-g1-nudge.md`） |
| G2 | `on_chat_session_end` → Curator/脏标记/learnings | **已落地**：DeleteSession → ChatSessionHooks → `TrySessionEnd*` |
| G3 | fork-agent 路径 cron 技能引用反写 | **已落地**（1:1 改名 + `GrowthWorker` rewrite 回调） |
| G4 | 失败模式 → harness 修复流水线 MVP | **已落地（MVP + G4.1）**：`FailureCaptureHook` → ERRORS.md；`harness-fix` Skill + `skill_manage` 人审。**G4.1 已落地**：workspace `harness/hooks.yaml` 声明式 Before-block（写盘走 danger confirm） |

### 4.5 表面执行面

| ID | 项 | 策略 |
|----|-----|------|
| S1 | workspace file / terminal 危险审批 / process | **已落地**：terminal Danger confirm；workspace_file DangerPath；**process**（background + list/poll/log/wait/kill/stdin + **notify 唤醒 Agent**）；**真 pty**（Unix pty / Windows ConPTY，`terminal(pty=true)` + background） |
| S2 | **浏览器 CDP（近期）** | **12 工具已落地**（Hermes 全栈）：B2 十工具 + **`browser_cdp` / `browser_dialog`**；confirm；下载 deny/workspace；snapshot 含 `pending_dialogs`；**vision LLM**（`browser_vision` + `vision_analyze`，`SATH_VISION_*`）。**仍 waiver**：cdp 多 tab/`frame_id` 完整路由 |
| S3 | 并行 tool 执行 | **已落地（默认关闭）**：`ParallelTools` + `MaxParallel`；含 `RequiresSequential` 则整轮串行；见 `harness-s3-parallel-tools` |
| S4 | Gateway / 多 IM | 近季不做 |

---

## 5. 分期路线图

| Phase | 周期（估） | 范围 | 完成定义 |
|-------|------------|------|----------|
| **0 脊柱** | 2–3 周 | C1–C3、A1–A4；C2=`on_chat_session_end`；可选 C5 雏形 | Before block 不调用 Execute；空 Hook 行为与今日一致；会话结束钩子可单测 |
| **1 证据+成长** | 3–5 周 | E1（Final-answer）、E2–E3（RCA 工具集）、E5（= binding design）、G2–G3；G1 可配置；C4 若已有多 fork | **已落地**（E1–E3、E5、G1–G3） |
| **2 表面** | 随后/可并行人力 | **S2 CDP 最小集** + `on_browser_session_end`、S1 审批补齐、可选 S3 | **表面主线已齐**（含 browser 12 + process notify + **真 pty** + **vision LLM**）。**未做**：S4 |
| **Backlog** | 近季不做除非加塞 | S4、cdp 多 tab/`frame_id` | — |

**依赖备注（Go / chromedp）**：framework 与 portal 的 `go.mod` 为 **Go 1.26**；S2 真实后端依赖 `github.com/chromedp/chromedp`。单测默认 Fake，不要求本机 Chrome；真实 CDP 需本机/远程 Chrome 可用（`BROWSER_CDP_URL` 或 local headless）。构建/测试环境若 Go &lt; 1.26 会失败，需对齐工具链。

**依赖铁律**：S2 不得早于 C1–C3 合并。E1 依赖 E2 的 `evidence_refs` 累积；可与 Phase 0 末期并行做 E2 字段，gate 逻辑进 Phase 1。

---

## 6. 错误处理

| 场景 | 行为 |
|------|------|
| Hook.Before block | 不执行；向模型返回可操作 reason；发事件（PermissionDenied 或 HookBlocked） |
| RCA 证据不足（Final-answer） | 默认 Soft：阻断「空口根因」终稿，注入回压 / `evidence_incomplete`；允许显式「证据不足」结论文案通过；可配 HardHalt |
| CDP/Jaeger/ES 暂态失败 | `error_code=transient` + retry hint |
| 永久错误（越权、无效 selector） | 不重试 |
| Hook panic | 可配 fail-open / fail-closed；生产 tool 路径建议 fail-closed |
| 成长/CDP 清理失败 | 不拖垮用户响应；日志 + 指标 + 异步重试 |

---

## 7. 测试要点

- 单测：Hook 顺序与 block；E1 门控表驱动；CDP CheckFn；有 spawn 时 C4 父子 budget  
- 集成：Portal 注册 RCA/CDP flag → schema 快照；confirm 环 E2E  
- 回归：ask_user / execute_write / Growth fork 不破  
- 默认空 Hook 切片时行为与改造前一致  

---

## 8. 风险

| 风险 | 缓解 |
|------|------|
| Hook 变成第二套中间件 | Hook=工具/会话策略；Middleware=HTTP/Agent 外围 |
| 证据门误杀「信息不足」合法答复 | Soft 默认 + 允许显式「证据不足」结论 |
| CDP 范围膨胀 | Phase 2 锁 4 动作；其余进 backlog |
| 与 hermes-gap P0 排期叙事冲突 | 本设计为优先级权威；hermes-gap 保留为能力/验收目录 |

---

## 9. 开放问题（实施前可再收口）

| ID | 问题 | 建议默认 |
|----|------|----------|
| Q1 | HookBlocked 用新事件 Kind 还是复用 PermissionDenied | 新 Kind，避免语义混淆 |
| Q2 | EvidenceGate 实现位置 | **Final-answer Evaluator**（与 ToolGuardrails 并列，**不**在每 tool After）；见 §3.3（已收口） |
| Q3 | CDP 后端（本地 Chrome vs remote） | Phase 2 先 local + 可配 endpoint；CheckFn 探活 |
| Q4 | ChatSession 结束信号由谁发 | Portal 会话关闭/TTL/用户结束；framework 只定义钩子接口 |

---

## 10. 下一步

1. 用户审阅本 spec  
2. 通过后调用 **writing-plans** 产出 Phase 0 实施计划（`docs/superpowers/plans/2026-07-11-harness-control-plane.md`）  
3. 实施时以本文件 ID（C1…S2）为追溯键，更新 hermes-gap 文档「优先级以 harness spine 为准」交叉引用  
