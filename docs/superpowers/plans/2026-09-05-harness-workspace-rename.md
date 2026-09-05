# S3 Harness + Workspace Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现网 `framework/agent` 迁到 `framework/harness`，抽出 `framework/workspace`（路径守卫 + `code/` 挂载）；仓内新代码改 import；留一季 `framework/agent` 别名转发。

**Architecture:** `portal → harness → context / workspace / tool`。`model` 不得 import `harness` / `context` / `agent`。别名包只 `type X = harness.X` 与构造函数转发，不复制逻辑。不改 ReAct 步数、Hook 顺序、PromptBuilder 规则。

**Tech Stack:** Go（`framework/harness`、`framework/workspace`、`framework/tool`、portal chat/server）

**规格:** [`2026-09-05-harness-workspace-rename-design.md`](../specs/2026-09-05-harness-workspace-rename-design.md)

**分支:** 从 `feature/s2-context-promptbuilder` 切 `feature/s3-harness-workspace-rename`。不要在 `main` 上改。PowerShell 无 HEREDOC：`git commit -m "..."`。不要 `--no-verify`。不要提交 `_neo4j_q/`。不要把 S1/S2 另开 PR 混进来（本分支叠在 S2 上即可）。

**磁盘事实（以 `Get-ChildItem` 为准，Cursor Grep 会扫到已删文件）：** `evidence_gate.go` / `inbound_gate.go` / `code_workset.go` / `plan_agent.go` / `post_model_policy.go` / `turn_intent_gate.go` **已不存在**。不要再 `git rm` 这些路径。`LoadWorkspaceHarnessHooks` 仍被 `portal/internal/service/growth_chat.go` 调用。

---

## File map

| 动作 | 路径 |
|------|------|
| 新建路径守卫 | `framework/workspace/path.go` + `path_test.go`（从 `framework/tool/pathguard.go` 原样迁行为） |
| 约定目录常量 | `framework/workspace/layout.go`（`skills/`、`harness/hooks.yaml`、`MEMORY.md`、`USER.md`、`code/`） |
| `code/` 挂载 | `framework/workspace/code_link.go` + `code_link_test.go`（从 `portal/internal/server/code_roots.go` 的 `linkWorkspaceCode` / 目标校验抽出纯函数） |
| 删 tool 重复守卫 | `git rm framework/tool/pathguard.go`；调用方改 `workspace.ResolveWorkspacePath` |
| HTTP 薄封装 | `portal/internal/server/code_roots.go` 调 `workspace.LinkCode`；浏览/ACL 留下 |
| Portal 常量 | `portal/internal/chat/code_roots.go` 的 `WorkspaceCodeLink` 改为 `workspace.CodeDir` |
| 包搬家 | `git mv framework/agent framework/harness`；`package harness` |
| 仓内 import | 见下方 live 列表，全部改为 `github.com/sixath/framework/harness` |
| 别名 | `framework/agent/alias.go` + `alias_test.go` |

**禁止:** 改 PromptBuilder 块顺序；改 Hook Before block 语义；`model` import harness；把 `framework/tool` 整包改名；把浏览 HTTP 搬进 workspace。

**Windows `code/` 测试:** `os.Symlink`；若错误含 `symlink` / `privilege` / `A required privilege is not held` → `t.Skip`（与现网 `code_roots_test.go` 一致）。**不要**在测试里写到 `allowedRoots` 之外。生产路径不静默 fallback 到任意目录；可在 `LinkCode` 内对 Windows 再试 `os.Symlink` 失败后返回原错误（可观测）。

---

### Task 1: `workspace.ResolveWorkspacePath`

**Files:** Create `framework/workspace/path.go`、`path_test.go`、`layout.go`

- [ ] **Step 1:** 从 `feature/s2-context-promptbuilder` 切 `feature/s3-harness-workspace-rename`，`SetActiveBranch`。

- [ ] **Step 2:** 把 `framework/tool/pathguard.go` 的实现搬到 `package workspace`，函数名仍为 `ResolveWorkspacePath`。空 root / 空 rel 非法；拒绝逃出根。错误字符串可保留 `pathguard:` 前缀以免改断言，或改为 `workspace:` 并同步测试。

