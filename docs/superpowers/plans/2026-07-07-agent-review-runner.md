# 复盘执行体升级（fork ReActAgent）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 growth 会话级技能复盘从「单次 LLM 生成 patch」升级为「fork 一个瘦身 ReActAgent 用 skillops 工具自主演化技能库」，双路径共存并自动降级。

**Architecture:** `framework/growth` 新增 `AgentReviewRunner`（实现 `Runner` 接口）+ `RunnerDeps.SpawnReviewAgent` 回调，growth 不 import agent；portal 实现 `SpawnReviewAgent`：构造瘦身 `ReActAgent`（默认 skillops 工具集、无 `ToolSuccessHook` 做递归保护、步数/超时硬上界），失败时 `AgentReviewRunner` 降级到现有 `SkillReviewRunner`。

**Tech Stack:** Go；`github.com/sixath/framework`（growth/agent/tool/model）；kratos + protobuf（portal conf）。

> **Git 说明**：当前工作区不是 git 仓库（`git init` 未执行）。计划中的 "Commit" 步骤写作"若已初始化 git 则提交"；若无 git，跳过该步、直接进入下一任务。

---

## 文件结构

**framework/growth（新增/改）**
- Create `framework/growth/review_context.go` — 从 `SkillReviewRunner` 抽出的共享上下文组装（transcript / index / summary）。
- Create `framework/growth/agent_review_runner.go` — `AgentReviewRunner`。
- Create `framework/growth/agent_review_runner_test.go` — 单测（fake SpawnReviewAgent）。
- Modify `framework/growth/runner.go` — `RunnerDeps` 加 `SpawnReviewAgent`。
- Modify `framework/growth/runner_factory.go` — `NewRunner` 签名改 `RunnerSelect` + 选择逻辑。
- Modify `framework/growth/skill_review_runner.go` — 方法改用 `review_context.go` 的共享函数。
- Modify `framework/growth/runner_factory_test.go` — 更新既有 `NewRunner` 调用。

**portal（新增/改）**
- Create `portal/internal/service/growth_agent_review.go` — `spawnReviewAgent` 实现 + system prompt + 输入组装。
- Create `portal/internal/service/growth_agent_review_test.go` — wiring 单测（fake ToolCallingModel）。
- Modify `portal/internal/service/growth_worker.go` — 注入 `SpawnReviewAgent`、暴露底层 model、`NewRunner` 调用改 `RunnerSelect`。
- Modify `portal/internal/conf/conf.proto` — 新增 4 字段（20-23）。
- Modify `portal/internal/conf/conf.pb.go` — 重新生成（或手工补 getter）。

---

## Task 1: RunnerDeps 新增 SpawnReviewAgent 回调

**Files:**
- Modify: `framework/growth/runner.go`

- [ ] **Step 1: 在 RunnerDeps 末尾追加回调字段**

在 `framework/growth/runner.go` 的 `RunnerDeps` 结构体内、`RewriteCronSkillRefs` 字段之后追加：

```go
	// SpawnReviewAgent 在 workspace 内 fork 一个瘦身复盘 agent，用 skillops 工具自主演化技能库
	// （fork-agent 路径，spec §4.1）。由 portal 注入（唯一同时依赖 framework/agent 的层）。
	// 实现负责：构造 ReActAgent（瘦身工具集 + 递归保护：不设 ToolSuccessHook）、注入 workspace_root 到 ctx、
	// Run，以及 Run 成功后的 InvalidateSkillsCache / ClearGrowthPending 收尾。
	// 为 nil 时 AgentReviewRunner 直接降级到 SkillReviewRunner。
	SpawnReviewAgent func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) error
```

- [ ] **Step 2: 编译验证**

Run: `cd framework && go build ./growth/...`
Expected: 编译通过（新增字段无引用，仍应通过）。

- [ ] **Step 3: Commit（若有 git）**

```bash
git add framework/growth/runner.go && git commit -m "feat(growth): add SpawnReviewAgent callback to RunnerDeps"
```

---

## Task 2: 抽取共享上下文组装到 review_context.go

**Files:**
- Create: `framework/growth/review_context.go`
- Modify: `framework/growth/skill_review_runner.go`

- [ ] **Step 1: 创建 review_context.go，把 SkillReviewRunner 的三个方法改为包内自由函数**

创建 `framework/growth/review_context.go`：

