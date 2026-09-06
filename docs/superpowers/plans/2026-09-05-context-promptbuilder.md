# S2 Context + PromptBuilder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 L0/L1/L2 管道迁到 `framework/context`，加上 PromptBuilder（Stable/Ephemeral/`prompt_stable_hash`）；Harness 在调 Model 之前做完拼接与压缩，`model` 变成傻 Provider。

**Architecture:** 新包 `github.com/sixath/framework/context`（import 别名 `fwctx`；stdlib 仍叫 `context`，包内用 `stdctx`）。PromptBuilder 只收字符串。Harness（现 `framework/agent`）每次 `beginModelInvocation` 后：Build → Encode → 替换第一条 system → `PrepareCtx` → `model.Chat*`。`model` 不得 import `context` / `agent`。

**Tech Stack:** Go（`framework/context`、`framework/model`、`framework/agent`、`framework/skills`、`framework/templates`、portal chat/service）

**规格:** [`2026-09-05-context-promptbuilder-design.md`](../specs/2026-09-05-context-promptbuilder-design.md)

**分支:** 从 `feature/s1-dead-code-hub-off` 切 `feature/s2-context-promptbuilder`（S1 未合入 assembler，不要从 assembler 切）。不要在 `main` 上改。PowerShell 无 HEREDOC：`git commit -m "..."`。不要 `--no-verify`。不要提交 `_neo4j_q/`。不要开始 S3（不改 `agent`→`harness` 包名、不抽 `workspace` 包）。

---

## File map

| 动作 | 路径 |
|------|------|
| 新建 PromptBuilder | `framework/context/prompt_builder.go` + `prompt_builder_test.go` + `encode.go` |
| 新建管道入口 | `framework/context/pipeline.go`（`Prepare` / `PrepareCtx` + `PipelineConfig`） |
| `git mv` 管道实现 | 见下方清单；改 `package context`，`Message` → `model.Message` |
| L1 卫生函数留下 | `framework/model/message_sanitize.go`（`openai_tools.go` 编码仍调用；`model` 不得 import `context`） |
| 抽 skills 渲染 | `framework/skills/prompt.go`；`templates.BuildSkillsAwarePrompt` 改为转发（打破 `agent`↔`templates` 环） |
| 拆 Provider 压缩 | `framework/model/openai.go`、`openai_tools.go`、`openai_tools_stream.go`：删 `PrepareChatContextCtx` |
| 瘦 `CallConfig` | `framework/model/model.go`：只留 Temperature / MaxTokens / ModelName；删压缩 `With*` |
| Harness 接线 | `framework/agent/react_agent.go`、`context_ops.go`、`trace.go` |
| Portal 停预拼 | `portal/internal/service/chat.go`、`agent.go`；`chat/agent_builder.go` 的 `BuildEffectiveSystemPrompt` 不再拼 skills |

**`git mv` 清单（model → context，单测随迁）：**

- `context_pipeline.go`（逻辑并入 `pipeline.go` 后删除，或改名）
- `context_pipeline_test.go`
- `context_budget.go`、`context_budget_test.go`、`context_budget_origin_test.go`
- `context_budget_pin.go`、`context_budget_pin_test.go`
- `context_code_pin.go`、`context_code_pin_test.go`
- `snip_compact.go`、`snip_compact_test.go`
- `l2_runtime.go`、`l2_runtime_test.go`
- `estimate_tokens.go`、`estimate_tokens_test.go`
- `redact_l2.go`
- `context_trace_option_test.go`、`trace_sink_test.go`（改为测 `PipelineConfig`，不再测 `model.ApplyOptions`）

**包名冲突锁定：**

- 新包名必须是 `package context`（模块路径 `github.com/sixath/framework/context`）。
- 该包文件 import stdlib：`stdctx "context"`。
- 其它包：`fwctx "github.com/sixath/framework/context"`。
- `Prepare` / `PrepareCtx` 签名：`PrepareCtx(ctx stdctx.Context, messages []model.Message, cfg *PipelineConfig) []model.Message`。
- **禁止**在 `model` 留 `PrepareChatContext` 转发（否则必须 import `context`）。

**Skills 渲染环锁定：** `templates` 已 import `agent`。Harness 不能 import `templates`。把 `BuildSkillsSummary` / `BuildSkillsAwarePrompt` / `buildSkillsAwareSystemPrompt` 挪到 `framework/skills/prompt.go`。`templates` 保留同名函数，一行转发到 `skills`。Harness import `skills`。PromptBuilder **不** import `skills`。

