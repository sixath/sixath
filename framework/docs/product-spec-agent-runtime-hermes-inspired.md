# 产品规格：Agent 运行时（Hermes 理念对齐）

**版本**: 0.1  
**状态**: 规格稿（与实现可渐进对齐）  
**依据设计**: [design-agent-runtime-hermes-inspired.md](./design-agent-runtime-hermes-inspired.md)（当前 **v1.1**）  
**关联规格**: [product-spec-memory-tools-hermes-parity.md](./product-spec-memory-tools-hermes-parity.md)（记忆、压缩、护栏；与本规格边界见第 11 节）  
**工具集映射**: [toolsets-hermes-mapping.md](./toolsets-hermes-mapping.md)（`web` / `file` / `skills` / `memory` / `terminal` / `mcp`；**已实现** `Tool.Toolset`、`ListByToolsets`、`PresetHermesCoreTags`）  
**范围**: 以 `github.com/sixath/framework` 编排、模型适配、工具过滤与循环语义为主；Portal 接入、合规与运维可观测为产品约束。

---

## 0. 实施留痕（对齐 using-superpowers 工作方式）

本规格落地时，实施方（人或 Agent）应在任务单 / PR 描述 / 内部 wiki 中保留**可追溯**的简明记录，满足以下最小集（短句即可，含路径或命令）：

| 类型 | 必填内容 |
|------|----------|
| **起始** | 目标、对照的设计章节（如 G-O3）、预期交付物（代码/文档/测试）。 |
| **动作** | 每次合并关键 PR 或提交：改了哪些包、为何、单测结果摘要。 |
| **决策** | 对设计第 16 节开放问题（Q1–Q4）的取舍及负责人。 |
| **异常** | 构建失败、测试失败、外部依赖异常：现象、已排查项、下一步。 |
| **收尾** | 完成项、未完成项、已知风险、建议下一里程碑。 |

**禁止**：仅写「已完成」而无证据（无 PR、无测试命令输出、无文档版本号）。

---

## 1. 文档目的与读者

- **目的**: 将设计稿中的工程目标 **G-O1～G-O9** 转写为**可验收的产品语言**，并与分阶段路线（设计第 12 节 P0–P6）对齐，供产品、后端、Portal、SRE 统一验收口径。  
- **读者**: 产品经理、技术负责人、Portal 负责人、负责 Agent 编排的工程师、值班与可观测接口人。

---

## 2. 背景与问题陈述（产品视角）

| 用户或运维感知 | 业务影响 | 与设计目标对应 |
|----------------|----------|------------------|
| CLI、API、定时任务对「带工具对话」行为不一致 | 排障难、回归面大 | G-O1 |
| 换模型厂商要改多处循环或消息拼装 | 迭代慢、易出 bug | G-O2 |
| 无法区分「身份提示」与「本回合告警」，缓存命中率差 | 成本与延迟 | G-O3 |
| 缺依赖的工具仍出现在模型可选列表里，反复 400 / 空转 | 配额浪费、体验差 | G-O4 |
| 多工具顺序错乱或取消后历史半截 | 厂商拒答、数据不可信 | G-O5、G-O6 |
| 子任务拖死主会话无上限 | 费用与超时 | G-O7 |
| 二进制体积过大、无关驱动进默认构建 | 分发与合规 | G-O8 |
| 无法跨会话检索或 CLI/API 历史分裂 | 粘性弱、支持成本高 | G-O9（可选） |

---

## 3. 产品目标与成功标准

与设计 [第 1.2 节 设计目标](design-agent-runtime-hermes-inspired.md) 一一对应；下列为**产品可验收**表述。

### 3.1 G-O1 — 单一编排核心

- **产品陈述**: 所有「带工具的多轮对话」经**同一编排路径**；新入口不得各自实现 `for` + `ChatWithTools`。  
- **验收**: 新增一种入口（如 Worker）时，代码审查确认**未复制** ReAct 主循环；仅调用统一 Runner（或等价 `ReActAgent` 封装）。可选：CI 对 Portal 包启用「禁止直接 `ChatWithTools` + 自建循环」规则（设计第 3.3 节）。