```go
package growth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/skills"
)

// fetchReviewTranscript 拉取会话 Markdown；deps.Transcript 为 nil 时返回空串。
func fetchReviewTranscript(ctx context.Context, job ReviewJob, deps RunnerDeps) (string, error) {
	if deps.Transcript == nil {
		return "", nil
	}
	tr, err := deps.Transcript(ctx, job.SessionID)
	if err != nil {
		return "", fmt.Errorf("growth: transcript: %w", err)
	}
	return tr, nil
}

// buildReviewIndex 扫描 workspace/skills 构建技能索引；无 skills 目录时返回空索引。
func buildReviewIndex(workspace string) (*skills.Index, error) {
	skillsDir := filepath.Join(workspace, "skills")
	if st, err := os.Stat(skillsDir); err == nil && st.IsDir() {
		idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("growth: skills index: %w", err)
		}
		return idx, nil
	}
	return skills.NewIndex(nil, nil, nil)
}

// fetchReviewMemoryState 容错拉取记忆状态摘要；nil/error 返回空串。
func fetchReviewMemoryState(ctx context.Context, job ReviewJob, deps RunnerDeps) string {
	if deps.MemoryState == nil {
		return ""
	}
	s, err := deps.MemoryState(ctx, job.SessionID)
	if err != nil {
		return ""
	}
	return s
}

// buildReviewSummary 组装技能索引快照 + 记忆状态 + workspace learnings。
func buildReviewSummary(ctx context.Context, job ReviewJob, idx *skills.Index, deps RunnerDeps) string {
	summary := FormatSkillsIndexSnapshot(idx, 64, 200)
	if mem := fetchReviewMemoryState(ctx, job, deps); mem != "" {
		summary = summary + "\n# Memory state\n" + mem + "\n"
	}
	if strings.TrimSpace(job.LearningsSummary) != "" {
		summary = summary + "\n# Workspace learnings (.learnings/)\n" + job.LearningsSummary + "\n"
	}
	return summary
}
```

- [ ] **Step 2: 删除 skill_review_runner.go 中被替换的方法体，改调共享函数**

在 `framework/growth/skill_review_runner.go` 中：

- 删除方法 `fetchTranscript`（第 142-151 行）、`buildIndex`（153-163）、`fetchMemoryState`（166-175）、`buildReviewSummary`（177-186）。
- 把 `runSkill` 内 `r.fetchTranscript(ctx, job, deps)` → `fetchReviewTranscript(ctx, job, deps)`；`r.buildIndex(workspace)` → `buildReviewIndex(workspace)`；`r.buildReviewSummary(job, idx, deps, ctx)` → `buildReviewSummary(ctx, job, idx, deps)`。
- 把 `runCombined` 内相同三处调用同样替换。

替换后 `runSkill` 的相关行应为：

```go
	transcript, err := fetchReviewTranscript(ctx, job, deps)
	if err != nil {
		return err
	}
	idx, err := buildReviewIndex(workspace)
	if err != nil {
		return err
	}
	summary := buildReviewSummary(ctx, job, idx, deps)
```

`runCombined` 同理。

- [ ] **Step 3: 运行既有 growth 测试，确认行为不变**

Run: `cd framework && go test ./growth/...`
Expected: PASS（既有 `skill_review_runner_test.go` 全绿——纯重构，行为不变）。

- [ ] **Step 4: Commit（若有 git）**

```bash
git add framework/growth/review_context.go framework/growth/skill_review_runner.go && git commit -m "refactor(growth): extract shared review context assembly"
```

---

## Task 3: NewRunner 改用 RunnerSelect + 选择 AgentReviewRunner

**Files:**
- Modify: `framework/growth/runner_factory.go`
- Modify: `framework/growth/runner_factory_test.go`

- [ ] **Step 1: 先更新既有测试到新签名（TDD：先让测试表达新契约）**

在 `framework/growth/runner_factory_test.go` 顶部，把 4 处 `NewRunner(false, ...)` / `NewRunner(true, ...)` 改为传 `RunnerSelect`：

- `TestNewRunner_stubPath`：`NewRunner(RunnerSelect{}, RunnerDeps{...})`
- `TestNewRunner_noopLLMDelegatesToStub`：`NewRunner(RunnerSelect{LLMReviewEnabled: true}, RunnerDeps{...})`
- `TestNewRunner_skillReviewRunnerWhenProposerSet`：`NewRunner(RunnerSelect{LLMReviewEnabled: true}, RunnerDeps{...})`
- `TestNewRunner_memoryNotifyStubOnly`：`NewRunner(RunnerSelect{}, RunnerDeps{...})`

追加一个新测试：

