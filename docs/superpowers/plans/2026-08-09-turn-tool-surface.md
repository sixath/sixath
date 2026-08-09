# Turn Tool Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每轮按意图收窄工具面（装配只注册 ActiveFamilies ∪ core），并用扩展后的 `TurnIntentGate` 丢弃跨族 `tool_call`，避免 GitLab 问询误调 `jaeger_trace` 等多 MCP 糊成一团。

**Architecture:** Portal `IntentResolver`（规则优先 → 低置信/多意图轻量分类 → Fail-narrow）在 `BuildRegistry` 前算出 `ActiveFamilies`；`BuildRegistry` / runtime 注册按族过滤；`TurnIntentGate` 携带 `ActiveFamilies` + `tool→family` 做执行前兜底。framework `PostModelPolicy` 接口不改。权威规格：[`2026-08-09-turn-tool-surface-design.md`](../specs/2026-08-09-turn-tool-surface-design.md)。

**Tech Stack:** Go（portal `internal/chat` + `internal/service/chat.go`）、既有 `framework/agent.PostModelPolicy`、`framework/model.Model`、`framework/tool.Registry`。

**Repos:** 实现改动在 nested `portal/`（独立 git）。本 plan/spec 在 sixath monorepo `docs/`。**Do not commit unless asked.**

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/chat/tool_families.go` | 族 ID 常量、内置工具→族、关键词别名、`BoundFamilies`、`FamilyForRegisteredTool`、`ToolSurfaceEnabled` |
| `portal/internal/chat/tool_families_test.go` | 族映射与 BoundFamilies 单测 |
| `portal/internal/chat/intent_resolver.go` | `IntentResolveResult`、`IntentResolver.Resolve`（规则 + 可选分类 + Fail-narrow） |
| `portal/internal/chat/intent_resolver_test.go` | 规则 / Fail-narrow / 分类 mock 单测 |
| `portal/internal/chat/intent_classifier.go` | `FamilyClassifier` 接口 + 默认 `ModelFamilyClassifier`（短 JSON） |
| `portal/internal/chat/intent_classifier_test.go` | 解析 JSON、非法/超时降级单测 |
| `portal/internal/chat/agent_builder.go` | `BuildRegistry` 增加 `RegistryBuildOptions`（ActiveFamilies 过滤）；`BuildReActAgent` / `NewTurnIntentGate` 接线 |
| `portal/internal/chat/agent_builder_test.go` | registry 过滤单测（扩展现有文件或新建） |
| `portal/internal/chat/runtime_tools.go` | `ActiveFamilies`：未激活则跳过 `web` / `knowledge` 注册 |
| `portal/internal/chat/turn_intent_gate.go` | 族感知过滤（先于 topic overlap） |
| `portal/internal/chat/turn_intent_gate_test.go` | `family_not_active` 等 |
| `portal/internal/service/chat.go` | `SendMessage` / `SendMessageStream`：Resolve → 过滤装配 → Gate 映射 → 日志 |
| `portal/docs/mcp-stdio-server.md` 或新建短文 | 可选：指向本能力与开关（若已有 chat harness 文档则改那处） |

---

### Task 1: Tool family catalog

**Files:**
- Create: `portal/internal/chat/tool_families.go`
- Create: `portal/internal/chat/tool_families_test.go`

- [ ] **Step 1: Write failing tests**

```go
package chat

import (
	"testing"

	"backend/internal/biz"
	"github.com/sixath/framework/tool"
)

func TestToolSurfaceEnabled_DefaultOn(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	if !ToolSurfaceEnabled() {
		t.Fatal("default should be enabled")
	}
}

func TestToolSurfaceEnabled_Off(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "0")
	if ToolSurfaceEnabled() {
		t.Fatal("0 should disable")
	}
}

func TestFamilyForBuiltinToolName(t *testing.T) {
	if FamilyForBuiltinToolName("jaeger_trace") != FamilyRCA {
		t.Fatal("jaeger → rca")
	}
	if FamilyForBuiltinToolName("web_search") != FamilyWeb {
		t.Fatal("web_search → web")
	}
	if FamilyForBuiltinToolName("knowledge_read") != FamilyKnowledge {
		t.Fatal("knowledge → knowledge")
	}
	if FamilyForBuiltinToolName("todo") != FamilyCore {
		t.Fatal("todo → core")
	}
}

func TestBoundFamiliesFromBindings(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}, {ID: "confluence", Name: "Confluence"}}
	tools := []*biz.ToolMeta{
		{Name: "rca-j", Type: biz.ToolTypeRCA, Config: map[string]any{"rca": map[string]any{"func_path": "jaeger_trace"}}},
	}
	bound := BoundFamiliesFrom(tools, servers, true /* web */, true /* knowledge */)
	want := []string{FamilyCore, FamilyRCA, FamilyWeb, FamilyKnowledge, "mcp:gitlab", "mcp:confluence"}
	for _, id := range want {
		if !familySet(bound)[id] {
			t.Fatalf("missing %s in %#v", id, bound)
		}
	}
}

func TestFamilyForRegisteredTool_MCPBinding(t *testing.T) {
	tl := tool.Tool{Name: "list_projects", Bindings: map[string]string{"mcp_server": "gitlab"}}
	if FamilyForRegisteredTool(tl) != "mcp:gitlab" {
		t.Fatalf("got %q", FamilyForRegisteredTool(tl))
	}
}
```

- [ ] **Step 2: Run tests — expect fail**

```bash
cd portal && go test ./internal/chat/ -run "TestToolSurfaceEnabled|TestFamilyFor|TestBoundFamilies" -count=1
```

Expected: undefined symbols.

- [ ] **Step 3: Implement catalog**

```go
package chat

