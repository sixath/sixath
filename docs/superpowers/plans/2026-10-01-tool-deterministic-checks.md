# 工具确定性校验（阶段 2）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在工具注册层加一层声明式校验中间件，执行前拦下"参数填错"（附候选），执行后对零结果做有预算的探测并标记 `suspect`，harness 在模型依据可疑空结果下"查不到"结论时提醒一次。

**Architecture:** `framework/tool` 新增 `suggest.go`（统一模糊候选）、`argcheck.go`（四类规则）、`middleware.go`（别名→schema→规则→执行→探测）、`empty_probe.go`、`es_field_cache.go`；`Registry.Register` 把中间件放在事件/trace 之内、超时之内，并用未导出 `checked` 标记防重复包装。各工具在注册时声明 `ArgAliases`/`ArgChecks`/`EmptyProbe`。harness 新增内置 `suspect_evidence` stop hook（独立预算 1 次）。

**Tech Stack:** Go 1.2x（`framework` 模块 `github.com/sixath/framework`，`evals/runner` 独立模块，`portal` Kratos）。

**Spec:** [`docs/superpowers/specs/2026-10-01-tool-deterministic-checks-design.md`](../specs/2026-10-01-tool-deterministic-checks-design.md)

---

## 通用约定（每个任务都适用）

- 机器内存小：测试一律 `-p 1`，PowerShell 先 `$env:GOGC=50`。
- framework 测试在 `framework/` 目录执行：`go test -p 1 ./tool/ -run <Name> -count=1`。
- evals 测试在 `evals/runner/` 执行；portal 测试在 `portal/` 执行。
- PowerShell 5 下 `go` 的 stderr 会显示为 `NativeCommandError`，以退出码和 `ok`/`FAIL` 行为准。
- 文件读写不要用 PowerShell `Set-Content`/`Get-Content`（BOM 与编码问题）。
- 代码注释只写代码本身表达不了的约束；不写"本次改动"类注释。
- 每个任务结束提交一次；提交信息中文，前缀 `feat(tool):` / `fix(tool):` / `feat(harness):` / `test(evals):` 等。

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/suggest.go` (+`_test`) | `Suggest`、`Levenshtein`（rune 级） |
| Modify `framework/tool/validate.go` (+`_test`) | `SchemaError.Candidates/Hint`、新 keyword 常量、`Error()` 文本、`RequiredArgsSummary` |
| Create `framework/tool/argcheck.go` (+`_test`) | `ArgCheck` 接口与 `OneOf`/`Pattern`/`Reject`/`FieldRefs` |
| Create `framework/tool/middleware.go` (+`_test`) | `applyArgAliases`、`wrapChecks`、check_skipped 记录 |
| Modify `framework/tool/tool.go` | `Tool` 新字段、`Register` 包装顺序、`checked` |
| Modify `framework/executor/reader.go` | `Diagnosis`/`ProbeCount` 类型、`QueryResult.Diagnosis` |
| Modify `framework/tool/query_spill.go` | `QuerySpillStub.Diagnosis` |
| Modify `framework/tool/evidence.go` (+`_test`) | `HitStatusSuspect`、白名单、`DiagnosisFromResult`、`MarkSuspect` |
| Create `framework/tool/empty_probe.go` (+`_test`) | `EmptyProbe`、`ProbeVariant`、`runEmptyProbe` |
| Create `framework/tool/es_field_cache.go` (+`_test`) | 带 TTL 的 `ESFieldMapper` 装饰器与按集群的缓存 |
| Modify `framework/tool/es_log_tool.go`, `es_log_mapping.go` (+tests) | `FieldRefs`、探测、删除 unknown_fields 分支、抽出 `buildESLogDSL` |
| Modify `framework/tool/data/execute_read.go`, `datasource_id.go` (+tests) | 别名、required、Reject、OneOf、SQL 字段校验、探测、Reader 修复 |
| Create `framework/tool/data/sql_refs.go` (+`_test`) | 单表 SQL 解析（表名、列名） |
| Modify `framework/tool/data/describe_table.go`, `list_tables.go` | `OneOf(datasource_id)` |
| Modify `framework/tool/jaeger_tool.go`, `http_tool.go` (+tests) | Pattern；method 默认 GET |
| Modify `framework/tool/rca_code_tools.go`, `rca_repos.go`, `file_tools.go` (+tests) | `OneOf(repo)`、`rca_read` 候选、glob/search 的 root 状态与 suspect |
| Modify `framework/tool/skillops/skill_tools.go`, `skill_manager_tool.go` (+tests) | `OneOf(name)` |
| Modify `framework/tool/mcp.go` (+test) | schema 归一化 |
| Modify `framework/harness/tool_not_found.go` | 编辑距离改用 `tool.Levenshtein`（排序规则不变） |
| Modify `framework/harness/react_agent.go` (+test) | C1 参数 JSON 错误回显 |
| Create `framework/harness/suspect_stop_hook.go` (+`_test`) | `suspect_evidence` hook、`DefaultStopHooks` |
| Modify `portal/internal/chat/agent_builder.go`, `evals/runner/live.go`, `framework/investigate/runner.go` | 合并注册默认 stop hook |
| Modify `evals/runner/trace_summary.go`, `metrics.go`, `answer_shape.go` (+tests) | suspect 率、参数拒绝率 |
| Modify `framework/investigate/playbook.go`, `evals/tasks_ci/answer_shape_ci.jsonl`, `evals/README.md` | 文案与 CI 任务 |

---

### Task 1: `Suggest` 共享候选函数

**Files:**
- Create: `framework/tool/suggest.go`
- Test: `framework/tool/suggest_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"reflect"
	"testing"
)

