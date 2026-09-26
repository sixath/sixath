# deep_investigate 冷启动调查工具 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增能力门控的兜底工具 `deep_investigate`：agent 同时具备代码仓库工具和日志类工具时对模型可见；模型在无线索的定位类问题中主动调用，工具内部跑子 ReAct 循环（代码优先 → 日志验证）并返回调查结论。

**Architecture:** 新包 `framework/investigate`（依赖 harness + tool，无循环依赖）；portal 在三个 agent 构建点注册。门控基于「registry 里工具存在即可用」（portal 注册失败即跳过的既有语义）。

**Tech Stack:** Go，framework（harness/model/tool），portal（backend/internal）。

**参考 spec:** `docs/superpowers/specs/2026-09-26-deep-investigate-design.md`

---

## 关键背景（实现者必读）

- `tool.Registry.Get(name)` 返回注册的工具；`CheckFn func(ctx) error` 返回非 nil 时该工具不进模型 schema（`ListForAPI` 过滤）。
- `tool.EnabledToolsetsFromContext(ctx)` 返回 enabled_toolsets 白名单（nil = 不过滤）。
- `tool.NewRegistry()` 会默认注册 `http_request`——子 registry 需要新增的 `tool.NewEmptyRegistry()`（Task 1）。
- 子 agent：`harness.NewReActAgent(m, nil, subReg, agent.WithReActMaxSteps(15), agent.WithReActSystemPrompt(playbook))`；`Run(ctx, &harness.Request{Messages: msgs})` 返回 `*harness.Response{Text, Messages}`。
- ReAct 循环要求 model 实现 `harness.ToolCallingModel`（`ChatWithTools`），否则退化 runPlain（无工具）——Register 时必须校验。
- 工具结果在 `Response.Messages` 里以 `role:"tool"` + JSON Content 存在；`tool.CollectEvidenceRefs(results...)` 可从解析后的 JSON 提取 evidence_refs。
- 错误码常量：`tool.ErrorTransient` / `tool.ErrorPermanent`。
- Go 文件用 **tab 缩进**；本仓库的 Edit 工具对 tab 文件可能匹配失败，必要时用 Write 重写整个文件或 sed。

---

### Task 1: tool.NewEmptyRegistry + investigate 包骨架（门控 + 注册）

**Files:**
- Modify: `framework/tool/tool.go`（在 `NewRegistry` 后加 `NewEmptyRegistry`）
- Create: `framework/investigate/investigate.go`
- Test: `framework/investigate/investigate_test.go`

- [ ] **Step 1: 写失败测试**

创建 `framework/investigate/investigate_test.go`：