import (
	"os"
	"strings"

	"backend/internal/biz"
	"github.com/sixath/framework/tool"
)

const (
	FamilyCore       = "core"
	FamilyRCA        = "rca"
	FamilyWeb        = "web"
	FamilyKnowledge  = "knowledge"
	turnToolSurfaceEnv = "SATH_TURN_TOOL_SURFACE"
)

func ToolSurfaceEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(turnToolSurfaceEnv)))
	return !(v == "0" || v == "false" || v == "off" || v == "no")
}

func MCPFamilyID(serverID string) string {
	return "mcp:" + strings.TrimSpace(serverID)
}

func LegacyMCPFamilyID(toolID string) string {
	return "mcp:legacy:" + strings.TrimSpace(toolID)
}

// builtinToolFamily maps known non-MCP tool names → family.
var builtinToolFamily = map[string]string{
	"jaeger_trace": "rca", "es_log_query": "rca",
	"rca_grep": "rca", "rca_glob": "rca", "rca_read": "rca",
	"rca_symbol_search": "rca", // add real RCA symbol names if registered under different names
	"web_search": "web", "web_extract": "web",
	"knowledge_search": "knowledge", "knowledge_read": "knowledge",
	"knowledge_write": "knowledge", "knowledge_approve": "knowledge",
}

// familyKeywords: family → aliases (lowercase). MCP families also match server id/name at resolve time.
var familyKeywords = map[string][]string{
	FamilyRCA:      {"jaeger", "trace", "span", "opentelemetry", "otel", "es_log", "elasticsearch", "日志排查", "链路"},
	FamilyWeb:      {"联网", "搜索网页", "web_search", "http://", "https://"},
	FamilyKnowledge: {"wiki", "knowledge", "知识库", "文档库"},
	// mcp:gitlab / mcp:confluence extras applied dynamically from server id+name in Resolve
}

func FamilyForBuiltinToolName(name string) string {
	if f, ok := builtinToolFamily[strings.TrimSpace(name)]; ok {
		return f
	}
	return FamilyCore
}

func FamilyForRegisteredTool(tl tool.Tool) string {
	if tl.Bindings != nil {
		if sid := strings.TrimSpace(tl.Bindings["mcp_server"]); sid != "" {
			return MCPFamilyID(sid)
		}
	}
	return FamilyForBuiltinToolName(tl.Name)
}

func BoundFamiliesFrom(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, webEnabled, knowledgeEnabled bool) []string {
	set := map[string]struct{}{FamilyCore: {}}
	for _, s := range servers {
		if s == nil || s.ID == "" {
			continue
		}
		set[MCPFamilyID(s.ID)] = struct{}{}
	}
	for _, t := range tools {
		if t == nil {
			continue
		}
		switch t.Type {
		case biz.ToolTypeRCA:
			set[FamilyRCA] = struct{}{}
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(toolConfigToMap(t.Config))
			if mc != nil && mc.Id != "" {
				set[MCPFamilyID(mc.Id)] = struct{}{}
			} else {
				set[LegacyMCPFamilyID(t.Name)] = struct{}{}
			}
		case biz.ToolTypeDatasource, biz.ToolTypeBuiltin:
			// phase-1: treat as core (always allowed when bound)
		}
	}
	if webEnabled {
		set[FamilyWeb] = struct{}{}
	}
	if knowledgeEnabled {
		set[FamilyKnowledge] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

func familySet(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func FamilyActive(active map[string]struct{}, family string) bool {
	if active == nil {
		return true // nil = no filter
	}
	_, ok := active[family]
	return ok
}
```

Notes:
- Reuse existing `toolConfigToMap` in `agent_builder.go` (same package).
- Expand `builtinToolFamily` to match whatever `registerRCATool` actually registers (read `rca_builder.go` / symbol tool names during implement).

- [ ] **Step 4: Run tests — expect pass**

```bash
cd portal && go test ./internal/chat/ -run "TestToolSurfaceEnabled|TestFamilyFor|TestBoundFamilies" -count=1
```

- [ ] **Step 5: Commit (only if user asked)**

```bash
cd portal && git add internal/chat/tool_families.go internal/chat/tool_families_test.go && git commit -m "feat(chat): add tool family catalog for turn surface"
```

---

### Task 2: IntentResolver — rules path

**Files:**
- Create: `portal/internal/chat/intent_resolver.go`
- Create: `portal/internal/chat/intent_resolver_test.go`

- [ ] **Step 1: Write failing tests**

```go
package chat

import (
	"context"
	"testing"

	"backend/internal/biz"
)

func TestIntentResolver_RulesUniqueGitLab(t *testing.T) {
	r := IntentResolver{}
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := BoundFamiliesFrom(nil, servers, false, false)
	// inject rca into bound manually
	bound = append(bound, FamilyRCA)
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "帮我查一下 GitLab 上有哪些项目",
		BoundFamilies: bound,
		Servers:       servers,
	})
	if res.Source != "rules" || res.Confidence != "high" {
		t.Fatalf("got source=%s conf=%s reason=%s", res.Source, res.Confidence, res.Reason)
	}
	set := familySet(res.ActiveFamilies)
	if !set[FamilyCore] || !set["mcp:gitlab"] {
		t.Fatalf("active=%v", res.ActiveFamilies)
	}
	if set[FamilyRCA] {
		t.Fatal("rca must not be active for gitlab-only query")
	}
}

func TestIntentResolver_RulesMultiIntentNeedsClassifierOrUnionPath(t *testing.T) {
	// Without classifier: Resolve should attempt classifier; with nil classifier → fail_narrow candidates
	r := IntentResolver{Classifier: nil}
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := append(BoundFamiliesFrom(nil, servers, false, false), FamilyRCA)
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "GitLab 部署失败，看下 Jaeger trace",
		BoundFamilies: bound,
		Servers:       servers,
	})
	// multi high → classifier nil → fail_narrow with both candidates ∪ core
	if res.Source != "fail_narrow" && res.Source != "classifier" {
		t.Fatalf("source=%s", res.Source)
	}
	set := familySet(res.ActiveFamilies)
	if !set["mcp:gitlab"] || !set[FamilyRCA] {
		// fail_narrow keeps Candidates; both should be candidates from rules
		t.Fatalf("expected both families in active via candidates, got %v candidates=%v", res.ActiveFamilies, res.Candidates)
	}
}