func TestSuggest(t *testing.T) {
	cands := []string{"game_flow", "game_flow_all", "backend", "Service.Name", "vm_state"}
	cases := []struct {
		in   string
		n    int
		want []string
	}{
		{"service.name", 3, []string{"Service.Name"}},
		{"game_flo", 3, []string{"game_flow", "game_flow_all"}},
		{"flow", 3, []string{"game_flow", "game_flow_all"}},
		{"vm_stat", 3, []string{"vm_state"}},
		{"zzzzzz", 3, nil},
		{"", 3, nil},
		{"game_flow", 1, []string{"game_flow"}},
	}
	for _, tc := range cases {
		if got := Suggest(tc.in, cands, tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Suggest(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestLevenshteinRunes(t *testing.T) {
	if d := Levenshtein("日志表", "日志"); d != 1 {
		t.Fatalf("got %d", d)
	}
	if d := Levenshtein("kitten", "sitting"); d != 3 {
		t.Fatalf("got %d", d)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestSuggest|TestLevenshteinRunes" -count=1`
Expected: FAIL（`undefined: Suggest`）

- [ ] **Step 3: 实现**

```go
package tool

import (
	"sort"
	"strings"
)

// Suggest 返回与 input 最接近的至多 n 个候选：精确（忽略大小写）> 前缀 > 子串 > 编辑距离。
// 编辑距离阈值 max(2, len(input)/3)（按 rune 计）；无匹配返回 nil。
func Suggest(input string, candidates []string, n int) []string {
	in := strings.ToLower(strings.TrimSpace(input))
	if in == "" || n <= 0 || len(candidates) == 0 {
		return nil
	}
	limit := len([]rune(in)) / 3
	if limit < 2 {
		limit = 2
	}
	type scored struct {
		s    string
		rank int
		dist int
		idx  int
	}
	var picks []scored
	seen := map[string]struct{}{}
	for i, c := range candidates {
		if _, dup := seen[c]; dup || strings.TrimSpace(c) == "" {
			continue
		}
		seen[c] = struct{}{}
		lc := strings.ToLower(c)
		switch {
		case lc == in:
			picks = append(picks, scored{c, 0, 0, i})
		case strings.HasPrefix(lc, in):
			picks = append(picks, scored{c, 1, len(lc) - len(in), i})
		case strings.Contains(lc, in) || strings.Contains(in, lc):
			picks = append(picks, scored{c, 2, Levenshtein(lc, in), i})
		default:
			if d := Levenshtein(lc, in); d <= limit {
				picks = append(picks, scored{c, 3, d, i})
			}
		}
	}
	sort.SliceStable(picks, func(a, b int) bool {
		if picks[a].rank != picks[b].rank {
			return picks[a].rank < picks[b].rank
		}
		if picks[a].dist != picks[b].dist {
			return picks[a].dist < picks[b].dist
		}
		return picks[a].idx < picks[b].idx
	})
	if len(picks) == 0 {
		return nil
	}
	if picks[0].rank == 0 {
		return []string{picks[0].s}
	}
	if len(picks) > n {
		picks = picks[:n]
	}
	out := make([]string, len(picks))
	for i, p := range picks {
		out[i] = p.s
	}
	return out
}

// Levenshtein 计算 rune 级编辑距离。
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "TestSuggest|TestLevenshteinRunes" -count=1`
Expected: PASS。若 `"flow"` 用例顺序不符，按 rank/dist 规则检查（`game_flow` 与 `game_flow_all` 同为子串，dist 小者在前）。

- [ ] **Step 5: 替换重复实现**

1. `framework/tool/es_log_mapping.go`：删除 `func levenshtein(a, b string) int`（约 1092 行），把文件内对 `levenshtein(` 的调用改为 `Levenshtein(`。`suggestSimilarMappedFields` 的匹配规则不变（它返回至多 5 个且有规范化逻辑，保留函数本身）。
2. `framework/tool/es_log_index.go`：若有对 `levenshtein(` 的调用，同样改为 `Levenshtein(`。
3. `framework/harness/tool_not_found.go`：删除本地 `levenshtein`，`suggestToolNames` 中改调 `tool.Levenshtein`（文件已在 harness 包，按需 `import "github.com/sixath/framework/tool"`；先 `rg -n '"github.com/sixath/framework/tool"' framework/harness/react_agent.go` 确认 harness 已依赖 tool 包，无循环）。

Run: `go test -p 1 ./tool/ ./harness/ -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add framework/tool/suggest.go framework/tool/suggest_test.go framework/tool/es_log_mapping.go framework/tool/es_log_index.go framework/harness/tool_not_found.go
git commit -m "feat(tool): 新增 Suggest 共享候选函数并合并重复的 levenshtein"
```

---

### Task 2: `SchemaError` 候选与提示、`RequiredArgsSummary`

**Files:**
- Modify: `framework/tool/validate.go`
- Test: `framework/tool/validate_test.go`

- [ ] **Step 1: 写失败测试**（追加到 `validate_test.go`）

```go
func TestInvalidArgumentsError_RendersCandidatesAndHint(t *testing.T) {
	e := &InvalidArgumentsError{Tool: "execute_read", Errors: []SchemaError{{
		Path: "datasource_id", Keyword: KeywordOneOf,
		Message:    `argument "datasource_id": unknown value "mysq1"`,
		Candidates: []string{"mysql1", "mysql2"},
		Hint:       "use one of the configured datasources",
	}}}
	got := e.Error()
	for _, want := range []string{`unknown value "mysq1"`, "did you mean: mysql1, mysql2", "use one of the configured datasources"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestRequiredArgsSummary(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dsl":           map[string]any{"type": "string"},
			"datasource_id": map[string]any{"type": "string"},
			"max_rows":      map[string]any{"type": "integer"},
		},
		"required": []any{"dsl"},
	}
	if got := RequiredArgsSummary(schema); got != "required: dsl(string)" {
		t.Fatalf("got %q", got)
	}
	delete(schema, "required")
	if got := RequiredArgsSummary(schema); got != "params: datasource_id(string), dsl(string), max_rows(integer)" {
		t.Fatalf("got %q", got)
	}
	if got := RequiredArgsSummary(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}
```

（若文件未导入 `strings`，补上。）

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestInvalidArgumentsError_RendersCandidatesAndHint|TestRequiredArgsSummary" -count=1`
Expected: FAIL（`unknown field Candidates`）

- [ ] **Step 3: 实现**

在 `SchemaError` 中追加字段，并新增常量：

```go
type SchemaError struct {
	Path    string `json:"path"`
	// Keyword 为命中的规则：JSON Schema 关键字（required|type|enum|minimum|maximum|additionalProperties）
	// 或中间件规则（one_of|pattern|reject|unknown_field）。
	Keyword    string   `json:"keyword"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
	Hint       string   `json:"hint,omitempty"`
}

const (
	KeywordOneOf        = "one_of"
	KeywordPattern      = "pattern"
	KeywordReject       = "reject"
	KeywordUnknownField = "unknown_field"
)
```

`Error()` 中把 `parts = append(parts, se.Message)` 改为 `parts = append(parts, se.render())`，并新增：

```go
func (se SchemaError) render() string {
	s := se.Message
	if len(se.Candidates) > 0 {
		s += " (did you mean: " + strings.Join(se.Candidates, ", ") + ")"
	}
	if se.Hint != "" {
		s += "; " + se.Hint
	}
	return s
}

// RequiredArgsSummary 生成面向模型的参数摘要："required: a(string), b(integer)"；
// 无 required 时列出全部参数（按名排序，至多 8 个）："params: ..."。schema 不可解析时返回空串。
func RequiredArgsSummary(schema any) string {
	root, ok := schemaObject(schema)
	if !ok {
		return ""
	}
	props, _ := schemaObject(root["properties"])
	describe := func(name string) string {
		if p, ok := schemaObject(props[name]); ok {
			if typ, _ := p["type"].(string); typ != "" {
				return name + "(" + typ + ")"
			}
		}
		return name
	}
	if req := stringSlice(root["required"]); len(req) > 0 {
		parts := make([]string, 0, len(req))
		for _, n := range req {
			parts = append(parts, describe(n))
		}
		return "required: " + strings.Join(parts, ", ")
	}
	names := sortedKeys(props)
	if len(names) == 0 {
		return ""
	}
	if len(names) > 8 {
		names = names[:8]
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, describe(n))
	}
	return "params: " + strings.Join(parts, ", ")
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "TestValidateArguments|TestInvalidArgumentsError|TestRequiredArgsSummary" -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/validate.go framework/tool/validate_test.go
git commit -m "feat(tool): 参数校验错误支持候选与提示，新增 RequiredArgsSummary"
```

---

### Task 3: 四类规则 `ArgCheck`

**Files:**
- Create: `framework/tool/argcheck.go`
- Test: `framework/tool/argcheck_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestOneOf(t *testing.T) {
	src := func(context.Context, map[string]any) ([]string, error) { return []string{"mysql1", "mysql2", "hive"}, nil }
	c := OneOf{Param: "datasource_id", Source: src}
	ctx := context.Background()

	if errs, err := c.Check(ctx, map[string]any{"datasource_id": "mysql1"}); err != nil || len(errs) != 0 {
		t.Fatalf("valid: %v %v", errs, err)
	}
	if errs, _ := c.Check(ctx, map[string]any{}); len(errs) != 0 {
		t.Fatalf("missing param must not be judged: %v", errs)
	}
	errs, err := c.Check(ctx, map[string]any{"datasource_id": "mysq1"})
	if err != nil || len(errs) != 1 || errs[0].Keyword != KeywordOneOf || errs[0].Candidates[0] != "mysql1" {
		t.Fatalf("invalid: %+v %v", errs, err)
	}
	errs, _ = c.Check(ctx, map[string]any{"datasource_id": "zzzzzzzz"})
	if len(errs) != 1 || len(errs[0].Candidates) != 3 {
		t.Fatalf("no fuzzy match must list candidates: %+v", errs)
	}

	norm := OneOf{Param: "datasource_id", Source: src, Normalize: func(_ map[string]any, v string) string {
		if v == "default" {
			return "mysql1"
		}
		return v
	}}
	if errs, _ := norm.Check(ctx, map[string]any{"datasource_id": "default"}); len(errs) != 0 {
		t.Fatalf("normalized: %v", errs)
	}

	when := OneOf{Param: "name", Source: src, When: func(p map[string]any) bool { return p["action"] != "create" }}
	if errs, _ := when.Check(ctx, map[string]any{"name": "new", "action": "create"}); len(errs) != 0 {
		t.Fatalf("When=false must skip: %v", errs)
	}

	failing := OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) { return nil, errors.New("down") }}
	if _, err := failing.Check(ctx, map[string]any{"x": "a"}); err == nil {
		t.Fatal("source error must surface as check error (fail-open upstream)")
	}
	empty := OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) { return nil, nil }}
	if errs, err := empty.Check(ctx, map[string]any{"x": "a"}); err != nil || len(errs) != 0 {
		t.Fatal("empty candidate set means not applicable: no errors, no check error")
	}
}

func TestPattern(t *testing.T) {
	c := Pattern{Param: "trace_id", Regex: regexp.MustCompile(`^([0-9a-fA-F]{16}|[0-9a-fA-F]{32})$`), Hint: "16 or 32 hex chars"}
	ctx := context.Background()
	if errs, _ := c.Check(ctx, map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"}); len(errs) != 0 {
		t.Fatalf("valid: %v", errs)
	}
	errs, _ := c.Check(ctx, map[string]any{"trace_id": "req-123"})
	if len(errs) != 1 || errs[0].Keyword != KeywordPattern || errs[0].Hint == "" {
		t.Fatalf("invalid: %+v", errs)
	}
}

func TestReject(t *testing.T) {
	c := Reject{Label: "non_sql", Redirect: "use es_log_query for Elasticsearch", Detect: func(p map[string]any) (string, string, bool) {
		if s, _ := p["dsl"].(string); s == "level:ERROR" {
			return "dsl", "looks like a Lucene expression", true
		}
		return "", "", false
	}}
	ctx := context.Background()
	if errs, _ := c.Check(ctx, map[string]any{"dsl": "SELECT 1"}); len(errs) != 0 {
		t.Fatalf("sql: %v", errs)
	}
	errs, _ := c.Check(ctx, map[string]any{"dsl": "level:ERROR"})
	if len(errs) != 1 || errs[0].Keyword != KeywordReject || errs[0].Path != "dsl" || errs[0].Hint == "" {
		t.Fatalf("lucene: %+v", errs)
	}
}

func TestFieldRefs(t *testing.T) {
	calls := 0
	c := FieldRefs{
		Extract: func(map[string]any) []FieldRef {
			return []FieldRef{{Param: "query", Field: "servcie"}, {Param: "sort", Field: "@timestamp"}}
		},
		Fields: func(_ context.Context, _ map[string]any, refresh bool) ([]string, error) {
			calls++
			if refresh {
				return []string{"service", "@timestamp", "message"}, nil
			}
			return []string{"service", "@timestamp"}, nil
		},
		BodyHint: func(map[string]any) string { return "free text goes to field message" },
	}
	errs, err := c.Check(context.Background(), nil)
	if err != nil || len(errs) != 1 {
		t.Fatalf("got %+v %v", errs, err)
	}
	e := errs[0]
	if e.Keyword != KeywordUnknownField || e.Path != "query" || e.Candidates[0] != "service" || e.Hint == "" {
		t.Fatalf("got %+v", e)
	}
	if calls != 2 {
		t.Fatalf("unknown field must trigger exactly one forced refresh, calls=%d", calls)
	}

	noCatalog := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "x"}} },
		Fields:  func(context.Context, map[string]any, bool) ([]string, error) { return nil, nil },
	}
	if errs, err := noCatalog.Check(context.Background(), nil); err != nil || len(errs) != 0 {
		t.Fatal("empty catalog means not applicable (no mapper / mapping unavailable)")
	}
	broken := FieldRefs{
		Extract: func(map[string]any) []FieldRef { return []FieldRef{{Param: "query", Field: "x"}} },
		Fields:  func(context.Context, map[string]any, bool) ([]string, error) { return nil, errors.New("timeout") },
	}
	if _, err := broken.Check(context.Background(), nil); err == nil {
		t.Fatal("fetch error must surface as check error (fail-open upstream)")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestOneOf|TestPattern|TestReject|TestFieldRefs" -count=1`
Expected: FAIL（`undefined: OneOf`）

- [ ] **Step 3: 实现**

```go
package tool

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ArgCheck 是执行前的声明式参数规则。约定：
//   - (nil, nil)：通过，或规则不适用（未配置候选源、没有 mapper、候选集为空）；
//   - (nil, err)：规则本应判定但失败（拉取出错、超时），中间件跳过该规则并记 check_skipped，
//     若结果为 0 条则标 suspect（fail-open）。
type ArgCheck interface {
	Name() string
	Check(ctx context.Context, params map[string]any) ([]SchemaError, error)
}

const (
	suggestN         = 3
	listCandidatesN  = 10
)

func stringParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return strings.TrimSpace(v)
}

// OneOf 要求参数值在动态候选集中。参数缺失/为空时不判定（必填由 JSON Schema 负责）。
type OneOf struct {
	Param     string
	Source    func(ctx context.Context, params map[string]any) ([]string, error)
	Normalize func(params map[string]any, v string) string
	When      func(params map[string]any) bool
	Hint      string
}

func (c OneOf) Name() string { return "one_of:" + c.Param }

func (c OneOf) Check(ctx context.Context, params map[string]any) ([]SchemaError, error) {
	if c.When != nil && !c.When(params) {
		return nil, nil
	}
	v := stringParam(params, c.Param)
	if v == "" || c.Source == nil {
		return nil, nil
	}
	if c.Normalize != nil {
		v = c.Normalize(params, v)
	}
	cands, err := c.Source(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, nil
	}
	for _, x := range cands {
		if x == v {
			return nil, nil
		}
	}
	sugg := Suggest(v, cands, suggestN)
	if len(sugg) == 0 {
		sorted := append([]string(nil), cands...)
		sort.Strings(sorted)
		if len(sorted) > listCandidatesN {
			sorted = sorted[:listCandidatesN]
		}
		sugg = sorted
	}
	return []SchemaError{{
		Path:       c.Param,
		Keyword:    KeywordOneOf,
		Message:    fmt.Sprintf("argument %q: unknown value %q", c.Param, v),
		Candidates: sugg,
		Hint:       c.Hint,
	}}, nil
}

// Pattern 校验参数格式。
type Pattern struct {
	Param string
	Regex *regexp.Regexp
	Hint  string
}

func (c Pattern) Name() string { return "pattern:" + c.Param }

func (c Pattern) Check(_ context.Context, params map[string]any) ([]SchemaError, error) {
	v := stringParam(params, c.Param)
	if v == "" || c.Regex == nil || c.Regex.MatchString(v) {
		return nil, nil
	}
	return []SchemaError{{
		Path:    c.Param,
		Keyword: KeywordPattern,
		Message: fmt.Sprintf("argument %q: invalid format %s", c.Param, describeValue(v)),
		Hint:    c.Hint,
	}}, nil
}

// Reject 在参数命中某类特征时拒绝调用，Redirect 告诉模型该用什么。
// Detect 返回 (参数路径, 原因, 是否命中)。
type Reject struct {
	Label    string
	Detect   func(params map[string]any) (path, reason string, hit bool)
	Redirect string
}

func (c Reject) Name() string { return "reject:" + c.Label }

func (c Reject) Check(_ context.Context, params map[string]any) ([]SchemaError, error) {
	if c.Detect == nil {
		return nil, nil
	}
	path, reason, hit := c.Detect(params)
	if !hit {
		return nil, nil
	}
	return []SchemaError{{
		Path:    path,
		Keyword: KeywordReject,
		Message: fmt.Sprintf("%s: %s", nameOf(path), reason),
		Hint:    c.Redirect,
	}}, nil
}

// FieldRef 是参数中引用的一个字段。
type FieldRef struct {
	Param string
	Field string
}

// FieldRefs 把参数中引用的字段与 mapping/表结构比对；发现未知字段时强制刷新一次再判定。
// Fields 返回空集合视为不适用；返回 error 视为规则失败。
type FieldRefs struct {
	Label    string
	Extract  func(params map[string]any) []FieldRef
	Fields   func(ctx context.Context, params map[string]any, refresh bool) ([]string, error)
	Known    func(field string, catalog []string) bool
	Similar  func(field string, catalog []string) []string
	BodyHint func(params map[string]any) string
}

func (c FieldRefs) Name() string { return "field_refs:" + c.Label }

func (c FieldRefs) Check(ctx context.Context, params map[string]any) ([]SchemaError, error) {
	if c.Extract == nil || c.Fields == nil {
		return nil, nil
	}
	refs := c.Extract(params)
	if len(refs) == 0 {
		return nil, nil
	}
	catalog, err := c.Fields(ctx, params, false)
	if err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, nil
	}
	unknown := c.unknown(refs, catalog)
	if len(unknown) == 0 {
		return nil, nil
	}
	if fresh, err := c.Fields(ctx, params, true); err == nil && len(fresh) > 0 {
		catalog = fresh
		unknown = c.unknown(refs, catalog)
	}
	hint := ""
	if c.BodyHint != nil {
		hint = c.BodyHint(params)
	}
	errs := make([]SchemaError, 0, len(unknown))
	for _, r := range unknown {
		var cands []string
		if c.Similar != nil {
			cands = c.Similar(r.Field, catalog)
		} else {
			cands = Suggest(r.Field, catalog, suggestN)
		}
		if len(cands) > suggestN {
			cands = cands[:suggestN]
		}
		errs = append(errs, SchemaError{
			Path:       r.Param,
			Keyword:    KeywordUnknownField,
			Message:    fmt.Sprintf("argument %q references unknown field %q", r.Param, r.Field),
			Candidates: cands,
			Hint:       hint,
		})
	}
	return errs, nil
}

func (c FieldRefs) unknown(refs []FieldRef, catalog []string) []FieldRef {
	known := c.Known
	if known == nil {
		set := make(map[string]struct{}, len(catalog))
		for _, f := range catalog {
			set[f] = struct{}{}
		}
		known = func(f string, _ []string) bool { _, ok := set[f]; return ok }
	}
	var out []FieldRef
	seen := map[string]struct{}{}
	for _, r := range refs {
		if r.Field == "" || known(r.Field, catalog) {
			continue
		}
		if _, dup := seen[r.Param+"\x00"+r.Field]; dup {
			continue
		}
		seen[r.Param+"\x00"+r.Field] = struct{}{}
		out = append(out, r)
	}
	return out
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "TestOneOf|TestPattern|TestReject|TestFieldRefs" -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/argcheck.go framework/tool/argcheck_test.go
git commit -m "feat(tool): 新增 OneOf/Pattern/Reject/FieldRefs 四类声明式参数规则"
```

---

### Task 4: `suspect` 状态与 `Diagnosis` 类型

**Files:**
- Modify: `framework/executor/reader.go`（`QueryResult` 旁）
- Modify: `framework/tool/query_spill.go`
- Modify: `framework/tool/evidence.go`
- Test: `framework/tool/evidence_test.go`

- [ ] **Step 1: 写失败测试**（追加到 `evidence_test.go`）

```go
func TestHitStatusSuspectRoundTrip(t *testing.T) {
	m := map[string]any{"hit_status": HitStatusSuspect}
	if st, _, _ := HitContractFromResult(m); st != HitStatusSuspect {
		t.Fatalf("map: %q", st)
	}
	qr := &executor.QueryResult{HitStatus: HitStatusSuspect}
	if st, _, _ := HitContractFromResult(qr); st != HitStatusSuspect {
		t.Fatalf("QueryResult: %q", st)
	}
}

func TestMarkSuspectAndDiagnosisFromResult(t *testing.T) {
	d := &executor.Diagnosis{Hint: "without:service:foo has 57"}

	m := MarkSuspect(map[string]any{"hit_status": HitStatusEmpty}, d)
	if m.(map[string]any)["hit_status"] != HitStatusSuspect || DiagnosisFromResult(m).Hint != d.Hint {
		t.Fatalf("map: %+v", m)
	}

	qr := MarkSuspect(&executor.QueryResult{HitStatus: HitStatusEmpty}, d).(*executor.QueryResult)
	if qr.HitStatus != HitStatusSuspect || DiagnosisFromResult(qr) != d {
		t.Fatalf("QueryResult: %+v", qr)
	}

	stub := MarkSuspect(&QuerySpillStub{HitStatus: HitStatusEmpty}, d).(*QuerySpillStub)
	if stub.HitStatus != HitStatusSuspect || DiagnosisFromResult(stub) != d {
		t.Fatalf("stub: %+v", stub)
	}

	attachOnly := AttachDiagnosis(map[string]any{"hit_status": HitStatusEmpty}, d).(map[string]any)
	if attachOnly["hit_status"] != HitStatusEmpty || attachOnly["diagnosis"] != d {
		t.Fatalf("AttachDiagnosis must not change status: %+v", attachOnly)
	}

	if DiagnosisFromResult("text") != nil {
		t.Fatal("unknown result type must give nil")
	}
	decoded := map[string]any{"diagnosis": map[string]any{"hint": "h", "probes": []any{map[string]any{"label": "a", "count": 3.0}}}}
	if got := DiagnosisFromResult(decoded); got == nil || got.Hint != "h" || got.Probes[0].Count != 3 {
		t.Fatalf("decoded map: %+v", got)
	}
}

func TestDecodeJSONResult(t *testing.T) {
	m, ok := DecodeJSONResult(`{"hit_status":"suspect"}`).(map[string]any)
	if !ok || m["hit_status"] != "suspect" {
		t.Fatalf("got %#v", m)
	}
	if DecodeJSONResult("plain text") != "plain text" {
		t.Fatal("non-JSON string must pass through")
	}
}
```

（确认 `evidence_test.go` 已导入 `github.com/sixath/framework/executor`，没有则补上；`evidence.go` 需 `encoding/json`、`strings` 导入。）

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestHitStatusSuspectRoundTrip|TestMarkSuspectAndDiagnosisFromResult" -count=1`
Expected: FAIL（`undefined: HitStatusSuspect`）

- [ ] **Step 3: 实现**

`framework/executor/reader.go`，在 `QueryResult` 前新增类型，并给 `QueryResult` 加字段：

```go
// Diagnosis 是零结果探测的结论：放宽条件后的计数，用于区分"条件写错"与"确实无数据"。
type Diagnosis struct {
	Probes    []ProbeCount `json:"probes,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
	Hint      string       `json:"hint,omitempty"`
	Errors    []string     `json:"errors,omitempty"`
}

type ProbeCount struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}
```

`QueryResult` 末尾追加：`Diagnosis *Diagnosis \`json:"diagnosis,omitempty"\``

`framework/tool/query_spill.go` 的 `QuerySpillStub` 追加：`Diagnosis *executor.Diagnosis \`json:"diagnosis,omitempty"\``（文件若未导入 executor 则补上；`tool` 包已依赖 `executor`，无循环）。
同时检查 `query_spill.go` 中把 `*executor.QueryResult` 转为 `QuerySpillStub` 的函数（`rg -n "QuerySpillStub{" framework/tool`），把 `Diagnosis: res.Diagnosis` 一并拷贝过去；es 的 map 结果转 stub 的路径若存在 `"diagnosis"` 键，也拷贝（`if d, ok := m["diagnosis"].(*executor.Diagnosis); ok { stub.Diagnosis = d }`）。

`framework/tool/evidence.go`：

```go
const (
	HitStatusHits  = "hits"
	HitStatusEmpty = "empty"
	HitStatusError = "error"
	// HitStatusSuspect 表示 0 条但有证据显示是查询条件问题（放宽后有数据、搜索根不存在等），
	// 不能据此断言"没有数据"。
	HitStatusSuspect = "suspect"
)
```

`hitStatusString` 的 `case` 加入 `HitStatusSuspect`。文件末尾新增：