```go
package investigate

import (
	"context"
	"testing"

	"github.com/sixath/framework/tool"
)

func stubTool(name string) tool.Tool {
	return tool.Tool{
		Name:       name,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Execute:    func(ctx context.Context, params map[string]any) (any, error) { return map[string]any{"ok": true}, nil },
	}
}

func TestGate_RequiresBothGroups(t *testing.T) {
	cases := []struct {
		name    string
		tools   []string
		visible bool
	}{
		{"none", nil, false},
		{"code only", []string{"rca_grep"}, false},
		{"log only", []string{"es_log_query"}, false},
		{"both", []string{"rca_grep", "es_log_query"}, true},
		{"both via other names", []string{"rca_read", "vm_run_cmd"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := tool.NewEmptyRegistry()
			for _, n := range c.tools {
				if err := reg.Register(stubTool(n)); err != nil {
					t.Fatal(err)
				}
			}
			g := gate(reg)
			err := g(context.Background())
			if c.visible && err != nil {
				t.Fatalf("want visible, got err=%v", err)
			}
			if !c.visible && err == nil {
				t.Fatal("want hidden, got nil err")
			}
		})
	}
}

func TestGate_RespectsToolCheckFn(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	gated := stubTool("es_log_query")
	gated.CheckFn = func(ctx context.Context) error { return errors.New("gated off") }
	_ = reg.Register(stubTool("rca_grep"))
	_ = reg.Register(gated)
	if err := gate(reg)(context.Background()); err == nil {
		t.Fatal("tool whose CheckFn fails must not satisfy the gate")
	}
}

func TestGate_RespectsEnabledToolsets(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	rca := stubTool("rca_grep")
	rca.Toolset = tool.ToolsetRCA
	es := stubTool("es_log_query")
	es.Toolset = tool.ToolsetRCA
	_ = reg.Register(rca)
	_ = reg.Register(es)
	// 白名单只放行 core → rca 工具被过滤 → 门控失败
	ctx := context.WithValue(context.Background(), tool.ContextKeyEnabledToolsets, []string{tool.ToolsetCore})
	if err := gate(reg)(ctx); err == nil {
		t.Fatal("tools filtered by enabled_toolsets must not satisfy the gate")
	}
	// 白名单含 rca → 通过
	ctx = context.WithValue(context.Background(), tool.ContextKeyEnabledToolsets, []string{tool.ToolsetRCA})
	if err := gate(reg)(ctx); err != nil {
		t.Fatalf("want pass, got %v", err)
	}
}

func TestRegister_ToolAppearsOnlyWhenGated(t *testing.T) {
	m := &fakeToolModel{finalText: "结论\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	if err := Register(reg, Config{Model: m}); err != nil {
		t.Fatal(err)
	}
	// 无底层工具 → 不可见
	if n := len(reg.ListForAPI(context.Background(), nil)); n != 0 {
		t.Fatalf("gate should hide tool, got %d tools in schema", n)
	}
	// 补齐两组 → 可见
	_ = reg.Register(stubTool("rca_grep"))
	_ = reg.Register(stubTool("es_log_query"))
	api := reg.ListForAPI(context.Background(), nil)
	if len(api) != 3 {
		t.Fatalf("want 3 tools visible, got %d", len(api))
	}
}

func TestRegister_RejectsNilModel(t *testing.T) {
	if err := Register(tool.NewEmptyRegistry(), Config{}); err == nil {
		t.Fatal("nil model must be rejected")
	}
}

func TestRegister_RejectsNonToolCallingModel(t *testing.T) {
	if err := Register(tool.NewEmptyRegistry(), Config{Model: &plainModel{}}); err == nil {
		t.Fatal("model without ChatWithTools must be rejected")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./investigate/ -run "TestGate|TestRegister" -v`
Expected: FAIL（包不存在 / undefined: NewEmptyRegistry, gate, Register, fakeToolModel, plainModel）

- [ ] **Step 3: 实现 NewEmptyRegistry**

`framework/tool/tool.go`，在 `NewRegistry` 函数后追加：

```go
// NewEmptyRegistry 返回不预注册任何内置工具的 Registry（NewRegistry 会默认注册 http_request）。
// 用于构建严格受限的子工具集（如 deep_investigate 的子 agent）。
func NewEmptyRegistry() *Registry {
	return &Registry{
		tools:        make(map[string]Tool),
		mcpServerIDs: make(map[string]struct{}),
	}
}
```

- [ ] **Step 4: 实现 investigate.go 骨架（门控 + 注册，Execute 先用占位）**

创建 `framework/investigate/investigate.go`：