**Portal 双 system 锁定（现网坑）：** `chat.go` 把 skills 拼进 `Request.Messages[0]`（system），同时 `BuildReActAgent` 又把 `agentMeta.SystemPrompt` 写进 `ReActConfig`。`messages()` 会再 prepend 一条 system → 两条 system。S2 后 Portal **Request 不再带 system**；AskUser / WeCom 文案并入传给 Harness 的 `systemPrompt` 字符串（仍算「只传 systemPrompt」）。Harness 用 PromptBuilder 写**唯一**第一条 system。

**Ephemeral v1：** 上一 invocation 若 `L0DroppedMessages > 0`，本 invocation 插入一行「上下文已按预算裁剪较早轮次。」；否则空。不进 hash。不发明厂商 cache / catalog 置顶块。

禁止：`FormatToolCatalogPrompt` 置顶；Skill 全文预注入；开始 S3；改 `skill_router.go` 生产路径（现网 Chat 已不调用 `ForTurn`，本切片不 `git rm`）。

---

### Task 1: Skills 渲染迁出 templates

**Files:** Create `framework/skills/prompt.go` + `prompt_test.go`；Modify `framework/templates/skills_handler.go`

- [ ] **Step 1:** 从 `feature/s1-dead-code-hub-off` 切 `feature/s2-context-promptbuilder`，`SetActiveBranch`。

- [ ] **Step 2:** 把 `BuildSkillsSummary` 与无 HyperTool 的 `BuildSkillsAwarePrompt` 搬到 `skills`。`templates.BuildSkillsSummary` / `BuildSkillsAwarePrompt` 一行转发，保证 `templates_test.go` 不用改断言。`buildSkillsAwareSystemPrompt(..., hyperToolEnabled)` **留在 templates**（`skills_handler.go:148` 仍拼 `tool.HyperToolPromptSnippet`）；Harness 只调用 `skills.BuildSkillsAwarePrompt`（无 HyperTool 段，与现网 `BuildEffectiveSystemPrompt` 一致）。

- [ ] **Step 3:** 跑 `cd framework && go test ./skills ./templates -count=1`。绿。

- [ ] **Step 4: Commit**

```
git add framework/skills/prompt.go framework/skills/prompt_test.go framework/templates/skills_handler.go
git commit -m "refactor(skills): move BuildSkillsAwarePrompt out of templates"
```

---

### Task 2: PromptBuilder + Encode + hash（TDD）

**Files:** Create `framework/context/prompt_builder.go`、`prompt_builder_test.go`

概念 API（规格 §4）：

```go
type Input struct {
	AgentSystem string
	SkillsIndex string
	MemoryMD    string
	UserMD      string
	ToolNames   []string
	Ephemeral   string
}

type Result struct {
	Stable     string
	Ephemeral  string
	StableHash string // SHA256(UTF-8 Stable) hex[:16]
}

func Build(in Input) Result
func Encode(stable, ephemeral string) string // 无 ephemeral 则等于 stable，不含 ---
```

Stable 块顺序、空块整段省略（含标题）、块间 `\n\n`、Tools 排序去重后 `- {name}`。

- [ ] **Step 1: 写失败测试**（`prompt_builder_test.go`）至少覆盖：

  1. 只改 Ephemeral → hash 不变，Encode 含 `\n\n---\n\n`。
  2. 无 Ephemeral → Encode == Stable，不含 `---`。
  3. ToolNames `[]string{"b","a","a"}` 与 `[]string{"a","b"}` → 同一 Stable / hash。
  4. 空 SkillsIndex / 空 MemoryMD / 空 UserMD / 空 ToolNames → 对应 `##` 整块不出现。
  5. Agent 文案无标题，位于最前。
  6. hash = 对 Stable 做 `sha256` hex 前 16 位（手算或 `sha256.Sum256` 对照）。

```go
func TestBuild_ToolNamesOrderIndependent(t *testing.T) {
	a := Build(Input{AgentSystem: "sys", ToolNames: []string{"b", "a", "a"}})
	b := Build(Input{AgentSystem: "sys", ToolNames: []string{"a", "b"}})
	if a.Stable != b.Stable || a.StableHash != b.StableHash {
		t.Fatalf("stable/hash must be order-independent")
	}
}
```

- [ ] **Step 2:** `cd framework && go test ./context -count=1` — 必须失败（包/符号不存在）。

- [ ] **Step 3:** 实现 `Build` / `Encode`。空块判断：`strings.TrimSpace` 后为空则省略。

- [ ] **Step 4:** 再跑 `./context` — 绿。

- [ ] **Step 5: Commit**

```
git add framework/context/prompt_builder.go framework/context/prompt_builder_test.go
git commit -m "feat(context): add PromptBuilder stable/ephemeral hash"
```

