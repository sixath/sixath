# ADR-0003：Plan-Execute 模式默认关闭（`Agent.Mode = react`）

- **状态**：已接受
- **日期**：2026-09-12

## 背景

结构化 Plan-Execute（Task 14）对复杂多步任务能降低步数、提升可解释性，但 planner 本身多一次模型调用，且规划失败会放大成本/延迟。已验证的 ReAct 主路径不应因新能力而默认改变行为。

## 决策

- `Agent.Mode = react | plan`，**默认 `react`**（保守）。
- planner 输出非法 → 修复重试（≤2），仍失败 → **回退 ReAct**（而不是失败整个 turn）。
- 步骤失败 → replan（≤2），仍失败 → 回退 ReAct。

## 理由

- 不推翻已验证的 ReAct 路径；Plan 模式作为可选执行策略叠加，风险可控。
- 用 eval（Task 13）量化对比后再决定是否切换默认，避免「感觉变聪明」的伪增益。

## 后果

- 默认行为不变，存量 agent 无感知。
- 代价：需要 Portal 配置按 agent 选择 `Mode`（装配未完）。