```go
// Package investigate 提供冷启动兜底调查工具 deep_investigate：
// agent 同时具备代码仓库工具与日志类工具时对模型可见；模型在无线索的
// 定位类问题中主动调用，工具内部跑子 ReAct 循环（代码优先 → 日志验证）
// 并返回调查结论。详见 docs/superpowers/specs/2026-09-26-deep-investigate-design.md。
package investigate

import (
	"context"
	"errors"
	"time"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// ToolName 是兜底调查工具的注册名。
const ToolName = "deep_investigate"

// codeToolNames 代码仓库工具组：任一存在即满足代码侧门控。
var codeToolNames = []string{"rca_grep", "rca_glob", "rca_read", "rca_symbol"}

// logToolNames 日志/链路工具组：任一存在即满足日志侧门控。
var logToolNames = []string{"es_log_query", "jaeger_trace", "vm_run_cmd"}

// Config 为 deep_investigate 的运行配置。
type Config struct {
	// Model 子 agent 使用的模型；必须实现 harness.ToolCallingModel（通常复用父 agent 的 chat 模型）。
	Model model.Model
	// MaxSteps 子 ReAct 循环步数上限；<=0 时默认 15。
	MaxSteps int
	// Timeout 整轮子调查超时；<=0 时默认 15 分钟。
	Timeout time.Duration
}

func (c Config) maxSteps() int {
	if c.MaxSteps > 0 {
		return c.MaxSteps
	}
	return 15
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 15 * time.Minute
}

// Register 把 deep_investigate 注册进 reg。可见性由 CheckFn 门控决定：
// 代码工具组与日志工具组各至少一个「可见」（存在、CheckFn 通过、未被
// enabled_toolsets 白名单过滤）时才出现在模型 schema 中。
func Register(reg *tool.Registry, cfg Config) error {
	if reg == nil {
		return errors.New("investigate: registry is nil")
	}
	if cfg.Model == nil {
		return errors.New("investigate: model is nil")
	}
	if _, ok := cfg.Model.(harness.ToolCallingModel); !ok {
		return errors.New("investigate: model does not support tool calling")
	}
	return reg.Register(tool.Tool{
		Name:               ToolName,
		Description:        toolDescription,
		Toolset:            tool.ToolsetRCA,
		AlwaysLoad:         true, // 兜底工具必须扛住 defer 过滤，始终可见
		RequiresSequential: true, // 重操作，不与其他工具并行
		CheckFn:            gate(reg),
		Timeout:            cfg.timeout(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The user's problem to investigate, restated faithfully (do not paraphrase away specifics like names/ids).",
				},
				"hints": map[string]any{
					"type":        "string",
					"description": "Optional known hints: service name, time window, vmid, interface name, etc.",
				},
			},
			"required": []string{"question"},
		},
		Execute: buildExecute(reg, cfg),
	})
}

// gate 返回 CheckFn：代码组与日志组各至少一个工具可见时放行。
func gate(reg *tool.Registry) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if !anyVisible(ctx, reg, codeToolNames) {
			return errors.New("deep_investigate: no code repository tools available")
		}
		if !anyVisible(ctx, reg, logToolNames) {
			return errors.New("deep_investigate: no log query tools available")
		}
		return nil
	}
}

// anyVisible 判断组内是否有至少一个工具对当前 ctx 可见：
// 已注册、自身 CheckFn 通过、未被 enabled_toolsets 白名单排除。
func anyVisible(ctx context.Context, reg *tool.Registry, names []string) bool {
	for _, name := range names {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}
		if t.CheckFn != nil && t.CheckFn(ctx) != nil {
			continue
		}
		if allowed := tool.EnabledToolsetsFromContext(ctx); allowed != nil {
			found := false
			for _, ts := range allowed {
				if ts == t.Toolset {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		return true
	}
	return false
}
```

注意：`toolDescription` 与 `playbookPrompt` 在 Task 3 填充完整文案；`buildExecute` 在 Task 2 实现。本步骤先在 `framework/investigate/playbook.go` 放占位常量让编译通过：

```go
package investigate

// toolDescription 是模型决定是否调用本工具时读到的文本（Task 3 填充完整文案）。
const toolDescription = "placeholder"

// playbookPrompt 是子 agent 的系统提示（Task 3 填充完整文案）。
const playbookPrompt = "placeholder"
```

- [ ] **Step 5: 补测试替身（fakeToolModel / plainModel）**

`framework/investigate/investigate_test.go` 末尾追加：

```go
// plainModel 只实现 model.Model，不支持 ChatWithTools。
type plainModel struct{}

func (plainModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, errors.New("not implemented")
}
func (plainModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "plain"}, nil
}
func (plainModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

// fakeToolModel 实现 harness.ToolCallingModel：第一次调用发起 rca_grep 工具调用，
// 之后返回最终文本。lastReg 记录模型实际收到的 registry（用于验证子 registry 过滤）。
type fakeToolModel struct {
	calls     int
	finalText string
	lastReg   *tool.Registry
}

func (f *fakeToolModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeToolModel) Chat(ctx context.Context, msgs []model.Message, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: f.finalText}, nil
}
func (f *fakeToolModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (f *fakeToolModel) ChatWithTools(ctx context.Context, msgs []model.Message, reg *tool.Registry, opts ...model.Option) (*model.Generation, error) {
	f.lastReg = reg
	f.calls++
	if f.calls == 1 {
		return &model.Generation{Raw: model.ToolStep{
			Used: true,
			ToolCalls: []model.ToolCall{{
				ID:        "call-1",
				Name:      "rca_grep",
				Arguments: map[string]any{"pattern": "x"},
			}},
		}}, nil
	}
	return &model.Generation{Text: f.finalText, Raw: model.ToolStep{Used: false}}, nil
}
```