### 3.2 G-O2 — 内部 canonical 消息

- **产品陈述**: 更换或新增云厂商时，用户可感知的对话质量与工具行为**不因循环内散落分支**而漂移；适配集中在模型层。  
- **验收**: 引入第二种 `APIMode` 的 PR 中，`react_agent.go` 步进循环 **diff 行数**不超过团队约定（设计建议 ≤80 行或零改循环）。

### 3.3 G-O3 — Prompt 稳定层与临时层

- **产品陈述**: 身份与长期一致内容与「本回合临时说明」分离；启用稳定哈希时，仅临时层变化不改变稳定指纹。  
- **验收**: 单测或集成测：Stable 固定、仅 Ephemeral 变化 → `prompt_stable_hash`（若产品启用）不变；与 [design-memory-tools-hermes-parity.md](design-memory-tools-hermes-parity.md) 中 `sixath.origin` 约定不冲突。

### 3.4 G-O4 — 工具可发现、可过滤、可审批

- **产品陈述**: 运维或租户可配置「启用哪些工具集」；缺依赖或策略拒绝的工具**不应**诱导模型无效调用（默认倾向**不出 schema**，见设计第 16 节 Q1）。  
- **验收**:  
  - 给定 `enabled_toolsets` 白名单时，下发给模型的 tools 列表**仅含**允许集合（与 [toolsets-hermes-mapping.md](toolsets-hermes-mapping.md) 标签一致；**已实现**部分：`ListByToolsets` + 内置 `Toolset`）。  
  - `Available(ctx)` 落地后：模拟缺环境变量 → 对应工具**不在**当次 schema 中（单测快照）。  
  - `PermissionPolicy` 与 Q1 最终产品决策一致并有文档。

### 3.5 G-O5 — 循环语义正确

- **产品陈述**: 多工具结果写入历史的顺序与模型 `tool_calls` 顺序一致；历史在入模型前经统一规范化，减少 400。  
- **验收**: `NormalizeHistory`（或等价单一入口）表驱动单测覆盖设计第 7.1 节默认策略；多 tool 顺序单测（串行路径必测；若上 D2 则加 race 测）。

### 3.6 G-O6 — 取消与超时

- **产品陈述**: 用户取消或超时后，不暴露「半条助手消息」到持久化会话；内存态可丢弃。  
- **验收**: 取消场景集成测或单测：Trace 含 `canceled=true`；Portal 已提交库中**无**未完成 assistant 步（设计第 7.3.1 节）；`go test -race` 子集无悬挂 goroutine（若 CI 启用）。

### 3.7 G-O7 — 预算与子会话

- **产品陈述**: 主对话与子代理（若产品提供）均有步数上限，子代理不得耗尽父级全部预算而无独立上限。  
- **验收**: Trace 或错误体含 `stop_reason=budget_exceeded`；子代理配置与 Q4 决策一致并有测试。

### 3.8 G-O8 — 可选构建

- **产品陈述**: 企业可构建「瘦」二进制，不包含未订购的可选驱动（如特定 MCP 后端）。  
- **验收**: CI 或发布流水线中 `go list -deps`（带约定 **minimal** 类 build tag）与允许依赖列表比对通过（设计第 9.2 节）。

### 3.9 G-O9 — 持久会话（可选）

- **产品陈述**: 若产品启用 framework 侧会话库：支持版本迁移、WAL；与 Portal 存储关系**明确为方案 A 或 B**（设计第 10.3 节），默认 **A**。  
- **验收**: 迁移可回滚；无双写同一 `session_id` 而无同步器（设计第 13 节）。

---

## 4. 范围

### 4.1 范围内（Must）

- 编排单一入口（Runner 或等价契约）、Provider/APIMode 边界、Prompt Stable/Ephemeral、工具过滤与钩子、循环与取消语义、预算、事件与 Trace 扩展字段（与设计第 14 节对齐）。  
- Portal：**调用统一编排**、工具集/权限配置 UI 或配置下发（与 Q1 一致）、取消与历史提交策略（第 7.3.1 节）。  
- 可观测：在现有 `events.Kind` 上扩展 Payload，避免重复枚举（设计第 8 节）。

