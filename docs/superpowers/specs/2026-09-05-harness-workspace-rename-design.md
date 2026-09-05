# S3 包搬家：`harness` + `workspace`

**日期**: 2026-09-05  
**状态**: 已确认（设计评审，2026-09-05）  
**范围**: 只改目录与 import，**不改循环语义**，不改 PromptBuilder 规则。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S1](./2026-09-05-dead-code-hub-off-design.md)、[S2](./2026-09-05-context-promptbuilder-design.md)

**一句话**: 把还活着的骨架迁到 `framework/harness`，把路径守卫与 `code/` 挂载纯函数迁到 `framework/workspace`；`model` 仍不得 import harness。

---

## 1. 背景

父规格 §3 的目标目录在减肉阶段故意后置。S2 已把血肉放到 `framework/context`。S3 完成骨架与 workspace 的包名，避免继续用 `agent` 这个把「骨架 + 旧闸」混在一起的词。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| harness | `github.com/sixath/framework/harness` ← 现 `framework/agent` 中仍存活的循环/Hook/预算/生命周期 |
| workspace | `github.com/sixath/framework/workspace`：`ResolveWorkspacePath`、约定目录文档、`code/` symlink 纯函数 |
| 别名 | 留一季 `framework/agent`：对 Harness 公开类型与 `New*` / `WithReAct*` 做转发；**新代码禁止** import `agent`；`model` 不得 import `agent`。**S13 已删包** |
| 旧闸 | **先删再搬**仅限已确认零生产调用者的 P1 领域闸文件。`plan_agent.go` / `post_model_policy.go` 不是强制删除项 |
| Portal | 仍拥有 Agent.workspace 列与 `{data_root}/agents/{id}/` 创建（P2）；HTTP/ACL 留在 portal |
| 不搬 | L0/L1/L2（已在 context）；MCP/器官实现；Growth / MEA / Hub 包名 |

---

## 3. 目标 import

```text
github.com/sixath/framework/harness
github.com/sixath/framework/workspace
github.com/sixath/framework/context
github.com/sixath/framework/model      // Provider only
```

依赖：`portal → harness → context / workspace / tool`；`model` **不得** import `harness` 或 `context`。

---

## 4. harness

迁入（非穷尽，以实施时仍存在的文件为准）：`react_agent.go`、`tool_hook.go`、`harness_hooks.go`、`chat_session_hook.go`、`trace.go`、`stream.go`、`guardrail_evaluator.go`、`failure_capture_hook.go`、`tool_guardrail.go`、request/metadata/context_ops。

**先删再搬**仅限已确认 **零生产调用者** 的 P1 领域闸（例如仍在磁盘上的 `evidence_gate.go`、`inbound_gate.go`、`code_workset.go` 及测试）。`plan_agent.go` / `post_model_policy.go` **不是**强制删除：无调用者才删，否则随包迁走，避免改循环语义。

别名包只做类型与构造函数转发，不复制逻辑。实施计划必须列出转发的公开符号。`model` 仍不得 import `harness` 或 `agent`。

---

## 5. workspace

- 从 `framework/tool/pathguard.go` 抽出 `ResolveWorkspacePath`：空 root 非法；拒绝逃出根。行为与现网一致。
- 约定目录（契约，不是新进程）：`skills/`、`harness/hooks.yaml`、`MEMORY.md` / `USER.md`、`code/`。
- `{workspace}/code` → symlink：纯函数从 Portal 挪进本包（目标必须在 `code_roots` 内的规则仍由调用方传入允许根列表）。`portal/internal/server/code_roots.go` 只保留 HTTP、鉴权、浏览；创建链接调用 `workspace` 包。
- `file_tools.go`、terminal 等文件器官改为 `workspace.ResolveWorkspacePath`，删除 tool 包内重复守卫。

Portal 创建 Agent 时 `MkdirAll {data_root}/agents/{id}/` 不变。

---

## 6. 非目标

- 不改 ReAct 步数、Hook 顺序、Permission/confirm
- 不改 PromptBuilder Stable/Ephemeral 规则
- 不强制已有整仓 Agent 迁移
- 不把 `framework/tool` 整包改名

---

## 7. 成功标准

1. `grep github.com/sixath/framework/agent` 仅出现在别名包、本 spec/计划、以及明确的迁移说明。
2. `go test ./framework/harness ./framework/workspace ./framework/context ./framework/model ./framework/tool ./framework/agent -count=1` 绿（`agent` 仅为别名包测试）。
3. 空 workspace root 仍拒跑；`hooks.yaml` block 仍生效；新建 Agent 仍有默认可写根。
4. 循环单测换 import 后断言不变（system prompt 注入、Hook Before block）。
5. `code/` 挂载：允许根内创建 symlink 成功；目标越出允许根拒绝；空 workspace root 仍非法。Windows 上若无法建 symlink，错误可观测且不写到允许根之外（实施计划写清跳过或用 junction 的测试策略）。

禁止把 `_neo4j_q/` 当夹具。禁止一份 PR 把 S3 与 S1/S2 混在一起。