同时在测试文件 import 中加 `"errors"` 和 `"github.com/sixath/framework/model"`。

- [ ] **Step 6: 运行测试**

Run: `cd framework && go test ./investigate/ -v`
Expected: `TestRegister_ToolAppearsOnlyWhenGated` 因 buildExecute 未定义而编译失败 → 临时把 `Execute: buildExecute(reg, cfg)` 换成占位 `Execute: func(ctx context.Context, params map[string]any) (any, error) { return nil, errors.New("not implemented") }`，跑通 Task 1 全部测试，Task 2 再换回真实实现。
Expected（占位后）: PASS

- [ ] **Step 7: 提交**

```bash
git add framework/tool/tool.go framework/investigate/
git commit -m "feat(investigate): add deep_investigate tool skeleton with capability gate"
```

---

### Task 2: 子 agent 执行器（runner.go）

**Files:**
- Create: `framework/investigate/runner.go`
- Test: `framework/investigate/runner_test.go`

- [ ] **Step 1: 写失败测试**

创建 `framework/investigate/runner_test.go`：

```go
package investigate

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func rcaStubWithEvidence() tool.Tool {
	t := stubTool("rca_grep")
	t.Execute = func(ctx context.Context, params map[string]any) (any, error) {
		return map[string]any{
			"ok": true,
			"evidence_refs": []map[string]any{{
				"kind": "code", "repo": "myrepo", "path": "internal/foo.go", "line": 42,
			}},
		}, nil
	}
	return t
}

func TestExecute_FullFlowReturnsConclusion(t *testing.T) {
	m := &fakeToolModel{finalText: "空响应由缓存未命中导致\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	if err := Register(reg, Config{Model: m}); err != nil {
		t.Fatal(err)
	}
	toolEntry, ok := reg.Get(ToolName)
	if !ok {
		t.Fatal("tool not registered")
	}
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "为什么接口偶尔返回空"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	res, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map result, got %T", out)
	}
	if res["ok"] != true {
		t.Fatalf("want ok=true, got %v", res)
	}
	conclusion, _ := res["conclusion"].(string)
	if !strings.Contains(conclusion, "缓存未命中") {
		t.Fatalf("conclusion missing model text: %q", conclusion)
	}
	if strings.Contains(conclusion, "状态:") {
		t.Fatalf("status line must be stripped from conclusion: %q", conclusion)
	}
	if res["insufficient_evidence"] != false {
		t.Fatalf("want insufficient_evidence=false, got %v", res["insufficient_evidence"])
	}
	if res["steps_taken"] != 1 {
		t.Fatalf("want steps_taken=1, got %v", res["steps_taken"])
	}
	refs, _ := res["evidence_refs"].([]tool.EvidenceRef)
	if len(refs) != 1 || refs[0].Path != "internal/foo.go" {
		t.Fatalf("evidence refs not collected: %#v", res["evidence_refs"])
	}
}

func TestExecute_InsufficientEvidenceParsed(t *testing.T) {
	m := &fakeToolModel{finalText: "查了代码和三处日志都没有发现异常\n状态: 证据不足"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "为什么慢"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	res := out.(map[string]any)
	if res["insufficient_evidence"] != true {
		t.Fatalf("want insufficient_evidence=true, got %v", res["insufficient_evidence"])
	}
}

func TestExecute_EmptyQuestionRejected(t *testing.T) {
	m := &fakeToolModel{finalText: "x"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	out, err := toolEntry.Execute(context.Background(), map[string]any{"question": "   "})
	if err == nil {
		t.Fatal("empty question must return error")
	}
	res := out.(map[string]any)
	if res["ok"] != false || res["error_code"] != tool.ErrorPermanent {
		t.Fatalf("want permanent error payload, got %#v", res)
	}
	if m.calls != 0 {
		t.Fatalf("model must not be called for empty question, got %d calls", m.calls)
	}
}

func TestExecute_SubRegistryExcludesSelfAndUnrelated(t *testing.T) {
	m := &fakeToolModel{finalText: "结论\n状态: 证据充分"}
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(rcaStubWithEvidence())
	_ = reg.Register(stubTool("es_log_query"))
	_ = reg.Register(stubTool("write_file")) // 不应进入子 registry
	_ = Register(reg, Config{Model: m})
	toolEntry, _ := reg.Get(ToolName)
	if _, err := toolEntry.Execute(context.Background(), map[string]any{"question": "q"}); err != nil {
		t.Fatal(err)
	}
	if m.lastReg == nil {
		t.Fatal("model never received a registry")
	}
	if _, ok := m.lastReg.Get(ToolName); ok {
		t.Fatal("sub registry must not contain deep_investigate itself (recursion)")
	}
	if _, ok := m.lastReg.Get("write_file"); ok {
		t.Fatal("unrelated tools must not enter sub registry")
	}
	if _, ok := m.lastReg.Get("rca_grep"); !ok {
		t.Fatal("code tools must be in sub registry")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./investigate/ -run TestExecute -v`