### 4.2 范围外（Must Not，与设计第 1.3 节一致）

- 不实现 Hermes 全量 IM 网关。  
- 不强制替换 Portal 会话存储协议。  
- 不在本规格内要求一次完成「技能市场」后端；目录与 agentskills.io 兼容可作为独立里程碑。

---

## 5. 用户场景与用户故事

### 5.1 平台管理员

- **As** 管理员，**I want** 按工具集（web/file/skills/memory/terminal/mcp）启用能力，**So that** 租户默认面暴露最小工具面。  
- **验收**: 配置变更后，同一模型请求前后 tools 列表diff可审计；与 [toolsets-hermes-mapping.md](toolsets-hermes-mapping.md) 一致。

### 5.2 终端用户

- **As** 用户，**I want** 取消长任务后聊天记录不出现半截回复，**So that** 我不困惑「这条是不是坏了」。  
- **验收**: 取消路径下 DB / UI 展示符合第 3.6 节。

### 5.3 运维 / on-call

- **As** 值班，**I want** 从一次 Run 的 trace 读出是否压缩、是否取消、是否触达步数上限，**So that** 我能区分配额、模型错误与上下文问题。  
- **验收**: `RunTrace`（或约定 JSON）含设计第 14 节字段子集，且与记忆规格中的 trace 字段可合并查询。

---

## 6. 功能需求明细（按史诗）

### 6.1 编排核心（Epic: ConversationRunner）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| R1 | 对外暴露统一 `Run(ctx, RunInput) (RunOutput, error)` 或文档等价契约；`ReActAgent` 可为默认实现 | P1 | 第 3 章 |
| R2 | Portal 仅组装入参与写回，不复制 tool 循环 | P0 | 第 3.2 节规则 1–2 |
| R3 | `PlanExecuteAgent` worker 阶段复用同一编排路径 | P1 | 第 3.2 节规则 3；记忆规格非目标 A3 |

### 6.2 模型与 Provider（Epic: APIMode）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| P1 | `APIMode` 枚举与解析顺序（显式 > 提供商 > URL > 默认） | P1 | 第 4.2.3 节 |
| P2 | `RequestEncoder` / `ResponseDecoder` 与 `ToolSchema` 构建路径单一 | P1 | 第 4.2.2 节 |
| P3 | Encode/Decode 失败语义进入 Trace 或 `RunError` | P1 | 第 4.2.4 节 |

### 6.3 Prompt（Epic: PromptBuilder）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| PR1 | Stable / Ephemeral 产出与合并规则（单 system 默认） | P1 | 第 5.2 节 |
| PR2 | 压缩在 Stable/Ephemeral 决策之后执行 | P0 | 第 5.2.3 节；记忆规格 |
| PR3 | 可选 `prompt_stable_hash` 写入 Trace | P2 | 第 5.3、14 节 |

### 6.4 工具运行时（Epic: Tool Runtime）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| T0 | 内置工具带 Hermes 对齐 `Toolset`；`ListByToolsets`；`PresetHermesCoreTags` | **已交付** | `tool/toolset.go` |
| T1 | `Tool.Available(ctx) error` 与构建 schema 时过滤 | P1 | 设计第 6.1–6.2 节 |
| T2 | `ListForAPI` 或等价：合并 toolset、黑名单、`Available`、`PermissionPolicy`（Q1） | P1 | 设计第 6.2 节 |
| T3 | 可选 `[]ToolHook`，顺序与设计第 6.3 节一致；`Before` error 不调 `Execute` | P2 | 设计第 6.3 节 |
| T4 | `RequiresSequential` 与 D1 整轮串行默认 | P0 行为保持 | 设计第 7.2.1 节；D2 为 P5 |
| T5 | 日志中 params 脱敏 | P0 | 设计第 6.5 节 |

