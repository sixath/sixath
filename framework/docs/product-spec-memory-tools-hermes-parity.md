# 产品规格：记忆与工具体系改进（Hermes 能力对齐）

**版本**: 0.1  
**状态**: 规格稿（与实现解耦）  
**依据设计**: [design-memory-tools-hermes-parity.md](design-memory-tools-hermes-parity.md)（当前 **v0.2**）  
**开发计划**: [dev-plan-memory-tools-hermes-parity.md](dev-plan-memory-tools-hermes-parity.md)（WBS、里程碑、与 using-superpowers 对齐的执行留痕）  
**范围**: 以 `github.com/sixath/framework` 能力为主；Portal 行为、合规与文案为产品约束。

---

## 1. 文档目的与读者

- **目的**: 把设计稿中的工程目标（G1–G5）转写为**可验收的产品语言**，供产品、前端/Portal、后端与 SRE 对齐范围与验收口径。  
- **读者**: 产品经理、技术负责人、Portal 负责人、安全与合规接口人。

---

## 2. 背景与问题陈述（产品视角）

| 用户或运维感知 | 业务影响 | 与设计对应 |
|----------------|----------|------------|
| 长对话后回答质量变差、漏约束 | 信任度下降、重试成本 | G2 L0/L2、工具链原子单元 |
| 流式回复里出现「不像用户说的话」的块 | 困惑、误以为模型幻觉 | G1 围栏 + SSE scrub |
| 同一工具反复失败仍空转 | 耗时、配额浪费 | G3 护栏 |
| `ssh_exec` 等常因缺 host 失败 | 任务中断 | G4 参数策略（与设计中的 ssh 方向一致） |
| 线上 400、压缩是否发生难以追溯 | 排障慢 | G5 Trace / `context_ops` |

---

## 3. 产品目标与成功标准

与设计 [§1.2 设计目标](design-memory-tools-hermes-parity.md) 一一对应，并补充**可量化或可演示**的验收口径。

### 3.1 G1 — 记忆工程化

- **产品陈述**: 召回的长期记忆只进入**模型上下文**，默认**不进入**用户可见的聊天流（含 SSE）；生命周期可配置（预取、写回与现有会话脏标记衔接）。  
- **验收**:
  - 开启记忆编排且存在召回内容时：用户 UI 流中**不出现**围栏内正文（与设计 §4.8 单测场景一致）。  
  - 关闭该能力时：行为与现网一致（零回归默认路径）。

### 3.2 G2 — 上下文分级（L0/L1/L2）

- **产品陈述**: 在现有「按 rune 预算裁剪」之上，可选开启「工具输出预剪枝 + LLM 摘要」；用户侧可感知为「超长会话仍可完成」，而非暴露内部摘要全文（HANDOFF 文案策略由 §10 与 Portal 约定）。  
- **验收**:
  - L2 关闭：与现网行为一致。  
  - L2 开启（灰度内）：极端长对话下请求失败率不高于基线；`RunTrace`（或等价元数据）可查询是否发生 L2 及摘要哈希（多 invocation 见设计 §5.3.2、§8.1）。

### 3.3 G3 — 工具护栏

- **产品陈述**: 可选检测「同参重复失败 / 同工具连续失败 / 长期无文本产出」；默认**仅告警**（事件可接监控），可选**硬停**并给用户可读说明（文案见 [§10 开放问题 3](design-memory-tools-hermes-parity.md)）。  
- **验收**:
  - 默认配置：连续失败可观测（事件/日志），不擅自中断用户任务，除非显式打开硬停。  
  - 硬停开启：用户收到明确结束原因（具体 UI 由 Portal 实现，规格要求「非空且非技术堆栈」）。

### 3.4 G4 — 参数策略表

