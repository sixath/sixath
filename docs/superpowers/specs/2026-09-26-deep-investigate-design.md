# deep_investigate 冷启动调查工具 — 设计文档

日期：2026-09-26
状态：已批准

## 背景与问题

当前 agent 的能力发现路径有三条：embedding skill 路由（自动注入 SKILL.md）、tool_search（BM25 检索 deferred 工具）、模型看摘要自主 load_skill。三条都依赖「问题能被映射到某个已命名的能力」。

但有一类问题天然没有映射入口：用户问「为什么这个接口偶尔返回空」这类**定位类问题**，没有 trace_id、没有报错原文、也不匹配任何 skill 描述。此时模型只能凭空回答。

现有的 `rca-investigation` skill 是**有线索优先**流程（报错原文/trace_id → jaeger → ES → 代码），入不了冷启动的场。

## 目标

做一个兜底工具 `deep_investigate`：agent 同时具备代码仓库工具和日志类工具时可见，模型在定位类问题无线索时主动调用，工具内部跑子 ReAct 循环（代码优先 → 日志验证），直接返回调查结论。

## 已确认的关键决策

| 决策点 | 结论 |
|---|---|
| 触发方式 | 模型主动调用（非路由自动注入、非事后重试） |
| 返回内容 | 内部子 Agent 跑完直接返回结论（非返回手册文本） |
| 子 Agent 工具范围 | 代码（rca_grep/rca_glob/rca_read/rca_symbol）+ 日志/链路（es_log_query/jaeger_trace/vm_run_cmd） |
| 子 Agent 模型 | 复用父 agent 的 chat 模型（质量优先，后续可加 aux 配置） |
| 工具名 | `deep_investigate` |
| embedding/日志等配置 | 零新增配置；可见性完全由门控决定 |

## 架构

```
framework/investigate（新包） → harness（子 ReAct 循环） → tool
portal/internal/chat（注册点，agent_builder.go BuildRegistry 末尾）
```

无循环依赖：harness 已依赖 tool，investigate 依赖两者。

## 组件设计

### 1. 门控（CheckFn）

portal 注册 RCA/ES 工具时失败即跳过，registry 里**存在即代表已配置**。门控规则：

- 代码工具 ≥1：`rca_grep` / `rca_glob` / `rca_read` / `rca_symbol`
- 日志链路工具 ≥1：`es_log_query` / `jaeger_trace` / `vm_run_cmd`
- 两组都齐 → 工具可见；否则从 schema 消失
- `AlwaysLoad: true`，扛住 defer 过滤
- `RequiresSequential: true`（重操作，不与其他工具并行）

### 2. 工具 schema

```
deep_investigate(question: string, hints?: string)
→ {ok, conclusion, evidence_refs, insufficient_evidence, steps_taken, duration_ms}
```

description 写死使用时机：仅在用户要求定位/诊断问题、且没有 trace_id、报错原文、匹配 skill 可用时调用；每个问题最多调一次。

### 3. 子 Agent 编排（Execute 内部）

- 模型：父 agent chat 模型
- 工具子集：从父 registry 按名过滤，构成独立子 registry；**不含 deep_investigate 自身（防递归）**，不含 load_skill、写操作
- 系统提示（代码优先手册）：
  1. rca_grep/rca_read 读相关代码，理解逻辑，找到打日志的点
  2. 形成假设：哪里可能出错、会产生什么日志/span
  3. es_log_query / jaeger_trace / vm_run_cmd 带假设验证
  4. 输出结论 + 证据引用；证据不足允许明说
- 护栏：MaxSteps 15、整轮超时 15 分钟

### 4. 返回契约

沿用 RCA 工具链 evidence 纪律：结论每个断言挂 evidence_refs；查不到返回 `insufficient_evidence: true`，禁止编造。

### 5. Portal 接线

`agent_builder.go` BuildRegistry 末尾无条件调 `investigate.Register(reg, deps)`，可见性交给 CheckFn。

## 测试

- framework/investigate 单测：门控开关（只配代码/只配日志/都配/都不配）、子 registry 过滤不含自身、假模型全流程、超时、证据透传
- portal 注册冒烟测试