```go
// DiagnosisFromResult 读取结果上的零结果诊断；兼容经 JSON 往返后的 map 形式。不支持的结果类型返回 nil。
func DiagnosisFromResult(v any) *executor.Diagnosis {
	switch x := v.(type) {
	case map[string]any:
		switch d := x["diagnosis"].(type) {
		case *executor.Diagnosis:
			return d
		case map[string]any:
			b, err := json.Marshal(d)
			if err != nil {
				return nil
			}
			var out executor.Diagnosis
			if json.Unmarshal(b, &out) != nil {
				return nil
			}
			return &out
		}
		return nil
	case *executor.QueryResult:
		if x != nil {
			return x.Diagnosis
		}
	case *QuerySpillStub:
		if x != nil {
			return x.Diagnosis
		}
	}
	return nil
}

// DecodeJSONResult 把 JSON 对象字符串形式的结果（如 eval 夹具、MCP 文本结果）解码为 map；其他值原样返回。
func DecodeJSONResult(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "{") {
		return v
	}
	var m map[string]any
	if json.Unmarshal([]byte(t), &m) != nil {
		return v
	}
	return m
}

// AttachDiagnosis 把诊断写到结果上，不改变 hit_status。
func AttachDiagnosis(v any, d *executor.Diagnosis) any {
	switch x := v.(type) {
	case map[string]any:
		x["diagnosis"] = d
	case *executor.QueryResult:
		if x != nil {
			x.Diagnosis = d
		}
	case *QuerySpillStub:
		if x != nil {
			x.Diagnosis = d
		}
	}
	return v
}

// MarkSuspect 写入诊断（可为 nil）并把 hit_status 置为 suspect。
func MarkSuspect(v any, d *executor.Diagnosis) any {
	if d != nil {
		v = AttachDiagnosis(v, d)
	}
	switch x := v.(type) {
	case map[string]any:
		x["hit_status"] = HitStatusSuspect
	case *executor.QueryResult:
		if x != nil {
			x.HitStatus = HitStatusSuspect
		}
	case *QuerySpillStub:
		if x != nil {
			x.HitStatus = HitStatusSuspect
		}
	}
	return v
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ ./executor/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/executor/reader.go framework/tool/query_spill.go framework/tool/evidence.go framework/tool/evidence_test.go
git commit -m "feat(tool): 新增 hit_status=suspect 与零结果诊断结构"
```

---

### Task 5: 零结果探测 `EmptyProbe`

**Files:**
- Create: `framework/tool/empty_probe.go`
- Test: `framework/tool/empty_probe_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sixath/framework/executor"
)

func emptyResult() map[string]any { return map[string]any{"ok": true, "hit_status": HitStatusEmpty} }

func TestRunEmptyProbe_MarksSuspectWhenRelaxedHasData(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant {
			return []ProbeVariant{{Label: "time_window_only"}, {Label: "without:service:foo"}}
		},
		Count: func(_ context.Context, v ProbeVariant) (int64, error) {
			if v.Label == "time_window_only" {
				return 12034, nil
			}
			return 57, nil
		},
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if out["hit_status"] != HitStatusSuspect {
		t.Fatalf("status %v", out["hit_status"])
	}
	d := out["diagnosis"].(*executor.Diagnosis)
	if len(d.Probes) != 2 || d.Hint == "" || d.Truncated {
		t.Fatalf("diag %+v", d)
	}
}

func TestRunEmptyProbe_KeepsEmptyWhenAllZero(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "time_window_only"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { return 0, nil },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if out["hit_status"] != HitStatusEmpty || out["diagnosis"] == nil {
		t.Fatalf("got %+v", out)
	}
}

func TestRunEmptyProbe_SkipsNonEmpty(t *testing.T) {
	called := false
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "x"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { called = true; return 1, nil },
	}
	runEmptyProbe(context.Background(), p, nil, map[string]any{"hit_status": HitStatusHits})
	if called {
		t.Fatal("must not probe non-empty results")
	}
}

func TestRunEmptyProbe_BudgetMaxThreeAndTruncated(t *testing.T) {
	n := 0
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant {
			return []ProbeVariant{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}}
		},
		Count: func(context.Context, ProbeVariant) (int64, error) { n++; return 0, nil },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if n != 3 || !out["diagnosis"].(*executor.Diagnosis).Truncated {
		t.Fatalf("n=%d diag=%+v", n, out["diagnosis"])
	}
}

func TestRunEmptyProbe_RespectsToolDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "slow"}, {Label: "slow2"}} },
		Count: func(c context.Context, _ ProbeVariant) (int64, error) {
			<-c.Done()
			return 0, c.Err()
		},
	}
	start := time.Now()
	out := runEmptyProbe(ctx, p, nil, emptyResult()).(map[string]any)
	if time.Since(start) > time.Second {
		t.Fatal("probe must stop at tool deadline")
	}
	if out["hit_status"] != HitStatusEmpty {
		t.Fatalf("original result must survive: %+v", out)
	}
}

func TestRunEmptyProbe_CountErrorRecorded(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { return 0, errors.New("boom") },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	d := out["diagnosis"].(*executor.Diagnosis)
	if len(d.Errors) != 1 || out["hit_status"] != HitStatusEmpty {
		t.Fatalf("got %+v", d)
	}
}

func TestRunEmptyProbe_DisabledByEnv(t *testing.T) {
	t.Setenv(EnvToolEmptyProbe, "off")
	called := false
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { called = true; return 1, nil },
	}
	runEmptyProbe(context.Background(), p, nil, emptyResult())
	if called {
		t.Fatal("env off must disable probing")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run TestRunEmptyProbe -count=1`
Expected: FAIL（`undefined: EmptyProbe`）

- [ ] **Step 3: 实现**

```go
package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sixath/framework/executor"
)

// EnvToolEmptyProbe=off/0/false 全局关闭零结果探测（默认开启）。
const EnvToolEmptyProbe = "SATH_TOOL_EMPTY_PROBE"

const (
	emptyProbeMaxVariants = 3
	emptyProbeBudget      = 3 * time.Second
	// 给工具自身收尾（序列化、spill）留出的余量，避免探测把成功的查询拖成超时。
	emptyProbeDeadlineMargin = 200 * time.Millisecond
)

// EmptyProbe 在结果为 0 条（hit_status=empty）时，按 Relax 给出的放宽变体逐个计数。
// 预算（变体数、超时）由中间件统一控制，工具只负责生成变体与计数。
type EmptyProbe struct {
	Relax func(params map[string]any) []ProbeVariant
	Count func(ctx context.Context, v ProbeVariant) (int64, error)
}

type ProbeVariant struct {
	Label  string
	Params map[string]any
}

func emptyProbeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvToolEmptyProbe))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return false
	}
	return true
}

func runEmptyProbe(ctx context.Context, p *EmptyProbe, params map[string]any, result any) any {
	if p == nil || p.Relax == nil || p.Count == nil || !emptyProbeEnabled() {
		return result
	}
	if st, _, _ := HitContractFromResult(result); st != HitStatusEmpty {
		return result
	}
	variants := p.Relax(params)
	if len(variants) == 0 {
		return result
	}
	budget := emptyProbeBudget
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl) - emptyProbeDeadlineMargin; left < budget {
			budget = left
		}
	}
	if budget <= 0 {
		return result
	}
	pctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	d := &executor.Diagnosis{}
	if len(variants) > emptyProbeMaxVariants {
		variants = variants[:emptyProbeMaxVariants]
		d.Truncated = true
	}
	var best *executor.ProbeCount
	for _, v := range variants {
		if pctx.Err() != nil {
			d.Truncated = true
			break
		}
		n, err := p.Count(pctx, v)
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", v.Label, err))
			continue
		}
		d.Probes = append(d.Probes, executor.ProbeCount{Label: v.Label, Count: n})
		if n > 0 && best == nil {
			pc := d.Probes[len(d.Probes)-1]
			best = &pc
		}
	}
	if len(d.Probes) == 0 && len(d.Errors) == 0 {
		return result
	}
	if best == nil {
		d.Hint = "relaxed variants also returned 0; the data likely does not exist in this range"
		return AttachDiagnosis(result, d)
	}
	d.Hint = fmt.Sprintf("original query returned 0 but %s returned %d; a condition is probably wrong (field/value/time range) — fix it before concluding there is no data", best.Label, best.Count)
	return MarkSuspect(result, d)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run TestRunEmptyProbe -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/empty_probe.go framework/tool/empty_probe_test.go
git commit -m "feat(tool): 新增有预算的零结果探测 EmptyProbe"
```

---

### Task 6: 校验中间件与 `Register` 包装顺序

**Files:**
- Create: `framework/tool/middleware.go`
- Modify: `framework/tool/tool.go:31-60`（`Tool`）、`framework/tool/tool.go:276-388`（`Register`）
- Test: `framework/tool/middleware_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sixath/framework/events"
)

func newTestRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
}

func TestMiddleware_AliasesBeforeSchema(t *testing.T) {
	r := newTestRegistry()
	var got map[string]any
	err := r.Register(Tool{
		Name:       "q",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"dsl": map[string]any{"type": "string"}}, "required": []any{"dsl"}},
		ArgAliases: map[string]string{"query": "dsl"},
		Execute: func(_ context.Context, p map[string]any) (any, error) { got = p; return "ok", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := r.Get("q")
	if _, err := tl.Execute(context.Background(), map[string]any{"query": "SELECT 1"}); err != nil {
		t.Fatalf("alias must satisfy required: %v", err)
	}
	if got["dsl"] != "SELECT 1" || got["query"] != nil {
		t.Fatalf("got %v", got)
	}
	_, _ = tl.Execute(context.Background(), map[string]any{"dsl": "A", "query": "B"})
	if got["dsl"] != "A" {
		t.Fatalf("alias must not override canonical: %v", got)
	}
}

func TestMiddleware_ChecksRejectAndAggregate(t *testing.T) {
	r := newTestRegistry()
	executed := false
	src := func(context.Context, map[string]any) ([]string, error) { return []string{"a1", "b1"}, nil }
	_ = r.Register(Tool{
		Name:      "t",
		ArgChecks: []ArgCheck{OneOf{Param: "x", Source: src}, OneOf{Param: "y", Source: src}},
		Execute:   func(context.Context, map[string]any) (any, error) { executed = true; return "ok", nil },
	})
	tl, _ := r.Get("t")
	res, err := tl.Execute(context.Background(), map[string]any{"x": "a2", "y": "b2"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) || len(iae.Errors) != 2 || executed {
		t.Fatalf("err=%v executed=%v", err, executed)
	}
	m := res.(map[string]any)
	if m["ok"] != false || m["error_code"] != ErrorPermanent || m["invalid_arguments"] == nil {
		t.Fatalf("result %+v", m)
	}
}

func TestMiddleware_CheckErrorFailsOpenAndMarksSuspectOnEmpty(t *testing.T) {
	r := newTestRegistry()
	_ = r.Register(Tool{
		Name: "t",
		ArgChecks: []ArgCheck{OneOf{Param: "x", Source: func(context.Context, map[string]any) ([]string, error) {
			return nil, errors.New("catalog down")
		}}},
		Execute: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true, "hit_status": HitStatusEmpty}, nil
		},
	})
	tl, _ := r.Get("t")
	res, err := tl.Execute(context.Background(), map[string]any{"x": "zz"})
	if err != nil {
		t.Fatalf("fail-open expected: %v", err)
	}
	m := res.(map[string]any)
	if m["hit_status"] != HitStatusSuspect || m["check_skipped"] == nil {
		t.Fatalf("got %+v", m)
	}
}

func TestMiddleware_ValidationEnvReadPerCall(t *testing.T) {
	r := newTestRegistry()
	_ = r.Register(Tool{
		Name:       "t",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
		Execute:    func(context.Context, map[string]any) (any, error) { return "ok", nil },
	})
	tl, _ := r.Get("t")
	if _, err := tl.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected rejection")
	}
	t.Setenv(EnvToolArgValidation, "off")
	if _, err := tl.Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("env off after Register must disable validation: %v", err)
	}
}

func TestMiddleware_RejectedCallsEmitEvents(t *testing.T) {
	bus := events.NewBus()
	var executed int32
	bus.Subscribe(false, func(_ context.Context, e events.Event) {
		if e.Kind == events.ToolExecuted {
			atomic.AddInt32(&executed, 1)
		}
	})
	r := newTestRegistry()
	r.eventBus = bus
	_ = r.Register(Tool{
		Name:       "t",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
		Execute:    func(context.Context, map[string]any) (any, error) { return "ok", nil },
	})
	tl, _ := r.Get("t")
	_, _ = tl.Execute(context.Background(), map[string]any{})
	if atomic.LoadInt32(&executed) != 1 {
		t.Fatalf("rejected call must publish ToolExecuted, got %d", executed)
	}
}

func TestMiddleware_NotWrappedTwice(t *testing.T) {
	var probes int32
	parent := newTestRegistry()
	_ = parent.Register(Tool{
		Name: "t",
		EmptyProbe: &EmptyProbe{
			Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
			Count: func(context.Context, ProbeVariant) (int64, error) { atomic.AddInt32(&probes, 1); return 0, nil },
		},
		Execute: func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true, "hit_status": HitStatusEmpty}, nil
		},
	})
	tl, _ := parent.Get("t")
	sub := newTestRegistry()
	if err := sub.Register(tl); err != nil {
		t.Fatal(err)
	}
	st, _ := sub.Get("t")
	_, _ = st.Execute(context.Background(), map[string]any{})
	if probes != 1 {
		t.Fatalf("probe ran %d times", probes)
	}
}
```

> `Subscribe(async bool, l Listener)` 见 `framework/events/bus.go:41`；`NewBus` 的构造名以该文件为准（`rg -n "^func New" framework/events/bus.go`）。

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run TestMiddleware -count=1`
Expected: FAIL（`unknown field ArgAliases`）

- [ ] **Step 3: 实现 `middleware.go`**

```go
package tool

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const argCheckTimeout = 2 * time.Second

func applyArgAliases(params map[string]any, aliases map[string]string) map[string]any {
	if len(aliases) == 0 || len(params) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	for alias, canonical := range aliases {
		v, ok := out[alias]
		if !ok {
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = v
		}
		delete(out, alias)
	}
	return out
}

func invalidArgsResult(name string, err error) map[string]any {
	result := map[string]any{
		"ok":         false,
		"tool":       name,
		"error":      err.Error(),
		"error_code": ErrorPermanent,
	}
	var iae *InvalidArgumentsError
	if errors.As(err, &iae) {
		result["invalid_arguments"] = iae.Errors
	}
	return result
}

// runArgChecks 依次执行规则并收集错误；规则自身失败或超时则跳过（fail-open），返回被跳过的规则名。
func runArgChecks(ctx context.Context, toolName string, checks []ArgCheck, params map[string]any) ([]SchemaError, []string) {
	var errs []SchemaError
	var skipped []string
	for _, c := range checks {
		if c == nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, argCheckTimeout)
		found, err := c.Check(cctx, params)
		cancel()
		if err != nil {
			skipped = append(skipped, c.Name())
			slog.InfoContext(ctx, "tool arg check skipped", "tool", toolName, "check", c.Name(), "reason", err.Error())
			continue
		}
		errs = append(errs, found...)
	}
	return errs, skipped
}