`layout.go` 只放常量与注释（契约，不是新进程）：

```go
const (
	SkillsDir    = "skills"
	HooksFileRel = "harness/hooks.yaml"
	MemoryFile   = "MEMORY.md"
	UserFile     = "USER.md"
	CodeDir      = "code"
)
```

- [ ] **Step 3:** 迁 `pathguard_test.go` 三则用例到 `workspace`，`cd framework && go test ./workspace -count=1` 绿。

- [ ] **Step 4: Commit**

```
git add framework/workspace
git commit -m "feat(workspace): add ResolveWorkspacePath and layout constants"
```

---

### Task 2: `workspace.LinkCode`

**Files:** Create `framework/workspace/code_link.go`、`code_link_test.go`

API（无 kratos）：

```go
var (
	ErrEmptyWorkspace   = errors.New("workspace root is empty")
	ErrEmptyTarget      = errors.New("target is empty")
	ErrTargetNotAllowed = errors.New("target not under allowed roots")
	ErrLinkConflict     = errors.New("workspace/code already exists with a different target")
)

func LinkCode(workspace, target string, allowedRoots []string) (linkPath, absTarget string, err error)
func ResolveCodeMount(workspace string) string // 现 chat.ResolveWorkspaceCodeRoot
```

行为对齐现网 `linkWorkspaceCode`：空 workspace 拒绝；目标必须在 `allowedRoots` 内（用与现网相同的 Abs/Clean/前缀规则）；已存在且同目标则幂等成功；不同目标 `ErrLinkConflict`；`MkdirAll(workspace)`；`os.Symlink(absTarget, filepath.Join(workspace, CodeDir))`。

- [ ] **Step 1:** 写失败测试：空 root；目标越出允许根；同目标幂等；越权 symlink 则 Skip。

- [ ] **Step 2:** 实现。`cd framework && go test ./workspace -count=1` 绿。

- [ ] **Step 3: Commit**

```
git add framework/workspace
git commit -m "feat(workspace): add code mount LinkCode helper"
```

---

### Task 3: 器官与 Portal 改用 workspace

**Files:** `framework/tool/pathguard.go`（删除）；`file_tools.go`、`terminal_tool.go`、`rca_repos.go`、`vision.go`、`browser_tools.go`、`query_spill.go`；`framework/tool/memory/store_tools.go`；`framework/tool/skillops/skill_manager_tool.go`；`portal/internal/server/code_roots.go` + test；`portal/internal/chat/code_roots.go`（`WorkspaceCodeLink` → `workspace.CodeDir`，`ResolveWorkspaceCodeRoot` → `workspace.ResolveCodeMount`）

- [ ] **Step 1:** 所有 `tool.ResolveWorkspacePath` / 同包 `ResolveWorkspacePath` 改为 `workspace.ResolveWorkspacePath`。`git rm framework/tool/pathguard.go`（及 `pathguard_test.go`）。

- [ ] **Step 2:** `linkWorkspaceCode` 改为调 `workspace.LinkCode`，把 sentinel 映射到现有 kratos `BadRequest` / `Conflict` / `Internal`。HTTP 测试仍 Skip 无权限 symlink。

- [ ] **Step 3:** `cd framework && go test ./workspace ./tool ./tool/memory ./tool/skillops -count=1`。`cd portal && go test ./internal/server ./internal/chat -count=1`（跳过预存 SQLITE_BUSY：`TestNotifySessionMessageIndexed_WithDetachedCaller`）。

- [ ] **Step 4: Commit**

```
git add framework/tool framework/workspace portal/internal/server portal/internal/chat
git commit -m "refactor: route path guard and code mount through workspace"
```

---

### Task 4: `agent` → `harness` 目录与 package 名

**Files:** `framework/agent/*` → `framework/harness/*`

- [ ] **Step 1:** 仓库根：`git mv framework/agent framework/harness`。每个 `*.go`：`package agent` → `package harness`。

- [ ] **Step 2:** `cd framework && go test ./harness -count=1` 应能编译测试包本身；其它包会因 import 路径失败——立刻做 Task 5，不要单独提交红树。

---

### Task 5: 仓内 import 全部改 harness

