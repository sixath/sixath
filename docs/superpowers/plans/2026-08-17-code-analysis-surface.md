# Code Analysis Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把跨仓代码检索从 RCA 排障工具面拆成独立的 `code` 族，使「根据代码分析 / 梳理流程」类问题能看见并优先使用 `rca_grep` 等工具，同时 RCA（trace/日志）单向并上 code 族。

**Architecture:** Portal `tool_families` 增加 `FamilyCode`；`BoundFamiliesFrom` / `filterToolsForSurface` 按 `rca.func_path` 分流；`IntentResolver` 增加代码关键词，并在 Active 含 `rca` 且 Bound 含 `code` 时并族。`code` 族激活时 `AppendCodeAnalysisPrompt`。framework 只改三个代码工具的 Description，工具名不变。

**Tech Stack:** Go（`portal/internal/chat`、`framework/tool/rca_code_tools.go`）。规格：[`2026-08-17-code-analysis-surface-design.md`](../specs/2026-08-17-code-analysis-surface-design.md)。

**Repos:** portal 为 nested git；framework 在 monorepo `framework/`。**Do not commit unless asked.**

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/chat/tool_families.go` | `FamilyCode`、工具→族、关键词、`rcaFuncPath` 分流、BoundFamilies |
| `portal/internal/chat/tool_families_test.go` | 族映射与 BoundFamilies |
| `portal/internal/chat/intent_resolver.go` | RCA→Code 单向并族 |
| `portal/internal/chat/intent_resolver_test.go` | 代码分析题 / Jaeger 并族 |
| `portal/internal/chat/agent_builder.go` | `filterToolsForSurface` 按 func_path 过滤 |
| `portal/internal/chat/agent_builder_test.go` 或新建 | code vs rca 过滤 |
| `portal/internal/chat/code_analysis_prompt.go` | `AppendCodeAnalysisPrompt` |
| `portal/internal/chat/code_analysis_prompt_test.go` | 泛化、无业务名 |
| `portal/internal/service/chat.go` | 装配后若 code 激活则追加提示（SendMessage + Stream 两处） |
| `portal/docs/turn-tool-surface.md` | 文档：code 族 |
| `framework/tool/rca_code_tools.go` | Description 去 RCA 品牌 |
| `framework/tool/rca_code_tools_test.go` | Description 字符串 |
| `framework/tool/file_tools.go` | workspace 工具 hint 补一句 |

---

### Task 1: FamilyCode catalog

**Files:**
- Modify: `portal/internal/chat/tool_families.go`
- Modify: `portal/internal/chat/tool_families_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestFamilyForBuiltinToolName_CodeVsRCA(t *testing.T) {
	if FamilyForBuiltinToolName("rca_grep") != FamilyCode {
		t.Fatal("rca_grep → code")
	}
	if FamilyForBuiltinToolName("jaeger_trace") != FamilyRCA {
		t.Fatal("jaeger → rca")
	}
}

func TestBoundFamiliesFrom_SplitsRCACodeAndLogs(t *testing.T) {
	codeTool := &biz.ToolMeta{Type: biz.ToolTypeRCA, Config: mustStruct(map[string]any{
		"rca": map[string]any{"func_path": "rca_code", "roots": []any{"D:\\workspace\\migu"}},
	})}
	esTool := &biz.ToolMeta{Type: biz.ToolTypeRCA, Config: mustStruct(map[string]any{
		"rca": map[string]any{"func_path": "es_log_query", "endpoint": "http://es"},
	})}
	bound := BoundFamiliesFrom([]*biz.ToolMeta{codeTool, esTool}, nil, false, false)
	set := familySet(bound)
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("want code, got %v", bound)
	}
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("want rca, got %v", bound)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL** (`FamilyCode` undefined or still `FamilyRCA`)

Run: `go test ./internal/chat -count=1 -run "TestFamilyForBuiltinToolName_CodeVsRCA|TestBoundFamiliesFrom_SplitsRCACodeAndLogs"`（在 `portal/`）

- [ ] **Step 3: Implement**

- 增加 `FamilyCode = "code"`
- `builtinToolFamily`: `rca_grep/glob/read/symbol` → `FamilyCode`；`jaeger_trace` / `es_log_query` 仍 `FamilyRCA`
- `familyKeywords[FamilyCode]`: `源码`, `代码分析`, `代码`, `调用链`, `模块关系`, `流程梳理`, `谁调用`, `仓库`, `grep`, `go.mod`
- `FamilyRCA` 关键词保持现网；不要把「流程梳理」放进 RCA
- `BoundFamiliesFrom`: `ToolTypeRCA` 时读 `config.rca.func_path`：`rca_code`/`rca_symbol` → `FamilyCode`；`jaeger_trace`/`es_log_query` → `FamilyRCA`；未知 path 保持计入 `FamilyRCA`（兼容）
- 抽小函数 `familyForRCATool(t *biz.ToolMeta) string`，避免复制 structpb 解析

- [ ] **Step 4: Tests PASS**

- [ ] **Step 5: Do not commit unless asked**

---

### Task 2: Intent — code hit + RCA unions code