func TestIntentResolver_NoHitFailNarrowCoreOnly(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyRCA, "mcp:gitlab"}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "你好",
		BoundFamilies: bound,
	})
	if res.Source != "fail_narrow" {
		t.Fatalf("source=%s", res.Source)
	}
	set := familySet(res.ActiveFamilies)
	if len(set) != 1 || !set[FamilyCore] {
		t.Fatalf("want only core, got %v", res.ActiveFamilies)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd portal && go test ./internal/chat/ -run TestIntentResolver_Rules -count=1
```

- [ ] **Step 3: Implement rules Resolve**

```go
package chat

import (
	"context"
	"strings"
)

type IntentResolveInput struct {
	UserText      string
	BoundFamilies []string
	Servers       []*biz.McpServerMeta // for id/name keyword boost
	Classifier    FamilyClassifier     // optional; also set on IntentResolver
}

type IntentResolveResult struct {
	ActiveFamilies []string
	Confidence     string // high | low
	Source         string // rules | classifier | fail_narrow
	Candidates     []string
	Reason         string
}

type IntentResolver struct {
	Classifier FamilyClassifier
}

func (r IntentResolver) Resolve(ctx context.Context, in IntentResolveInput) IntentResolveResult {
	bound := familySet(in.BoundFamilies)
	if len(bound) == 0 {
		bound[FamilyCore] = struct{}{}
	}
	scores := scoreFamilies(in.UserText, bound, in.Servers)
	var hits []string
	for id, sc := range scores {
		if sc > 0 {
			hits = append(hits, id)
		}
	}
	ensureCore := func(ids []string) []string {
		s := familySet(ids)
		s[FamilyCore] = struct{}{}
		out := make([]string, 0, len(s))
		for id := range s {
			if _, ok := bound[id]; ok || id == FamilyCore {
				out = append(out, id)
			}
		}
		return out
	}

	clf := r.Classifier
	if in.Classifier != nil {
		clf = in.Classifier
	}

	switch {
	case len(hits) == 1:
		return IntentResolveResult{
			ActiveFamilies: ensureCore(hits),
			Confidence:     "high",
			Source:         "rules",
			Candidates:     hits,
			Reason:         "unique_rule_hit",
		}
	case len(hits) == 0:
		return r.classifyOrNarrow(ctx, in.UserText, bound, nil, clf, "no_rule_hit")
	default:
		return r.classifyOrNarrow(ctx, in.UserText, bound, hits, clf, "multi_rule_hit")
	}
}

func (r IntentResolver) classifyOrNarrow(ctx context.Context, user string, bound map[string]struct{}, candidates []string, clf FamilyClassifier, reason string) IntentResolveResult {
	boundList := make([]string, 0, len(bound))
	for id := range bound {
		boundList = append(boundList, id)
	}
	if clf != nil {
		selected, conf, err := clf.Classify(ctx, user, boundList, candidates)
		if err == nil && conf == "high" && len(selected) > 0 {
			clean := filterBoundOnly(selected, bound)
			if len(clean) > 0 {
				return IntentResolveResult{
					ActiveFamilies: withCore(clean),
					Confidence:     "high",
					Source:         "classifier",
					Candidates:     candidates,
					Reason:         reason + ":classifier_ok",
				}
			}
		}
	}
	// Fail-narrow
	narrow := filterBoundOnly(candidates, bound)
	if len(narrow) == 0 {
		narrow = []string{FamilyCore}
	} else {
		narrow = withCore(narrow)
	}
	return IntentResolveResult{
		ActiveFamilies: narrow,
		Confidence:     "low",
		Source:         "fail_narrow",
		Candidates:     candidates,
		Reason:         reason + ":fail_narrow",
	}
}

func scoreFamilies(user string, bound map[string]struct{}, servers []*biz.McpServerMeta) map[string]int {
	toks := tokenizeForOverlap(user) // existing in turn_intent_gate.go
	scores := map[string]int{}
	lower := strings.ToLower(user)
	for fam, kws := range familyKeywords {
		if _, ok := bound[fam]; !ok {
			continue
		}
		for _, kw := range kws {
			kw = strings.ToLower(kw)
			if kw == "" {
				continue
			}
			if strings.Contains(lower, kw) {
				scores[fam]++
				continue
			}
			if _, ok := toks[kw]; ok {
				scores[fam]++
			}
		}
	}
	for _, s := range servers {
		if s == nil || s.ID == "" {
			continue
		}
		fid := MCPFamilyID(s.ID)
		if _, ok := bound[fid]; !ok {
			continue
		}
		for _, tip := range []string{s.ID, s.Name} {
			tip = strings.ToLower(strings.TrimSpace(tip))
			if tip == "" {
				continue
			}
			if strings.Contains(lower, tip) {
				scores[fid] += 2
			}
			if _, ok := toks[tip]; ok {
				scores[fid] += 2
			}
		}
	}
	// legacy mcp:legacy:<tool> — match tool id substring if present in bound
	for fid := range bound {
		if strings.HasPrefix(fid, "mcp:legacy:") {
			name := strings.TrimPrefix(fid, "mcp:legacy:")
			if name != "" && strings.Contains(lower, strings.ToLower(name)) {
				scores[fid]++
			}
		}
	}
	return scores
}

func withCore(ids []string) []string {
	s := familySet(ids)
	s[FamilyCore] = struct{}{}
	out := make([]string, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	return out
}

func filterBoundOnly(ids []string, bound map[string]struct{}) []string {
	var out []string
	for _, id := range ids {
		if _, ok := bound[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
```

Fix multi-intent test expectation: with nil classifier and multi hits, Fail-narrow keeps **Candidates ∪ core**, so both gitlab and rca stay — that matches “显式多意图” even without classifier. Good.

- [ ] **Step 4: Run — expect pass**

```bash
cd portal && go test ./internal/chat/ -run TestIntentResolver_ -count=1
```

- [ ] **Step 5: Commit if asked**

```bash
cd portal && git add internal/chat/intent_resolver.go internal/chat/intent_resolver_test.go && git commit -m "feat(chat): add rules-based IntentResolver for tool surface"
```

---

### Task 3: Model FamilyClassifier

**Files:**
- Create: `portal/internal/chat/intent_classifier.go`
- Create: `portal/internal/chat/intent_classifier_test.go`

- [ ] **Step 1: Write failing tests**

```go
package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/model"
)

type stubClassifyModel struct {
	text string
	err  error
	delay time.Duration
}

func (s stubClassifyModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &model.Generation{Text: s.text}, nil
}
func (s stubClassifyModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return s.Generate(ctx, "")
}

func TestParseClassifierJSON(t *testing.T) {
	fams, conf, err := parseClassifierJSON(`{"families":["mcp:gitlab","rca"],"confidence":"high"}`)
	if err != nil || conf != "high" || len(fams) != 2 {
		t.Fatalf("%v %v %v", fams, conf, err)
	}
}

func TestModelFamilyClassifier_High(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{text: `{"families":["mcp:gitlab"],"confidence":"high"}`}, Timeout: time.Second}
	sel, conf, err := c.Classify(context.Background(), "gitlab projects", []string{FamilyCore, "mcp:gitlab", FamilyRCA}, nil)
	if err != nil || conf != "high" || len(sel) != 1 || sel[0] != "mcp:gitlab" {
		t.Fatalf("%v %s %v", sel, conf, err)
	}
}

func TestModelFamilyClassifier_BadJSON(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{text: "not-json"}, Timeout: time.Second}
	_, _, err := c.Classify(context.Background(), "x", []string{FamilyCore}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestModelFamilyClassifier_Timeout(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{delay: 200 * time.Millisecond}, Timeout: 20 * time.Millisecond}
	_, _, err := c.Classify(context.Background(), "x", []string{FamilyCore}, nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestIntentResolver_ClassifierErrorFailNarrow(t *testing.T) {
	r := IntentResolver{Classifier: ModelFamilyClassifier{Model: stubClassifyModel{err: errors.New("boom")}, Timeout: time.Second}}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "你好",
		BoundFamilies: []string{FamilyCore, FamilyRCA},
	})
	if res.Source != "fail_narrow" || familySet(res.ActiveFamilies)[FamilyRCA] {
		t.Fatalf("%+v", res)
	}
	_ = strings.Contains
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd portal && go test ./internal/chat/ -run "TestParseClassifier|TestModelFamilyClassifier|TestIntentResolver_ClassifierError" -count=1
```

- [ ] **Step 3: Implement classifier**

```go
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

type FamilyClassifier interface {
	Classify(ctx context.Context, userText string, bound, candidates []string) (selected []string, confidence string, err error)
}

type ModelFamilyClassifier struct {
	Model   model.Model
	Timeout time.Duration // default 3s
}

func (c ModelFamilyClassifier) Classify(ctx context.Context, userText string, bound, candidates []string) ([]string, string, error) {
	if c.Model == nil {
		return nil, "", fmt.Errorf("nil model")
	}
	to := c.Timeout
	if to <= 0 {
		to = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	prompt := buildFamilyClassifyPrompt(userText, bound, candidates)
	gen, err := c.Model.Generate(ctx, prompt, model.WithTemperature(0), model.WithMaxTokens(256))
	if err != nil {
		return nil, "", err
	}
	return parseClassifierJSON(gen.Text)
}

func buildFamilyClassifyPrompt(user string, bound, candidates []string) string {
	return fmt.Sprintf(`You classify which tool families are needed for this user message.
Reply with ONLY JSON: {"families":["..."],"confidence":"high"|"low"}
Rules:
- families must be a subset of bound: %s
- prefer candidates when relevant: %s
- include multiple families only for explicit multi-intent
- confidence=high only when sure

User message:
%s`, strings.Join(bound, ", "), strings.Join(candidates, ", "), user)
}

func parseClassifierJSON(text string) ([]string, string, error) {
	text = strings.TrimSpace(text)
	// strip optional ```json fences
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j > i {
			text = text[i : j+1]
		}
	}
	var raw struct {
		Families   []string `json:"families"`
		Confidence string   `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, "", err
	}
	conf := strings.ToLower(strings.TrimSpace(raw.Confidence))
	if conf != "high" && conf != "low" {
		conf = "low"
	}
	return raw.Families, conf, nil
}
```

Verify `model.WithTemperature` / `WithMaxTokens` exist (they do in framework). If `Generate` options differ, use `Chat` with one user message.

- [ ] **Step 4: Run — expect pass**

```bash
cd portal && go test ./internal/chat/ -run "TestParseClassifier|TestModelFamilyClassifier|TestIntentResolver_ClassifierError" -count=1
```

- [ ] **Step 5: Commit if asked**

---

### Task 4: BuildRegistry ActiveFamilies filter

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`
- Modify or create: `portal/internal/chat/registry_surface_test.go`

- [ ] **Step 1: Write failing test**

```go
package chat

import (
	"testing"

	"backend/internal/biz"
	"github.com/sixath/framework/tool"
)

func TestBuildRegistry_FiltersInactiveMCPAndRCA(t *testing.T) {
	reg := tool.NewRegistry()
	tools := []*biz.ToolMeta{{
		Name: "rca-j", Type: biz.ToolTypeRCA,
		Config: map[string]any{"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j:16686"}},
	}}
	// Use HTTP MCP that won't dial if we skip registration entirely — inactive server must not RegisterMcpTool.
	servers := []*biz.McpServerMeta{
		{ID: "gitlab", Name: "GitLab", Transport: "http", Endpoint: "http://127.0.0.1:9", Backend: "mark3labs"},
		{ID: "other", Name: "Other", Transport: "http", Endpoint: "http://127.0.0.1:9", Backend: "mark3labs"},
	}
	active := familySet([]string{FamilyCore, "mcp:gitlab"})
	_, err := BuildRegistry(tools, servers, reg, RegistryBuildOptions{ActiveFamilies: active})
	if err != nil {
		// Inactive MCP skipped; active gitlab may fail dial — if HasMcpServer required, mock or accept error only for gitlab.
		// Prefer: filter before Register so inactive never called; for active, use a server id that we can skip network:
		// Implementation should skip Register when inactive; for test use only filtering assertion via custom approach:
	}
	_ = err
	if _, ok := reg.Get("jaeger_trace"); ok {
		t.Fatal("jaeger_trace must not register when rca inactive")
	}
}
```

Refine test strategy in implementation: **unit-test the filter helpers** `filterToolsForSurface` / `filterServersForSurface` without live MCP:

```go
func TestFilterServersForSurface(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab"}, {ID: "confluence"}}
	got := filterServersForSurface(servers, familySet([]string{FamilyCore, "mcp:gitlab"}))
	if len(got) != 1 || got[0].ID != "gitlab" {
		t.Fatalf("%v", got)
	}
}

func TestFilterToolsForSurface_DropsRCA(t *testing.T) {
	tools := []*biz.ToolMeta{
		{Name: "rca-j", Type: biz.ToolTypeRCA, Config: map[string]any{"rca": map[string]any{"func_path": "jaeger_trace"}}},
		{Name: "ssh", Type: biz.ToolTypeBuiltin, Config: map[string]any{"func_path": "ssh_exec"}},
	}
	got := filterToolsForSurface(tools, familySet([]string{FamilyCore}))
	if len(got) != 1 || got[0].Name != "ssh" {
		t.Fatalf("%v", got)
	}
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd portal && go test ./internal/chat/ -run "TestFilterServersForSurface|TestFilterToolsForSurface" -count=1
```

- [ ] **Step 3: Implement filter + wire BuildRegistry**

```go
type RegistryBuildOptions struct {
	// ActiveFamilies nil => no filtering (legacy full bind).
	ActiveFamilies map[string]struct{}
}

func BuildRegistry(tools []*biz.ToolMeta, servers []*biz.McpServerMeta, reg *tool.Registry, opts ...RegistryBuildOptions) (*RegistryBuildResult, error) {
	var o RegistryBuildOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	tools = filterToolsForSurface(tools, o.ActiveFamilies)
	servers = filterServersForSurface(servers, o.ActiveFamilies)
	// ... existing body unchanged ...
}

func filterServersForSurface(servers []*biz.McpServerMeta, active map[string]struct{}) []*biz.McpServerMeta {
	if active == nil {
		return servers
	}
	var out []*biz.McpServerMeta
	for _, s := range servers {
		if s == nil {
			continue
		}
		if FamilyActive(active, MCPFamilyID(s.ID)) {
			out = append(out, s)
		}
	}
	return out
}

func filterToolsForSurface(tools []*biz.ToolMeta, active map[string]struct{}) []*biz.ToolMeta {
	if active == nil {
		return tools
	}
	var out []*biz.ToolMeta
	for _, t := range tools {
		if t == nil {
			continue
		}
		switch t.Type {
		case biz.ToolTypeRCA:
			if FamilyActive(active, FamilyRCA) {
				out = append(out, t)
			}
		case biz.ToolTypeMCP:
			mc := tool.McpConfigFromMap(toolConfigToMap(t.Config))
			fid := LegacyMCPFamilyID(t.Name)
			if mc != nil && mc.Id != "" {
				fid = MCPFamilyID(mc.Id)
			}
			if FamilyActive(active, fid) {
				out = append(out, t)
			}
		default:
			// datasource + builtin → core lane
			if FamilyActive(active, FamilyCore) {
				out = append(out, t)
			}
		}
	}
	return out
}
```

Update all `BuildRegistry(...)` call sites in portal to still compile (variadic opts → existing calls OK).

- [ ] **Step 4: Run full chat package subset**

```bash
cd portal && go test ./internal/chat/ -run "TestFilter|TestBuildRegistry" -count=1
```

- [ ] **Step 5: Commit if asked**

---

### Task 5: Runtime tools — skip web/knowledge when inactive

**Files:**
- Modify: `portal/internal/chat/runtime_tools.go`
- Create: `portal/internal/chat/runtime_tools_surface_test.go`

- [ ] **Step 1: Failing test**

```go
func TestRegisterAgentRuntimeTools_SkipsWebWhenInactive(t *testing.T) {
	reg := tool.NewRegistry()
	flags := HermesP0ToolFlags{WebToolsEnabled: true, TodoEnabled: true}
	err := RegisterAgentRuntimeTools(reg, AgentRuntimeToolsOptions{
		Flags:          &flags,
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}), // no web
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("web_search"); ok {
		t.Fatal("web_search should be skipped")
	}
	if _, ok := reg.Get("todo"); !ok {
		// todo may need TodoEnabled path — if todo tool name differs, assert Names() contains core tool
	}
}
```

- [ ] **Step 2: Implement**

Add to `AgentRuntimeToolsOptions`:

```go
ActiveFamilies map[string]struct{} // nil = no surface filter
```

In `RegisterAgentRuntimeTools`:

```go
	if flags.WebToolsEnabled && FamilyActive(opts.ActiveFamilies, FamilyWeb) {
		if err := registerWebTools(reg, true); err != nil {
			return err
		}
	}
	// knowledge:
	if FamilyActive(opts.ActiveFamilies, FamilyKnowledge) {
		if err := RegisterKnowledgeHubTools(reg, opts.RuntimeTools); err != nil {
			return err
		}
	}
```

Move existing unconditional `RegisterKnowledgeHubTools` behind the family check. Other runtime tools stay (core).

- [ ] **Step 3: Run tests**

```bash
cd portal && go test ./internal/chat/ -run TestRegisterAgentRuntimeTools_SkipsWeb -count=1
```

- [ ] **Step 4: Commit if asked**

---

### Task 6: TurnIntentGate family filter

**Files:**
- Modify: `portal/internal/chat/turn_intent_gate.go`
- Modify: `portal/internal/chat/turn_intent_gate_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestTurnIntentGate_FamilyNotActive(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, "mcp:gitlab"}),
		ToolFamily:     map[string]string{"jaeger_trace": FamilyRCA, "list_projects": "mcp:gitlab"},
	}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "查 GitLab 项目"}}},
		AssistantText: "查 trace",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "jaeger_trace", Arguments: map[string]any{"service": "gitlab"},
		}}},
	})
	if res.Decision != agent.PostModelFinish || res.Reason != "family_not_active" {
		t.Fatalf("%v %q", res.Decision, res.Reason)
	}
}