```go
func TestNewRunner_agentReviewRunnerWhenSpawnSet(t *testing.T) {
	r := NewRunner(
		RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		RunnerDeps{
			ClearGrowthPending:  func(ctx context.Context, _ string, _, _ bool) error { return nil },
			ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) { return nil, nil },
			SpawnReviewAgent:    func(ctx context.Context, _ ReviewJob, _, _ string) error { return nil },
		},
	)
	if _, ok := r.(*AgentReviewRunner); !ok {
		t.Fatalf("expected *AgentReviewRunner, got %T", r)
	}
}

func TestNewRunner_fallsBackWhenAgentEnabledButNoSpawn(t *testing.T) {
	r := NewRunner(
		RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		RunnerDeps{
			ClearGrowthPending:  func(ctx context.Context, _ string, _, _ bool) error { return nil },
			ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) { return nil, nil },
			// SpawnReviewAgent 为 nil
		},
	)
	if _, ok := r.(*SkillReviewRunner); !ok {
		t.Fatalf("expected *SkillReviewRunner fallback, got %T", r)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./growth/ -run TestNewRunner -v`
Expected: 编译失败（`RunnerSelect` 未定义、`AgentReviewRunner` 未定义）。

- [ ] **Step 3: 改 runner_factory.go**

把 `framework/growth/runner_factory.go` 的 `NewRunner` 整体替换为：

```go
// RunnerSelect 控制 NewRunner 的路径选择。
type RunnerSelect struct {
	// LLMReviewEnabled 关闭时一律返回 StubRunner。
	LLMReviewEnabled bool
	// AgentReviewEnabled 为 true 且 deps.SpawnReviewAgent!=nil 时选 fork-agent 路径。
	AgentReviewEnabled bool
}

// NewRunner 按配置选择 Runner：
// - LLMReviewEnabled=false → StubRunner；
// - AgentReviewEnabled 且 SpawnReviewAgent!=nil → AgentReviewRunner（fork-agent，失败降级 SkillReviewRunner）；
// - ProposeSkillPatches!=nil → SkillReviewRunner（单次 LLM patch）；
// - 否则 → NoopLLMRunner。
func NewRunner(sel RunnerSelect, deps RunnerDeps) Runner {
	if !sel.LLMReviewEnabled {
		return &StubRunner{
			MemoryNotify:       deps.MemoryNotify,
			ClearGrowthPending: deps.ClearGrowthPending,
		}
	}
	if sel.AgentReviewEnabled && deps.SpawnReviewAgent != nil {
		return &AgentReviewRunner{
			deps:     deps,
			fallback: &SkillReviewRunner{deps: deps},
		}
	}
	if deps.ProposeSkillPatches != nil {
		return &SkillReviewRunner{deps: deps}
	}
	return &NoopLLMRunner{stub: &StubRunner{
		MemoryNotify:       deps.MemoryNotify,
		ClearGrowthPending: deps.ClearGrowthPending,
	}}
}
```

（`AgentReviewRunner` 在 Task 4 定义；本步测试仍会因它未定义而编译失败——先接受，Task 4 补齐。）

- [ ] **Step 4: 暂不运行，进入 Task 4 定义 AgentReviewRunner 后统一验证**

说明：Task 3 与 Task 4 有循环引用（工厂引用类型），须一起编译。此处不单独跑测试。

---

## Task 4: 实现 AgentReviewRunner

**Files:**
- Create: `framework/growth/agent_review_runner.go`
- Create: `framework/growth/agent_review_runner_test.go`

- [ ] **Step 1: 写失败测试**

创建 `framework/growth/agent_review_runner_test.go`：

```go
package growth

import (
	"context"
	"errors"
	"testing"
)

func TestAgentReviewRunner_success_noFallback(t *testing.T) {
	spawnCalled := false
	fallbackCalled := false
	r := &AgentReviewRunner{
		deps: RunnerDeps{
			Transcript: func(ctx context.Context, _ string) (string, error) { return "t", nil },
			SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, tr, _ string) error {
				spawnCalled = true
				if tr != "t" {
					t.Fatalf("transcript not passed through: %q", tr)
				}
				return nil
			},
			ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) {
				fallbackCalled = true
				return nil, nil
			},
			ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
		},
	}
	r.fallback = &SkillReviewRunner{deps: r.deps}
	job := ReviewJob{SessionID: "s1", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !spawnCalled {
		t.Fatal("SpawnReviewAgent not called")
	}
	if fallbackCalled {
		t.Fatal("fallback should not run on success")
	}
}

func TestAgentReviewRunner_fallbackOnError(t *testing.T) {
	fallbackCalled := false
	deps := RunnerDeps{
		Transcript:       func(ctx context.Context, _ string) (string, error) { return "t", nil },
		SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, _, _ string) error { return errors.New("boom") },
		ProposeSkillPatches: func(ctx context.Context, _ ReviewJob, _, _ string) ([]Patch, error) {
			fallbackCalled = true
			return nil, nil
		},
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := &AgentReviewRunner{deps: deps, fallback: &SkillReviewRunner{deps: deps}}
	job := ReviewJob{SessionID: "s1", WorkspaceRoot: t.TempDir(), PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !fallbackCalled {
		t.Fatal("fallback should run when SpawnReviewAgent errors")
	}
}

func TestAgentReviewRunner_memoryOnlyUsesFallback(t *testing.T) {
	spawnCalled := false
	memNotified := false
	deps := RunnerDeps{
		SpawnReviewAgent: func(ctx context.Context, _ ReviewJob, _, _ string) error { spawnCalled = true; return nil },
		MemoryNotify:     func(ctx context.Context, _ string) { memNotified = true },
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := &AgentReviewRunner{deps: deps, fallback: &SkillReviewRunner{deps: deps}}
	job := ReviewJob{SessionID: "s1", PendingMemory: true} // 无 PendingSkill
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if spawnCalled {
		t.Fatal("memory-only job must not spawn agent")
	}
	if !memNotified {
		t.Fatal("memory branch should notify memory")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./growth/ -run TestAgentReviewRunner -v`
