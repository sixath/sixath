# MEA M1：LLM Auditor

> 状态：已实现  
> 日期：2026-08-13  
> 分支：`feature/mea-minimal-subset`  
> 前置：[M0 规格](./2026-08-12-mea-minimal-subset-design.md) §8  
> 端到端现状（入口分流 / ReAct / SSE / 排障）：[task-handling current](./2026-08-15-task-handling-current-design.md)

## 交付

| 组件 | 行为 |
|------|------|
| `LLMAuditor` | 新鲜上下文（goal / state / contract / o_i）；只读目录名入 prompt；JSON → AuditReport |
| `CascadeAuditor` | 有 `AcceptanceChecks` → 只跑 Rules（失败一票否决）；仅文本 `Acceptance` → LLM |
| `BootstrapManager` | `Checks` 或 `Acceptance` 任一非空可 execute |
| Portal | `mea-acceptance` fence；流式路径传入 Agent model |

## 消息示例

````text
确认报告写清楚了验收结论。

```mea-acceptance
["workspace 下存在 summary.md 且内容说明任务已完成"]
```
````

与 `mea-checks` 可并存；有 checks 时仍以机检为准，不调 LLM 覆盖失败。