func TestTurnIntentGate_FamilyActiveContinuesThenTopicRules(t *testing.T) {
	gate := TurnIntentGate{
		ActiveFamilies: familySet([]string{FamilyCore, FamilyWeb}),
		ToolFamily:     map[string]string{"web_search": FamilyWeb},
	}
	// on-topic web should continue (existing behavior)
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req: &agent.Request{Messages: []model.Message{{Role: "user", Content: "查一下七日无理由退货的法律规定"}}},
		AssistantText: "检索",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{{
			ID: "1", Name: "web_search", Arguments: map[string]any{"query": "七日无理由退货 法律规定"},
		}}},
	})
	if res.Decision != agent.PostModelContinue {
		t.Fatalf("%v %q", res.Decision, res.Reason)
	}
}
```

- [ ] **Step 2: Implement gate extension**

Change `TurnIntentGate` to:

```go
type TurnIntentGate struct {
	ActiveFamilies map[string]struct{} // nil => skip family filter
	ToolFamily     map[string]string   // tool name → family id
}

func NewTurnIntentGate() agent.PostModelPolicy {
	// env off → nil (unchanged)
	...
	return TurnIntentGate{}
}

func NewTurnIntentGateWithSurface(active map[string]struct{}, toolFamily map[string]string) agent.PostModelPolicy {
	if NewTurnIntentGate() == nil {
		return nil
	}
	return TurnIntentGate{ActiveFamilies: active, ToolFamily: toolFamily}
}
```

In `Evaluate`, after empty-calls check, **before** final-answer check or after — order per spec: family filter first among tool filters; keep final-answer as today (either order OK if documented). Spec: 先族，再 overlap. Final-answer can stay first (discard all tools) — keep final-answer first, then family filter, then drift overlap:

```go
	if looksLikeFinalAnswer(in.AssistantText) { ... }

	calls = filterCallsByFamily(calls, g.ActiveFamilies, g.ToolFamily)
	if len(calls) == 0 {
		return PostModelFinish, Reason: "family_not_active"
	}
	// rebuild ToolStep conceptually: continue with filtered calls for drift logic
	// ... existing drift filter on `calls` ...