// wrapChecks 构造校验中间件：别名 → JSON Schema → ArgChecks → Execute → EmptyProbe。
// 开关每次调用读取，便于线上即时回退。
func wrapChecks(t Tool) ExecuteFunc {
	name, schema, aliases, checks, probe, inner := t.Name, t.Parameters, t.ArgAliases, t.ArgChecks, t.EmptyProbe, t.Execute
	return func(ctx context.Context, params map[string]any) (any, error) {
		params = applyArgAliases(params, aliases)
		var skipped []string
		if toolArgValidationEnabled() {
			if err := ValidateArguments(name, schema, params); err != nil {
				return invalidArgsResult(name, err), err
			}
			var errs []SchemaError
			errs, skipped = runArgChecks(ctx, name, checks, params)
			if len(errs) > 0 {
				err := &InvalidArgumentsError{Tool: name, Errors: errs}
				return invalidArgsResult(name, err), err
			}
		}
		result, err := inner(ctx, params)
		if err != nil {
			return result, err
		}
		if probe != nil {
			result = runEmptyProbe(ctx, probe, params, result)
		}
		if len(skipped) > 0 {
			if st, _, _ := HitContractFromResult(result); st == HitStatusEmpty {
				result = MarkSuspect(result, nil)
			}
			if m, ok := result.(map[string]any); ok {
				m["check_skipped"] = skipped
			}
		}
		return result, nil
	}
}
```

- [ ] **Step 4: 修改 `Tool` 与 `Register`**

`Tool` 末尾追加：

```go
	// ArgAliases 别名 -> 正式参数名，在校验前改写；正式名已存在时丢弃别名。
	ArgAliases map[string]string
	// ArgChecks 执行前的声明式规则（见 argcheck.go），在 JSON Schema 校验之后运行。
	ArgChecks []ArgCheck
	// EmptyProbe 结果为 hit_status=empty 时的放宽探测（见 empty_probe.go）。
	EmptyProbe *EmptyProbe
	// checked 表示 Execute 已含校验中间件；同一工具注册到多个 Registry（如 investigate 子代理）时不重复包装。
	checked bool
```

`Register`：在 `bus := r.eventBus` 之前插入：

```go
	if !t.checked {
		t.Execute = wrapChecks(t)
		t.checked = true
	}
```

删除原 `// 入参 schema 校验：fail-open…` 整段 `if toolArgValidationEnabled() { … }`（约 359-384 行）。

包装后调用顺序（外→内）：超时 → 事件/trace → 校验中间件 → 原 Execute。

- [ ] **Step 5: 运行确认通过**

Run: `go test -p 1 ./tool/ -count=1`
Expected: PASS。若有既有测试断言"Register 时关闭校验"的语义，改为在调用前 `t.Setenv`；若有测试断言被拒调用**不**产生事件，按新行为更新断言。

- [ ] **Step 6: 跑依赖方**

Run: `go test -p 1 ./harness/ ./investigate/ -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add framework/tool/middleware.go framework/tool/middleware_test.go framework/tool/tool.go
git commit -m "feat(tool): 注册层校验中间件（别名/规则/探测），校验移入事件层且防重复包装"
```

---

### Task 7: ES mapping 缓存

**Files:**
- Create: `framework/tool/es_field_cache.go`
- Test: `framework/tool/es_field_cache_test.go`

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"context"
	"testing"
	"time"
)

type countingMapper struct {
	fields []string
	calls  int
}

func (m *countingMapper) Lookup(context.Context, string, string) (ESFieldMapping, bool) {
	return ESFieldMapping{}, false
}
func (m *countingMapper) ListFields(context.Context, string) []string { m.calls++; return m.fields }

func TestCachedFieldMapper_TTLAndRefresh(t *testing.T) {
	inner := &countingMapper{fields: []string{"service"}}
	now := time.Unix(0, 0)
	c := newCachedFieldMapper(inner, 5*time.Minute)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	c.ListFields(ctx, "logs-*")
	c.ListFields(ctx, "logs-*")
	if inner.calls != 1 {
		t.Fatalf("cached: calls=%d", inner.calls)
	}
	inner.fields = []string{"service", "new_field"}
	if got := c.Refresh(ctx, "logs-*"); len(got) != 2 || inner.calls != 2 {
		t.Fatalf("refresh: %v calls=%d", got, inner.calls)
	}
	now = now.Add(6 * time.Minute)
	c.ListFields(ctx, "logs-*")
	if inner.calls != 3 {
		t.Fatalf("expired: calls=%d", inner.calls)
	}
	inner.fields = nil
	c.Refresh(ctx, "other")
	c.ListFields(ctx, "other")
	if inner.calls != 5 {
		t.Fatalf("empty results must not be cached: calls=%d", inner.calls)
	}
}

func TestESMapperCache_PerCluster(t *testing.T) {
	made := map[string]int{}
	mc := newESMapperCache(nil, func(cluster string) ESFieldMapper {
		made[cluster]++
		return &countingMapper{fields: []string{cluster}}
	}, time.Minute)
	if mc.For("a") != mc.For("a") || made["a"] != 1 {
		t.Fatalf("must reuse per cluster: %v", made)
	}
	if got := mc.For("b").ListFields(context.Background(), "x"); got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestCachedFieldMapper|TestESMapperCache" -count=1`
Expected: FAIL

- [ ] **Step 3: 实现**

```go
package tool

import (
	"context"
	"sync"
	"time"
)

const esMappingCacheTTL = 5 * time.Minute

// cachedFieldMapper 为 ListFields 加 TTL 缓存；空结果（mapping 拉取失败）不缓存。
type cachedFieldMapper struct {
	inner ESFieldMapper
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	lists map[string]cachedFieldList
}

type cachedFieldList struct {
	fields []string
	at     time.Time
}

func newCachedFieldMapper(inner ESFieldMapper, ttl time.Duration) *cachedFieldMapper {
	return &cachedFieldMapper{inner: inner, ttl: ttl, now: time.Now, lists: map[string]cachedFieldList{}}
}

func (m *cachedFieldMapper) Lookup(ctx context.Context, index, field string) (ESFieldMapping, bool) {
	return m.inner.Lookup(ctx, index, field)
}

func (m *cachedFieldMapper) ListFields(ctx context.Context, index string) []string {
	m.mu.Lock()
	c, ok := m.lists[index]
	m.mu.Unlock()
	if ok && m.now().Sub(c.at) < m.ttl {
		return c.fields
	}
	return m.Refresh(ctx, index)
}

// Refresh 绕过缓存重新拉取并写回（非空时）。
func (m *cachedFieldMapper) Refresh(ctx context.Context, index string) []string {
	fields := m.inner.ListFields(ctx, index)
	if len(fields) > 0 {
		m.mu.Lock()
		m.lists[index] = cachedFieldList{fields: fields, at: m.now()}
		m.mu.Unlock()
	}
	return fields
}

// esMapperCache 按集群持有带缓存的 mapper；fixed 非空时所有集群共用它（测试/显式注入）。
type esMapperCache struct {
	mu        sync.Mutex
	fixed     *cachedFieldMapper
	newMapper func(cluster string) ESFieldMapper
	ttl       time.Duration
	byClust   map[string]*cachedFieldMapper
}

func newESMapperCache(fixed ESFieldMapper, newMapper func(string) ESFieldMapper, ttl time.Duration) *esMapperCache {
	mc := &esMapperCache{newMapper: newMapper, ttl: ttl, byClust: map[string]*cachedFieldMapper{}}
	if fixed != nil {
		mc.fixed = newCachedFieldMapper(fixed, ttl)
	}
	return mc
}

func (mc *esMapperCache) For(cluster string) *cachedFieldMapper {
	if mc.fixed != nil {
		return mc.fixed
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if m, ok := mc.byClust[cluster]; ok {
		return m
	}
	inner := mc.newMapper(cluster)
	if inner == nil {
		return nil
	}
	m := newCachedFieldMapper(inner, mc.ttl)
	mc.byClust[cluster] = m
	return m
}
```

> `mapperFromReader(reader, cl.ID)` 返回 `ESFieldMapper` 接口，不支持 mapping 时为字面量 nil（`es_log_mapping.go:395`），可直接作为 `newMapper` 传入；`For` 返回 nil 时调用方按"无 mapper"处理。

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "TestCachedFieldMapper|TestESMapperCache" -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/es_field_cache.go framework/tool/es_field_cache_test.go
git commit -m "feat(tool): ES mapping 按集群 TTL 缓存"
```

---

### Task 8: `es_log_query` 接入字段校验与零结果探测

**Files:**
- Modify: `framework/tool/es_log_tool.go`（`RegisterESLogTool` 约 89-412 行）
- Modify: `framework/tool/es_log_mapping.go`（删除 `rewriteUnknownQueryFields` 及仅它使用的辅助函数）
- Test: `framework/tool/es_log_tool_test.go`、新建 `framework/tool/es_log_checks_test.go`

- [ ] **Step 1: 抽出 `buildESLogDSL`（纯重构）**

把 `Execute` 中 218-243 行"按 trace_id / query 生成 inner、组装 dslObj（size/from）、`parseESLogQueryOpts` + `qopts.apply`"抽成：

```go
// buildESLogDSL 按参数构造查询 DSL；size/from 由调用方传入。
func buildESLogDSL(params map[string]any, cl ESLogCluster, traceField string, size, from int) (map[string]any, esLogQueryOpts, error)
```

`Execute` 改为调用它，行为不变。

Run: `go test -p 1 ./tool/ -run "ESLog|EsLog|es_log" -count=1`
Expected: PASS（纯重构）

- [ ] **Step 2: 写失败测试** `es_log_checks_test.go`

```go
package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/executor"
)

func esCheckRegistry(t *testing.T, r *fakeReader, mapper ESFieldMapper) Tool {
	t.Helper()
	reg := newTestRegistry()
	err := RegisterESLogTool(reg, r, ESLogConfig{
		FieldMapper:  mapper,
		IndexCatalog: &memIndexCatalog{byPattern: map[string][]string{"logs-*": {"logs-2026.10.01"}}},
		Clusters:     []ESLogCluster{{ID: "es", DefaultIndex: "logs-*", BodyField: "message"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	return tl
}

func TestESLog_UnknownFieldRejectedBeforeExecute(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{}}
	tl := esCheckRegistry(t, r, mapFieldMapper{
		"service":    {Type: "keyword"},
		"message":    {Type: "text"},
		"@timestamp": {Type: "date"},
	})
	res, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "servcie:foo", "sort": "levle:desc"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) {
		t.Fatalf("want InvalidArgumentsError, got %v %+v", err, res)
	}
	if r.gotDSL != "" {
		t.Fatal("query must not be executed")
	}
	var sawQuery bool
	for _, e := range iae.Errors {
		if e.Keyword == KeywordUnknownField && e.Path == "query" && len(e.Candidates) > 0 && e.Candidates[0] == "service" && e.Hint != "" {
			sawQuery = true
		}
	}
	if !sawQuery {
		t.Fatalf("errors %+v", iae.Errors)
	}
}

func TestESLog_KnownFieldsExecute(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{Rows: [][]any{{"x"}}, Columns: []string{"_source"}}}
	tl := esCheckRegistry(t, r, mapFieldMapper{"service": {Type: "keyword"}, "@timestamp": {Type: "date"}})
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "service:foo"}); err != nil {
		t.Fatal(err)
	}
	if r.gotDSL == "" {
		t.Fatal("expected execution")
	}
}

func TestESLog_FreeTextHasNoFieldRefs(t *testing.T) {
	r := &fakeReader{result: &executor.QueryResult{Rows: [][]any{{"x"}}, Columns: []string{"_source"}}}
	tl := esCheckRegistry(t, r, mapFieldMapper{"service": {Type: "keyword"}})
	if _, err := tl.Execute(context.Background(), map[string]any{"cluster": "es", "query": "prestart failed"}); err != nil {
		t.Fatalf("free text must not be rejected: %v", err)
	}
}
```

再加一个探测用例：需要一个按 DSL 返回不同结果的 reader。追加：

```go
type scriptedReader struct {
	calls []string
	reply func(dsl string) *executor.QueryResult
}

func (s *scriptedReader) Query(_ context.Context, _ string, dsl string, _ executor.QueryOptions) (*executor.QueryResult, error) {
	s.calls = append(s.calls, dsl)
	return s.reply(dsl), nil
}

