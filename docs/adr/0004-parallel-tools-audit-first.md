# ADR-0004：并行工具「先审计 `RequiresSequential` 再默认开启」

- **状态**：已接受
- **日期**：2026-09-12

## 背景

ReAct 并行工具能显著降低多步任务的端到端延迟，但并发执行写入/审批/终端/浏览器类工具有竞态与破坏风险。历史默认 `ParallelTools=false` 压住了性能上限。

## 决策

- `ParallelTools` 默认改为 `true`，新增 `MaxParallelTools`（默认 8）。
- **前置审计**：逐个确认工具的 `RequiresSequential` 标记正确——写入/审批/终端/浏览器类必须串行；审计结论落在代码注释 + 测试。
- 保留 kill switch：`SATH_PARALLEL_TOOLS=0` 或配置 `parallel_tools: false`。

## 理由

- 性能收益不应用正确性/安全性买单；审计先行把风险前移。
- kill switch 保证灰度期间可一键回退。

## 后果

- 默认并行可能暴露既有工具的竞态 → 用 `-race` 单测 + 灰度 + kill switch 兜底。
- 代价：审计工作前置，且需持续维护 `RequiresSequential` 的准确性。