Expected: 编译失败（`AgentReviewRunner` 未定义）。

- [ ] **Step 3: 实现 AgentReviewRunner**

创建 `framework/growth/agent_review_runner.go`：

```go
package growth

import (
	"context"
	"fmt"
)

// AgentReviewRunner fork-agent 复盘路径（spec §4.2）：
// PendingSkill 时组装上下文交由 deps.SpawnReviewAgent 自主演化技能库；失败/超时降级到 SkillReviewRunner。
// PendingMemory 分支复用 SkillReviewRunner 的记忆通知语义。
type AgentReviewRunner struct {
	deps     RunnerDeps
	fallback *SkillReviewRunner
}

func (r *AgentReviewRunner) Run(ctx context.Context, job ReviewJob) error {
	if r == nil {
		return nil
	}
	// 技能分支走 fork-agent；成功后由 SpawnReviewAgent 内部完成 ClearGrowthPending(skill)。
	if job.PendingSkill && r.deps.SpawnReviewAgent != nil {
		if err := r.runAgentSkill(ctx, job); err != nil {
			// 降级：fork-agent 失败时回退单次 LLM patch（仅技能分支）。
			if r.fallback != nil {
				return r.fallback.runSkill(ctx, job, r.deps, workspaceOf(job))
			}
			return err
		}
	} else if job.PendingSkill && r.fallback != nil {
		// SpawnReviewAgent 未注入：直接单次 LLM。
		if err := r.fallback.runSkill(ctx, job, r.deps, workspaceOf(job)); err != nil {
			return err
		}
	}

	// 记忆分支：与 SkillReviewRunner 一致。
	if job.PendingMemory && r.deps.MemoryNotify != nil {
		r.deps.MemoryNotify(ctx, job.SessionID)
	}
	if job.PendingMemory && r.deps.ClearGrowthPending != nil {
		if err := r.deps.ClearGrowthPending(ctx, job.SessionID, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentReviewRunner) runAgentSkill(ctx context.Context, job ReviewJob) error {
	workspace := workspaceOf(job)
	if workspace == "" {
		return fmt.Errorf("growth: agent review requires workspace root")
	}
	transcript, err := fetchReviewTranscript(ctx, job, r.deps)
	if err != nil {
		return err
	}
	idx, err := buildReviewIndex(workspace)
	if err != nil {
		return err
	}
	summary := buildReviewSummary(ctx, job, idx, r.deps)
	return r.deps.SpawnReviewAgent(ctx, job, transcript, summary)
}

// workspaceOf 优先 WorkspaceRoot，退回 WorkspaceKey。
func workspaceOf(job ReviewJob) string {
	if job.WorkspaceRoot != "" {
		return job.WorkspaceRoot
	}
	return job.WorkspaceKey
}
```

> 注意：`SkillReviewRunner.runSkill` 现签名为 `runSkill(ctx, job, deps, workspace)`（见 skill_review_runner.go:65），本实现直接复用它，不重复写 patch 应用逻辑。

- [ ] **Step 4: 运行 Task 3 + Task 4 全部测试**

Run: `cd framework && go test ./growth/ -run 'TestNewRunner|TestAgentReviewRunner' -v`
Expected: PASS（工厂选择 + runner 行为 + 降级全绿）。

- [ ] **Step 5: 全量 growth 测试**

Run: `cd framework && go test ./growth/...`
Expected: PASS。