func TestESLog_EmptyProbeMarksSuspect(t *testing.T) {
	sr := &scriptedReader{reply: func(dsl string) *executor.QueryResult {
		if strings.Contains(dsl, `"size":0`) && !strings.Contains(dsl, "service") {
			return &executor.QueryResult{EstimatedTotal: 57}
		}
		return &executor.QueryResult{}
	}}
	reg := newTestRegistry()
	_ = RegisterESLogTool(reg, sr, ESLogConfig{
		FieldMapper:  mapFieldMapper{"service": {Type: "keyword"}, "level": {Type: "keyword"}, "@timestamp": {Type: "date"}},
		IndexCatalog: &memIndexCatalog{byPattern: map[string][]string{"logs-*": {"logs-1"}}},
		Clusters:     []ESLogCluster{{ID: "es", DefaultIndex: "logs-*"}},
	})
	tl, _ := reg.Get("es_log_query")
	res, err := tl.Execute(context.Background(), map[string]any{
		"cluster": "es", "query": "service:foo AND level:ERROR", "time_from": "now-1h", "time_to": "now",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := HitContractFromResult(res); st != HitStatusSuspect {
		t.Fatalf("status %q res %+v", st, res)
	}
	if d := DiagnosisFromResult(res); d == nil || len(d.Probes) == 0 {
		t.Fatalf("diag %+v", d)
	}
}
```

（`executor.Reader` 只有 `Query` 一个方法，`scriptedReader` 已满足。补 `strings` 导入。）

再追加 `TestESLogRelax`（表驱动）：无时间窗 + 单子句 → 空；无时间窗 + `a:1 AND b:2` → 仅两个 `without:` 变体；`time_from:"now-1h"` + 两子句 → 依次 `time_window_only`、`without:a:1`、`without:b:2`、`time_window_24h`；`time_from:"now-7d"` → 无 `time_window_24h`；带 `trace_id` → 空；`query:"{...}"` → 空。以及 `TestSplitTopLevelAND`：`(a AND b) AND c:"x AND y"` → `["(a AND b)", "c:\"x AND y\""]`。

- [ ] **Step 3: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestESLog_" -count=1`
Expected: FAIL（未知字段仍被执行；无 suspect）

- [ ] **Step 4: 实现**

在 `RegisterESLogTool` 中（Tool 字面量之前）：

```go
	mappers := newESMapperCache(cfg.FieldMapper, func(cluster string) ESFieldMapper {
		return mapperFromReader(reader, cluster)
	}, esMappingCacheTTL)

	resolveIndex := func(params map[string]any) (ESLogCluster, string, bool) {
		cl, ok := lookupCluster(clusters, stringParam(params, "cluster"))
		if !ok {
			return cl, "", false
		}
		index := cl.DefaultIndex
		if v := stringParam(params, "index"); v != "" {
			index = v
		}
		return cl, index, strings.TrimSpace(index) != ""
	}

	fieldCheck := FieldRefs{
		Label:   "es_fields",
		Extract: esLogFieldRefs,
		Fields: func(ctx context.Context, params map[string]any, refresh bool) ([]string, error) {
			cl, index, ok := resolveIndex(params)
			if !ok {
				return nil, nil
			}
			m := mappers.For(cl.ID)
			if m == nil {
				return nil, nil
			}
			if refresh {
				return m.Refresh(ctx, index), nil
			}
			return m.ListFields(ctx, index), nil
		},
		Known: func(field string, catalog []string) bool {
			return len(unknownQueryFields([]string{field}, catalog)) == 0
		},
		Similar: suggestSimilarMappedFields,
		BodyHint: func(params map[string]any) string {
			cl, _, _ := resolveIndex(params)
			body := cl.BodyField
			if body == "" {
				body = cfg.BodyField
			}
			if body == "" {
				return "for free-text search drop the field prefix (plain words search the log body)"
			}
			return fmt.Sprintf("for free-text search drop the field prefix or use %s:<text>", body)
		},
	}
```

字段抽取函数（文件级）。query_string 不复用 `parseLuceneQueryFields`：它会把 `url:http://x` 中值里的 `http` 当成字段，过去只用于改写，现在会导致误拒。

```go
var (
	luceneQuoted   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	luceneFieldRef = regexp.MustCompile(`(?:^|[\s(!+\-])([A-Za-z@][\w.@-]*)\s*:`)
)

// esLogFieldRefs 抽取参数中显式引用的字段：query_string 的 field: 前缀、agg_field、fields、
// sort 的 <field>:<order>、显式 time_field。trace_id 查询与 JSON body 查询不抽取；
// 含通配符或以 _ 开头的元字段（_id/_score/_doc）不校验。
func esLogFieldRefs(params map[string]any) []FieldRef {
	if stringParam(params, "trace_id") != "" {
		return nil
	}
	var refs []FieldRef
	add := func(param, field string) {
		field = strings.TrimSpace(field)
		if field == "" || strings.HasPrefix(field, "_") || strings.Contains(field, "*") {
			return
		}
		refs = append(refs, FieldRef{Param: param, Field: baseFieldName(field)})
	}
	if q := stringParam(params, "query"); q != "" && !strings.HasPrefix(q, "{") {
		for _, m := range luceneFieldRef.FindAllStringSubmatch(luceneQuoted.ReplaceAllString(q, `""`), -1) {
			add("query", m[1])
		}
	}
	add("agg_field", stringParam(params, "agg_field"))
	add("time_field", stringParam(params, "time_field"))
	if s := stringParam(params, "sort"); s != "" {
		if i := strings.LastIndex(s, ":"); i > 0 {
			add("sort", s[:i])
		}
	}
	if o, err := parseESLogQueryOpts(map[string]any{"fields": params["fields"]}, ""); err == nil {
		for _, f := range o.fields {
			add("fields", f)
		}
	}
	return refs
}
```

`luceneFieldRef` 要求字段名前是行首、空白、括号或 `!+-` 运算符，因此 `url:http://x` 只抽出 `url`。在 `es_log_checks_test.go` 追加表驱动用例 `TestESLogFieldRefs`：`"service:foo AND level:ERROR"`→`[service level]`；`"url:http://x"`→`[url]`；`"msg:\"a:b\""`→`[msg]`；`"prestart failed"`→空；`fields:["kubernetes.*","_id","host"]`→`[host]`；`sort:"_score:desc"`→空。

零结果探测：

```go
	probe := &EmptyProbe{
		Relax: esLogRelax,
		Count: func(ctx context.Context, v ProbeVariant) (int64, error) {
			cl, index, ok := resolveIndex(v.Params)
			if !ok {
				return 0, fmt.Errorf("no index")
			}
			traceField := firstNonEmpty(cl.TraceIDField, cfg.TraceIDField)
			dsl, _, err := buildESLogDSL(v.Params, cl, traceField, 0, 0)
			if err != nil {
				return 0, err
			}
			delete(dsl, "aggs")
			delete(dsl, "aggregations")
			delete(dsl, "sort")
			dsl["track_total_hits"] = true
			b, err := json.Marshal(dsl)
			if err != nil {
				return 0, err
			}
			res, err := reader.Query(ctx, cl.ID, string(b), executor.QueryOptions{MaxRows: 0, Extras: map[string]any{"index": index}})
			if err != nil {
				return 0, err
			}
			return int64(totalFromResult(res)), nil
		},
	}
```

（`firstNonEmpty` 若不存在则内联；traceField 的取法与 `Execute` 207-214 行保持一致。）

```go
// esLogRelax 生成放宽变体（有序）：只保留时间窗、逐个去掉顶层 AND 子句、时间窗放宽到 24h。
func esLogRelax(params map[string]any) []ProbeVariant {
	if stringParam(params, "trace_id") != "" {
		return nil
	}
	q := stringParam(params, "query")
	if strings.HasPrefix(q, "{") {
		return nil
	}
	hasWindow := stringParam(params, "time_from") != "" || stringParam(params, "time_to") != ""
	with := func(mut func(p map[string]any)) map[string]any {
		p := make(map[string]any, len(params))
		for k, v := range params {
			p[k] = v
		}
		mut(p)
		return p
	}
	var out []ProbeVariant
	if hasWindow && q != "" {
		out = append(out, ProbeVariant{Label: "time_window_only", Params: with(func(p map[string]any) { p["query"] = "*" })})
	}
	clauses := splitTopLevelAND(q)
	if len(clauses) > 1 {
		for i, c := range clauses {
			rest := append(append([]string{}, clauses[:i]...), clauses[i+1:]...)
			out = append(out, ProbeVariant{Label: "without:" + c, Params: with(func(p map[string]any) { p["query"] = strings.Join(rest, " AND ") })})
		}
	}
	if relativeWindowUnder24h(stringParam(params, "time_from")) {
		out = append(out, ProbeVariant{Label: "time_window_24h", Params: with(func(p map[string]any) { p["time_from"] = "now-24h"; delete(p, "time_to") })})
	}
	return out
}

// es_log_tool.go 需导入 regexp、strconv、time。
var relativeNow = regexp.MustCompile(`^now-(\d+)([smh])$`)

// relativeWindowUnder24h 仅对 now-<N><s|m|h> 且短于 24h 的起点返回 true；绝对时间或更长窗口不放宽，避免反而收窄/平移窗口。
func relativeWindowUnder24h(from string) bool {
	m := relativeNow.FindStringSubmatch(strings.TrimSpace(from))
	if m == nil {
		return false
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour}[m[2]]
	return time.Duration(n)*unit < 24*time.Hour
}

// splitTopLevelAND 按顶层 AND（大写，括号与引号外）切分 query_string。
func splitTopLevelAND(q string) []string {
	var parts []string
	depth, inQuote, start := 0, false, 0
	for i := 0; i < len(q); i++ {
		switch c := q[i]; {
		case c == '"' && (i == 0 || q[i-1] != '\\'):
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(q[i:], " AND "):
			parts = append(parts, strings.TrimSpace(q[start:i]))
			start = i + len(" AND ")
			i += len(" AND ") - 1
		}
	}
	parts = append(parts, strings.TrimSpace(q[start:]))
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

`Tool` 字面量加：`ArgChecks: []ArgCheck{fieldCheck}, EmptyProbe: probe,`。

`Execute` 中：
- `mapper` 改为 `mapper := ESFieldMapper(nil); if m := mappers.For(cl.ID); m != nil { mapper = m }`（替换 207-214 行对 `cfg.FieldMapper`/`mapperFromReader` 的直接使用）。
- 删除空命中分支里 unknown 字段相关代码（`names := collectQueryFieldNames…` 到 `rewriteUnknownQueryFields` 的 if 块，约 288-307 行），保留 `fields := lookupQueryFields(...)` 与 `rewriteEmptyHitQuery` 重查。
- 删除 395-401 行 `unknown_fields`/`similar_fields`/`mapping_error` 写入及对应变量声明。
- 工具描述（约 109 行）中 hit_status 说明追加：`suspect = 0 hits but relaxed probes found data (see diagnosis); fix the condition before concluding there is no data.`

`es_log_mapping.go`：删除 `rewriteUnknownQueryFields` 与仅被它使用的辅助函数（用 `rg -n "rewriteUnknownQueryFields|<helper>" framework/tool` 确认无其他引用）；`unknownFieldsNote` 若无引用一并删除。

- [ ] **Step 5: 更新既有测试**

Run: `go test -p 1 ./tool/ -count=1`
既有用例里断言 `unknown_fields`/`similar_fields`/`mapping_error`/`rewriteUnknownQueryFields` 的，改为断言执行前返回 `InvalidArgumentsError`（keyword=`unknown_field`），或删除仅覆盖已删函数的用例。`QuerySpillStub` 的 `UnknownFields`/`SimilarFields`/`MappingError` 字段保留（spill 历史数据兼容），不再写入。
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add framework/tool/es_log_tool.go framework/tool/es_log_mapping.go framework/tool/es_log_checks_test.go framework/tool/es_log_tool_test.go framework/tool/es_log_mapping_test.go
git commit -m "feat(tool): es_log_query 执行前校验字段并对零结果探测，移除空结果后补救的未知字段分支"
```

---

### Task 9: `execute_read` 接入（含 Reader-only 修复）

**Files:**
- Create: `framework/tool/data/sql_refs.go`, `framework/tool/data/sql_refs_test.go`
- Modify: `framework/tool/data/execute_read.go`
- Modify: `framework/tool/data/datasource_id.go`（新增候选源）
- Test: `framework/tool/data/execute_read_test.go`

- [ ] **Step 1: 写 SQL 解析失败测试** `sql_refs_test.go`

```go
package tooldata

import (
	"reflect"
	"testing"
)

func TestParseSingleTableSQL(t *testing.T) {
	cases := []struct {
		sql     string
		table   string
		cols    []string
		hasCond bool
		ok      bool
	}{
		{"SELECT id, state FROM vm_assign WHERE state = 3 ORDER BY id DESC LIMIT 10", "vm_assign", []string{"id", "state"}, true, true},
		{"select * from `pool_config` where image_version like '1.%'", "pool_config", []string{"image_version"}, true, true},
		{"SELECT COUNT(*) FROM t WHERE a.b = 1 AND c IN (1,2) AND d IS NULL", "t", []string{"b", "c", "d"}, true, true},
		{"SELECT name FROM db1.users", "db1.users", []string{"name"}, false, true},
		{"SELECT state, COUNT(*) cnt FROM t GROUP BY state ORDER BY cnt DESC", "t", []string{"state"}, false, true},
		{"SELECT state AS s FROM t ORDER BY s", "t", nil, false, true},
		{"SELECT a FROM t1 JOIN t2 ON t1.id = t2.id", "", nil, false, false},
		{"SELECT a FROM (SELECT a FROM t) x", "", nil, false, false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", "", nil, false, false},
		{"SHOW TABLES", "", nil, false, false},
		{"SELECT a FROM t WHERE note = 'x = y'", "t", []string{"a", "note"}, true, true},
	}
	for _, tc := range cases {
		got, ok := parseSingleTableSQL(tc.sql)
		if ok != tc.ok {
			t.Fatalf("%q ok=%v", tc.sql, ok)
		}
		if !ok {
			continue
		}
		if got.Table != tc.table || !reflect.DeepEqual(got.Columns, tc.cols) || got.HasCondition != tc.hasCond {
			t.Errorf("%q => %+v", tc.sql, got)
		}
	}
}

func TestNonSQLReason(t *testing.T) {
	for _, s := range []string{"level:ERROR AND service:foo", `{"query":{"match_all":{}}}`, "service:foo"} {
		if _, hit := nonSQLReason(s); !hit {
			t.Errorf("%q should be rejected", s)
		}
	}
	for _, s := range []string{"SELECT 1", "  show tables", "DESC t", "WITH x AS (SELECT 1) SELECT * FROM x", "(SELECT 1)", "explain select 1",
		"/* q1 */ SELECT a FROM t WHERE x = 1 AND y = 2", "-- note\nSELECT a FROM t WHERE x = 1 AND y = 2"} {
		if r, hit := nonSQLReason(s); hit {
			t.Errorf("%q wrongly rejected: %s", s, r)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/data/ -run "TestParseSingleTableSQL|TestNonSQLReason" -count=1`
Expected: FAIL

- [ ] **Step 3: 实现 `sql_refs.go`**

```go
package tooldata

import (
	"regexp"
	"strings"
)

// sqlRefs.Table 保留原始限定名（如 db1.users）；Qualified 时跳过表结构校验（默认库的 schema 不适用）。
type sqlRefs struct {
	Table        string
	Qualified    bool
	Columns      []string
	HasCondition bool
}

var (
	sqlLeadComment = regexp.MustCompile(`(?s)^\s*(/\*.*?\*/|--[^\n]*\n)\s*`)
	sqlLead       = regexp.MustCompile(`(?is)^\s*(\(|select|show|desc|describe|explain|with)\b`)
	sqlSelectAlias = regexp.MustCompile(`(?i)^.+?\s+(?:as\s+)?([A-Za-z_]\w*)$`)
	luceneField   = regexp.MustCompile(`^[\w.@-]+:\S`)
	sqlStringLit  = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
	sqlSingle     = regexp.MustCompile("(?is)^\\s*select\\s+(.+?)\\s+from\\s+([`\\w.]+)(?:\\s+(?:as\\s+)?(\\w+))?\\s*(?:(where|order\\s+by|group\\s+by|limit)\\b(.*))?;?\\s*$")
	sqlCondColumn = regexp.MustCompile("(?i)([`\\w.]+)\\s*(?:=|!=|<>|>=|<=|>|<|\\s(?:not\\s+)?like\\s|\\s(?:not\\s+)?in\\s*\\(|\\sis\\s|\\sbetween\\s)")
	sqlIdent      = regexp.MustCompile("^`?([A-Za-z_][\\w]*)`?$")
	sqlKeywords   = map[string]struct{}{"and": {}, "or": {}, "not": {}, "null": {}, "where": {}, "by": {}}
)

// nonSQLReason 判断 dsl 是否明显不是 SQL（Lucene 或 ES JSON DSL）。
func nonSQLReason(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	if strings.HasPrefix(t, "{") {
		return "looks like Elasticsearch JSON DSL, not SQL", true
	}
	for sqlLeadComment.MatchString(t) {
		t = sqlLeadComment.ReplaceAllString(t, "")
	}
	if sqlLead.MatchString(t) {
		return "", false
	}
	if luceneField.MatchString(t) || strings.Contains(t, " AND ") || strings.Contains(t, " OR ") {
		return "looks like a Lucene/query_string expression, not SQL", true
	}
	return "", false
}

// parseSingleTableSQL 只识别单表 SELECT（无 JOIN/子查询/CTE），返回表名与 SELECT/WHERE/ORDER BY 中可识别的列名。
// 无法确定时返回 ok=false，调用方应放行。
func parseSingleTableSQL(sql string) (sqlRefs, bool) {
	clean := sqlStringLit.ReplaceAllString(sql, "''")
	low := strings.ToLower(clean)
	if strings.Contains(low, " join ") || strings.Count(low, "select") != 1 {
		return sqlRefs{}, false
	}
	m := sqlSingle.FindStringSubmatch(clean)
	if m == nil {
		return sqlRefs{}, false
	}
	table := strings.ReplaceAll(m[2], "`", "")
	refs := sqlRefs{Table: table, Qualified: strings.Contains(table, ".")}
	seen := map[string]struct{}{}
	for _, part := range strings.Split(m[1], ",") {
		if am := sqlSelectAlias.FindStringSubmatch(strings.TrimSpace(part)); am != nil {
			seen[am[1]] = struct{}{}
		}
	}
	add := func(raw string) {
		raw = strings.Trim(raw, "`")
		if i := strings.LastIndex(raw, "."); i >= 0 {
			raw = raw[i+1:]
		}
		id := sqlIdent.FindStringSubmatch(raw)
		if id == nil {
			return
		}
		name := id[1]
		if _, kw := sqlKeywords[strings.ToLower(name)]; kw {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		refs.Columns = append(refs.Columns, name)
	}
	for _, part := range strings.Split(m[1], ",") {
		p := strings.TrimSpace(part)
		if p == "*" || strings.ContainsAny(p, "() ") {
			continue
		}
		add(p)
	}
	if tail := m[5]; tail != "" {
		refs.HasCondition = strings.EqualFold(strings.Fields(m[4])[0], "where")
		for _, cm := range sqlCondColumn.FindAllStringSubmatch(m[4]+tail, -1) {
			add(cm[1])
		}
		if ob := regexp.MustCompile(`(?i)order\s+by\s+([` + "`" + `\w.]+)`).FindStringSubmatch(tail); ob != nil {
			add(ob[1])
		}
	}
	return refs, true
}
```

> SELECT 别名预先放进 `seen`，因此 `ORDER BY cnt`、`ORDER BY s` 不会被当成列；`state AS s` 这一项因含空格不作为列抽取（保守放行）。期望列表按"SELECT 列 → WHERE 列 → ORDER BY 列"首次出现顺序去重。

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/data/ -run "TestParseSingleTableSQL|TestNonSQLReason" -count=1`
Expected: PASS

- [ ] **Step 5: 写 execute_read 集成失败测试**（追加到 `execute_read_test.go`，沿用文件里的 `fakeExecutor`；数据源注册表用 `datasource.NewRegistry()` + 注册 mysql/mongo 数据源，按文件中现有构造方式）

覆盖以下用例（每个一个 `t.Run`）：
1. `query` 别名：`{"query":"SELECT 1","datasource_id":"mysql1"}` 正常执行。
2. 缺 `dsl` 与 `query`：返回 `InvalidArgumentsError`，keyword=`required`。
3. mysql 数据源 + `dsl:"level:ERROR AND x:y"`：keyword=`reject`，hint 含 `es_log_query`，未执行。
4. mysql 数据源 + 传 `index:"logs-*"`：keyword=`reject`。
5. mongodb 数据源 + `dsl:"{\"find\":\"c\"}"`：**不**拒绝。
6. `datasource_id:"mysq1"`：keyword=`one_of`，候选含 `mysql1`；`datasource_id:"default"` 正常执行。
7. 仅配置 `Reader`、`Exec=nil`：可执行（修复 bug）。
8. 未知列：store 中 `vm_assign(id,state)`，`SELECT id, stat FROM vm_assign` → keyword=`unknown_field`，候选含 `state`；未知表 `SELECT id FROM vm_asign` → keyword=`unknown_field`，path=`dsl`，候选含 `vm_assign`。
9. 探测：`SELECT * FROM vm_assign WHERE state = 9` 返回 0 行，而 `SELECT COUNT(*)` 返回 `[[120]]` → `HitStatus=="suspect"`，`Diagnosis.Probes[0].Label=="table_total"`。

Run: `go test -p 1 ./tool/data/ -run TestExecuteRead -count=1`
Expected: FAIL

- [ ] **Step 6: 实现**

`datasource_id.go` 追加：

```go
// DatasourceIDs 返回注册表中的数据源 id；reg 为空返回空（规则不适用）。
func DatasourceIDs(reg *datasource.Registry) ([]string, error) {
	if reg == nil {
		return nil, nil
	}
	var ids []string
	for _, ds := range reg.List() {
		ids = append(ids, ds.ID())
	}
	return ids, nil
}

// DatasourceIDCheck 是数据源 id 的候选规则；先按 ResolveDatasourceID 解析 "default" 等别名。
func DatasourceIDCheck(reg *datasource.Registry, cfgDefault string) core.OneOf {
	return core.OneOf{
		Param:  "datasource_id",
		Source: func(context.Context, map[string]any) ([]string, error) { return DatasourceIDs(reg) },
		Normalize: func(params map[string]any, _ string) string {
			return ResolveDatasourceID(params, cfgDefault, reg)
		},
	}
}

func datasourceType(reg *datasource.Registry, id string) string {
	if reg == nil || id == "" {
		return ""
	}
	ds, err := reg.Get(id)
	if err != nil {
		return ""
	}
	return ds.Type()
}

func isSQLDatasource(typ string) bool {
	return typ == datasource.TypeMySQL || typ == datasource.TypeHive
}
```

（`core` 为 `github.com/sixath/framework/tool` 的别名，按本包既有导入方式；补 `context` 导入。）

`execute_read.go`：
1. schema：删除 `query`、`index` 属性；`dsl` 描述改为 `"SQL statement (MySQL/Hive) or the native query of the datasource; for Elasticsearch use es_log_query"`；`"required": []string{"dsl"}`。
2. `Tool` 字面量加 `ArgAliases: map[string]string{"query": "dsl"}`；`ArgChecks`/`EmptyProbe` **仅在 `cfg != nil` 时设置**（既有测试 `execute_read_test.go:155`、`describe_table_test.go:70`、`list_tables_test.go:139` 以 nil cfg 注册，不能在注册期或闭包里解引用 nil）。`cfg != nil` 时：

```go
	ArgChecks: []core.ArgCheck{
		DatasourceIDCheck(cfg.Registry, cfg.DefaultDatasourceID),
		core.Reject{
			Label:    "non_sql",
			Redirect: "for Elasticsearch logs use es_log_query (query_string / DSL go there)",
			Detect: func(params map[string]any) (string, string, bool) {
				id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
				if !isSQLDatasource(datasourceType(cfg.Registry, id)) {
					return "", "", false
				}
				if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
					return "index", "execute_read does not take an index (SQL datasource)", true
				}
				dsl, _ := params["dsl"].(string)
				if reason, hit := nonSQLReason(dsl); hit {
					return "dsl", reason, true
				}
				return "", "", false
			},
		},
		sqlSchemaCheck(cfg),
	},
	EmptyProbe: sqlEmptyProbe(cfg),
```

3. `buildExecuteReadExecute`：
   - 91 行改为 `if cfg == nil || (cfg.Reader == nil && cfg.Exec == nil) {`。
   - 删除对 `params["query"]` 的回退读取（别名已在中间件归一化），错误文案改为 `"execute_read: dsl is required and must be a string"`。
   - 删除 186-189 行 `params["index"]` 读取，`res.QueriedIndex` 不再设置。

4. 新增（同文件或 `sql_refs.go`）：

```go
// sqlSchemaCheck 对 MySQL 单表 SELECT 校验表名与列名；表结构来自 metadata store（未知时强制刷新一次）。
func sqlSchemaCheck(cfg *ExecuteReadConfig) core.ArgCheck {
	return sqlSchemaRule{cfg: cfg}
}

type sqlSchemaRule struct{ cfg *ExecuteReadConfig }

func (sqlSchemaRule) Name() string { return "sql_schema" }

func (r sqlSchemaRule) Check(ctx context.Context, params map[string]any) ([]core.SchemaError, error) {
	cfg := r.cfg
	if cfg == nil || cfg.Store == nil || cfg.Registry == nil {
		return nil, nil
	}
	id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
	if datasourceType(cfg.Registry, id) != datasource.TypeMySQL {
		return nil, nil
	}
	dsl, _ := params["dsl"].(string)
	refs, ok := parseSingleTableSQL(dsl)
	if !ok || refs.Qualified {
		return nil, nil
	}
	tables, err := r.tables(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, nil
	}
	tbl, found := findTable(tables, refs.Table)
	if !found || len(missingColumns(tbl, refs.Columns)) > 0 {
		if fresh, ferr := r.tables(ctx, id, true); ferr == nil {
			tables = fresh
			tbl, found = findTable(tables, refs.Table)
		}
	}
	if !found {
		names := make([]string, 0, len(tables))
		for _, t := range tables {
			names = append(names, t.Name)
		}
		return []core.SchemaError{{
			Path: "dsl", Keyword: core.KeywordUnknownField,
			Message:    fmt.Sprintf("table %q does not exist in datasource %q", refs.Table, id),
			Candidates: core.Suggest(refs.Table, names, 3),
			Hint:       "use list_tables / describe_table to find the right table",
		}}, nil
	}
	if len(tbl.Columns) == 0 {
		return nil, nil
	}
	cols := make([]string, 0, len(tbl.Columns))
	for _, c := range tbl.Columns {
		cols = append(cols, c.Name)
	}
	var errs []core.SchemaError
	for _, c := range missingColumns(tbl, refs.Columns) {
		errs = append(errs, core.SchemaError{
			Path: "dsl", Keyword: core.KeywordUnknownField,
			Message:    fmt.Sprintf("column %q does not exist in table %q", c, tbl.Name),
			Candidates: core.Suggest(c, cols, 3),
			Hint:       "use describe_table " + tbl.Name + " to see columns",
		})
	}
	return errs, nil
}

func (r sqlSchemaRule) tables(ctx context.Context, id string, refresh bool) ([]metadata.Table, error) {
	var (
		s   *metadata.Schema
		err error
	)
	if refresh {
		s, err = metadata.RefreshFromRegistry(ctx, r.cfg.Registry, r.cfg.Store, id)
	} else {
		s, err = metadata.EnsureSchemaForDatasource(ctx, r.cfg.Registry, r.cfg.Store, id)
	}
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return s.Tables, nil
}

func findTable(tables []metadata.Table, name string) (metadata.Table, bool) {
	for _, t := range tables {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return metadata.Table{}, false
}

func missingColumns(tbl metadata.Table, refs []string) []string {
	if len(tbl.Columns) == 0 {
		return nil
	}
	var out []string
	for _, r := range refs {
		found := false
		for _, c := range tbl.Columns {
			if strings.EqualFold(c.Name, r) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, r)
		}
	}
	return out
}

func quoteSQLTable(t string) string {
	parts := strings.Split(t, ".")
	for i, p := range parts {
		parts[i] = "`" + p + "`"
	}
	return strings.Join(parts, ".")
}

// sqlEmptyProbe 对 SQL 单表带条件查询的 0 行结果做一次主表 COUNT(*)。
func sqlEmptyProbe(cfg *ExecuteReadConfig) *core.EmptyProbe {
	return &core.EmptyProbe{
		Relax: func(params map[string]any) []core.ProbeVariant {
			if cfg == nil {
				return nil
			}
			id := ResolveDatasourceID(params, cfg.DefaultDatasourceID, cfg.Registry)
			if !isSQLDatasource(datasourceType(cfg.Registry, id)) {
				return nil
			}
			dsl, _ := params["dsl"].(string)
			refs, ok := parseSingleTableSQL(dsl)
			if !ok || !refs.HasCondition {
				return nil
			}
			return []core.ProbeVariant{{Label: "table_total", Params: map[string]any{
				"datasource_id": id,
				"dsl":           "SELECT COUNT(*) FROM " + quoteSQLTable(refs.Table),
			}}}
		},
		Count: func(ctx context.Context, v core.ProbeVariant) (int64, error) {
			reader := executor.CoalesceReader(cfg.Reader, cfg.Exec)
			if reader == nil {
				return 0, errors.New("reader not configured")
			}
			id, _ := v.Params["datasource_id"].(string)
			dsl, _ := v.Params["dsl"].(string)
			res, err := reader.Query(ctx, id, dsl, executor.QueryOptions{MaxRows: 1})
			if err != nil {
				return 0, err
			}
			if res == nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
				return 0, errors.New("empty count result")
			}
			return toInt64(res.Rows[0][0])
		},
	}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case []byte:
		return strconv.ParseInt(string(x), 10, 64)
	case string:
		return strconv.ParseInt(x, 10, 64)
	}
	return 0, fmt.Errorf("unexpected count type %T", v)
}
```

（`metadata.Schema` 的 Tables 字段若为 `[]*Table` 或其他形状，按 `framework/metadata/types.go` 实际类型调整；Hive 不做表结构校验但做探测。）

- [ ] **Step 7: 运行确认通过**

Run: `go test -p 1 ./tool/data/ -count=1`
Expected: PASS（既有用例中依赖 `query` 回退或 `index` 的断言按新行为更新）

- [ ] **Step 8: 提交**

```bash
git add framework/tool/data/
git commit -m "feat(tool): execute_read 校验 SQL（别名/非 SQL 拦截/数据源候选/表列校验/零结果探测），修复仅配 Reader 报未配置"
```

---

### Task 10: `describe_table` / `list_tables` 数据源候选

**Files:**
- Modify: `framework/tool/data/describe_table.go`, `framework/tool/data/list_tables.go`
- Test: `framework/tool/data/describe_table_test.go`（或现有测试文件）

- [ ] **Step 1: 写失败测试**：`describe_table` 传 `datasource_id:"mysq1"`（注册表中有 `mysql1`）→ `InvalidArgumentsError`，keyword=`one_of`，候选含 `mysql1`；`list_tables` 同理；`"default"` 正常。

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/data/ -run "DescribeTable|ListTables" -count=1`

- [ ] **Step 3: 实现**：两个工具在 `cfg != nil` 时设置 `ArgChecks: []core.ArgCheck{DatasourceIDCheck(cfg.Registry, cfg.DefaultDatasourceID)}`；`cfg == nil` 时不设置（既有测试以 nil cfg 注册）。

- [ ] **Step 4: 运行确认通过**，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/data/describe_table.go framework/tool/data/list_tables.go framework/tool/data/*_test.go
git commit -m "feat(tool): describe_table/list_tables 数据源 id 候选校验"
```

---

### Task 11: `jaeger_trace` 格式校验、`http_request` 默认 GET

**Files:**
- Modify: `framework/tool/jaeger_tool.go`, `framework/tool/http_tool.go:53-70`
- Test: `framework/tool/jaeger_tool_test.go`, `framework/tool/http_tool_test.go`

- [ ] **Step 1: 写失败测试**

`jaeger_tool_test.go` 追加：

```go
func TestJaegerTrace_RejectsMalformedTraceID(t *testing.T) {
	reg := newTestRegistry()
	if err := RegisterJaegerTool(reg, "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("jaeger_trace")
	_, err := tl.Execute(context.Background(), map[string]any{"trace_id": "req-123"})
	var iae *InvalidArgumentsError
	if !errors.As(err, &iae) || iae.Errors[0].Keyword != KeywordPattern {
		t.Fatalf("got %v", err)
	}
}
```

`http_tool_test.go` 追加：用现有 httptest 服务器，调用参数只给 `url`，断言服务器收到 `GET`。

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "TestJaegerTrace_RejectsMalformedTraceID|HTTP" -count=1`

- [ ] **Step 3: 实现**

`jaeger_tool.go` 的 Tool 字面量加：

```go
	ArgChecks: []ArgCheck{Pattern{
		Param: "trace_id",
		Regex: regexp.MustCompile(`^([0-9a-fA-F]{16}|[0-9a-fA-F]{32})$`),
		Hint:  "trace_id must be 16 or 32 hex characters (copy it from a log line's trace field)",
	}},
```

`http_tool.go`：`"required": []string{"url"}`；删除 `if rawMethod == "" { return nil, errors.New(...) }`，保留后面的 GET 默认值。

`effect.go:86-87`：`HTTPMethodEffect` 目前对空 method 返回 `EffectUnknown`（回退到工具静态 `EffectWrite`，会触发写操作审批）。改为空 method 按 GET 处理返回 `EffectRead`，并在 `effect_test.go`（或现有 HTTPMethodEffect 测试）加用例 `HTTPMethodEffect(map[string]any{"url": "http://x"}) == EffectRead`。

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "Jaeger|HTTP|Http" -count=1`，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/jaeger_tool.go framework/tool/jaeger_tool_test.go framework/tool/http_tool.go framework/tool/http_tool_test.go framework/tool/effect.go framework/tool/effect_test.go
git commit -m "feat(tool): jaeger_trace 校验 trace_id 格式；http_request method 缺省 GET"
```

---

### Task 12: RCA 工具与 `search_files`

**Files:**
- Modify: `framework/tool/rca_code_tools.go`, `framework/tool/rca_repos.go`, `framework/tool/file_tools.go`（`search_files`，约 467-546 行）
- Test: `framework/tool/rca_code_tools_test.go`, `framework/tool/file_tools_test.go`

- [ ] **Step 1: 写失败测试**（用 `t.TempDir()` 建 `repoA/main.go`、`repoA/internal/handler.go`）

1. `rca_grep` 传 `repo:"repoa"`（实际 `repoA`）→ `InvalidArgumentsError`，keyword=`one_of`，候选 `repoA`。
2. `rca_read` 读 `internal/handlr.go` → 结果含 `similar`，首个为 `internal/handler.go`。
3. `rca_glob` 配置的 root 不存在 → `hit_status=="suspect"`，结果含 `roots_missing`。
4. `rca_glob` root 存在但 0 命中 → `hit_status=="empty"`；有命中 → `hits`。
5. `search_files` 的 `path` 指向不存在目录 → `hit_status=="suspect"`、`root_missing==true`；存在但 0 命中 → `empty`。

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run "RCA|Rca|SearchFiles" -count=1`

- [ ] **Step 3: 实现**

`rca_repos.go` 追加：

```go
// repoCheck 是 rca_* 工具 repo 参数的候选规则。
func repoCheck(roots []string) OneOf {
	return OneOf{
		Param: "repo",
		Source: func(context.Context, map[string]any) ([]string, error) {
			return repoNames(roots), nil
		},
		Hint: "omit repo to search all configured repositories",
	}
}

const rcaSimilarScanLimit = 2000

// suggestRCAFiles 为不存在的相对路径给出候选：先同目录按文件名，再在仓库内按文件名（遍历至多 2000 个文件）。
func suggestRCAFiles(root, rel string) []string {
	base := filepath.Base(rel)
	dir := filepath.Join(root, filepath.Dir(rel))
	if entries, err := os.ReadDir(dir); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if s := Suggest(base, names, 3); len(s) > 0 {
			out := make([]string, len(s))
			for i, n := range s {
				out[i] = filepath.ToSlash(filepath.Join(filepath.Dir(rel), n))
			}
			return out
		}
	}
	var paths, names []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= rcaSimilarScanLimit {
			return filepath.SkipAll
		}
		r, _ := filepath.Rel(root, p)
		paths = append(paths, filepath.ToSlash(r))
		names = append(names, d.Name())
		return nil
	})
	var out []string
	for _, n := range Suggest(base, names, 3) {
		for i, nm := range names {
			if nm == n {
				out = append(out, paths[i])
				break
			}
		}
	}
	return out
}
```

`rca_code_tools.go`：
- `rca_grep`、`rca_glob`、`rca_read`、（`rca_symbol_tool.go` 的 `rca_symbol` 若有 `repo` 参数）的 Tool 字面量加 `ArgChecks: []ArgCheck{repoCheck(roots)}`。
- 修正这些工具 schema 中 `repo` 的描述为：`"Optional repository name (one of the configured repos); omit to search all."`。
- `rca_read` 文件不存在分支：在 payload 中加 `"similar": suggestRCAFiles(root, file)`（`root` 取 `resolveInRepos` 返回值）。
- `rca_glob`：`searchFilesByGlob` 对不存在的根返回 walk 错误（`file_tools.go:882-885`），原代码会走 `rcaErrFrom`。因此在 sel 循环（约 159 行）**开头**先检查并跳过缺失的根：

```go
			var missing []string
			for _, root := range sel {
				if st, err := os.Stat(root); err != nil || !st.IsDir() {
					missing = append(missing, repoNameFromRoot(root))
					continue
				}
				// ...原有 searchFilesByGlob 调用与累加...
			}
```

  结尾替换为：

```go
			payload := map[string]any{"matches": matches, "truncated": truncated, "roots": repoNames(sel)}
			status := HitStatusFromCount(true, len(matches))
			if len(matches) == 0 {
				payload["hint"] = "No matches. Try basename (go.mod) / path (**/go.mod)."
				if len(missing) > 0 {
					payload["roots_missing"] = missing
					status = HitStatusSuspect
				}
			}
			payload = StampHitContract(payload, HitStamp{Status: status, Tool: toolName, Ctx: ctx})
			return rcaOK(toolName, payload), nil
```

`file_tools.go` 的 `search_files`：在 `root, err := fwws.ResolveWorkspacePath(ws, searchPath)` 之后：

```go
			rootMissing := false
			if st, statErr := os.Stat(root); statErr != nil || !st.IsDir() {
				rootMissing = true
			}
```

`rootMissing` 为 true 时**不调用** `searchFileContents`/`searchFilesByGlob`（它们会因根不存在报错），直接以空结果走下面的返回。两个返回分支统一改为：

```go
			out := map[string]any{"target": target, "matches": results, "root": searchPath}
			status := HitStatusFromCount(true, len(results))
			if len(results) == 0 && rootMissing {
				out["root_missing"] = true
				status = HitStatusSuspect
			}
			return StampHitContract(out, HitStamp{Status: status, Tool: "search_files", Ctx: ctx}), nil
```

（`target` 与 `results` 变量名按现有代码；`len(results)` 需对 content/files 两种切片分别取。`searchPath` 为空时写 `"."`。）

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -count=1`，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/rca_code_tools.go framework/tool/rca_repos.go framework/tool/rca_symbol_tool.go framework/tool/file_tools.go framework/tool/*_test.go
git commit -m "feat(tool): rca repo 候选、rca_read 相似路径、glob/search_files 标记搜索根缺失为 suspect"
```

---

### Task 13: 技能工具名称候选

**Files:**
- Modify: `framework/tool/skillops/skill_tools.go`（`load_skill` 约 85 行、`read_skill_file` 约 137 行、`skill_view` 约 424 行）、`framework/tool/skillops/skill_manager_tool.go`（约 54 行）
- Test: `framework/tool/skillops/skill_tools_test.go`

- [ ] **Step 1: 写失败测试**：索引中有 `rca-flow`、`vm-pool`；`load_skill` 传 `name:"rca-flw"` → keyword=`one_of`，候选 `rca-flow`；`skill_view`、`read_skill_file` 同理；`skill_manage` `action:"create", name:"brand-new"` 不被拒；`action:"edit", name:"vm-pol"` 被拒。

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/skillops/ -count=1`

- [ ] **Step 3: 实现**：在 `skill_tools.go` 新增：

```go
func skillNameCheck(idx func() *skills.Index, when func(map[string]any) bool) tool.OneOf {
	return tool.OneOf{
		Param: "name",
		When:  when,
		Source: func(context.Context, map[string]any) ([]string, error) {
			ix := idx()
			if ix == nil {
				return nil, nil
			}
			var names []string
			for _, m := range ix.All() {
				names = append(names, m.Name)
			}
			return names, nil
		},
		Hint: "use skills_list to see available skills",
	}
}
```

各 Tool 字面量加 `ArgChecks: []tool.ArgCheck{skillNameCheck(func() *skills.Index { return idx }, nil)}`；`skill_manage` 用 `func() *skills.Index { if cfg == nil { return nil }; return cfg.Index }` 与 `when: func(p map[string]any) bool { a, _ := p["action"].(string); return a != "" && a != "create" }`。（包内对 tool 包的导入别名以文件现有写法为准。）

- [ ] **Step 4: 运行确认通过**，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/skillops/
git commit -m "feat(tool): 技能工具 name 参数候选校验"
```

---

### Task 14: MCP schema 归一化

**Files:**
- Modify: `framework/tool/mcp.go:464-468`, `framework/tool/mcp.go:599-603`
- Test: `framework/tool/mcp_schema_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package tool

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNormalizeMCPSchema(t *testing.T) {
	s := mcp.ToolInputSchema{Type: "object", Properties: map[string]any{"q": map[string]any{"type": "string"}}, Required: []string{"q"}}
	got, ok := normalizeMCPSchema(s, nil).(map[string]any)
	if !ok || got["type"] != "object" {
		t.Fatalf("got %#v", got)
	}
	if err := ValidateArguments("m", got, map[string]any{}); err == nil {
		t.Fatal("required must be enforced after normalization")
	}
	raw := []byte(`{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}`)
	if m, _ := normalizeMCPSchema(s, raw).(map[string]any); m["required"] == nil {
		t.Fatalf("raw schema must take precedence: %#v", m)
	}
	already := map[string]any{"type": "object"}
	if normalizeMCPSchema(already, nil).(map[string]any)["type"] != "object" {
		t.Fatal("map passthrough")
	}
}
```

（`mcp.ToolInputSchema` 的字段名以 `mark3labs/mcp-go v0.43.2` 为准：`rg -n "type ToolInputSchema|type ToolArgumentsSchema" -A8 $(go env GOMODCACHE)/github.com/mark3labs/mcp-go@v0.43.2/mcp/tools.go`。）

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./tool/ -run TestNormalizeMCPSchema -count=1`

- [ ] **Step 3: 实现**（`mcp.go` 末尾）

```go
// normalizeMCPSchema 把 MCP 工具的 InputSchema（结构体或任意 JSON 值）转为 map[string]any，
// 使 ValidateArguments 能识别；raw（RawInputSchema）非空时优先。失败时原样返回（校验 fail-open）。
func normalizeMCPSchema(schema any, raw json.RawMessage) any {
	if len(raw) > 0 {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil && len(m) > 0 {
			return m
		}
	}
	if m, ok := schema.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return schema
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || len(m) == 0 {
		return schema
	}
	return m
}
```

第 467 行改为 `Parameters: normalizeMCPSchema(t.InputSchema, nil),`；第 602 行改为 `Parameters: normalizeMCPSchema(t.InputSchema, t.RawInputSchema),`。

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./tool/ -run "MCP|Mcp" -count=1`，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/mcp.go framework/tool/mcp_schema_test.go
git commit -m "fix(tool): MCP 工具 schema 归一化为 map，使入参校验生效"
```

---

### Task 15: C1 参数 JSON 解析错误回显必填参数

**Files:**
- Modify: `framework/harness/react_agent.go:1070-1083`
- Test: `framework/harness/react_agent_args_hint_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestArgsParseErrorHint(t *testing.T) {
	reg := tool.NewEmptyRegistry()
	_ = reg.Register(tool.Tool{
		Name: "execute_read",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"dsl": map[string]any{"type": "string"}}, "required": []any{"dsl"}},
		Execute: func(context.Context, map[string]any) (any, error) { return nil, nil },
	})
	if got := argsParseErrorHint(reg, "execute_read", ""); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("direct: %q", got)
	}
	if got := argsParseErrorHint(reg, tool.ToolCallName, `{"name":"execute_read","arguments":{"dsl":"SEL`); !strings.Contains(got, "required: dsl(string)") {
		t.Fatalf("tool_call wrapper: %q", got)
	}
	if got := argsParseErrorHint(reg, "nope", ""); !strings.Contains(got, "valid JSON object") {
		t.Fatalf("unknown: %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./harness/ -run TestArgsParseErrorHint -count=1`

- [ ] **Step 3: 实现**

```go
var (
	toolCallLeadingName = regexp.MustCompile(`^\s*\{\s*"name"\s*:\s*"([^"]+)"`)
	toolCallAnyName     = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
)