---

### Task 3: 管道迁到 `framework/context`

**Files:** `git mv` 清单中的实现；新建 `framework/context/pipeline.go`；改测试里的 `CallConfig` → `PipelineConfig`。

`PipelineConfig`：

```go
type PipelineConfig struct {
	MaxContextRunes      int
	MaxContextTokensSoft int
	TokenEstimateAlpha   float64
	Trace                ContextTraceFunc
	L2                   *L2Runtime
	SnipCompactEnabled   bool
}
```

`Prepare` / `PrepareCtx` 顺序与现网 `PrepareChatContextCtx` 完全一致：L1 → snip → L2 预剪枝 → code pin → L0 rune → L0 token 软阈值 → strip orphan → L2 MaybeSummarize。

L1 调用 `model.ApplyL1SanitizeToMessages`。`L2Runtime.aux` 类型改为 `model.Model`。`ContextTraceFunc` / `TraceSink` 随管道迁到本包。

- [ ] **Step 1:** `git mv` 文件到 `framework/context/`。每个文件：`package context`；import `github.com/sixath/framework/model`；`Message` → `model.Message`；`Model` → `model.Model`。`PrepareChatContext*` 改名为 `Prepare` / `PrepareCtx`，第三个参数改为 `*PipelineConfig`。

- [ ] **Step 2:** 测试里所有 `PrepareChatContext` / `CallConfig{MaxContextRunes:...}` / `WithMaxContextRunes` 改为 `Prepare` + `PipelineConfig`。`NewL2Runtime` 仍在本包。

- [ ] **Step 3:** `cd framework && go test ./context -count=1` — 绿（含迁过来的 L0/L1/L2 单测 + Task 2）。

- [ ] **Step 4:** 若 `git mv` 后 `./model` 或 `./agent` 无法编译（`PrepareChatContextCtx` / `model.L2Runtime` 已走），**不要单独提交红树**：立刻做 Task 4 Step 1–2，再 `go test ./context ./model ./agent ./tool -count=1` 绿后与 Task 4 合成一次或两次紧挨着的提交。

---

### Task 4: Model 变傻 Provider

**Files:** `framework/model/model.go`、`openai.go`、`openai_tools.go`、`openai_tools_stream.go`；删已迁走的 model 文件（若 Task 3 `git mv` 已完成则无残留）。`framework/agent/react_agent.go` 的 `modelOpts` 先仍引用已删 Option — 本 Task 一并改掉压缩 Option 的**编译**（完整 Harness 接线在 Task 5）：`modelOpts` 只留 `WithMaxTokens`。

- [ ] **Step 1:** `CallConfig` 只留 `Temperature`、`MaxTokens`、`ModelName`。删除 `MaxContextRunes` / `MaxContextTokensSoft` / `TokenEstimateAlpha` / `ContextTrace` / `L2` / `SnipCompactEnabled` 及对应 `With*`。保留 `SanitizeMessageContent`。

- [ ] **Step 2:** `openai.go` Chat + ChatStream、`openai_tools.go`、`openai_tools_stream.go`：删 `msgs := PrepareChatContextCtx(...)`，直接用入参 `messages`。其它 Provider（ollama/dashscope）确认无 `PrepareChatContext` 调用。

- [ ] **Step 3:** `cd framework && go test ./model ./context -count=1` — 绿。`grep PrepareChatContext framework/model` 无生产命中（测试也不该再有）。

- [ ] **Step 4: Commit**

```
git add framework/model framework/agent
git commit -m "fix(model): stop compressing inside providers"
```

---

### Task 5: Harness 在 beginModelInvocation 后接线

**Files:** `framework/agent/react_agent.go`、`context_ops.go`、`trace.go`、相关 `*_test.go`

`ReActConfig` 增加：`Workspace string`、`SkillsDirs []string`（Portal extra 共享技能目录）。压缩字段仍留在 ReActConfig，改为填 `fwctx.PipelineConfig`，不再塞 `model.Option`。`L2Runtime` 类型改为 `*fwctx.L2Runtime`。`WithReActCompactConfig` 里 `NewL2Runtime` 改 `fwctx.NewL2Runtime`。`DefaultMaxContextRunes` 改 `fwctx.DefaultMaxContextRunes`。

每次模型调用（`react_agent.go` 里所有 `beginModelInvocation` 之后）：

```go
beginModelInvocation(trace, mode)
messages = a.prepareModelMessages(ctx, messages, trace)
gen, err := tm.ChatWithTools(ctx, messages, a.tools, a.modelOpts()...)
```

