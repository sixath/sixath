# 复盘执行体升级：单次 LLM patch → fork ReActAgent

> 日期：2026-07-07
> 目标支柱：Hermes「技能自演化」支柱的执行体对齐（[hermes-growth-architecture.md](../../../hermes-growth-architecture.md) 支柱一）

## 1. 背景与问题

当前 sixath 的成长复盘（`framework/growth`）是**外部策展 LLM 单次调用**：
`SkillReviewRunner` 拉 transcript + 技能索引摘要，交给 `LLMClient.Complete` 生成一个 JSON patch 数组，portal 校验后写盘（[skill_review_runner.go](../../../framework/growth/skill_review_runner.go)、[runner_llm.go](../../../framework/growth/runner_llm.go)）。

这条路径的天花板受限：复盘 LLM **没有工具、没有多步推理**，只能"看着 transcript 写 diff"。它不能自主浏览现有技能库、读 `.learnings/`、按需拆分伞形技能，也无法在决定改哪个技能前先检索。

Hermes 的做法是 **fork 一个瘦身版 AIAgent 自身**，复用同一段对话循环与 toolset，让复盘 agent 用工具自主完成技能库演化。本设计把 sixath 的复盘执行体升级到这个范式，同时保留现有单次 LLM 路径作为降级兜底。

## 2. 目标与非目标

**目标**
- 新增 fork-agent 复盘路径：复盘由一个瘦身 `ReActAgent` 用 skillops 工具集自主执行。
- 双路径共存：新路径默认关闭、可配置开启；失败/超时自动降级到现有 `SkillReviewRunner`。
- 保持 `framework/growth` 不 import `framework/agent`（依赖方向不破）。
- 递归保护：复盘 agent 的工具调用绝不再触发 growth 计数。
- 后台无人值守下的成本/安全上界：默认 skillops 安全工具集、步数与超时硬上界。

**非目标（YAGNI）**
- 不做统一记忆基底、用户建模、训练数据回流（其它支柱，另立 spec）。
- 不改 Curator（`CuratorRunner` 维持单次 LLM patch）。本次只升级会话级技能复盘。
- 不新增工具；复用已存在的 `framework/tool/skillops`（`skill_manage`/`skill_tools`/`learnings_tools`）。

## 3. 架构

依赖方向不变：`portal → framework/growth`；`framework/growth` 不依赖 `framework/agent`。

```
GrowthWorker (portal/internal/service/growth_worker.go)
  → growth.NewRunner(cfg, deps)
      ├─ AgentReviewRunner   ← 新增，实现 growth.Runner
      │     └─ deps.SpawnReviewAgent(ctx, ReviewAgentJob) error   ← 新回调，portal 注入
      │            └─ portal: agent.NewReActAgent(瘦身工具集).Run(...)
      │                        复盘 agent 通过 skill_manage 等工具自主写盘
      └─ SkillReviewRunner   ← 现有单次 LLM 路径（降级兜底 / agent_review 关闭时）
```

`framework/growth` 通过一个**回调接口**（`SpawnReviewAgent`）与 agent 解耦——与现有 `ProposeSkillPatches`、`RewriteCronSkillRefs` 完全一致的注入模式。portal 是唯一同时 import `growth` 与 `agent` 的层。

## 4. 组件设计

### 4.1 growth.RunnerDeps 新增回调

```go
// RunnerDeps 追加：
// SpawnReviewAgent 在 workspace 内 fork 一个瘦身复盘 agent 自主演化技能库（fork-agent 路径）。
// 由 portal 注入（唯一同时依赖 framework/agent 的层）；为 nil 时 AgentReviewRunner 直接降级。
// 实现负责：构造 ReActAgent（瘦身工具集 + 递归保护）、注入 workspace_root 到 ctx、Run、
// 以及 Run 成功后的 InvalidateSkillsCache / rewriteCronSkillRefs / ClearGrowthPending 收尾。
SpawnReviewAgent func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) error
```

`ReviewJob` 已含 `SessionID / WorkspaceKey / WorkspaceRoot / PendingSkill / PendingMemory / LearningsSummary`，无需扩展。

### 4.2 growth.AgentReviewRunner（新文件 `framework/growth/agent_review_runner.go`）

实现 `growth.Runner` 接口。职责：

```go
type AgentReviewRunner struct {
    deps     RunnerDeps
    fallback *SkillReviewRunner   // 降级兜底
}

func (r *AgentReviewRunner) Run(ctx context.Context, job ReviewJob) error {
    // 1. 仅 PendingSkill 时走 agent 路径；PendingMemory 分支仍复用 SkillReviewRunner 语义。
    // 2. 组装 transcript + skillsSummary（复用 fetchTranscript / buildIndex / buildReviewSummary，
    //    这些从 SkillReviewRunner 抽为包内共享辅助函数）。
    // 3. 调 deps.SpawnReviewAgent(ctx, job, transcript, summary)。
    //    - 成功：ClearGrowthPending 由 SpawnReviewAgent 内部完成（见 4.4），此处不重复清。
    //    - 失败/超时：记 warn，回退 r.fallback.Run(ctx, job)（单次 LLM patch）。
}
```