// argsParseErrorHint 为参数 JSON 解析失败补充该工具的参数摘要；tool_call 包装时从原始参数中提取内层工具名
// （优先取对象开头的顶层 "name"，否则取首个 "name" 且须是已注册工具）。
func argsParseErrorHint(reg *tool.Registry, name, rawPreview string) string {
	const generic = "; arguments must be a valid JSON object"
	if name == tool.ToolCallName {
		m := toolCallLeadingName.FindStringSubmatch(rawPreview)
		if m == nil {
			m = toolCallAnyName.FindStringSubmatch(rawPreview)
		}
		if m == nil {
			return generic
		}
		name = m[1]
	}
	if reg == nil {
		return generic
	}
	t, ok := reg.Get(name)
	if !ok {
		return generic
	}
	if s := tool.RequiredArgsSummary(t.Parameters); s != "" {
		return generic + "; " + name + " " + s
	}
	return generic
}
```

1070-1071 行改为：

```go
	if call.RawArgumentsParseError != "" {
		record.Error = "invalid tool arguments json: " + call.RawArgumentsParseError + argsParseErrorHint(a.tools, call.Name, call.RawArgumentsPreview)
```

（`a.tools` 的类型若不是 `*tool.Registry`，按实际类型把 `argsParseErrorHint` 的第一个参数改成含 `Get(string) (tool.Tool, bool)` 的小接口。）

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./harness/ -count=1`，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/harness/react_agent.go framework/harness/react_agent_args_hint_test.go
git commit -m "feat(harness): 工具参数 JSON 解析失败时回显必填参数"
```

---

### Task 16: `suspect_evidence` stop hook 与注册

**Files:**
- Create: `framework/harness/suspect_stop_hook.go`, `framework/harness/suspect_stop_hook_test.go`
- Modify: `portal/internal/chat/agent_builder.go:482-501` 与 `530-545`、`evals/runner/live.go:80-90`、`framework/investigate/runner.go:59-65`

- [ ] **Step 1: 写失败测试**

```go
package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

func suspectRec(name string) ToolCallRecord {
	return ToolCallRecord{ToolName: name, Allowed: true, Result: map[string]any{
		"hit_status": tool.HitStatusSuspect,
		"diagnosis":  &executor.Diagnosis{Hint: "without:service:foo returned 57"},
	}}
}

func TestSuspectHook(t *testing.T) {
	h := NewSuspectEvidenceHook()
	ctx := context.Background()

	d := h.OnStop(ctx, StopHookInput{Text: "经查询，没有数据。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}})
	if !d.Continue || !strings.HasPrefix(d.RuleID, SuspectEvidenceRuleID) || !strings.Contains(d.Message, "without:service:foo returned 57") {
		t.Fatalf("got %+v", d)
	}
	if d := h.OnStop(ctx, StopHookInput{Text: "共找到 3 条记录。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}); d.Continue {
		t.Fatal("no negative claim")
	}
	if d := h.OnStop(ctx, StopHookInput{Text: "如果查不到，请扩大时间范围。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query")}}); d.Continue {
		t.Fatal("conditional sentence is exempt")
	}
	hits := ToolCallRecord{ToolName: "execute_read", Allowed: true, Result: map[string]any{"hit_status": tool.HitStatusHits}}
	if d := h.OnStop(ctx, StopHookInput{Text: "查不到相关日志。", ToolCalls: []ToolCallRecord{suspectRec("es_log_query"), hits}}); d.Continue {
		t.Fatal("other evidence with hits: do not nudge")
	}
	fixture := ToolCallRecord{ToolName: "es_log_query", Allowed: true, Result: `{"hit_status":"suspect","diagnosis":{"hint":"probe hint"}}`}
	if d := h.OnStop(ctx, StopHookInput{Text: "没有数据", ToolCalls: []ToolCallRecord{fixture}}); !d.Continue || !strings.Contains(d.Message, "probe hint") {
		t.Fatalf("JSON string results (eval fixtures) must be understood: %+v", d)
	}
	empty := ToolCallRecord{ToolName: "es_log_query", Allowed: true, Result: map[string]any{"hit_status": tool.HitStatusEmpty}}
	if d := h.OnStop(ctx, StopHookInput{Text: "No data found.", ToolCalls: []ToolCallRecord{empty}}); d.Continue {
		t.Fatal("only empty (no suspect): do not nudge")
	}
	if p, max := h.(BudgetedStopHook).StopBudget(); p != SuspectEvidenceRuleID || max != 1 {
		t.Fatalf("budget %q %d", p, max)
	}
}

func TestDefaultStopHooksEnv(t *testing.T) {
	if len(DefaultStopHooks()) != 1 {
		t.Fatal("default on")
	}
	t.Setenv(EnvStopSuspect, "off")
	if len(DefaultStopHooks()) != 0 {
		t.Fatal("env off")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -p 1 ./harness/ -run "TestSuspectHook|TestDefaultStopHooksEnv" -count=1`

- [ ] **Step 3: 实现**

```go
package harness

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sixath/framework/tool"
)

const (
	SuspectEvidenceRuleID = "suspect_evidence"
	EnvStopSuspect        = "SATH_STOP_SUSPECT"
)

var (
	negativeClaim = regexp.MustCompile(`(?i)(没有(查到|找到|数据|记录|日志|结果|相关)|查不到|未查到|未找到|找不到|不存在|未发现|无(数据|记录|结果|日志|匹配)|0\s*条|no (data|results?|matches|records|logs)|not found|nothing found)`)
	negativeExempt = regexp.MustCompile(`(?i)(如果|若|假如|一旦|如未|如无|\bif\b|\bunless\b|\bwhen\b)`)
)

type suspectEvidenceHook struct{}

// NewSuspectEvidenceHook 在最终回答断言"查不到/没有数据"、而本轮检索结果只有 empty/suspect 且至少一个 suspect 时提醒一次。
func NewSuspectEvidenceHook() StopHook { return suspectEvidenceHook{} }

// DefaultStopHooks 返回框架内置、默认开启的 stop hook；调用方须与工作区规则合并后一次性传给 WithReActStopHooks（该选项为替换语义）。
func DefaultStopHooks() []StopHook {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvStopSuspect))) {
	case "0", "false", "off", "no", "disable", "disabled":
		return nil
	}
	return []StopHook{NewSuspectEvidenceHook()}
}