```

Helper:

```go
func filterCallsByFamily(calls []model.ToolCall, active map[string]struct{}, toolFamily map[string]string) ([]model.ToolCall, int) {
	if active == nil {
		return calls, 0
	}
	var kept []model.ToolCall
	dropped := 0
	for _, c := range calls {
		fam := FamilyCore
		if toolFamily != nil {
			if f, ok := toolFamily[c.Name]; ok {
				fam = f
			}
		}
		if FamilyActive(active, fam) {
			kept = append(kept, c)
		} else {
			dropped++
		}
	}
	return kept, dropped
}
```

Unknown tool names default to `core` (allowed) to avoid breaking new core tools; MCP/RCA must be in `ToolFamily` map built from `reg.List()`.

- [ ] **Step 3: `BuildToolFamilyIndex`**

```go
func BuildToolFamilyIndex(reg *tool.Registry) map[string]string {
	out := map[string]string{}
	if reg == nil {
		return out
	}
	for _, tl := range reg.List() {
		out[tl.Name] = FamilyForRegisteredTool(tl)
	}
	return out
}
```

- [ ] **Step 4: Run gate tests**

```bash
cd portal && go test ./internal/chat/ -run TestTurnIntentGate_ -count=1
```

- [ ] **Step 5: Commit if asked**

---

### Task 7: BuildReActAgent accepts surface gate override

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`