**共享辅助函数抽取**：`fetchTranscript`、`buildIndex`、`buildReviewSummary` 目前是 `SkillReviewRunner` 的方法，改为包内自由函数（或抽到 `review_context.go`），供两个 runner 复用。这是本次唯一的"顺手改进"，因为两条路径都需要相同的上下文组装。

### 4.3 NewRunner 选择逻辑（改 `framework/growth/runner_factory.go`）

```go
func NewRunner(cfg RunnerSelect, deps RunnerDeps) Runner {
    if !cfg.LLMReviewEnabled {
        return &StubRunner{...}
    }
    if cfg.AgentReviewEnabled && deps.SpawnReviewAgent != nil {
        return &AgentReviewRunner{
            deps:     deps,
            fallback: &SkillReviewRunner{deps: deps},
        }
    }
    if deps.ProposeSkillPatches != nil {
        return &SkillReviewRunner{deps: deps}
    }
    return &NoopLLMRunner{...}
}
```

`NewRunner` 现签名是 `(llmReviewEnabled bool, deps RunnerDeps)`。改为接受一个小 `RunnerSelect{LLMReviewEnabled, AgentReviewEnabled bool}` 结构，避免布尔参数继续膨胀。所有现有调用点同步更新。

### 4.4 portal 的 SpawnReviewAgent 实现（`portal/internal/service/growth_worker.go` 或新文件 `growth_agent_review.go`）

```go
func (w *GrowthWorker) spawnReviewAgent(ctx context.Context, job growth.ReviewJob, transcript, summary string) error {
    workspace := job.WorkspaceRoot; if workspace == "" { workspace = job.WorkspaceKey }

    // 1. 复用 auxiliary cheap 模型（不占主对话配额）。
    m := w.reviewModel   // 由 newGrowthModelClient 的底层 model.Model 复用（见 5.）

    // 2. 瘦身工具集：默认仅 skillops；agent_review_full_tools=true 时用全量 registry。
    reg := w.skillopsRegistry
    if w.growthCfg.GetAgentReviewFullTools() { reg = w.fullRegistry }

    // 3. 构造瘦身 ReActAgent —— 递归保护：不设 ToolSuccessHook。
    a := agent.NewReActAgent(m, nil, reg,
        agent.WithReActMaxSteps(int(cfgMaxSteps)),      // 默认 12，硬上界
        // 显式不设 ToolSuccessHook → 复盘 agent 的工具调用不触发 growth 计数（递归保护）
    )
    // system prompt 走 per-run Request.SystemPrompt（见步骤 5），不在 config 重复设置

    // 4. ctx 注入 workspace_root（skill_manage 依赖 tool.ContextKeyWorkspaceRoot）+ 超时。
    ctx = context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, workspace)
    ctx, cancel := context.WithTimeout(ctx, cfgTimeout)   // 默认 120s
    defer cancel()

    // 5. Run：把 transcript + 技能摘要 + learnings 作为首条 user 消息。
    req := &agent.Request{
        SystemPrompt: agentReviewSystemPrompt,
        Messages: []model.Message{{Role: "user", Content: buildAgentReviewInput(job, transcript, summary)}},
    }
    if _, err := a.Run(ctx, req); err != nil { return err }   // 失败 → runner 层降级

    // 6. 收尾（skill_manage 已完成 pin 检查 / lease / ApplyPatchBatch / IndexTracker.Bump）：
    //    仅补 portal 侧动作 —— 缓存失效、cron 反写、清 pending。
    w.invalidateSkillsCache(ctx, workspace)
    // cron 反写：agent 路径下无 patch 列表可推导 renames。见 6. 开放问题。
    return w.growthUC.ClearGrowthPending(ctx, job.SessionID, true /*skill*/, false)
}
```

**system prompt** 复用并改写现有 `DefaultSkillReviewSystemPrompt` 的策展指令（patch 现有 → 加 reference → 新建 umbrella；禁止一次性产物变技能名），但改为**指导 agent 用工具**而非输出 JSON。

### 4.5 配置（新增 `conf.Growth` 字段）

```yaml
growth:
  agent_review_enabled: false      # fork-agent 复盘总开关，默认关
  agent_review_full_tools: false   # false=仅 skillops 安全集；true=全量工具
  agent_review_max_steps: 12       # 后台复盘 agent 步数硬上界
  agent_review_timeout: 120s       # 单次复盘超时（触发降级）
```

## 5. 数据流