### 6.5 循环与取消（Epic: Agent Loop）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| L1 | `NormalizeHistory` 单一入口 + 默认合并/报错策略 | P0 | 第 7.1 节 |
| L2 | `context` 贯穿模型与工具；取消 Trace `canceled` | P0 | 第 7.3–7.5 节 |
| L3 | 持久化不提交半截 assistant 步 | P0 | 第 7.3.1 节 |
| L4 | D2 并行可选，`parallel_tools` 与 `tool.max_parallel` | P2 | 第 7.2.2、12 节 P5 |

### 6.6 可观测（Epic: Observability）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| O1 | 新观测需求优先扩展现有 `events.Kind` 的 Payload | P1 | 第 8 节 |
| O2 | Trace 字段与设计第 14 节对齐渐进落地 | P2 | 第 14 节 |

### 6.7 构建与会话（Epic: Build & Session）

| ID | 需求 | 优先级 | 设计锚点 |
|----|------|--------|----------|
| B1 | minimal 类 tag 的依赖门禁 | P2 | 第 9 章 |
| S1 | 可选 `framework/session` + 方案 A/B 文档化 | P3 | 第 10 章、P6 |

---

## 7. 非功能需求（NFR）

| ID | 类别 | 要求 |
|----|------|------|
| N1 | 可靠性 | 取消与超时不得导致进程级泄漏；关键路径可测。 |
| N2 | 可维护性 | 编排、编码、工具过滤职责分包清晰；禁止 Portal 内嵌套第二套 ReAct 循环（R2）。 |
| N3 | 安全 | Hook 与 Bus 职责遵守设计第 6.5 节；密钥不出现在明文日志。 |
| N4 | 兼容性 | 默认配置下与现网行为一致（D1 串行、未开 Prompt 分层时退化路径明确）。 |

---

## 8. 里程碑与版本对齐（对应设计第 12 节）

| 里程碑 | 设计阶段 | 规格交付物 |
|--------|----------|------------|
| M0 | P0 | `NormalizeHistory` 语义冻结 + 测试 + 留痕 |
| M1 | P1 | T1/T2 + Q1 产品决策文档化 |
| M2 | P2 | PR1–PR3 最小可用 |
| M3 | P3 | R1/R2 + Portal 改调 |
| M4 | P4 | P1–P3（APIMode 第二厂商） |
| M5 | P5 | L4（D2）可选上线 + race CI |
| M6 | P6 | S1（会话模块）产品选型后 |

---

## 9. 开放问题产品结论表（跟踪设计第 16 节）

实施前须在工单中**关闭**或**延期**下列项并记录决策留痕：

| ID | 问题 | 规格要求 |
|----|------|----------|
| Q1 | 拒绝工具：不出 schema vs 执行拒绝 | 产品书面选择 + Portal 与框架一致 |
| Q2 | D2 fail-fast | 默认否；若改为是需版本说明 |
| Q3 | `ToolHook.After` 顺序 | 默认与 Before 同序；变更需评审 |
| Q4 | 子代理与父 `MaxSteps` 耦合 | 产品与框架一致 + 测试 |

---

## 10. 验收清单（发布前抽检）

- [ ] G-O1：新入口无重复 ReAct 循环（代码审查证据）。  
- [ ] G-O5：多 tool 顺序单测通过（串行）。  
- [ ] G-O6：取消路径 Trace 与持久化符合第 3.6 / 7.3.1 节。  
- [ ] G-O4：工具集白名单与 `ListByToolsets`（或等价）一致；Q1 已决。  
- [ ] 与记忆规格：Prefetch / 压缩顺序与 PromptBuilder 无冲突（联合回归）。  
- [ ] 第 0 节留痕：PR 或工单含起始/收尾与关键决策。

---

## 11. 文档维护

| 事件 | 动作 |
|------|------|
| 设计 `design-agent-runtime-hermes-inspired.md` 升版本 | 本规格核对 G-O 与史诗表，更新「依据设计」版本号 |
| 发布 M1–M6 任一项 | 更新本规格版本号与「状态」；勾选第 10 节相关项 |

---

## 12. 版本历史

| 版本 | 日期 | 说明 |
|------|------|------|
| 0.1 | 2026-05-02 | 初稿：对应设计 v1.1；含 using-superpowers 实施留痕要求；标注 Toolset 已交付项 |