**Live 列表（`rg -l github.com/sixath/framework/agent --glob '*.go'`，排除 `_neo4j_q`）：**

framework: `cli/{serve,init,demo}.go`、`cmd/demo/main.go`、`turntrace/store.go`、`templates/*`、`examples/**`、`mea/rules_auditor.go`、`middleware/*`

portal: `internal/service/{agent,chat,chat_stream,growth_chat,background_review,insights,stream_agent,trace_digest,process_session_hooks,browser_session_hooks}.go` 及对应 `*_test.go`；`internal/chat/{agent_builder,compact_boundary,fork_on_compact,portal_agent_extra,turn_trace_persist,catalog_integration_test,trajectory_phase1_integration_test,agent_builder_react_opts_test}.go`；`internal/data/turn_trace_mysql.go` + test

- [ ] **Step 1:** 机械替换 import 路径 `github.com/sixath/framework/agent` → `github.com/sixath/framework/harness`。标识符仍是 `NewReActAgent` 等；若 import 名是 `agent`，改为 `harness` 或保持显式名 `harness`。

- [ ] **Step 2:** `cd framework && go test ./harness ./workspace ./context ./model ./tool ./templates ./middleware -count=1` 绿。`cd portal && go test ./internal/chat ./internal/service -count=1`（skip SQLITE_BUSY）。

- [ ] **Step 3: Commit**（含 Task 4 的 git mv）

```
git add framework portal
git commit -m "refactor: rename framework/agent package to harness"
```

---

### Task 6: 一季别名 `framework/agent`

**Files:** Create `framework/agent/alias.go`、`alias_test.go`

只转发、不复制。类型用 `type X = harness.X`；函数用同名包装或 `var NewReActAgent = harness.NewReActAgent`。`go doc -all ./harness` 核对导出符号，至少包括：

- `Agent`、`Request`、`Response`、`ChatAgent`、`NewChatAgent`、`WithMaxHistory`、`WithEventBus`
- `ReActAgent`、`ReActConfig`、`ReActOption`、全部 `WithReAct*`、`NewReActAgent`、`ContextCompressionConfig`
- `RunTrace`、`RunError`、`ToolCallRecord`、`StreamEvent`、`PermissionPolicy`、`AllowAllTools`、`DenyTools`
- `ToolHook`、`ErrToolHookBlocked`、`LoadWorkspaceHarnessHooks`、`ParseHarnessHooksYAML`
- `ChatSessionHook`、`NewChatSessionHookRegistry`
- `ToolGuardrailsConfig`、`ToolGuardrailsFromConfig`、`NewGuardrailEvaluator`
- `FailureCaptureHook`、`NewFailureCaptureHook`、`WithRequestMetadata`
- `AgentContext`、`EnsureContext`、`BuildTurnTrace`
- `IsBoundEvidenceTool`、`HasSuccessfulBoundEvidence`
- 错误变量：`ErrToolPermissionDenied`、`ErrToolNotFound`、`ErrToolGuardrailHalt`

别名包 **不得** 再实现 ReAct。测试：`agent.NewReActAgent` 返回 `*harness.ReActAgent`（类型别名后可赋给 `agent.Agent`）。

- [ ] **Step 1:** 写 `alias_test.go`（构造 ReAct + Hook block 仍可用别名类型编译）。

- [ ] **Step 2:** 实现转发。`cd framework && go test ./agent ./harness -count=1` 绿。

- [ ] **Step 3: Commit**

```
git add framework/agent
git commit -m "feat(agent): add one-season alias to harness"
```

---

### Task 7: 回归

- [ ] **Step 1:** `cd framework && go test ./harness ./workspace ./context ./model ./tool ./agent -count=1`

- [ ] **Step 2:** `rg "github.com/sixath/framework/agent" --glob "*.go"` 仅出现在 `framework/agent/` 别名包。specs/plans 里的路径字符串允许保留。

- [ ] **Step 3:** 确认 `model` 的 import 无 harness/context/agent。空 workspace root 的 path 测试仍失败。`LoadWorkspaceHarnessHooks` 缺文件仍 OK（现有单测随迁）。

- [ ] **Step 4:** 不要开始下一切片。不要 merge/push，除非用户明确要求。