- [ ] **Step 1: Change default gate injection**

Today:

```go
	if gate := NewTurnIntentGate(); gate != nil {
		opts = append(opts, agent.WithReActPostModelPolicy(gate))
	}
	opts = append(opts, extra...)
```

Problem: `extra` applied after can override PostModelPolicy — good. Chat service should pass `WithReActPostModelPolicy(NewTurnIntentGateWithSurface(...))` in extra to replace empty gate.

Better: **remove** automatic `NewTurnIntentGate()` from `BuildReActAgent` and always pass from chat service — riskier for other callers.

Safer approach: keep default empty `TurnIntentGate{}`; chat **overrides** via extra:

```go
agent.WithReActPostModelPolicy(NewTurnIntentGateWithSurface(active, index))
```

Because `extra` is appended last, it overrides. Confirm `NewReActAgent` / options application: later options win — verify in `WithReActPostModelPolicy` (last write wins). If first wins, change BuildReActAgent to skip default when extra provides policy, or stop injecting default and inject only in chat.

**Required check during implement:** read how `ReActOption` merges. If last wins, chat override is enough. If not, change to:

```go
func BuildReActAgent(..., extra ...agent.ReActOption) {
	...
	// do not auto-inject gate here
	opts = append(opts, extra...)
	// if no PostModelPolicy in config after options, inject NewTurnIntentGate()
}
```