func (suspectEvidenceHook) StopBudget() (string, int) { return SuspectEvidenceRuleID, 1 }

func (suspectEvidenceHook) OnStop(_ context.Context, in StopHookInput) StopDecision {
	if !hasNegativeClaim(in.Text) {
		return StopDecision{}
	}
	var lines []string
	for _, rec := range in.ToolCalls {
		if rec.Error != "" || !rec.Allowed {
			continue
		}
		result := tool.DecodeJSONResult(rec.Result)
		st, _, _ := tool.HitContractFromResult(result)
		switch st {
		case tool.HitStatusHits:
			return StopDecision{}
		case tool.HitStatusSuspect:
			hint := "result flagged as suspect"
			if d := tool.DiagnosisFromResult(result); d != nil && d.Hint != "" {
				hint = d.Hint
			} else if m, ok := result.(map[string]any); ok {
				if miss, ok := m["roots_missing"]; ok {
					hint = fmt.Sprintf("search roots missing: %v", miss)
				} else if m["root_missing"] == true {
					hint = fmt.Sprintf("search root %v does not exist", m["root"])
				}
			}
			lines = append(lines, fmt.Sprintf("- %s：%s", rec.ToolName, hint))
		}
	}
	if len(lines) == 0 {
		return StopDecision{}
	}
	msg := "你的结论是「查不到/没有数据」，但以下检索结果被标记为可疑（suspect），更可能是查询条件写错而不是真的没有数据：\n" +
		strings.Join(lines, "\n") +
		"\n请按提示修正条件后重查；若确认无法查到，在回答中明确说明这些结果可疑及原因。"
	return StopDecision{Continue: true, RuleID: SuspectEvidenceRuleID, Message: msg}
}