- **产品陈述**: 高危/高频工具关键字段具备统一别名解析与默认值（与 `ssh_exec` 的 host 策略同向）；减少「模型漏一个字段就全失败」。  
- **验收**: `ssh_exec` 既有用例与回归测试通过；策略表对至少一类字段的泛化单测通过（设计 §7.3）。

### 3.5 G5 — 可观测

- **产品陈述**: 一次 Agent Run 可在元数据中查询：压缩、sanitize、护栏、prefetch 跳过等**结构化**信息，便于仪表盘与工单。  
- **验收**: 一次 Run 结束后 `trace`（或约定字段）可 JSON 序列化且含 `context_ops`（若发生过相关变换）。

---

## 4. 范围

### 4.1 范围内（Must）

- ReAct / worker 路径上的记忆预取注入、上下文变换、护栏、参数策略与 Trace 扩展（与设计一致）。  
- Portal：**SSE scrub 最小集**（与设计 §4.3、§4.6 一致）、配置开关与灰度维度（与设计 §3 原则 5、§9 路线 A 一致）。

### 4.2 范围外（Must Not，本阶段）

与设计 [§1.3 非目标](design-memory-tools-hermes-parity.md) 对齐：

- 不替换 Portal 会话存储或 protobuf 协议。  
- 不在本阶段合并多个外置向量库 schema。  
- 不在 framework 内一次实现 Hermes `context_compressor` 全量特性。  
- **PlanExecuteAgent 规划阶段**默认不接 Prefetch（设计 §1.3 A3）；若未来产品需要，单独立项。

---

## 5. 用户场景与用户故事

### 5.1 企业用户（多租户）

- **As** 租户管理员，**I want** 长期记忆检索按租户隔离，**So that** 不会泄漏其他租户文档。  
- **验收**: `PrefetchQuery.Identity` 在生产为必填约定（设计 §4.4）；集成测试覆盖跨租户不可见。

### 5.2 终端用户（聊天 + 流式）

- **As** 使用流式聊天的用户，**I want** 只看到我与助手的对话，**So that** 召回的技术块不会混在正文里。  
- **验收**: SSE 输出经 scrub；异常 EOF 策略不泄露半段围栏内容（设计 §4.3、§4.6）。

### 5.3 运维 /  on-call

- **As** 值班人员，**I want** 从一次失败的 Run 里读出是否发生了压缩/护栏/L2，**So that** 我能区分「模型胡写」与「上下文被截断」。  
- **验收**: `ContextOpsTrace` 与多 invocation 记录（设计 §8.1）在文档化字段中可查。

---

## 6. 功能需求明细（按史诗）

### 6.1 记忆编排（Epic: Memory Orchestrator）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| M1 | 每 user turn 进入 ReAct 前可注入 0..1 条带围栏的记忆消息 | P0 | 设计 §4 |
| M2 | 围栏带 per-turn nonce，防正文闭合标签攻击 | P0 | 设计 §4.3 |
| M3 | Backend 超时 fail-open，可配置 fail-closed | P1 | 设计 §4.6 |
| M4 | Prefetch 输入含 `Recent`、`Identity` | P0 | 设计 §4.4 |
| M5 | 注入消息带 `Metadata["sixath.origin"] = memory_fence` | P0 | 设计 §3.1 |

### 6.2 上下文与 L2（Epic: Context）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| C1 | L0/L1 默认行为不变；L2 opt-in | P0 | 设计 §5 |
| C2 | 裁剪以「assistant(tool_calls)+全部 tool」为原子单元 | P0 | 设计 §5.3.1 |
| C3 | 同一次 Run 多次 `Chat` 的 trace 分 invocation 记录 | P0 | 设计 §5.3.2、§8.1 |
| C4 | Token 粗估对中文偏差有文档与运维余量 | P1 | 设计 §5.4 |
| C5 | L2 冷却进入与退出策略可配置、可观测 | P1 | 设计 §5.6 |