Simplest reliable pattern used in plan:

```go
	injectedGate := false
	for _, opt := range extra { /* can't inspect easily */ }

	// CHANGE: BuildReActAgent does NOT inject TurnIntentGate.
	// Add EnsureTurnIntentGate(extra) helper used by chat, OR inject only if env on:
```

**Final decision for this plan:** Remove auto-inject from `BuildReActAgent`. Add:

```go
func TurnIntentGateOption(active map[string]struct{}, toolFamily map[string]string) agent.ReActOption {
	gate := NewTurnIntentGateWithSurface(active, toolFamily)
	if gate == nil {
		return func(*agent.ReActConfig) {}
	}
	return agent.WithReActPostModelPolicy(gate)
}
```

Update `TestBuildReActAgent_*` if they assumed gate present — gate still injectable in tests.

Grep callers of `BuildReActAgent` and add `TurnIntentGateOption(nil, nil)` for v0 behavior when surface off (`active=nil` means family filter off, v0 rules still on).

- [ ] **Step 2: Run react opts tests**

```bash
cd portal && go test ./internal/chat/ -run TestBuildReActAgent -count=1
```

- [ ] **Step 3: Commit if asked**

---

### Task 8: Wire ChatService SendMessage + SendMessageStream

**Files:**
- Modify: `portal/internal/service/chat.go` (both sync and stream paths around BuildRegistry)

- [ ] **Step 1: Extract helper in chat package**

```go
// PrepareTurnToolSurface resolves intent and returns registry filter + gate maps.
func PrepareTurnToolSurface(ctx context.Context, userText string, tools []*biz.ToolMeta, servers []*biz.McpServerMeta, agentMeta *biz.AgentMeta, m model.Model) (active map[string]struct{}, result IntentResolveResult) {
	if !ToolSurfaceEnabled() {
		return nil, IntentResolveResult{Source: "disabled", Reason: "SATH_TURN_TOOL_SURFACE off"}
	}
	flags := RuntimeToolsForAgent(agentMeta)
	knowledgeOn := true // if hub knowledge registration depends on config, mirror RegisterKnowledgeHubTools precondition
	if agentMeta != nil && agentMeta.RuntimeTools.HubKnowledge == nil && /* existing skip conditions */ false {
		knowledgeOn = false
	}
	// Simpler: knowledgeOn = true when RegisterKnowledgeHubTools would register; read hub_knowledge_tools.go and mirror.
	bound := BoundFamiliesFrom(tools, servers, flags.WebToolsEnabled, knowledgeOn)
	resolver := IntentResolver{Classifier: ModelFamilyClassifier{Model: m, Timeout: 3 * time.Second}}
	res := resolver.Resolve(ctx, IntentResolveInput{UserText: userText, BoundFamilies: bound, Servers: servers})
	return familySet(res.ActiveFamilies), res
}
```

- [ ] **Step 2: In `SendMessageStream` (and sync `SendMessage`), after `BuildModel`, before `BuildRegistry`:**

```go
	userForIntent := content
	if ir != nil {
		userForIntent = chat.UserMessageContentForTurn(content, ir) // or content only if simpler for phase-1
	}
	active, surfaceRes := chat.PrepareTurnToolSurface(ctx, userForIntent, tools, mcpServerMetas, agentMeta, m)
	s.log.Infof("turn tool surface: session_id=%s source=%s conf=%s active=%v candidates=%v reason=%s",
		sessionID, surfaceRes.Source, surfaceRes.Confidence, surfaceRes.ActiveFamilies, surfaceRes.Candidates, surfaceRes.Reason)

	regResult, err := chat.BuildRegistry(tools, mcpServerMetas, reg, chat.RegistryBuildOptions{ActiveFamilies: active})
	...
	if err := chat.RegisterAgentRuntimeTools(reg, chat.AgentRuntimeToolsOptions{
		...
		ActiveFamilies: active,
	}); err != nil { ... }

	toolFamily := chat.BuildToolFamilyIndex(reg)
	a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory,
		append(chat.ReActOptionsFromAgent(*agentMeta),
			append(s.growthReActOptions(agentMeta.Workspace),
				chat.TurnIntentGateOption(active, toolFamily),
				agent.WithReActEventBus(turnBus),
			)...)...)
```