func hasNegativeClaim(text string) bool {
	for _, loc := range negativeClaim.FindAllStringIndex(text, -1) {
		if !negativeExempt.MatchString(enclosingSentence(text, loc[0], loc[1])) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test -p 1 ./harness/ -run "TestSuspectHook|TestDefaultStopHooksEnv" -count=1`，Expected: PASS

- [ ] **Step 5: 合并注册（三处）**

`portal/internal/chat/agent_builder.go`（约 482-501 行）改为：

```go
	var hooks []agent.ToolHook
	stopHooks := agent.DefaultStopHooks()
	maxNudges := 0
	if ws := strings.TrimSpace(workspace); ws != "" {
		if loaded, err := agent.LoadWorkspaceHarnessHooks(ws); err == nil {
			hooks = append(hooks, loaded...)
		}
		wsStop, n, err := agent.LoadWorkspaceStopHooks(ws, workspaceStopHookOptions())
		if err != nil {
			slog.Warn("harness hooks: skip stop_rules", "workspace", ws, "err", err)
			wsStop = nil
		}
		stopHooks = append(wsStop, stopHooks...)
		maxNudges = n
		if critic := CriticStopHook(m, ws); critic != nil {
			stopHooks = append(stopHooks, critic)
		}
		if agent.WorkspaceInvestigationLedgerEnabled(ws) {
			opts = append(opts, agent.WithReActToolSuccessHook(agent.InvestigationObserver(tool.DefaultInvestigationStore)))
		}
	}
	if len(stopHooks) > 0 {
		opts = append(opts, agent.WithReActStopHooks(stopHooks...), agent.WithReActMaxStopNudges(maxNudges))
	}
```

`InvestigateConfig`（portal，约 530-545 行）**不改**；子代理的默认 hook 统一在 `framework/investigate/runner.go` 合并，覆盖所有构造 `investigate.Config` 的调用方。

`framework/investigate/runner.go`（约 59 行）把 `if len(cfg.StopHooks) > 0 {` 一段改为：

```go
	stopHooks := cfg.StopHooks
	if !harness.HasStopHookPrefix(stopHooks, harness.SuspectEvidenceRuleID) {
		stopHooks = append(append([]harness.StopHook{}, stopHooks...), harness.DefaultStopHooks()...)
	}
	if len(stopHooks) > 0 {
		agentOpts = append(agentOpts, harness.WithReActStopHooks(stopHooks...))
		if cfg.MaxStopNudges > 0 {
			agentOpts = append(agentOpts, harness.WithReActMaxStopNudges(cfg.MaxStopNudges))
		}
	}
```

并在 `suspect_stop_hook.go` 追加：

```go
// HasStopHookPrefix 报告 hooks 中是否已有预算前缀为 prefix 的 BudgetedStopHook（用于合并默认 hook 时去重）。
func HasStopHookPrefix(hooks []StopHook, prefix string) bool {
	for _, h := range hooks {
		if b, ok := h.(BudgetedStopHook); ok {
			if p, _ := b.StopBudget(); p == prefix {
				return true
			}
		}
	}
	return false
}
```

`evals/runner/live.go`（约 80-90 行）：`hooks := append([]agent.StopHook{}, lo.StopHooks...)` 之后加 `hooks = append(hooks, agent.DefaultStopHooks()...)`。

portal 测试：`portal/internal/chat/agent_builder_react_opts_test.go:139/:164` 统计的是 `InvestigateConfig(...).StopHooks`，而 `InvestigateConfig` 不改，这两处**保持不变**；只更新 `HarnessReActOptionsFor` 在无 workspace 时的期望（现在也会注册 stop hooks）。

**预算独立性测试**：在 harness 中找到现有 `evaluateStopHooks`/`BudgetedStopHook` 的测试（`rg -n "BudgetedStopHook|evaluateStopHooks" framework/harness/*_test.go`），按同样方式构造 agent，追加用例：声明式 hook 已用满 `max_stop_nudges` 时 suspect hook 仍能触发一次；suspect hook 触发一次后不再触发，且不计入声明式 hook 的共享预算。

- [ ] **Step 6: 运行**

Run（framework）：`go test -p 1 ./harness/ ./investigate/ -count=1`
Run（portal 目录）：`go test -p 1 ./internal/chat/ -count=1`
Run（evals/runner 目录）：`go test -p 1 ./... -count=1`
Expected: PASS。portal 里断言"无 workspace 时不注册 stop hooks"的测试按新行为更新。

- [ ] **Step 7: 提交**

```bash
git add framework/harness/suspect_stop_hook.go framework/harness/suspect_stop_hook_test.go portal/internal/chat/agent_builder.go evals/runner/live.go framework/investigate/runner.go
git commit -m "feat(harness): 内置 suspect_evidence stop hook，依据可疑空结果下否定结论时提醒一次"
```

---

### Task 17: 可观测性与评测指标（suspect 率、参数拒绝率）

**Part A — `ToolCallRecord`（framework/harness）**

- [ ] **Step A1: 写失败测试**（`framework/harness/trace_record_test.go`）：构造被中间件拒绝的工具（返回 `InvalidArgumentsError`，两条 keyword `one_of`、`unknown_field`）与返回 `hit_status=suspect` 的工具，经 ReAct 执行单步（参考现有 `react_agent` 工具执行测试的 fake model 写法，`rg -n "func Test.*ToolCall" framework/harness/*_test.go`），断言 trace 中对应 `ToolCallRecord.CheckRejects == ["one_of","unknown_field"]`、`HitStatus == "suspect"`。

- [ ] **Step A2: 实现**：`ToolCallRecord`（`trace.go:176-187`）追加

```go
	// HitStatus 为结果上的命中契约（hits/empty/suspect/error），无契约时为空。
	HitStatus string `json:"hit_status,omitempty"`
	// CheckRejects 为参数被拒时命中的规则 keyword（required/one_of/unknown_field…）。
	CheckRejects []string `json:"check_rejects,omitempty"`
```

在 `react_agent.go` 约 1179 行 `record.Result = result` **之后、`if err != nil { ... return record, nil }`（约 1180-1185 行）之前**插入（否则被拒调用提前返回，`CheckRejects` 不会被设置）：

```go
	record.HitStatus, _, _ = tool.HitContractFromResult(tool.DecodeJSONResult(record.Result))
	var iae *tool.InvalidArgumentsError
	if errors.As(err, &iae) {
		for _, e := range iae.Errors {
			record.CheckRejects = append(record.CheckRejects, e.Keyword)
		}
	}
```

（补 `errors` 导入；`err` 为该处工具执行返回的 error。）`check_skipped` 留在结果 map 中、探测次数由 `diagnosis.probes` 体现，不另设字段。

- [ ] **Step A3: 运行** `go test -p 1 ./harness/ -count=1`，Expected: PASS；提交 `feat(harness): 工具调用记录命中状态与参数拒绝规则`。

**Part B — evals 指标**

**Files:**
- Modify: `evals/runner/trace_summary.go`（`TraceCall` 约 21 行、`newTraceCall` 约 90 行、`classifyValue` 约 239 行）
- Modify: `evals/runner/metrics.go`（answer_shape 汇总结构，含 `tool_error_rate`/`empty_rate` 的位置）、`evals/runner/answer_shape.go`（汇总计算处）
- Test: `evals/runner/trace_summary_test.go`, `evals/runner/answer_shape_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestTraceCall_SuspectAndRejects(t *testing.T) {
	c := newTraceCall("es_log_query", "{}", map[string]any{"ok": true, "hit_status": "suspect"}, "")
	if !c.Suspect || !c.Empty {
		t.Fatalf("suspect: %+v", c)
	}
	r := newTraceCall("execute_read", "{}", map[string]any{
		"ok": false, "error": "invalid", "error_code": "permanent",
		"invalid_arguments": []any{map[string]any{"keyword": "one_of"}, map[string]any{"keyword": "unknown_field"}},
	}, "")
	if len(r.RejectKeywords) != 2 || r.RejectKeywords[0] != "one_of" {
		t.Fatalf("rejects: %+v", r)
	}
}
```

并在 `answer_shape_test.go` 中为汇总加断言：给 2 个 run，共 4 次调用（1 suspect、1 被拒），期望 `suspect_rate == 0.25`、`arg_reject_rate == 0.25`、`arg_rejects_by_keyword["one_of"] == 1`。

- [ ] **Step 2: 运行确认失败**

Run（evals/runner）：`go test -p 1 ./... -run "TestTraceCall_SuspectAndRejects|AnswerShape" -count=1`

- [ ] **Step 3: 实现**
- `TraceCall` 加 `Suspect bool \`json:"suspect,omitempty"\``、`RejectKeywords []string \`json:"reject_keywords,omitempty"\``。
- `newTraceCall`：结果为 map 时，`hit_status=="suspect"` 置 `Suspect=true`（`Empty` 维持既有判定为 true）；遍历 `invalid_arguments`（`[]any` 或 `[]map[string]any`，以及 JSON 字符串结果解码后的同名字段）收集 `keyword`。
- 汇总结构加 `SuspectRate float64 \`json:"suspect_rate"\``、`ArgRejectRate float64 \`json:"arg_reject_rate"\``、`ArgRejectsByKeyword map[string]int \`json:"arg_rejects_by_keyword,omitempty"\``，与 `tool_error_rate` 同处计算（分母同为总调用数）。
- `evals/README.md` 的指标说明表追加这三项。

- [ ] **Step 4: 运行确认通过**

Run（evals/runner）：`go test -p 1 ./... -count=1`，Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add evals/runner/trace_summary.go evals/runner/metrics.go evals/runner/answer_shape.go evals/runner/*_test.go evals/README.md
git commit -m "feat(evals): 统计 suspect 率与参数拒绝率"
```

---

### Task 18: 文案与 CI 评测任务

**Files:**
- Modify: `framework/investigate/playbook.go:35`
- Modify: `evals/tasks_ci/answer_shape_ci.jsonl`
- Test: `evals/runner/task_test.go`（`TestAnswerShapeTaskSetsLoad` 已覆盖加载）

- [ ] **Step 1: playbook 文案**

第 35 行改为：

```
- es_log_query 返回 hit_status=empty 表示查询有效但无匹配（不是没有日志）；hit_status=suspect 表示 0 条但放宽条件后有数据（见 diagnosis），先修正条件再下结论；index_error=unresolved 表示索引不存在，用返回的 suggested_index_patterns 重试；参数被拒（invalid_arguments）时按 candidates 修正后重试。
```

- [ ] **Step 2: 新增 3 个 CI 夹具任务**

以文件中 `shape-ci-restart-after-index-error` 为模板（同样的字段：`id`、`category:"answer_shape"`、`answer_type`、`prompt`、`fixtures`、判定规则等），追加：

1. `shape-ci-unknown-field-rejected`（enumerate）：`es_log_query` 第一次调用的夹具返回 `{"ok":false,"tool":"es_log_query","error":"tool \"es_log_query\": invalid arguments: argument \"query\" references unknown field \"servcie\" (did you mean: service)","error_code":"permanent","invalid_arguments":[{"path":"query","keyword":"unknown_field","candidates":["service"]}]}`，第二次（含 `service:`）返回 2 条命中；期望回答列出这 2 条。
2. `shape-ci-datasource-typo`（count）：`execute_read` 首次夹具返回 `keyword:"one_of"`、`candidates:["mysql_main"]` 的拒绝，第二次返回 `{"Columns":["c"],"Rows":[[42]]}`；期望回答给出 42。
3. `shape-ci-suspect-empty`（lookup）：`es_log_query` 夹具返回 `{"ok":true,"hits":[],"total":0,"hit_status":"suspect","diagnosis":{"probes":[{"label":"without:level:ERRO","count":31}],"hint":"original query returned 0 but without:level:ERRO returned 31; a condition is probably wrong"}}`，后续含 `level:ERROR` 的调用返回命中；期望回答**不**断言"没有数据"，给出命中内容。

（夹具按调用序/参数匹配的语法以现有任务为准；若夹具不支持"同一工具多次不同返回"，先 `rg -n "fixtures" evals/runner/*.go` 查看匹配方式，按参数子串匹配编写。）

- [ ] **Step 3: 运行**

Run（evals/runner）：`go test -p 1 ./... -count=1`
Run（evals/runner）：`go run . -mode=mock -tasks ../tasks_ci -baseline-results ../baselines/answer_shape_ci.jsonl`（按 `evals/README.md` 的 CI 命令；需要 `SATH_EVAL_API_KEY` 与网关环境变量，与现有 CI 一致）
Expected: 测试 PASS；CI eval 无 per-task 回退，新任务通过（若新任务不通过，记录 trace 分析是夹具问题还是模型问题，不为通过而改判定规则）。

- [ ] **Step 4: 提交**

```bash
git add framework/investigate/playbook.go evals/tasks_ci/answer_shape_ci.jsonl
git commit -m "test(evals): 新增参数拒绝与 suspect 空结果的 CI 夹具任务，更新 playbook 文案"
```

---

### Task 19: 全量验证

- [ ] **Step 1: framework**

Run（framework）：`$env:GOGC=50; go vet ./tool/... ./harness/... ./investigate/... ; go test -p 1 ./... -count=1`
Expected: 全部 `ok`

- [ ] **Step 2: portal 与 evals**

Run（portal）：`go build ./... ; go test -p 1 ./internal/... -count=1`
Run（evals/runner）：`go test -p 1 ./... -count=1`
Expected: 全部通过

- [ ] **Step 3: 回退开关冒烟**

Run（framework）：`$env:SATH_TOOL_ARG_VALIDATION="off"; $env:SATH_TOOL_EMPTY_PROBE="off"; $env:SATH_STOP_SUSPECT="off"; go test -p 1 ./tool/ ./harness/ -count=1; Remove-Item Env:SATH_TOOL_ARG_VALIDATION, Env:SATH_TOOL_EMPTY_PROBE, Env:SATH_STOP_SUSPECT`
Expected: 除显式依赖开关开启的用例外通过（这些用例应自行 `t.Setenv` 覆盖，若失败则补 `t.Setenv(..., "")`）。

- [ ] **Step 4: 规格对照**

逐条对照规格 §5 表格与 §8 测试清单，确认每项都有对应提交与测试；未做的项写入最终汇报。