**Files:**
- Modify: `portal/internal/chat/intent_resolver.go`
- Modify: `portal/internal/chat/intent_resolver_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestIntentResolver_CodeAnalysisActivatesCodeNotRCA(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyRCA, "mcp:gitlab"}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "根据代码分析 存档迁移整体流程梳理",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("code must be active, got %v source=%s", res.ActiveFamilies, res.Source)
	}
	if _, ok := set[FamilyRCA]; ok {
		t.Fatalf("rca must not activate for code-flow question, got %v", res.ActiveFamilies)
	}
	if _, ok := set["mcp:gitlab"]; ok {
		t.Fatal("gitlab must not activate")
	}
}

func TestIntentResolver_JaegerUnionsCodeWhenBound(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyCode, FamilyRCA}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "看下这条 Jaeger trace",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyRCA]; !ok {
		t.Fatalf("rca missing: %v", res.ActiveFamilies)
	}
	if _, ok := set[FamilyCode]; !ok {
		t.Fatalf("code should be unioned: %v", res.ActiveFamilies)
	}
}

func TestIntentResolver_JaegerDoesNotInventCode(t *testing.T) {
	r := IntentResolver{}
	bound := []string{FamilyCore, FamilyRCA}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "看下这条 Jaeger trace",
		BoundFamilies: bound,
	})
	set := familySet(res.ActiveFamilies)
	if _, ok := set[FamilyCode]; ok {
		t.Fatal("must not invent code family")
	}
}
```

保留现有 GitLab-only 不激活 RCA 的测试；把 `bound` 补上 `FamilyCode` 后仍不应激活 code。

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement union in `Resolve` 返回前**

```go
func unionCodeWhenRCA(active, bound []string) []string {
	b, a := familySet(bound), familySet(active)
	if _, rca := a[FamilyRCA]; rca {
		if _, has := b[FamilyCode]; has {
			a[FamilyCode] = struct{}{}
		}
	}
	out := make([]string, 0, len(a))
	for id := range a {
		out = append(out, id)
	}
	return out
}
```

对 `unique_rule_hit` / `classifier_ok` / `fail_narrow` 三条返回路径都并族（fail_narrow 若 candidates 含 rca 同样并）。

- [ ] **Step 4: Tests PASS**（含旧 GitLab 测）

- [ ] **Step 5: Do not commit unless asked**

---

### Task 3: filterToolsForSurface 按 func_path

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`
- Test: `portal/internal/chat/agent_builder_test.go`（或 `tool_families_test.go` 同包）

- [ ] **Step 1: Failing test** — code 激活时 `rca_code` 工具留下、`es_log_query` 丢掉；rca 激活时相反（es 留下；rca_code 仅当 code 也激活才留下）。

- [ ] **Step 2: Run FAIL**

- [ ] **Step 3: 改 `filterToolsForSurface`**

`ToolTypeRCA` 分支改为 `FamilyActive(active, familyForRCATool(t))`，不要一律 `FamilyRCA`。

- [ ] **Step 4: PASS**

---

### Task 4: Code analysis system hint

**Files:**
- Create: `portal/internal/chat/code_analysis_prompt.go`
- Create: `portal/internal/chat/code_analysis_prompt_test.go`
- Modify: `portal/internal/service/chat.go`（`AppendTurnIntentPrompt` 附近两处：普通 Send 与 Stream）

- [ ] **Step 1: Test prompt 含 `rca_grep`、`code roots`、`入边`；不含 `存档迁移`、`咪咕`、`union-archiver`**

- [ ] **Step 2: Implement `AppendCodeAnalysisPrompt`；仅当 `FamilyActive(active, FamilyCode)` 时调用**（`active==nil` 表示 surface 关闭：也追加，因为全量工具里已有 rca_grep，需要同一纪律）

关闭 surface（`active==nil`）时追加提示是对的：工具都在，更需要告诉模型别读 txt。

- [ ] **Step 3: `chat.go` 两处 `effectivePrompt = AppendTurnIntentPrompt(...)` 之后接 `AppendCodeAnalysisPromptIf(active, effectivePrompt)`**

- [ ] **Step 4: Tests PASS**

---

### Task 5: Tool descriptions

**Files:**
- Modify: `framework/tool/rca_code_tools.go`
- Modify: `framework/tool/rca_code_tools_test.go`（若无 Description 断言则加）
- Modify: `framework/tool/file_tools.go`（`workspaceFileScopeHint`）

- [ ] **Step 1: 断言 `rca_grep` Description 包含 `code roots` 或 `configured code`；`search_files` hint 提到优先 `rca_*` 做源码分析**

- [ ] **Step 2: 改文案。Name 与 Execute 不变。**

- [ ] **Step 3: `cd framework && go test ./tool -count=1 -run RCA`

---

### Task 6: Docs + 回归

**Files:**
- Modify: `portal/docs/turn-tool-surface.md`
- 跑：`cd portal && go test ./internal/chat ./internal/service -count=1`（service 若过重可只 chat）
- 跑：`cd framework && go test ./tool ./templates -count=1 -run "RCA|Family|Code"`

文档补三行：`code` 族、与 `rca` 的单向并族、`SATH_TURN_TOOL_SURFACE=0` 时提示仍追加。

- [ ] **Do not commit unless asked**

---

## 手工验收（portal 起来后）

1. migu-agent 发「根据代码分析 存档迁移整体流程梳理」
2. 看 turn 日志 `ActiveFamilies` 含 `code`
3. timeline 应出现 `rca_grep` / `rca_glob`，而不是只读根目录 `*.txt`
4. 负例：「帮我查 GitLab 上有哪些项目」families 不含 code

---

## 明确不做（防范围膨胀）

- 不改 Agent 系统提示里的咪咕业务段落（那是租户配置，不是平台）
- 不新增 `code_refs` 工具
- 不自动 `load_skill(rca-sync-archive-migrate)`
- 不 force-push / 不擅自 commit