Expected: 编译失败（undefined: buildExecute）

- [ ] **Step 3: 实现 runner.go**

创建 `framework/investigate/runner.go`：

```go
package investigate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// buildExecute 返回 deep_investigate 的执行体：组装子 registry（代码组 + 日志组，
// 不含自身与其他无关工具），跑子 ReAct 循环，把结论与证据引用打包返回。
func buildExecute(parent *tool.Registry, cfg Config) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		started := time.Now()
		question, _ := params["question"].(string)
		question = strings.TrimSpace(question)
		if question == "" {
			err := errors.New("investigate: question is required")
			return map[string]any{"ok": false, "error": err.Error(), "error_code": tool.ErrorPermanent}, err
		}
		hints, _ := params["hints"].(string)

		sub := tool.NewEmptyRegistry()
		names := 0
		for _, name := range append(append([]string{}, codeToolNames...), logToolNames...) {
			if t, ok := parent.Get(name); ok {
				// 重复包装（timeout/参数校验）无副作用：内层先触发，语义不变。
				if err := sub.Register(t); err == nil {
					names++
				}
			}
		}
		if names == 0 {
			err := errors.New("investigate: no investigation tools available")
			return map[string]any{"ok": false, "error": err.Error(), "error_code": tool.ErrorPermanent}, err
		}

		userText := question
		if h := strings.TrimSpace(hints); h != "" {
			userText += "\n\n已知线索：" + h
		}
		agentOpts := []harness.ReActOption{
			harness.WithReActMaxSteps(cfg.maxSteps()),
			harness.WithReActSystemPrompt(playbookPrompt),
		}
		subAgent := harness.NewReActAgent(cfg.Model, nil, sub, agentOpts...)

		resp, err := subAgent.Run(ctx, &harness.Request{
			Messages: []model.Message{{Role: "user", Content: userText}},
		})
		if err != nil {
			return map[string]any{
				"ok":           false,
				"error":        err.Error(),
				"error_code":   tool.ErrorTransient,
				"duration_ms":  time.Since(started).Milliseconds(),
				"steps_taken":  0,
			}, err
		}

		conclusion, insufficient := parseConclusion(resp.Text)
		refs := collectRefs(resp.Messages)
		return map[string]any{
			"ok":                    true,
			"conclusion":            conclusion,
			"evidence_refs":         refs,
			"insufficient_evidence": insufficient,
			"steps_taken":           countToolMessages(resp.Messages),
			"duration_ms":           time.Since(started).Milliseconds(),
		}, nil
	}
}

// parseConclusion 从子 agent 最终文本中剥离状态行，返回正文与「证据不足」标记。
// 状态行约定见 playbookPrompt：「状态: 证据充分」或「状态: 证据不足」（全角冒号兼容）。
func parseConclusion(text string) (conclusion string, insufficient bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	keep := lines[:0]
	for _, l := range lines {
		norm := strings.NewReplacer("：", ":", " ", "").Replace(strings.TrimSpace(l))
		if norm == "状态:证据充分" {
			continue
		}
		if norm == "状态:证据不足" {
			insufficient = true
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), insufficient
}

// collectRefs 从子 agent 的 tool 消息中提取全部 evidence_refs。
func collectRefs(msgs []model.Message) []tool.EvidenceRef {
	var results []any
	for _, m := range msgs {
		if !strings.EqualFold(m.Role, "tool") {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(m.Content), &v); err == nil {
			results = append(results, v)
		}
	}
	return tool.CollectEvidenceRefs(results...)
}

// countToolMessages 统计子 agent 消息中的工具调用结果数（即实际工具步数）。
func countToolMessages(msgs []model.Message) int {
	n := 0
	for _, m := range msgs {
		if strings.EqualFold(m.Role, "tool") {
			n++
		}
	}
	return n
}
```

