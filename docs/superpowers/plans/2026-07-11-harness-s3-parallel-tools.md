# S3：并行 Tool 执行 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 可选开启同轮多 tool 并行（Hermes D2），默认关闭以保持零行为回归；任一轮若含 `RequiresSequential` 工具则整轮串行（D1）。

**Architecture:** `Tool.RequiresSequential` + 内置写/交互工具默认 true；`ReActConfig.ParallelTools`（默认 false）与 `MaxParallel`（默认 8）；`executeToolStep` 在可并行时用 semaphore + 按 slot 写回。

**Tech Stack:** Go；`framework/tool`、`framework/agent`；`golang.org/x/sync` 非必需（自管 WaitGroup + sem）。

**Spec:** gap design S3；`design-agent-runtime-hermes-inspired.md` §7.2。

> **Git：** 无仓库则跳过 Commit。  
> **非目标：** fail_fast 配置、process 后台栈、改默认生产开启并行。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Modify `framework/tool/tool.go` | `RequiresSequential` 字段 |
| Modify `framework/tool/toolset.go` 或新 `sequential.go` | 内置名默认序列表；Register 时填充 |
| Modify `framework/agent/react_agent.go` | ParallelTools / MaxParallel；`executeToolStep` D2 |
| Modify `framework/agent/trace.go` | `ParallelTools bool`（本步若并行则为 true） |
| Create `framework/agent/react_parallel_tools_test.go` | 顺序写回、sequential 强制串行、默认关闭 |
| Modify gap design S3 行 | 已落地 |

---

### Task 1: RequiresSequential

- 写冲突 / 交互默认 true：`write_file`、`patch`、`execute_write`、`skill_manage`、`terminal`、`ssh_exec`、`scp`、`ask_user`、`todo`、`cronjob`、全部 `browser_*`
- 只读默认 false：`read_file`、`web_search`、`execute_read`、`rca_*`、`jaeger_trace`、`es_log_query` 等
- Register：若调用方未显式设 `RequiresSequential`，按 builtin map 填充（与 Toolset 填充同模式）

- [x] 实现 + 单测 Lookup

### Task 2: executeToolStep 并行

- `WithReActParallelTools(enabled bool)`；`WithReActMaxParallel(n int)`（n<=0 → 8）
- `shouldParallelize`：`ParallelTools && N>1 && 无 RequiresSequential`
- 并行：slot 写回；完成后按 index 返回首个 hard error（PermissionDenied / NotFound）；Trace.ParallelTools=true
- 默认 ParallelTools=false → 现有串行路径

- [x] 测：两只慢只读并行完成，历史顺序 = tool_calls 顺序
- [x] 测：含 terminal → 串行
- [x] 测：默认关闭时行为与旧一致
- [x] `go test -race ./agent/ -run Parallel -count=1`

### Task 3: Docs

- gap S3 已落地；说明默认关、Portal 若要开需显式 option

- [x] Docs

---

## 验收

- [x] 默认关闭：零回归
- [x] 开启 + 全只读：并行且写回有序
- [x] 含 RequiresSequential：整轮串行