- [ ] **Step 6: Commit（若有 git）**

```bash
git add framework/growth/agent_review_runner.go framework/growth/agent_review_runner_test.go framework/growth/runner_factory.go framework/growth/runner_factory_test.go && git commit -m "feat(growth): add AgentReviewRunner with fallback to SkillReviewRunner"
```

---

## Task 5: conf.proto 新增 agent_review 字段

**Files:**
- Modify: `portal/internal/conf/conf.proto`
- Modify: `portal/internal/conf/conf.pb.go`

- [ ] **Step 1: 在 conf.proto 的 Growth message 末尾（learnings_max_runes = 19 之后）追加字段**

```proto
  // agent_review_enabled 为 true 且 llm 已配置时，会话级技能复盘走 fork ReActAgent 路径（spec §4.5）；默认 false。
  bool agent_review_enabled = 20;
  // agent_review_full_tools 为 true 时复盘 agent 使用全量工具集；默认 false（仅 skillops 安全集）。
  bool agent_review_full_tools = 21;
  // agent_review_max_steps 复盘 agent 步数硬上界；<=0 时默认 12。
  int32 agent_review_max_steps = 22;
  // agent_review_timeout 单次复盘超时（触发降级）；未设置时默认 120s。
  google.protobuf.Duration agent_review_timeout = 23;
```

- [ ] **Step 2: 重新生成 conf.pb.go**