Apply the same pattern to the non-stream path (~line 364).

- [ ] **Step 3: Compile**

```bash
cd portal && go build -o NUL ./cmd/backend/
```

- [ ] **Step 4: Unit test helper with fake model (optional file `turn_surface_wire_test.go`)**

Assert `PrepareTurnToolSurface` for「查 GitLab」+ bound gitlab/rca → active has mcp:gitlab, no rca.

- [ ] **Step 5: Commit if asked**

---

### Task 9: Golden-path package tests + docs touch

**Files:**
- Create: `portal/internal/chat/turn_surface_golden_test.go`
- Modify: `portal/docs/mcp-stdio-server.md` **or** add `portal/docs/turn-tool-surface.md` (short)

- [ ] **Step 1: Golden tests**

```go
func TestGolden_GitLabQuery_NoJaegerInRegistry(t *testing.T) {
	// BoundFamilies + Resolve + filterTools/Servers + pretend runtime skip
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	tools := []*biz.ToolMeta{{Name: "rca-j", Type: biz.ToolTypeRCA, Config: map[string]any{"rca": map[string]any{"func_path": "jaeger_trace", "query_url": "http://j"}}}}
	bound := append(BoundFamiliesFrom(tools, servers, false, false))
	res := IntentResolver{}.Resolve(context.Background(), IntentResolveInput{
		UserText: "查 GitLab 项目列表", BoundFamilies: bound, Servers: servers,
	})
	active := familySet(res.ActiveFamilies)
	ft := filterToolsForSurface(tools, active)
	for _, tmeta := range ft {
		if tmeta.Type == biz.ToolTypeRCA {
			t.Fatal("RCA tool must be filtered out")
		}
	}
	fs := filterServersForSurface(servers, active)
	if len(fs) != 1 || fs[0].ID != "gitlab" {
		t.Fatalf("%v", fs)
	}
}

func TestGolden_MultiIntentKeepsGitLabAndRCA(t *testing.T) {
	servers := []*biz.McpServerMeta{{ID: "gitlab", Name: "GitLab"}}
	bound := append(BoundFamiliesFrom(nil, servers, false, false), FamilyRCA)
	res := IntentResolver{}.Resolve(context.Background(), IntentResolveInput{
		UserText: "GitLab 部署失败，看下 Jaeger", BoundFamilies: bound, Servers: servers,
	})
	set := familySet(res.ActiveFamilies)
	if !set["mcp:gitlab"] || !set[FamilyRCA] {
		t.Fatalf("%+v", res)
	}
}

func TestGolden_SurfaceOff_NoFilter(t *testing.T) {
	t.Setenv("SATH_TURN_TOOL_SURFACE", "0")
	if ToolSurfaceEnabled() {
		t.Fatal("off")
	}
}
```

- [ ] **Step 2: Run**

```bash
cd portal && go test ./internal/chat/ -run TestGolden_ -count=1
```

- [ ] **Step 3: Doc snippet** (`portal/docs/turn-tool-surface.md`)

```markdown
# Turn Tool Surface

每轮按意图收窄 MCP/RCA/web/knowledge 工具面；`TurnIntentGate` 丢弃跨族调用。

- 关闭：`SATH_TURN_TOOL_SURFACE=0`
- 关闭门控：`SATH_TURN_INTENT_GATE=0`（仅装配收窄、无 PostModel 兜底）
- 规格：sixath `docs/superpowers/specs/2026-08-09-turn-tool-surface-design.md`
```

- [ ] **Step 4: Full chat tests**

```bash
cd portal && go test ./internal/chat/ -count=1
```

Expected: PASS (fix any breakage from BuildRegistry signature / BuildReActAgent gate).

---

## Spec coverage self-check

| Spec item | Task |
|-----------|------|
| 装配收窄 Active ∪ core | T4, T5, T8 |
| TurnIntentGate 族过滤兜底 | T6, T7, T8 |
| 规则优先 + 分类 + Fail-narrow | T2, T3 |
| 族 = MCP 自动 + 内置标签 | T1 |
| 分类失败不 Fail-open | T2, T3 |
| 开关 SATH_TURN_TOOL_SURFACE | T1, T8, T9 |
| GitLab 金路径 / 多意图 / 仅 core | T9 |
| 不改 PostModelPolicy 接口 | 全程 |
| 鉴权 fail-closed / 同轮扩族 | 明确不做 |

## Placeholder scan

无 TBD；分类超时默认 **3s**；分类复用 **本轮 Agent `model.Model`**。

## Type consistency

- `ActiveFamilies` 在过滤层用 `map[string]struct{}`；`IntentResolveResult.ActiveFamilies` 为 `[]string`；交接用 `familySet`。
- `RegistryBuildOptions.ActiveFamilies` / `AgentRuntimeToolsOptions.ActiveFamilies` / `TurnIntentGate.ActiveFamilies` 同形。
- `FamilyClassifier.Classify` → `(selected []string, confidence string, err error)`。

## Known gap (一期接受)

`load_skill` 动态注册 MCP 可能把未激活族工具加回 registry；Gate 若 `ToolFamily` 在注册后未刷新，可能漏拦或误标 `core`。一期不重算中途 registry；二期可在 skill 注册后刷新 index 或禁止 skill 拉未激活 server。