### 6.3 工具护栏（Epic: Guardrails）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| G1 | R1/R2/R3 可配置阈值；默认 warnings_only | P0 | 设计 §6 |
| G2 | R1 使用 `stableArgsKey`/规范化，非裸 `json.Marshal` | P0 | 设计 §6.1 |
| G3 | 幂等/变更工具集可配置 | P1 | 设计 §6.3 |
| G4 | 硬停注入带 `sixath.origin=guardrail_halt` | P0 | 设计 §6.2、§3.1 |

### 6.4 参数策略（Epic: Param Policy）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| P1 | `ssh_exec` 迁移到声明式策略注册 | P1 | 设计 §7 |
| P2 | 策略包可被其他工具复用 | P2 | 设计 §7.2 |

### 6.5 可观测（Epic: Trace）

| ID | 需求 | 优先级 | 备注 |
|----|------|--------|------|
| O1 | `RunTrace` 扩展 `ContextOps` / `Invocations` | P0 | 设计 §8 |
| O2 | TraceSink 不引入 model→agent 循环依赖 | P0 | 设计 §8.1 |

---

## 7. 非功能需求（NFR）

| 类别 | 要求 | 设计依据 |
|------|------|----------|
| 兼容性 | 全能力默认关闭时与现网行为一致 | §3 原则 1 |
| 性能 | Prefetch 有硬超时；L2  auxiliary 调用不阻塞无限长 | §4.6、§5 |
| 安全 | 多租户隔离；召回内容视为不可信；围栏+nonce | §4、§12 溯源 |
| 合规 | 落库、导出、审计以书面结论为准；L2 与回放策略 | [§10.1](design-memory-tools-hermes-parity.md) |
| 可运维 | 灰度：agent 白名单 / traffic hash / 影子流量 | §3 原则 5 |
| 国际化 | 压缩说明等允许 i18n；逻辑以 `sixath.origin` 为准 | §3.1 |

---

## 8. 与中间件及 Portal 的边界

- **内容安全（`ContentSafetyMiddleware`）**: 仍负责用户可见链路的政策类拦截；**不**被记忆 scrub 替代。  
- **记忆 scrub**: 仅保证「模型可见、UI 不可见」的块不展示。  
- **护栏**: 工具健康度与停机，**不**替代内容安全。  

判据见设计 [§4.7](design-memory-tools-hermes-parity.md)。

---

## 9. 开放产品决策（阻塞实现前需拍板）

与设计 [§10](design-memory-tools-hermes-parity.md) 及 [§10.1](design-memory-tools-hermes-parity.md) 对齐，需在 Sprint 0 关闭或明确「延后」：

1. Prefetch 使用 `system` 还是 `user`，及与压缩说明消息的相对顺序。  
2. L2 auxiliary 是否与主模型共用 API key / 厂商。  
3. 护栏硬停在 Portal 的专用文案与错误码约定。  
4. 外置记忆后端是否严格单实例。  
5. 落库：原始流 / scrub 后 / 双写；导出与审计以哪份为准；L2 是否启用旁路 `ReplayDataset`。

---

## 10. 验收与发布闸门

- **功能验收**: 各史诗表格 + 设计稿 §4.8、§5.7、§6.4、§7.3、§8.2。  
- **回归**: L2 全关、Orchestrator 全关时，现有 ReAct / Portal 主路径自动化测试通过。  
- **灰度**: 首发生产环境须配置白名单或低比例 traffic，避免默认全开（设计 §3 原则 5、§9）。

---

## 11. 文档维护

- 设计稿升级至 v0.3+ 时，同步本规格中的章节锚点与开放问题状态。  
- 实现落地后，在 [api-reference.md](api-reference.md) 或 Portal 架构文档中增加配置索引链接（设计 §11）。  
- **开发排期**以 [dev-plan-memory-tools-hermes-parity.md](dev-plan-memory-tools-hermes-parity.md) 为准；规格变更后应同步该计划 §1.1 追溯矩阵与 Sprint 0 清单。
