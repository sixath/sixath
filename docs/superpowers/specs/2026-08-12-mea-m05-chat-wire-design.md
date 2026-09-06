# MEA M0.5：Chat 薄接线

> 状态：已实现（M0.5）  
> 日期：2026-08-12  
> 分支：`feature/mea-minimal-subset`  
> 前置：[M0 规格](./2026-08-12-mea-minimal-subset-design.md)、[M0 计划](../plans/2026-08-12-mea-minimal-subset-m0.md)  
> 端到端现状（入口分流 / ReAct / SSE / 排障）：[task-handling current](./2026-08-15-task-handling-current-design.md)

## 目标

在 **不默全开** 前提下，让 `SendMessageStream` 在 flag/pilot 开启且用户消息带有可机检 `AcceptanceChecks` 时，走 `RunRulesMEA`（ReAct 作 Executor + RulesAuditor），把 M0 接到线上路径。

## 进入条件（同时满足）

1. `MEAEnabledForAgent(agentID)`  
2. 用户消息解析出 **非空** `AcceptanceChecks`  
3. `WorkDir` = `agentMeta.Workspace`（空则 **不进入** MEA，降级普通流式，避免 CWD 假通过）

不满足 → **现网路径零行为变化**。

## Checks 语法（用户消息）

独立 fenced 块（从消息中剥离后再持久化/送模型）：

````text
用户目标描述……

```mea-checks
[
  {"type":"path_exists","path":"out.txt"},
  {"type":"file_contains","path":"out.txt","pattern":"hello"}
]
```
````

- JSON 数组，元素为 M0 `AcceptanceCheck`  
- 解析失败或空数组 → 不进入 MEA（可打 warn 日志）

## 执行

- `BootstrapManager{Goal: cleanUserText, Checks}`  
- `Executor`：每轮用当前 `Contract.Goal`（及 acceptance 摘要）跑 **一次** 现有 ReAct `RunEvents`，事件仍进原 SSE channel  
- `RulesAuditor{WorkDir: workspace}`  
- 轮次结束后向 SSE 发 `mea` 事件（状态摘要），不阻断 chunk/tool_call  

## 非目标

- LLM Auditor（M1）  
- UI 进度条（M2）  
- 无 checks 时自动猜 acceptance  
- 改 SendMessage 非流式路径（可同逻辑 follow-up）