Run（在 portal 目录，若装了 buf/protoc；仓库通常有 Makefile target）：
```bash
cd portal && make config 2>/dev/null || buf generate 2>/dev/null || echo "MANUAL: regenerate conf.pb.go"
```
Expected: `conf.pb.go` 更新出 `GetAgentReviewEnabled()` / `GetAgentReviewFullTools()` / `GetAgentReviewMaxSteps()` / `GetAgentReviewTimeout()` 四个 getter。若无生成工具链，手工按现有字段样式在 `Growth` struct 与 getter 区补齐这 4 个字段（bool/bool/int32/*durationpb.Duration）。

- [ ] **Step 3: 编译验证**

Run: `cd portal && go build ./internal/conf/...`
Expected: PASS，getter 可用。

- [ ] **Step 4: Commit（若有 git）**

```bash
git add portal/internal/conf/conf.proto portal/internal/conf/conf.pb.go && git commit -m "feat(conf): add growth agent_review_* fields"
```

---

## Task 6: portal 实现 spawnReviewAgent

**Files:**
- Create: `portal/internal/service/growth_agent_review.go`
- Create: `portal/internal/service/growth_agent_review_test.go`

- [ ] **Step 1: 写失败测试（fake ToolCallingModel 验证递归保护 + 工具集 + ctx）**

创建 `portal/internal/service/growth_agent_review_test.go`：

```go
package service

import (
	"context"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// fakeToolModel 记录 ChatWithTools 收到的 registry 与 ctx，用于断言瘦身工具集与 workspace 注入。
type fakeToolModel struct {
	sawWorkspace string
	toolNames    []string
}

func (f *fakeToolModel) Generate(ctx context.Context, prompt string) (*model.Generation, error) {
	return &model.Generation{Text: "done"}, nil
}
func (f *fakeToolModel) ChatWithTools(ctx context.Context, msgs []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	if ws, ok := ctx.Value(tool.ContextKeyWorkspaceRoot).(string); ok {
		f.sawWorkspace = ws
	}
	if reg != nil {
		f.toolNames = reg.Names()
	}
	// 不产生 tool_call，直接返回 final，让 ReActAgent 一步收敛。
	return &model.Generation{Text: "no changes"}, nil
}

func TestSpawnReviewAgent_injectsWorkspaceAndSkillopsTools(t *testing.T) {
	ws := t.TempDir()
	fm := &fakeToolModel{}
	w := newTestGrowthWorkerWithModel(t, fm, false /*fullTools*/)

	err := w.spawnReviewAgent(context.Background(),
		testReviewJob("s1", ws), "transcript", "summary")
	if err != nil {
		t.Fatal(err)
	}
	if fm.sawWorkspace != ws {
		t.Fatalf("workspace_root not injected into ctx: got %q want %q", fm.sawWorkspace, ws)
	}
	// 默认工具集应包含 skill_manage/skill_view，不包含 terminal/shell 类。
	if !containsName(fm.toolNames, "skill_manage") {
		t.Fatalf("expected skill_manage in tools, got %v", fm.toolNames)
	}
	if containsName(fm.toolNames, "terminal") || containsName(fm.toolNames, "run_shell") {
		t.Fatalf("skillops-only set must not include shell tools, got %v", fm.toolNames)
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
```

> `newTestGrowthWorkerWithModel`、`testReviewJob`、`reg.Names()` 需存在：
> - `reg.Names()`：若 `tool.Registry` 无 `Names()`，Task 6 Step 3 顺带在 `framework/tool/tool.go` 加一个只读 `Names() []string`（遍历内部 map 返回键）。
> - `newTestGrowthWorkerWithModel` / `testReviewJob`：在本测试文件内定义为构造最小 `GrowthWorker`（只填 spawnReviewAgent 依赖：growthCfg 桩、model 工厂返回 fm、growthUC 桩 ClearGrowthPending no-op）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd portal && go test ./internal/service/ -run TestSpawnReviewAgent -v`
Expected: 编译失败（`spawnReviewAgent` 未定义）。

- [ ] **Step 3: 实现 growth_agent_review.go**

创建 `portal/internal/service/growth_agent_review.go`：

```go
package service

import (
	"context"
	"time"

	"backend/internal/chat"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// agentReviewSystemPrompt 指导 fork 复盘 agent 用工具演化技能库（对齐 Hermes 优先级链路）。
const agentReviewSystemPrompt = `你是一名技能策展员（Skill Curator），运行在后台复盘环节。
你的目标：根据本次会话 transcript 与现有技能库，用工具把可复用的经验沉淀为 SKILL.md。
优先级链路：patch 现有技能 → 为其加 reference/template → 仅当确有新类别时才新建 umbrella 技能。
约束：
- 禁止把 PR 编号、错误字符串、一次性产物变成技能名。
- 禁止对已被 cron 定时任务引用的技能改名（改名会断链）。
- 若确无可沉淀内容，直接结束，不要强行制造变更。
可用工具：skill_view / skills_list 浏览现有技能；read_skill_file 读子文件；skill_manage 创建/修改/删除 SKILL.md。`

// spawnReviewAgent 实现 growth.RunnerDeps.SpawnReviewAgent（fork-agent 复盘路径）。
func (w *GrowthWorker) spawnReviewAgent(ctx context.Context, job growth.ReviewJob, transcript, summary string) error {
	workspace := job.WorkspaceRoot
	if workspace == "" {
		workspace = job.WorkspaceKey
	}

	// 1. 复用 auxiliary cheap 模型（不占主对话配额）。
	m := w.reviewModel
	if m == nil {
		return errNoReviewModel
	}

	// 2. 瘦身工具集：默认仅 skillops；full-tools 时用全量。
	reg, err := w.buildReviewRegistry(workspace)
	if err != nil {
		return err
	}

	// 3. 构造瘦身 ReActAgent —— 递归保护：不设 ToolSuccessHook。
	maxSteps := int(w.growthCfg.GetAgentReviewMaxSteps())
	if maxSteps <= 0 {
		maxSteps = 12
	}
	a := agent.NewReActAgent(m, nil, reg,
		agent.WithReActMaxSteps(maxSteps),
	)

	// 4. ctx 注入 workspace_root（skill_manage 依赖）+ 超时。
	timeout := 120 * time.Second
	if d := w.growthCfg.GetAgentReviewTimeout().AsDuration(); d > 0 {
		timeout = d
	}
	rctx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, workspace)
	rctx, cancel := context.WithTimeout(rctx, timeout)
	defer cancel()

	// 5. Run：transcript + 技能摘要作为首条 user 消息。
	req := &agent.Request{
		SystemPrompt: agentReviewSystemPrompt,
		Messages: []model.Message{{
			Role:    "user",
			Content: "# Skills index snapshot\n" + summary + "\n\n# Transcript\n" + transcript,
		}},
	}
	if _, err := a.Run(rctx, req); err != nil {
		return err // 失败 → AgentReviewRunner 降级
	}

	// 6. 收尾：skill_manage 已完成 pin/lease/ApplyPatchBatch/IndexTracker.Bump；
	//    portal 侧只补缓存失效 + 清 pending(skill)。
	// 注意（spec §7）：fork-agent 路径无 patch 列表可推导技能改名，一期不反写 cron 引用；
	// 已在 system prompt 中禁止对 cron 引用的技能改名作为兜底。
	if w.deps.InvalidateSkillsCache != nil {
		w.deps.InvalidateSkillsCache(rctx, workspace)
	}
	if w.growthCfg.GetAgentReviewFullTools() {
		// full-tools 下 agent 可能改名；一期不反写 cron，记 warn 便于观测已知缺口。
		w.log.Warn("growth: fork-agent review does not rewrite cron skill refs (phase-1 gap)")
	}
	return w.growthUC.ClearGrowthPending(context.Background(), job.SessionID, true, false)
}

// buildReviewRegistry 构造复盘 agent 的工具集。
func (w *GrowthWorker) buildReviewRegistry(workspace string) (*tool.Registry, error) {
	skillsDir := workspace + "/skills"
	idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
	if err != nil {
		return nil, err
	}
	reg := tool.NewRegistry()
	// 复用 portal 既有 skillops 装配（含 skill_manage / skills_list / skill_view / read_skill_file）。
	if err := chat.RegisterSkillTools(reg, idx, nil, true); err != nil {
		return nil, err
	}
	if w.growthCfg.GetAgentReviewFullTools() {
		// full-tools：追加通用工具集（复用 portal 既有 registry 构造）。
		if err := chat.RegisterCommonAgentTools(reg, workspace); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
```

> **落地时须核实并对齐的两处 portal 符号**（若名称不同，用等价物替换，勿新建重复逻辑）：
> - `chat.RegisterSkillTools(reg, idx, nil, true)` —— 已存在于 `portal/internal/chat/agent_builder.go:277`。
> - `chat.RegisterCommonAgentTools(reg, workspace)` —— full-tools 分支用；若 portal 无此单一入口，改为调用 `chat.BuildReActAgent` 所用的同一组 `Register*` 助手，或直接跳过 full-tools（记 TODO），默认路径不受影响。
>
> 另需在 `GrowthWorker` 结构体补三个字段：`reviewModel model.Model`（Task 7 填充）、`deps growth.RunnerDeps`（若尚未持有则保存一份引用）、`growthUC *biz.GrowthUsecase`（worker 通常已持有，确认字段名）。`errNoReviewModel` 定义为包级 `var errNoReviewModel = errors.New("growth: review model not configured")`。

- [ ] **Step 4: 若 tool.Registry 无 Names()，补一个只读方法**

在 `framework/tool/tool.go` 的 `Registry` 方法区追加（仅当不存在时）：

```go
// Names 返回已注册工具名（无序），用于测试与诊断。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	return out
}
```

> 落地前先 `grep -n "func (r \*Registry) Names" framework/tool/tool.go` 确认字段名（`r.tools` / `r.mu`）与真实实现一致，否则按真实字段改写。

- [ ] **Step 5: 运行测试**

Run: `cd portal && go test ./internal/service/ -run TestSpawnReviewAgent -v`
Expected: PASS。

- [ ] **Step 6: Commit（若有 git）**

```bash
git add portal/internal/service/growth_agent_review.go portal/internal/service/growth_agent_review_test.go framework/tool/tool.go && git commit -m "feat(portal): implement spawnReviewAgent fork-agent review"
```

---

## Task 7: 在 GrowthWorker 装配处注入 SpawnReviewAgent + reviewModel

**Files:**
- Modify: `portal/internal/service/growth_worker.go`

- [ ] **Step 1: newGrowthModelClient 额外返回底层 model.Model**

在 `portal/internal/service/growth_worker.go` 找到 `newGrowthModelClient`（约 508 行），新增一个姊妹函数复用其构造逻辑，返回裸 `model.Model`：

```go
// newGrowthReviewModel 复用 newGrowthModelClient 的 auxiliary-优先选择，返回裸 model.Model 供 ReActAgent 使用。
func newGrowthReviewModel(cfg *conf.GrowthLLM) (model.Model, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil GrowthLLM config")
	}
	target := cfg
	if aux := cfg.GetAuxiliary(); aux != nil && strings.TrimSpace(aux.GetModel()) != "" {
		target = aux
	}
	return model.NewModelFromConfig(model.ModelConfig{
		Provider: target.GetProvider(),
		Model:    target.GetModel(),
		APIKey:   target.GetApiKey(),
		BaseURL:  target.GetBaseUrl(),
	})
}
```

- [ ] **Step 2: 在 worker 装配处填充 reviewModel + 注入 SpawnReviewAgent + 改 NewRunner 调用**

在 deps 组装块（约 100-144 行，`w.runner = growth.NewRunner(...)` 之前）追加：

```go
	// fork-agent 复盘路径（spec §4）：仅在 agent_review_enabled 且 LLM 已配置时装配。
	agentReviewEnabled := growthCfg != nil && growthCfg.GetAgentReviewEnabled() &&
		growthCfg.GetLlm() != nil && strings.TrimSpace(growthCfg.GetLlm().GetModel()) != ""
	if agentReviewEnabled {
		if rm, err := newGrowthReviewModel(growthCfg.GetLlm()); err != nil {
			helper.Warnf("growth: agent review model failed, disabling fork-agent path: %v", err)
			agentReviewEnabled = false
		} else {
			w.reviewModel = rm
			w.deps = deps // 供 spawnReviewAgent 读 InvalidateSkillsCache
			deps.SpawnReviewAgent = w.spawnReviewAgent
		}
	}
```

把原来的
```go
	w.runner = growth.NewRunner(llmReviewEnabled, deps)
```
改为
```go
	w.runner = growth.NewRunner(growth.RunnerSelect{
		LLMReviewEnabled:   llmReviewEnabled,
		AgentReviewEnabled: agentReviewEnabled,
	}, deps)
```

并在 `GrowthWorker` 结构体（约 31 行附近）补字段：
```go
	reviewModel model.Model
	deps        growth.RunnerDeps
```
（`growthUC` 若已是字段则复用；确认其字段名，spawnReviewAgent 里的 `w.growthUC` 与之一致。）

- [ ] **Step 3: 确认 model / errors 已 import**

在文件 import 块确保有 `"github.com/sixath/framework/model"` 与 `"errors"`（`errNoReviewModel` 在 Task 6 文件，同包共享）。

- [ ] **Step 4: 编译 + 全量 service 测试**

Run: `cd portal && go build ./... && go test ./internal/service/...`
Expected: PASS（含既有 `growth_llm_wiring_test.go`；如它调用旧 `NewRunner(bool,...)` 需同步改为 `RunnerSelect`——若报错按 Task 3 同样方式修正该测试）。

- [ ] **Step 5: Commit（若有 git）**

```bash
git add portal/internal/service/growth_worker.go && git commit -m "feat(portal): wire SpawnReviewAgent into GrowthWorker"
```

---

## Task 8: 端到端降级验证 + 全量测试

**Files:**
- Modify: `portal/internal/service/growth_agent_review_test.go`（追加降级用例）

- [ ] **Step 1: 追加"model 必失败 → 降级单次 LLM patch"端到端测试**

在 `growth_agent_review_test.go` 追加：

```go
func TestAgentReview_fallsBackToPatchOnModelError(t *testing.T) {
	// 用一个 Run 必返回 error 的 model，验证 AgentReviewRunner 降级到 SkillReviewRunner。
	ws := t.TempDir()
	fallbackHit := false
	deps := growth.RunnerDeps{
		Transcript:       func(ctx context.Context, _ string) (string, error) { return "t", nil },
		SpawnReviewAgent: func(ctx context.Context, _ growth.ReviewJob, _, _ string) error {
			return context.DeadlineExceeded // 模拟 agent 失败/超时
		},
		ProposeSkillPatches: func(ctx context.Context, _ growth.ReviewJob, _, _ string) ([]growth.Patch, error) {
			fallbackHit = true
			return nil, nil // 空 patch，ApplyPatchBatch 应接受
		},
		ClearGrowthPending: func(ctx context.Context, _ string, _, _ bool) error { return nil },
	}
	r := growth.NewRunner(
		growth.RunnerSelect{LLMReviewEnabled: true, AgentReviewEnabled: true},
		deps,
	)
	job := growth.ReviewJob{SessionID: "s1", WorkspaceRoot: ws, PendingSkill: true}
	if err := r.Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !fallbackHit {
		t.Fatal("expected fallback to single-shot LLM patch on agent failure")
	}
}
```

> import 需加 `"github.com/sixath/framework/growth"`。

- [ ] **Step 2: 运行降级测试**

Run: `cd portal && go test ./internal/service/ -run TestAgentReview_fallsBackToPatchOnModelError -v`
Expected: PASS。

- [ ] **Step 3: 全量测试收尾**

Run: `cd framework && go test ./... && cd ../portal && go test ./...`
Expected: 全 PASS。

- [ ] **Step 4: Commit（若有 git）**

```bash
git add portal/internal/service/growth_agent_review_test.go && git commit -m "test(growth): end-to-end fallback from fork-agent to patch path"
```

---

## 验证清单（实施完成后）

- [ ] `agent_review_enabled: false`（默认）时行为与今日完全一致（走 SkillReviewRunner / Stub）。
- [ ] `agent_review_enabled: true` + 有效 LLM：复盘走 fork ReActAgent，写盘经 skill_manage。
- [ ] fork-agent 失败/超时：自动降级到单次 LLM patch，pending 不丢。
- [ ] 复盘 agent 无 `ToolSuccessHook`（递归保护）：其工具调用不再置 pending。
- [ ] 默认工具集不含 shell/terminal；`agent_review_full_tools: true` 才追加全量。
- [ ] `framework/growth` 未 import `framework/agent`（`go list -deps` 或 grep import 确认）。