并把 investigate.go 中的占位 `Execute:` 换回 `Execute: buildExecute(reg, cfg)`。

- [ ] **Step 4: 运行测试**

Run: `cd framework && go test ./investigate/ -v`
Expected: 全部 PASS（含 Task 1 的门控测试与 Task 2 的执行测试）

- [ ] **Step 5: 提交**

```bash
git add framework/investigate/
git commit -m "feat(investigate): run sub-agent investigation loop and return conclusion with evidence"
```

---

### Task 3: 调查手册 prompt（playbook.go）

**Files:**
- Create: `framework/investigate/playbook.go`

- [ ] **Step 1: 用完整文案替换占位**

创建/重写 `framework/investigate/playbook.go`：

```go
package investigate

// toolDescription 是模型决定是否调用本工具时读到的文本。
// 必须写清「何时调用」与「何时不要调用」，避免兜底工具被滥用成首选。
const toolDescription = `Deep cold-start investigation for locating problems that have NO direct lead: no trace id, no quoted error text, and no skill/tool that directly applies. Runs an internal investigation loop (code analysis first, then logs/traces) and returns a conclusion with evidence references.

Use ONLY when ALL of the following hold:
- The user asks to locate / diagnose / find the root cause of a problem;
- You do NOT have a trace id, quoted error message, or an obvious matching skill or tool;
- Answering without investigating would be speculation.

Do NOT use for: general Q&A, how-to questions, or problems already covered by a skill. Call at most once per user question.`

// playbookPrompt 是子 agent 的系统提示：代码优先 → 日志验证 → 结论。
// 与 rca-investigation skill（有线索优先：trace/报错 → 日志 → 代码）互补。
const playbookPrompt = `你是一个冷启动问题定位调查器。用户的问题没有现成线索（无 trace_id、无报错原文），你的任务是：先理解代码逻辑，再用日志/链路数据验证，最终定位问题。

## 调查顺序（必须遵守）
1. 代码理解：用 rca_grep 搜索与问题相关的关键词（接口名、函数名、错误码、配置项），用 rca_glob/rca_read 读关键文件。理解：正常逻辑应该是什么、代码里哪些地方会返回异常/打日志。
2. 形成假设：基于代码理解，列出最可能的 1-3 个失败点，以及每个失败点对应的日志关键词或 span 特征。
3. 日志验证：带假设去查。es_log_query 的 cluster 参数必填（工具描述里列出了可用的 cluster 名，禁止编造）；有 trace 线索时用 jaeger_trace；日志不在 ES 时用 vm_run_cmd 进实例查（只支持 cmd.exe 命令：type/dir/findstr/tasklist，禁止 PowerShell）。
4. 得出结论：按 症状 → 证据（代码位置/日志/span）→ 根因 的链条输出。

## 纪律
- 每个结论必须来自工具返回的证据；禁止编造日志内容、文件内容或代码行号。
- 工具返回 ok:false 且 error_code=transient 时可换更窄的条件重试一次；permanent 错误换路径，不要纠缠。
- es_log_query 返回 hit_status=empty 表示查询有效但无匹配（不是没有日志）；index_error=unresolved 表示索引不存在，用返回的 suggested_index_patterns 重试。
- vm_run_cmd 返回 output_empty:true 表示命令成功但无输出，不是日志缺失。
- 做完诚实的尝试后仍无证据，明确说明证据不足，不要硬凑结论。