```
worker 抢 workspace 租约
  → AgentReviewRunner.Run(job)
      → 组装 transcript + skillsSummary + learnings
      → SpawnReviewAgent
          → fork ReActAgent（cheap aux 模型 + skillops 工具 + 无 ToolSuccessHook）
          → agent 多步：skill_view/skill_tools 浏览 → 读 .learnings → skill_manage 写 SKILL.md
              （skill_manage 内部完成 pin 检查 + lease + ApplyPatchBatch + IndexTracker.Bump）
          → agent 返回
          → portal 收尾：InvalidateSkillsCache + ClearGrowthPending(skill)
      → 成功：返回 nil
      → 失败/超时：降级 SkillReviewRunner.Run(job)（单次 LLM patch）
```

模型复用：`newGrowthModelClient` 当前把 `model.Model` 包成 `LLMClient`。复盘 agent 需要裸 `model.Model`（`ReActAgent` 要 `ToolCallingModel`）。改动：`newGrowthModelClient` 额外暴露底层 `model.Model`，供 agent 路径构造 `ReActAgent`；单次 LLM 路径仍用包装后的 `LLMClient`。

## 6. 错误处理与降级

| 情形 | 处理 |
|------|------|
| `SpawnReviewAgent == nil` | `NewRunner` 不选 AgentReviewRunner，直接用 SkillReviewRunner |
| agent Run 返回 error | `AgentReviewRunner` 记 warn，回退 `fallback.Run`（单次 LLM） |
| agent 超时（ctx deadline） | 同上，降级 |
| 降级路径也失败 | 走现有 `RecordReviewRunFailure` + 重试计数 + `DropPendingAfterMaxRetry`（不变） |
| workspace_root 未注入 | skill_manage 返回 `workspace_root not set`；由 spawn 前置注入避免 |

**递归保护**（关键）：复盘 agent 构造时**不传** `ToolSuccessHook`，其 `skill_manage` 等工具调用不会经过 `GrowthUsecase.OnToolSuccess`，因此不会再置 pending。等价于 Hermes 的"内层 nudge 设 0"。

## 7. 开放问题

**cron 技能引用反写**：单次 LLM 路径能从 patch 列表 `ExtractSkillRenamesFromPatches` 推导改名并反写 cron（[skill_review_runner.go:131](../../../framework/growth/skill_review_runner.go#L131)）。fork-agent 路径下 agent 直接写盘，portal 拿不到"改了哪些名"。

**决策**：一期 fork-agent 路径**不做 cron 反写**（记为已知缺口，log warn 提示）；agent 的 system prompt 显式**禁止对已被 cron 引用的技能改名**（把 pinned/cron-referenced 技能名注入 prompt 作为约束）。二期可让 `skill_manage` 的 rename action 落一条 rename 审计记录，portal 读审计后反写。这样避免为一期引入复杂的 diff 追踪。

## 8. 测试策略

**framework/growth（纯单测，不碰真 agent）**
- `AgentReviewRunner_Run_success`：fake `SpawnReviewAgent` 返回 nil → 不调 fallback。
- `AgentReviewRunner_Run_fallbackOnError`：fake 返回 error → 调用 fallback（用 spy `SkillReviewRunner` deps 验证）。
- `AgentReviewRunner_Run_memoryBranch`：PendingMemory-only → 走 SkillReviewRunner 语义。
- `NewRunner` 选择矩阵：`{LLMReviewEnabled, AgentReviewEnabled, SpawnReviewAgent!=nil}` 各组合选对 runner。

**portal（wiring 单测，fake model）**
- `spawnReviewAgent` 用 fake `ToolCallingModel`：验证 (a) 默认工具集只含 skillops；(b) full-tools 开关切到全量；(c) 构造的 ReActConfig 里 `ToolSuccessHook == nil`；(d) ctx 携带 `ContextKeyWorkspaceRoot`；(e) 超时/步数配置生效。
- 降级链路端到端：SpawnReviewAgent 注入一个必失败的 model → 断言最终走单次 LLM patch。

**收尾**：`cd framework && go test ./growth/...`；`cd portal && go test ./internal/service/... ./internal/biz/...`；全量 `go test ./...`。

## 9. 落地边界（文件清单）

新增：
- `framework/growth/agent_review_runner.go` + `_test.go`
- `framework/growth/review_context.go`（抽取共享上下文组装）
- `portal/internal/service/growth_agent_review.go` + `_test.go`

修改：
- `framework/growth/runner_factory.go`（`NewRunner` 签名 + 选择逻辑）
- `framework/growth/runner.go`（`RunnerDeps` 加 `SpawnReviewAgent`）
- `framework/growth/skill_review_runner.go`（方法抽为共享函数）
- `portal/internal/service/growth_worker.go`（wiring：注入 SpawnReviewAgent、暴露底层 model、工具集构造）
- `portal/internal/conf/*.proto` + 生成的 `conf.pb.go`（4 个新字段）
- 现有 `NewRunner` 所有调用点