`prepareModelMessages`：

1. 读 workspace 根 `MEMORY.md` / `USER.md`（缺或空白 → 空字符串；读失败当空，不 fail Run）。
2. `BuildSkillsIndex` 等价逻辑：`workspace/skills` + `SkillsDirs` → `skills.NewIndex` → `skills.BuildSkillsAwarePrompt`（idx nil 或空 → SkillsIndex `""`）。
3. `ToolNames`：当时 `a.tools.List()` 的 `Name`（registry nil → 省略 Tools 块）。
4. `fwctx.Build` → `fwctx.Encode` → `replaceOrInsertFirstSystem`（已有 system 只改第一条；没有则插到 index 0；禁止 append 第二条）。
5. 把 `StableHash` 写到 `lastContextOpsInvocation(trace).PromptStableHash`。
6. `fwctx.PrepareCtx(ctx, msgs, a.pipelineConfig(trace))`，Trace 仍用现有 `contextTraceMerge`（改返回 `fwctx.ContextTraceFunc`）。

`replaceOrInsertFirstSystem` 必须有单测：已有 system 不增加条数；无 system 插到最前。

`ContextOpsInvocation` 加 `PromptStableHash string \`json:"prompt_stable_hash,omitempty"\``。

Agent 里现有 `model.PrepareChatContext` 调用（`react_agent_test.go`）全部改为 `fwctx.Prepare` + `fwctx.PipelineConfig`。

- [ ] **Step 1:** 写 `replaceOrInsertFirstSystem` 与 `Build` 接线的失败测试（假 Model 记录收到的第一条 system：含 `## Tools`、hash 写入 trace）。

- [ ] **Step 2:** 实现 `prepareModelMessages`；所有 Chat/ChatStream/ChatWithTools 入口走它。`modelOpts` 只返回 `WithMaxTokens`。

- [ ] **Step 3:** `cd framework && go test ./agent ./context ./model ./tool ./skills -count=1` — 绿。

- [ ] **Step 4: Commit**

```
git add framework/agent framework/context
git commit -m "feat(agent): build prompt and compress before model calls"
```

---

### Task 6: Portal 只传 systemPrompt + workspace + registry

**Files:** `portal/internal/service/chat.go`、`agent.go`；`portal/internal/chat/agent_builder.go` 及 `evalgolden_test.go`、`catalog_integration_test.go`、`agent_builder.go` 里 `DefaultMaxContextRunes`。

- [ ] **Step 1:** `BuildEffectiveSystemPrompt` 改为只返回 `userPrompt`（不再调用 `templates.BuildSkillsAwarePrompt`）。更新断言 skills 文案不再出现在该函数返回值里的测试。skills 索引改由 Harness PromptBuilder 负责。

- [ ] **Step 2:** `BuildReActAgent` 默认选项：`WithReActMaxContextRunes(fwctx.DefaultMaxContextRunes)`。

- [ ] **Step 3:** `chat.go` / `agent.go` 装配：
  - `BuildReActAgent(..., agentText, ...)` 其中 `agentText` = `agentMeta.SystemPrompt` + `AppendAskUserToolPrompt` + `appendWecomBoundSystemPrompt`（**不含** skills 索引）。
  - extra：`WithReActWorkspace(agentMeta.Workspace)`、`WithReActSkillsDirs(extraSkillDirs)`。
  - `Request.Messages` **不要**再 prepend system；只放 user/assistant 历史。
  - 仍用 `BuildSkillsIndex` 注册 `load_skill` 等运行时工具（这是 registry，不是 PromptBuilder）。

- [ ] **Step 4:** `cd portal && go test ./internal/chat ./internal/service -count=1`。跳过预存失败：`TestNotifySessionMessageIndexed_WithDetachedCaller`、`TestSearchSessionsWithAgentFilterRequiresAgentUse`（整包 SQLITE_BUSY 时与 S1 相同，不要为此改生产代码）。

- [ ] **Step 5: Commit**

```
git add portal/internal/chat portal/internal/service
git commit -m "fix(portal): pass raw system prompt and workspace into harness"
```

---

### Task 7: 回归

- [ ] **Step 1:** `cd framework && go test ./context ./model ./agent ./tool -count=1`（规格 §6）。

- [ ] **Step 2:** 在 `framework/model` 整树确认无 `PrepareChatContext`（生产 + 测试）。Provider 文件无压缩调用。

- [ ] **Step 3:** 若有假 Model + 计算器 / 无工具的 portal SSE 测试，跑对应包确认仍绿。

- [ ] **Step 4:** 不要开始 S3。不要 merge 进 assembler，除非用户明确要求。