## 输出格式
正文输出调查结论（中文）。最后一行必须单独写（不要遗漏、不要加其他内容）：
状态: 证据充分
或
状态: 证据不足`
```

- [ ] **Step 2: 回归测试 + 提交**

Run: `cd framework && go test ./investigate/ -v`
Expected: 全部 PASS

```bash
git add framework/investigate/playbook.go
git commit -m "feat(investigate): add cold-start investigation playbook and tool description"
```

---

### Task 4: Portal 接线（3 个 agent 构建点）

**Files:**
- Modify: `portal/internal/service/agent.go`（BuildRegistry 调用成功后、BuildAgent 之前）
- Modify: `portal/internal/service/chat.go`（两处，同上）

- [ ] **Step 1: agent.go 接线**

`portal/internal/service/agent.go`，在 `chat.BuildRegistry(...)` 成功返回之后（约 line 315，`mcpServers := regResult.McpServers` 之前）插入：

```go
	// deep_investigate 兜底调查工具：门控可见（代码+日志工具都配置才进 schema）。
	if err := investigate.Register(reg, investigate.Config{Model: m}); err != nil {
		s.log.Errorf("Chat register deep_investigate failed: agent_id=%s err=%v", agentID, err)
	}
```

import 加 `"github.com/sixath/framework/investigate"`。

- [ ] **Step 2: chat.go 两处接线**

`portal/internal/service/chat.go` 两处 `chat.BuildRegistry(...)` 成功返回之后（约 line 422 与 line 720，均以 `a := chat.BuildAgent(m, reg, ...)` 为后续锚点）各插入同样的注册块。chat.go 与 agent.go 同样使用 `s.log.Errorf` 记录错误：

```go
	// deep_investigate 兜底调查工具：门控可见（代码+日志工具都配置才进 schema）。
	if err := investigate.Register(reg, investigate.Config{Model: m}); err != nil {
		s.log.Errorf("register deep_investigate failed: err=%v", err)
	}
```

import 加 `"github.com/sixath/framework/investigate"`。

- [ ] **Step 3: 编译验证**

Run: `cd portal && go build ./...`
Expected: 无输出（编译通过）

- [ ] **Step 4: 提交**

```bash
git add portal/internal/service/agent.go portal/internal/service/chat.go
git commit -m "feat(portal): register deep_investigate fallback tool at agent build sites"
```

---

### Task 5: 全量回归 + 冒烟验证

**Files:** 无新增

- [ ] **Step 1: framework 全量测试**

Run: `cd framework && go test ./... 2>&1 | grep -E "^(FAIL|--- FAIL)"`
Expected: 仅既有的 3 个 `TestVMRunCmd_*` 失败（tool 包，预存在问题，与本次无关）

- [ ] **Step 2: portal 构建 + 相关包测试**

Run: `cd portal && go build ./... && go test ./internal/chat/ -run "TestSkillsCatalog" -v`
Expected: BUILD OK；PASS（`TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook` 与 `TestNotifySessionMessageIndexed_WithDetachedCaller` 为预存失败，如全量跑会看到，与本次无关）

- [ ] **Step 3: 端到端冒烟（人工）**

后端运行中（:8000），任选一个同时配置了 rca 代码工具和 es_log_query 的 agent，在测试对话里提一个无线索的定位类问题（如「为什么 XX 接口偶尔返回空」），确认：
1. 模型调用了 `deep_investigate`
2. 工具返回 `ok:true` + `conclusion` + `evidence_refs` + `insufficient_evidence` + `steps_taken`
3. 模型的最终回答基于该结论
4. 再选一个没配 rca 工具的 agent，确认 schema 里没有 `deep_investigate`（可问模型「你有哪些工具」间接确认，或调 list_tools）

- [ ] **Step 4: 最终提交（如有修复）并推送**

```bash
git add -A && git commit -m "test(investigate): smoke verification fixes" || true
git push
```
