# 答案质量改进 阶段 0 + 阶段 1 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 先做零风险的立即修复（凭据脱敏、ES 默认索引提示、空回复诊断），再给 `evals/runner` 加上端到端"答案对题"评测（judge 打分、Portal 真实环境模式、重复运行、门禁），产出第一份基线。

**Architecture:** 阶段 0 改动落在 framework（新 `redact` 包、`es_log_query` 描述、`Generation.FinishReason`、`RunTrace.EmptyFinal`）和 portal 的唯一工具载荷出口 `toolCallPayloadFromRecord`。阶段 1 全部在 `evals/runner`：新类别 `answer_shape`，live 与 portal 两种运行方式都先归一成 `TraceSummary`，再交给同一个 `Judge`；多次运行合并为一条 `TaskResult`；`answer_shape` 单独汇总为 `ShapeSummary`，不影响现有 `completion_rate` 门禁。

**Tech Stack:** Go 1.26，标准库 `net/http`/`httptest`/`bufio`/`regexp`，`github.com/sashabaranov/go-openai`（已有依赖）。

**Spec:** [`docs/superpowers/specs/2026-10-01-answer-quality-roadmap-design.md`](../specs/2026-10-01-answer-quality-roadmap-design.md)

**与 spec 的偏差（实施前已确认的代码事实）:**

- §4.1 工具描述只列集群已配置的 `IndexPriority`，不在注册时请求 ES `_cat/indices`（注册发生在每次构建 agent 时，网络请求会拖慢建链）。报错路径已经通过 `attachSuggestedPatterns` 列出实时索引模式，测试 `TestESLogQuery_MissingIndexListsPatterns` 覆盖。
- §4.3 技能 `new-prelaunch-troubleshoot` 不在仓库里，位于 agent 运行时工作区 `<agent.Workspace>/skills/`，改为运维任务（Task 6）。
- §4.4 模型返回结构没有 finish reason，流式路径也拿不到 token 用量。本计划补 `FinishReason`；流式用量不补（需要 `stream_options.include_usage`，部分兼容网关不支持）。
- 现有测试 `TestReActAgent_EmptyIdleAfterToolsDoesNotInject` 禁止空回复注入重试，本计划只记日志，不冲突。
- §4.4 "原始响应前 2KB"：空终答的正文本身为空，有信息量的是推理内容，所以记录推理内容前 2KB（`reasoning_preview`）。
- §5.3 live 模式的 `TraceSummary` 只从 `RunTrace` 构造：fixture 工具的返回值已作为 `ToolCallRecord.Result` 记入 trace，不需要另读 fixture 命中记录。
- 题目目录：真实环境题放 `evals/tasks_portal/`，CI 题放 `evals/tasks_ci/`，都不放 `evals/tasks/`。现有每晚命令 `-mode=live -tasks ../tasks` 会加载该目录全部文件，混入 answer_shape 题会因缺 `-judge-model` 退出。

**提交约定:** 工作区有大量与本计划无关的未提交改动。每个提交步骤只 `git add` 该任务列出的文件，绝不 `git add -A` / `git add .`。

---

## 文件结构

| 文件 | 动作 | 职责 |
|------|------|------|
| `framework/redact/redact.go` | 新建 | 文本与嵌套值的凭据脱敏 |
| `framework/redact/redact_test.go` | 新建 | 表驱动单测 |
| `portal/internal/service/chat_stream.go` | 修改 | `toolCallPayloadFromRecord` 截断前脱敏（同时覆盖 SSE 与 timeline 落库） |
| `portal/internal/service/chat_stream_toolcall_test.go` | 修改 | 脱敏用例 |
| `framework/harness/turn_trace.go` | 修改 | `redactArgs` 对非敏感键的值也做文本脱敏；复制 `EmptyFinal` |
| `framework/tool/es_log_tool.go` | 修改 | 集群无默认索引时描述标明必须传 index |
| `framework/tool/es_log_tool_test.go` | 修改 | 描述用例 |
| `framework/model/model.go` | 修改 | `Generation.FinishReason` |
| `framework/model/openai_tools.go` | 修改 | 非流式文本路径填 `FinishReason` |
| `framework/model/openai_tools_stream.go` | 修改 | 流式终止帧 / EOF 填 `FinishReason` |
| `framework/model/openai_tools_usage_test.go`、`openai_tools_stream_test.go` | 修改 | finish reason 用例 |
| `framework/harness/empty_final.go` | 新建 | `EmptyFinalDiag` 与 `recordEmptyFinal` |
| `framework/harness/trace.go` | 修改 | `RunTrace.EmptyFinal` 字段 |
| `framework/harness/react_agent.go` | 修改 | 五处终答出口调用 `recordEmptyFinal` |
| `framework/harness/empty_final_test.go` | 新建 | 空终答诊断用例 |
| `evals/runner/task.go` | 修改 | `answer_shape` 类别与字段 |
| `evals/runner/mock.go` | 修改 | mock 跳过 `answer_shape` |
| `evals/runner/trace_summary.go` | 新建 | `TraceSummary`：从 `RunTrace` / Portal timeline 构造 |
| `evals/runner/judge.go` | 新建 | judge 提示词、解析、重试、规则 4 预检 |
| `evals/runner/answer_shape.go` | 新建 | 单次运行打分、多次运行合并 |
| `evals/runner/metrics.go` | 修改 | `TaskResult` 新字段、`ShapeSummary` |
| `evals/runner/gate.go` | 修改 | `answer_shape` 门禁、单题回归、`gateError` |
| `evals/runner/live.go` | 修改 | `answer_shape` 分支、`-repeat` |
| `evals/runner/portal.go` | 新建 | Portal HTTP/SSE 客户端与运行器 |
| `evals/runner/main.go` | 修改 | 新参数、`portal` 模式、统一收尾 |
| `evals/tasks_portal/answer_shape.jsonl` | 新建 | 真实环境题（portal 模式） |
| `evals/tasks_ci/answer_shape_ci.jsonl` | 新建 | 带 fixtures 的 CI 题（live 模式） |
| `evals/README.md` | 修改 | 用法与门禁说明 |

---

# 阶段 0：立即修复

### Task 1: 凭据脱敏包

**Files:**
- Create: `framework/redact/redact.go`
- Test: `framework/redact/redact_test.go`

- [ ] **Step 1: 写失败测试**

```go
package redact

import (
	"reflect"
	"testing"
)

func TestString(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"curl user", "curl -u admin:s3cret http://x", "curl -u admin:*** http://x"},
		{"curl user quoted", "curl -u 'admin:s3cret' http://x", "curl -u 'admin:***' http://x"},
		{"curl --user", "curl --user=admin:s3cret http://x", "curl --user=admin:*** http://x"},
		{"password kv", "mysql password=abc123 db", "mysql password=*** db"},
		{"passwd colon", "passwd: abc123", "passwd: ***"},
		{"pwd kv", "login pwd=abc", "login pwd=***"},
		{"api_key query", "GET /v1?api_key=XYZ&x=1", "GET /v1?api_key=***&x=1"},
		{"bearer header", "-H 'Authorization: Bearer abc.def'", "-H 'Authorization: Bearer ***'"},
		{"basic header", "Authorization: Basic Zm9vOmJhcg==", "Authorization: Basic ***"},
		{"url creds", "ftp://lightplay:pa55@10.0.0.1/a", "ftp://lightplay:***@10.0.0.1/a"},
		{"sshpass", "sshpass -p 'pa55' ssh root@h", "sshpass -p '***' ssh root@h"},
		{"plain text untouched", "SELECT * FROM t WHERE max_tokens=5", "SELECT * FROM t WHERE max_tokens=5"},
		{"url without creds", "http://host:8080/path", "http://host:8080/path"},
	}
	for _, c := range cases {
		if got := String(c.in); got != c.want {
			t.Errorf("%s: String(%q)=%q want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestValue_Nested(t *testing.T) {
	in := map[string]any{
		"command": "curl -u a:b http://x",
		"headers": map[string]any{"Authorization": "Bearer t", "Accept": "json"},
		"list":    []any{"password=p", 3},
		"count":   float64(2),
	}
	want := map[string]any{
		"command": "curl -u a:*** http://x",
		"headers": map[string]any{"Authorization": Mask, "Accept": "json"},
		"list":    []any{"password=***", 3},
		"count":   float64(2),
	}
	if got := Value(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("Value=%#v\nwant %#v", got, want)
	}
	if in["command"] != "curl -u a:b http://x" {
		t.Fatal("Value must not mutate its input")
	}
}

func TestValue_StringMaps(t *testing.T) {
	got := Value(map[string]string{"token": "x", "host": "h"})
	want := map[string]string{"token": Mask, "host": "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestSecretKey(t *testing.T) {
	for _, k := range []string{"password", "X-Api-Token", "Authorization", "access_token", "cookie"} {
		if !SecretKey(k) {
			t.Errorf("%q should be secret", k)
		}
	}
	for _, k := range []string{"max_tokens", "input_tokens", "output_tokens", "host", "command"} {
		if SecretKey(k) {
			t.Errorf("%q should not be secret", k)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./redact/ -count=1`
Expected: FAIL，`undefined: String` / `undefined: Value` / `undefined: Mask`

- [ ] **Step 3: 实现**

```go
// Package redact 在展示与持久化前遮盖文本或结构化参数中的凭据；不改变实际执行的参数。
package redact

import (
	"regexp"
	"strings"
)

// Mask 替换凭据的占位符。
const Mask = "***"

var (
	reAuthHeader = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)((?:bearer|basic)\s+)?[^\s'",]+`)
	reKeyValue   = regexp.MustCompile(`(?i)\b(password|passwd|pwd|api_key|apikey|secret|token)(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&'",]+)`)
	reCurlUser   = regexp.MustCompile(`((?:^|\s)(?:-u\s+|--user[=\s]+))(['"]?)([^:\s'"]+):([^\s'"]+)`)
	reURLCreds   = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+):([^@\s/]+)@`)
	reSshpass    = regexp.MustCompile(`(sshpass\s+-p\s*)(['"]?)[^\s'"]+`)
)

var secretKeySubstrings = []string{"password", "passwd", "token", "secret", "api_key", "apikey", "authorization", "cookie"}

// SecretKey 判断结构化参数的键名是否表示凭据。以 tokens 结尾的键（max_tokens、input_tokens）是计量字段，不算。
func SecretKey(key string) bool {
	k := strings.ToLower(key)
	if strings.HasSuffix(k, "tokens") {
		return false
	}
	for _, s := range secretKeySubstrings {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// String 遮盖文本中的常见凭据形式：Authorization 头、key=value、curl -u、URL user:pass@、sshpass -p。
func String(s string) string {
	if s == "" {
		return s
	}
	s = reAuthHeader.ReplaceAllString(s, "${1}${2}"+Mask)
	s = reKeyValue.ReplaceAllStringFunc(s, func(m string) string {
		sub := reKeyValue.FindStringSubmatch(m)
		v := sub[3]
		if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
			return sub[1] + sub[2] + v[:1] + Mask + v[:1]
		}
		return sub[1] + sub[2] + Mask
	})
	s = reCurlUser.ReplaceAllString(s, "${1}${2}${3}:"+Mask)
	s = reURLCreds.ReplaceAllString(s, "${1}:"+Mask+"@")
	s = reSshpass.ReplaceAllString(s, "${1}${2}"+Mask)
	return s
}

// Value 递归遮盖 map / slice / string；键名为凭据时整值替换为 Mask。返回新值，不修改输入。
func Value(v any) any {
	switch t := v.(type) {
	case string:
		return String(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if SecretKey(k) {
				out[k] = Mask
				continue
			}
			out[k] = Value(x)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, x := range t {
			if SecretKey(k) {
				out[k] = Mask
				continue
			}
			out[k] = String(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = Value(x)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, x := range t {
			out[i] = String(x)
		}
		return out
	default:
		return v
	}
}
```

注意 `sshpass -p 'pa55'` 中 `reKeyValue` 不会命中（`-p` 不是键名），由 `reSshpass` 处理；`reCurlUser` 用 `(?:^|\s)` 防止误伤 `--user-agent` 之类以外的普通文本。

- [ ] **Step 4: 运行确认通过**

Run: `cd framework && go test ./redact/ -count=1`
Expected: PASS。若 `curl --user=` 用例失败，检查 `reCurlUser` 中 `--user[=\s]+` 是否吞掉了 `=`（期望输出保留 `=`，因为它在第 1 组内）。

- [ ] **Step 5: 提交**

```bash
git add framework/redact/redact.go framework/redact/redact_test.go
git commit -m "feat(redact): add credential redaction for display and persistence"
```

---

### Task 2: 在工具载荷出口与 turn trace 接入脱敏

**Files:**
- Modify: `portal/internal/service/chat_stream.go`（`toolCallPayloadFromRecord`，约 L117）
- Modify: `framework/harness/turn_trace.go`（`redactArgs`，约 L104）
- Test: `portal/internal/service/chat_stream_toolcall_test.go`、`framework/harness/turn_trace_test.go`

- [ ] **Step 1: 写失败测试（portal）**

追加到 `portal/internal/service/chat_stream_toolcall_test.go`：

```go
func TestToolCallPayloadFromRecord_RedactsCredentials(t *testing.T) {
	rec := agent.ToolCallRecord{
		ToolCallID: "call_r",
		ToolName:   "terminal",
		Arguments: map[string]any{
			"command": "curl -u ftpuser:hunter2 ftp://10.0.0.1/a",
			"headers": map[string]any{"Authorization": "Bearer abc"},
		},
		Result: "connected with password=hunter2",
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	args, _ := p.Arguments.(map[string]any)
	if cmd, _ := args["command"].(string); strings.Contains(cmd, "hunter2") {
		t.Fatalf("command not redacted: %q", cmd)
	}
	h, _ := args["headers"].(map[string]any)
	if h["Authorization"] != "***" {
		t.Fatalf("header not redacted: %#v", h)
	}
	if s, _ := p.Result.(string); strings.Contains(s, "hunter2") {
		t.Fatalf("string result not redacted: %q", s)
	}
	if strings.Contains(rec.Arguments["command"].(string), "***") {
		t.Fatal("record arguments must not be mutated")
	}
}
```

- [ ] **Step 2: 写失败测试（turn trace）**

追加到 `framework/harness/turn_trace_test.go`：

```go
func TestBuildTurnTrace_RedactsCredentialsInValues(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName:  "ssh_exec",
		Arguments: map[string]any{"command": "sshpass -p 's3cret' ssh root@h"},
	}}}
	out := BuildTurnTrace(TurnTraceMeta{}, tr)
	cmd, _ := out.Calls[0].Arguments["command"].(string)
	if strings.Contains(cmd, "s3cret") {
		t.Fatalf("command not redacted: %q", cmd)
	}
}
```

若 `turn_trace_test.go` 未导入 `strings`，补上。

- [ ] **Step 3: 运行确认失败**

Run: `cd portal && go test ./internal/service/ -run TestToolCallPayloadFromRecord -count=1`
Expected: FAIL，`command not redacted`
Run: `cd framework && go test ./harness/ -run TestBuildTurnTrace -count=1`
Expected: FAIL，`command not redacted`

- [ ] **Step 4: 实现**

`portal/internal/service/chat_stream.go`：在 import 中加 `"github.com/sixath/framework/redact"`，把 `toolCallPayloadFromRecord` 中两行截断改为先脱敏：

```go
	var argsIn any
	if rec.Arguments != nil {
		argsIn = redact.Value(rec.Arguments)
	}
	args, aTrunc := truncateField(argsIn)
	res, rTrunc := truncateField(redactResult(rec.Result))
```

（`rec.Arguments` 为 nil 时保持 nil，否则 `redact.Value` 会返回空 map，SSE/timeline 里原本不显示的参数会变成 `{}`。）

并在同文件追加：

```go
// redactResult 只处理字符串与 map/slice 结果；结构体结果（如 QuerySpillStub）原样保留，避免破坏截断逻辑对字段的读取。
func redactResult(v any) any {
	switch v.(type) {
	case string, map[string]any, []any:
		return redact.Value(v)
	default:
		return v
	}
}
```

`framework/harness/turn_trace.go`：import 加 `"github.com/sixath/framework/redact"`，`redactArgs` 的非敏感分支改为：

```go
		} else {
			out[k] = redact.Value(v)
		}
```

（敏感键分支仍写 `"[redacted]"`，保持 `TestBuildTurnTrace_RedactsSecretKeysAndTruncates` 不变。）

- [ ] **Step 5: 运行确认通过**

Run: `cd portal && go test ./internal/service/ -count=1`
Expected: PASS（含原有 `TruncatesLargeResult`、`SpillStubKeepsPathWhenTruncated`）
Run: `cd framework && go test ./harness/ -run TurnTrace -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add portal/internal/service/chat_stream.go portal/internal/service/chat_stream_toolcall_test.go framework/harness/turn_trace.go framework/harness/turn_trace_test.go
git commit -m "fix(security): redact credentials in tool payloads, timeline and turn trace"
```

---

### Task 3: `es_log_query` 描述标明无默认索引的集群必须传 index

**Files:**
- Modify: `framework/tool/es_log_tool.go:110-117`
- Test: `framework/tool/es_log_tool_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `framework/tool/es_log_tool_test.go`：

```go
func TestESLogQueryDescription_MarksIndexRequiredWithoutDefault(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, &fakeReader{result: &executor.QueryResult{}}, ESLogConfig{Clusters: []ESLogCluster{
		{ID: "zj-elk", Purpose: "应用", IndexPriority: []string{"backend-sched-planner-*", "vm-manager-*"}},
		{ID: "mg-rca-es", Purpose: "RCA"},
		{ID: "ok-elk", Purpose: "有默认", DefaultIndex: "app-*"},
	}})
	tl, _ := reg.Get("es_log_query")
	d := tl.Description
	for _, want := range []string{
		"`zj-elk` — 应用; no default index: index is REQUIRED (known patterns: backend-sched-planner-*, vm-manager-*)",
		"`mg-rca-es` — RCA; no default index: index is REQUIRED",
		"`ok-elk` — 有默认; default index `app-*`",
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("description missing %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "default index ``") {
		t.Fatal("empty default index must not render as default index ``")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./tool/ -run TestESLogQueryDescription_MarksIndexRequired -count=1`
Expected: FAIL，`description missing`

- [ ] **Step 3: 实现**

把 `es_log_tool.go` 中构建每个集群行的循环替换为：

```go
	for i, c := range clusters {
		clusterIDs[i] = c.ID
		var line string
		if strings.TrimSpace(c.DefaultIndex) == "" {
			line = fmt.Sprintf("\n`%s` — %s; no default index: index is REQUIRED", c.ID, c.Purpose)
			if len(c.IndexPriority) > 0 {
				line += fmt.Sprintf(" (known patterns: %s)", strings.Join(c.IndexPriority, ", "))
			}
		} else {
			line = fmt.Sprintf("\n`%s` — %s; default index `%s`", c.ID, c.Purpose, c.DefaultIndex)
		}
		if strings.TrimSpace(c.BodyField) != "" {
			line += fmt.Sprintf("; body field `%s`", c.BodyField)
		}
		desc += line
	}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd framework && go test ./tool/ -run ESLog -count=1`
Expected: PASS（含 `TestESLogQueryDescriptionForbidsInventingIndex`、`TestESLogQuery_MissingIndexListsPatterns`）

- [ ] **Step 5: 提交**

```bash
git add framework/tool/es_log_tool.go framework/tool/es_log_tool_test.go
git commit -m "fix(es_log_query): mark index as required for clusters without default_index"
```

---

### Task 4: `Generation.FinishReason`

**Files:**
- Modify: `framework/model/model.go:42-49`
- Modify: `framework/model/openai_tools.go:126-134`
- Modify: `framework/model/openai_tools_stream.go:122-128, 173-182`
- Test: `framework/model/openai_tools_usage_test.go`、`framework/model/openai_tools_stream_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `openai_tools_usage_test.go`：

```go
func TestOpenAIClient_ChatWithTools_ReportsFinishReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{{
				Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: ""},
				FinishReason: openai.FinishReasonLength,
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	gen, err := openAITestClient(ts).ChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if gen.FinishReason != "length" {
		t.Fatalf("FinishReason=%q want length", gen.FinishReason)
	}
}
```

追加到 `openai_tools_stream_test.go`：

```go
func TestChatWithToolsStream_ReportsFinishReason(t *testing.T) {
	cases := []struct {
		name   string
		frames []string
		want   string
	}{
		{"named stop", []string{
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`,
			`[DONE]`,
		}, "content_filter"},
		{"eof only", []string{
			`{"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
			`[DONE]`,
		}, "eof"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, c.frames)
			}))
			defer ts.Close()
			reg := tool.NewRegistry()
			registerFakeTool(t, reg, "fake_tool")
			textCh, genCh, err := openAITestClient(ts).ChatWithToolsStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, reg)
			if err != nil {
				t.Fatal(err)
			}
			for range textCh {
			}
			gen := <-genCh
			if gen == nil || gen.FinishReason != c.want {
				t.Fatalf("gen=%#v want FinishReason %q", gen, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./model/ -run FinishReason -count=1`
Expected: 编译失败，`gen.FinishReason undefined`

- [ ] **Step 3: 实现**

`model.go` 的 `Generation` 增加字段：

```go
type Generation struct {
	Text       string
	Raw        any
	TokenUsage *TokenUsage
	// FinishReason 为 provider 返回的结束原因（stop/length/content_filter/tool_calls）；
	// 流式无具名终止帧、靠 EOF 收尾时为 "eof"；provider 未返回时为空。
	FinishReason string
	// Err 非空表示流式调用在 setup 成功后失败（如 Recv 网络错误）。
	// ChatWithToolsStream 通过 finalGenCh 传递，避免 channel 空关闭被误判为 missing generation。
	Err error
}
```

`openai_tools.go` 文本分支：

```go
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return &Generation{
			Text: msg.Content,
			Raw: ToolStep{
				Used:             false,
				ReasoningContent: msg.ReasoningContent,
			},
			TokenUsage:   tokenUsageFromOpenAI(resp.Usage),
			FinishReason: string(resp.Choices[0].FinishReason),
		}, nil
	}
```

`openai_tools_stream.go` EOF 文本分支：

```go
				} else {
					genCh <- &Generation{
						Text: contentAccum.String(),
						Raw: ToolStep{
							Used:             false,
							ReasoningContent: reasoningAccum.String(),
						},
						FinishReason: "eof",
					}
				}
```

具名终止帧分支：

```go
			case openai.FinishReasonStop, openai.FinishReasonLength, openai.FinishReasonContentFilter:
				genCh <- &Generation{
					Text: contentAccum.String(),
					Raw: ToolStep{
						Used:             false,
						ReasoningContent: reasoningAccum.String(),
					},
					FinishReason: string(choice.FinishReason),
				}
				return
```

- [ ] **Step 4: 运行确认通过**

Run: `cd framework && go test ./model/ -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/model/model.go framework/model/openai_tools.go framework/model/openai_tools_stream.go framework/model/openai_tools_usage_test.go framework/model/openai_tools_stream_test.go
git commit -m "feat(model): expose finish reason on generations"
```

---

### Task 5: 空终答诊断（只记录，不重试）

**Files:**
- Create: `framework/harness/empty_final.go`
- Modify: `framework/harness/trace.go`（`RunTrace` 字段，放在 `EmptyIdleNudges` 之后）
- Modify: `framework/harness/react_agent.go`（五处终答出口）
- Modify: `framework/harness/turn_trace.go`（`TurnTrace` 字段与 `BuildTurnTrace` 复制）
- Test: `framework/harness/empty_final_test.go`

- [ ] **Step 1: 写失败测试**

```go
package harness

import (
	"context"
	"testing"

	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func TestRecordEmptyFinal_CapturesDiagnostics(t *testing.T) {
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{ToolName: "es_log_query"}}}
	tr.recordEmptyFinal(&model.Generation{
		Text:         " \n",
		FinishReason: "length",
		TokenUsage:   &model.TokenUsage{InputTokens: 9000, OutputTokens: 1024},
		Raw:          model.ToolStep{ReasoningContent: "思考了很久"},
	}, 15)
	d := tr.EmptyFinal
	if d == nil {
		t.Fatal("expected EmptyFinal")
	}
	if d.Step != 15 || d.FinishReason != "length" || d.InputTokens != 9000 || d.OutputTokens != 1024 || d.ToolCalls != 1 {
		t.Fatalf("diag=%+v", d)
	}
	if d.ReasoningChars != 5 || d.ReasoningPreview != "思考了很久" {
		t.Fatalf("reasoning diag=%+v", d)
	}
}

func TestRecordEmptyFinal_IgnoresNonEmpty(t *testing.T) {
	tr := &RunTrace{}
	tr.recordEmptyFinal(&model.Generation{Text: "答案"}, 1)
	if tr.EmptyFinal != nil {
		t.Fatalf("non-empty reply must not record: %+v", tr.EmptyFinal)
	}
}

func TestReActAgent_EmptyFinalRecordedInTrace(t *testing.T) {
	fake := &fakeOpenAIClient{finalReply: "\n\n"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	react := NewReActAgent(fake, memory.NewBufferMemory(5), reg)
	resp, err := react.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "你好"}}})
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil || tr.EmptyFinal == nil {
		t.Fatalf("expected EmptyFinal in trace, got %#v", tr)
	}
}

func TestBuildTurnTrace_CopiesEmptyFinal(t *testing.T) {
	tr := &RunTrace{EmptyFinal: &EmptyFinalDiag{Step: 3, FinishReason: "stop"}}
	out := BuildTurnTrace(TurnTraceMeta{}, tr)
	if out.EmptyFinal == nil || out.EmptyFinal.Step != 3 {
		t.Fatalf("EmptyFinal not copied: %+v", out.EmptyFinal)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./harness/ -run EmptyFinal -count=1`
Expected: 编译失败，`tr.recordEmptyFinal undefined`

- [ ] **Step 3: 实现**

`framework/harness/empty_final.go`：

```go
package harness

import (
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

const emptyFinalReasoningPreviewBytes = 2048

// EmptyFinalDiag 记录终答正文为空时的现场，用于判断是截断、拒答还是只输出了推理内容。只记录，不重试。
type EmptyFinalDiag struct {
	Step             int    `json:"step"`
	FinishReason     string `json:"finish_reason,omitempty"`
	InputTokens      int    `json:"input_tokens,omitempty"`
	OutputTokens     int    `json:"output_tokens,omitempty"`
	ReasoningChars   int    `json:"reasoning_chars,omitempty"`
	ReasoningPreview string `json:"reasoning_preview,omitempty"`
	ToolCalls        int    `json:"tool_calls"`
}

func (t *RunTrace) recordEmptyFinal(gen *model.Generation, step int) {
	if t == nil || gen == nil || strings.TrimSpace(gen.Text) != "" {
		return
	}
	d := &EmptyFinalDiag{Step: step, FinishReason: gen.FinishReason, ToolCalls: len(t.ToolCalls)}
	if gen.TokenUsage != nil {
		d.InputTokens = gen.TokenUsage.InputTokens
		d.OutputTokens = gen.TokenUsage.OutputTokens
	}
	if st, ok := gen.Raw.(model.ToolStep); ok && st.ReasoningContent != "" {
		d.ReasoningChars = utf8.RuneCountInString(st.ReasoningContent)
		d.ReasoningPreview = truncateUTF8(st.ReasoningContent, emptyFinalReasoningPreviewBytes)
	}
	t.EmptyFinal = d
	slog.Warn("react: empty final reply",
		"request_id", t.RequestID, "step", step, "finish_reason", d.FinishReason,
		"input_tokens", d.InputTokens, "output_tokens", d.OutputTokens,
		"reasoning_chars", d.ReasoningChars, "tool_calls", d.ToolCalls)
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
```

先确认包内没有同名 `truncateUTF8`：`rg "func truncateUTF8" framework/harness`。若已存在且语义相同则删除上面的定义直接复用；语义不同则把本文件的函数改名为 `truncateEmptyFinalPreview`。

`trace.go` 在 `EmptyIdleNudges` 字段后加：

```go
	// EmptyFinal 终答正文为空时的诊断（只记录不重试）。
	EmptyFinal *EmptyFinalDiag `json:"empty_final,omitempty"`
```

`react_agent.go` 五处终答出口，在 `storeAssistant` 之前插入一行：

1. 非流式 `Run`（`lastAnswer = gen.Text` 之前）：
```go
			trace.recordEmptyFinal(gen, step)
			lastAnswer = gen.Text
```
2. `runToolEventsSync`（`_ = a.storeAssistant(ctx, gen.Text)` 之前，紧跟 stop hook 判定块之后）：
```go
			trace.recordEmptyFinal(gen, step)
			_ = a.storeAssistant(ctx, gen.Text)
			if gen.Text != "" && !send(StreamEvent{Type: StreamEventDelta, Text: gen.Text, Trace: trace}) {
```
3. `runToolEvents`（流式，同样在 stop hook 块之后的 `_ = a.storeAssistant(ctx, gen.Text)` 之前）：
```go
			trace.recordEmptyFinal(gen, step)
			_ = a.storeAssistant(ctx, gen.Text)
			emit(events.RunCompleted, map[string]any{"text_length": len(gen.Text), "stream": true})
```
4. `runPlainEvents` 的非流式分支（约 L726，`_ = a.storeAssistant(ctx, gen.Text)` 之前；无工具或模型不支持工具调用时 Portal 走这里）：
```go
	trace.recordEmptyFinal(gen, -1)
	_ = a.storeAssistant(ctx, gen.Text)
```
5. `runPlain`（约 L1199，`_ = a.storeAssistant(ctx, gen.Text)` 之前）：
```go
	trace.recordEmptyFinal(gen, -1)
	_ = a.storeAssistant(ctx, gen.Text)
```

用 `rg -n "storeAssistant\(ctx, (gen.Text|lastAnswer)\)" framework/harness/react_agent.go` 核对：共 6 处匹配。循环结束后的 `_ = a.storeAssistant(ctx, lastAnswer)`（约 L472）没有 `gen`，跳过；其余 5 处前一行都应是 `recordEmptyFinal`（`Run` 中那处在 `lastAnswer = gen.Text` 之前）。

`turn_trace.go`：`TurnTrace` 末尾加字段，`BuildTurnTrace` 字面量里复制：

```go
	// EmptyFinal 终答为空时的诊断，随 payload_json 持久化。
	EmptyFinal *EmptyFinalDiag `json:"empty_final,omitempty"`
```

```go
		EstimatedCostUSD: tr.EstimatedCostUSD,
		EmptyFinal:       tr.EmptyFinal,
```

- [ ] **Step 4: 运行确认通过**

Run: `cd framework && go test ./harness/ -count=1`
Expected: PASS（含 `TestReActAgent_EmptyIdleAfterToolsDoesNotInject`、`TestReActAgent_EmptyIdleWithoutToolsFinishes`）

- [ ] **Step 5: 提交**

```bash
git add framework/harness/empty_final.go framework/harness/empty_final_test.go framework/harness/trace.go framework/harness/react_agent.go framework/harness/turn_trace.go
git commit -m "feat(harness): record diagnostics when the final reply is empty"
```

---

### Task 6: 运维项（不改代码）

- [ ] **Step 1: 配置 ES 集群默认索引**

在 Portal 数据源管理中，为 `zj-elk`、`zj-elk_flow`、`mg-rca-es` 填写 `default_index`（字段名 `default_index`，由 `portal/internal/chat/es_log_clusters.go` 读取）。取值向集群负责人确认；没有单一默认索引的集群改为填写 `index_priority`（Task 3 会把它写进工具描述）。

- [ ] **Step 2: 修复技能悬空引用**

在线上 agent `e8107fb3-e40a-4207-9d9a-6768847aaf79` 的工作区 `<agent.Workspace>/skills/new-prelaunch-troubleshoot/` 下：补上 `references/prelaunch-diagnostics.md`，或从 `SKILL.md` 删除对它的引用。二选一。

- [ ] **Step 3: 验证**

部署 Task 1–5 后，在 Portal 对该 agent 发问"查看最近一个小时一直预启动失败的 vmid"，检查：
- 会话 `metadata.timeline` 中没有 `index is required when cluster default_index is empty`。
- 不再出现 `read_skill_file` 报文件不存在。
- 若终答为空，`turn_trace` 的 `payload_json` 含 `empty_final`。

一周后汇总 `empty_final.finish_reason` 分布，写入 `framework/docs/improvements/04-harness.md` B1 条目，作为阶段 3 是否做重试的依据。

---

# 阶段 1：端到端评测

所有命令在 `evals/runner` 目录执行。

### Task 7: `answer_shape` 任务类别

**Files:**
- Modify: `evals/runner/task.go`
- Modify: `evals/runner/mock.go`（`runMock`）
- Test: `evals/runner/task_test.go`、`evals/runner/mock_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `task_test.go`：

```go
func TestValidate_AnswerShape(t *testing.T) {
	ok := Task{ID: "s1", Category: "answer_shape", Input: "q", MaxSteps: 30, Expect: Expectation{AnswerType: "enumerate"}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid answer_shape rejected: %v", err)
	}
	bad := ok
	bad.Expect.AnswerType = "list"
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid answer_type must be rejected")
	}
	noSteps := ok
	noSteps.MaxSteps = 0
	if err := noSteps.Validate(); err == nil {
		t.Fatal("answer_shape still requires max_steps")
	}
}
```

追加到 `mock_test.go`：

```go
func TestRunMock_SkipsAnswerShape(t *testing.T) {
	tasks := []Task{
		{ID: "a", Category: "answer_shape", Input: "q", MaxSteps: 30, Expect: Expectation{AnswerType: "count"}},
		{ID: "b", Category: "single_tool", Input: "q", MaxSteps: 3, Expect: Expectation{Tools: []string{"list_tables"}}},
	}
	res := runMock(tasks)
	if len(res) != 1 || res[0].TaskID != "b" {
		t.Fatalf("mock must skip answer_shape, got %+v", res)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "AnswerShape" -count=1`
Expected: FAIL / 编译失败（`AnswerType` 未定义）

- [ ] **Step 3: 实现**

`task.go`：

`Task` 增加字段（放在 `Fixtures` 之后）：
```go
	// SourceSession 题目来源的线上会话 ID（answer_shape 用于回溯）。
	SourceSession string `json:"source_session,omitempty"`
```

`Expectation` 末尾增加：
```go
	// AnswerType answer_shape 期望的答案形式：enumerate|count|lookup|diagnose|howto。
	AnswerType string `json:"answer_type,omitempty"`
	// ShapeMust / ShapeForbid 是给 judge 的自然语言要求与禁止项（区别于正则 forbid_output）。
	ShapeMust   []string `json:"shape_must,omitempty"`
	ShapeForbid []string `json:"shape_forbid,omitempty"`
```

更新 `Expectation` 上方注释，在 `investigation` 行后加一行：
```go
//   - answer_shape: answer_type（必填）+ shape_must / shape_forbid，由 judge 判定答案形式
```

`ValidCategories` 加 `"answer_shape": true,`，并新增：
```go
// ValidAnswerTypes answer_shape 的合法答案形式。
var ValidAnswerTypes = map[string]bool{
	"enumerate": true,
	"count":     true,
	"lookup":    true,
	"diagnose":  true,
	"howto":     true,
}
```

`Validate` 的类别 switch 中加：
```go
	case "answer_shape":
		if !ValidAnswerTypes[t.Expect.AnswerType] {
			return fmt.Errorf("answer_shape %s: expect.answer_type must be one of enumerate|count|lookup|diagnose|howto", t.ID)
		}
```

`mock.go` 的 `runMock` 循环开头：
```go
	for _, task := range tasks {
		// answer_shape 需要真实模型与 judge，脚本化回放没有意义。
		if task.Category == "answer_shape" {
			continue
		}
		results = append(results, runMockTask(task, reg))
	}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add evals/runner/task.go evals/runner/mock.go evals/runner/task_test.go evals/runner/mock_test.go
git commit -m "feat(evals): add answer_shape task category"
```

---

### Task 8: `TraceSummary`

**Files:**
- Create: `evals/runner/trace_summary.go`
- Test: `evals/runner/trace_summary_test.go`

- [ ] **Step 1: 写失败测试**

```go
package main

import (
	"testing"

	agent "github.com/sixath/framework/harness"
)

func TestResultHits(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"nil", nil, -1},
		{"es total", map[string]any{"total": float64(12)}, 12},
		{"es suspect", map[string]any{"total": float64(0), "hit_status": "suspect"}, 0},
		{"rows", map[string]any{"rows": []any{1, 2}}, 2},
		{"json string", `{"ok":true,"total":0}`, 0},
		{"plain string", "ok", -1},
		{"empty string", "", 0},
		{"array", []any{1}, 1},
		{"no count keys", map[string]any{"ok": true}, -1},
	}
	for _, c := range cases {
		if got := resultHits(c.in); got != c.want {
			t.Errorf("%s: resultHits=%d want %d", c.name, got, c.want)
		}
	}
}

func TestSummarizeRunTrace(t *testing.T) {
	tr := &agent.RunTrace{ToolCalls: []agent.ToolCallRecord{
		{ToolName: "es_log_query", Arguments: map[string]any{"query": "vmId:1"}, Result: `{"total":0,"hit_status":"suspect"}`},
		{ToolName: "execute_read", Error: "syntax error"},
		{ToolName: "es_log_query", Result: map[string]any{"total": float64(3)}},
	}}
	s := summarizeRunTrace(tr)
	if len(s.Calls) != 3 || s.EmptyCount() != 1 || s.ErrorCount() != 1 {
		t.Fatalf("summary=%+v empty=%d err=%d", s, s.EmptyCount(), s.ErrorCount())
	}
	if s.Calls[0].Args != `{"query":"vmId:1"}` {
		t.Fatalf("args=%q", s.Calls[0].Args)
	}
	if got := summarizeRunTrace(nil); len(got.Calls) != 0 {
		t.Fatal("nil trace must give empty summary")
	}
}

func TestSummarizeTimeline(t *testing.T) {
	tl := []any{
		map[string]any{"kind": "model", "step": float64(0), "phase": "responded"},
		map[string]any{"kind": "tool", "toolName": "es_log_query", "phase": "completed", "arguments": map[string]any{"q": "x"}, "result": map[string]any{"total": float64(0)}},
		map[string]any{"kind": "tool", "toolName": "ssh_exec", "phase": "failed"},
		map[string]any{"kind": "tool", "toolName": "terminal", "phase": "interrupted"},
	}
	s := summarizeTimeline(tl)
	if len(s.Calls) != 3 {
		t.Fatalf("want 3 tool calls, got %+v", s.Calls)
	}
	if !s.Calls[0].Empty || s.Calls[1].Error != "failed" || s.Calls[2].Error != "interrupted" {
		t.Fatalf("calls=%+v", s.Calls)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "ResultHits|Summarize" -count=1`
Expected: 编译失败，`undefined: resultHits`

- [ ] **Step 3: 实现**

```go
package main

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	agent "github.com/sixath/framework/harness"
)

const traceArgsMaxRunes = 500

// TraceCall 一次工具调用的摘要。Hits 为 -1 表示无法从结果判断命中数。
type TraceCall struct {
	Tool  string `json:"tool"`
	Args  string `json:"args,omitempty"`
	Error string `json:"error,omitempty"`
	Hits  int    `json:"hits"`
	Empty bool   `json:"empty,omitempty"`
}

// TraceSummary live 与 portal 两种运行方式共用的轨迹摘要；judge 与辅助指标只依赖它。
type TraceSummary struct {
	Calls []TraceCall `json:"calls"`
}

func (s TraceSummary) ErrorCount() int {
	n := 0
	for _, c := range s.Calls {
		if c.Error != "" {
			n++
		}
	}
	return n
}

func (s TraceSummary) EmptyCount() int {
	n := 0
	for _, c := range s.Calls {
		if c.Empty {
			n++
		}
	}
	return n
}

func summarizeRunTrace(tr *agent.RunTrace) TraceSummary {
	var s TraceSummary
	if tr == nil {
		return s
	}
	for _, c := range tr.ToolCalls {
		s.Calls = append(s.Calls, newTraceCall(c.ToolName, c.Arguments, c.Result, c.Error))
	}
	return s
}

// summarizeTimeline 从 Portal 消息 metadata.timeline（camelCase 键）构造摘要，只取 kind=tool 的节点。
func summarizeTimeline(timeline []any) TraceSummary {
	var s TraceSummary
	for _, n := range timeline {
		m, ok := n.(map[string]any)
		if !ok || m["kind"] != "tool" {
			continue
		}
		name, _ := m["toolName"].(string)
		errStr, _ := m["error"].(string)
		phase, _ := m["phase"].(string)
		if errStr == "" && (phase == "failed" || phase == "interrupted") {
			errStr = phase
		}
		s.Calls = append(s.Calls, newTraceCall(name, m["arguments"], m["result"], errStr))
	}
	return s
}

func newTraceCall(name string, args, result any, errStr string) TraceCall {
	c := TraceCall{Tool: name, Error: errStr, Hits: -1}
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			c.Args = truncateRunes(string(raw), traceArgsMaxRunes)
		}
	}
	if errStr == "" {
		c.Hits = resultHits(result)
		c.Empty = c.Hits == 0
	}
	return c
}

// resultHits 从工具结果中提取命中数；无法判断时返回 -1。
func resultHits(v any) int {
	switch t := v.(type) {
	case nil:
		return -1
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			switch parsed.(type) {
			case map[string]any, []any:
				return resultHits(parsed)
			}
		}
		return -1
	case []any:
		return len(t)
	case map[string]any:
		if hs, _ := t["hit_status"].(string); hs == "empty" || hs == "suspect" {
			return 0
		}
		for _, k := range []string{"total", "row_count", "count"} {
			if n, ok := asInt(t[k]); ok {
				return n
			}
		}
		for _, k := range []string{"rows", "hits", "items", "results"} {
			if arr, ok := t[k].([]any); ok {
				return len(arr)
			}
		}
		return -1
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return -1
		}
		var parsed any
		if json.Unmarshal(raw, &parsed) != nil {
			return -1
		}
		switch parsed.(type) {
		case map[string]any, []any:
			return resultHits(parsed)
		}
		return -1
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
```

先确认 runner 包内没有同名函数：`rg "func (asInt|truncateRunes)\(" evals/runner`。有冲突就把本文件的函数改名（如 `traceAsInt`）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add evals/runner/trace_summary.go evals/runner/trace_summary_test.go
git commit -m "feat(evals): add shared trace summary for live and portal runs"
```

---

### Task 9: Judge

**Files:**
- Create: `evals/runner/judge.go`
- Test: `evals/runner/judge_test.go`

- [ ] **Step 1: 写失败测试**

```go
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

type stubJudgeModel struct {
	replies []string
	errs    []error
	calls   int
	prompts []string
}

func (m *stubJudgeModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return nil, errors.New("unused")
}

func (m *stubJudgeModel) Chat(_ context.Context, msgs []model.Message, _ ...model.Option) (*model.Generation, error) {
	i := m.calls
	m.calls++
	m.prompts = append(m.prompts, msgs[len(msgs)-1].Content)
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i < len(m.replies) {
		return &model.Generation{Text: m.replies[i]}, nil
	}
	return &model.Generation{Text: ""}, nil
}

func (m *stubJudgeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

const allPass = `{"checks":[{"id":1,"pass":true,"reason":"清单"},{"id":2,"pass":true,"reason":"r"},{"id":3,"pass":true,"reason":"r"},{"id":4,"pass":true,"reason":"r"}],"attribution":""}`

var shapeTask = Task{ID: "s", Category: "answer_shape", Input: "最近一小时一直预启动失败的 vmid", MaxSteps: 30,
	Expect: Expectation{AnswerType: "enumerate", ShapeMust: []string{"开头列出 vmid"}, ShapeForbid: []string{"根因分析"}}}

func TestParseVerdict(t *testing.T) {
	v, err := parseVerdict("好的：\n```json\n" + allPass + "\n```")
	if err != nil || !v.Passed() {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if _, err := parseVerdict(`{"checks":[{"id":1,"pass":true}]}`); err == nil {
		t.Fatal("verdict with fewer than 4 checks must be rejected")
	}
	if _, err := parseVerdict(`{"checks":[{"id":1,"pass":true},{"id":2,"pass":true},{"id":3,"pass":true},{"id":4,"pass":false}],"attribution":"weird"}`); err == nil {
		t.Fatal("unknown attribution must be rejected")
	}
	if _, err := parseVerdict("no json"); err == nil {
		t.Fatal("text without JSON must be rejected")
	}
}

func TestJudge_RetriesOnceThenFails(t *testing.T) {
	m := &stubJudgeModel{replies: []string{"garbage", allPass}}
	v, err := (&Judge{Model: m}).Evaluate(context.Background(), shapeTask, "198002\n198065", TraceSummary{})
	if err != nil || !v.Passed() || m.calls != 2 {
		t.Fatalf("v=%+v err=%v calls=%d", v, err, m.calls)
	}
	m2 := &stubJudgeModel{replies: []string{"garbage", "garbage"}}
	if _, err := (&Judge{Model: m2}).Evaluate(context.Background(), shapeTask, "x", TraceSummary{}); err == nil || m2.calls != 2 {
		t.Fatalf("expected error after 2 attempts, err=%v calls=%d", err, m2.calls)
	}
}

func TestJudge_PromptCarriesExpectationsAndSuspectQueries(t *testing.T) {
	m := &stubJudgeModel{replies: []string{allPass}}
	ts := TraceSummary{Calls: []TraceCall{{Tool: "es_log_query", Args: `{"query":"flow_id:abc"}`, Hits: 0, Empty: true}}}
	_, _ = (&Judge{Model: m}).Evaluate(context.Background(), shapeTask, "没有找到相关日志", ts)
	p := m.prompts[0]
	for _, want := range []string{"enumerate", "开头列出 vmid", "根因分析", "没有找到相关日志", "flow_id:abc", "可疑空结果"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestSuspectAbsence(t *testing.T) {
	ts := TraceSummary{Calls: []TraceCall{{Tool: "a", Empty: true}, {Tool: "b", Hits: 3}}}
	if got := suspectAbsence("查不到任何记录", ts); len(got) != 1 || got[0].Tool != "a" {
		t.Fatalf("got %+v", got)
	}
	if got := suspectAbsence("共 3 台：1、2、3", ts); got != nil {
		t.Fatalf("no negation claim must give nil, got %+v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "Verdict|Judge|SuspectAbsence" -count=1`
Expected: 编译失败，`undefined: parseVerdict`

- [ ] **Step 3: 实现**

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sixath/framework/model"
)

const judgeMaxCalls = 40

// negationWords 答案中出现这些词视为"不存在"类结论，需要核对对应查询是否为可疑空结果（规则 4）。
var negationWords = []string{"没有", "不存在", "未发现", "未找到", "0 条", "0条", "无记录", "查不到"}

var validJudgeAttributions = map[string]bool{
	"": true, "prompt": true, "tool": true, "schema": true, "model": true, "harness": true, "understanding": true,
}

// JudgeCheck 一条规则的判定。
type JudgeCheck struct {
	ID     int    `json:"id"`
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

// JudgeVerdict judge 对一次运行的判定；四条规则全过才算通过。
type JudgeVerdict struct {
	Checks      []JudgeCheck `json:"checks"`
	Attribution string       `json:"attribution,omitempty"`
}

func (v JudgeVerdict) Passed() bool {
	if len(v.Checks) != 4 {
		return false
	}
	for _, c := range v.Checks {
		if !c.Pass {
			return false
		}
	}
	return true
}

// Judge 用独立模型判定答案形式是否回应了问题。
type Judge struct {
	Model model.Model
}

// Evaluate 调用 judge 模型；输出无法解析时重试 1 次。
func (j *Judge) Evaluate(ctx context.Context, task Task, answer string, ts TraceSummary) (JudgeVerdict, error) {
	prompt := buildJudgePrompt(task, answer, ts)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		gen, err := j.Model.Chat(ctx, []model.Message{{Role: "user", Content: prompt}},
			model.WithTemperature(0), model.WithMaxTokens(1024))
		if err != nil {
			lastErr = err
			continue
		}
		v, err := parseVerdict(gen.Text)
		if err == nil {
			return v, nil
		}
		lastErr = err
	}
	return JudgeVerdict{}, fmt.Errorf("judge: %w", lastErr)
}

func parseVerdict(text string) (JudgeVerdict, error) {
	i, k := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || k < i {
		return JudgeVerdict{}, errors.New("no JSON object in judge output")
	}
	var v JudgeVerdict
	if err := json.Unmarshal([]byte(text[i:k+1]), &v); err != nil {
		return JudgeVerdict{}, err
	}
	if len(v.Checks) != 4 {
		return JudgeVerdict{}, fmt.Errorf("judge returned %d checks, want 4", len(v.Checks))
	}
	for idx, c := range v.Checks {
		if c.ID != idx+1 {
			return JudgeVerdict{}, fmt.Errorf("judge check %d has id %d", idx+1, c.ID)
		}
	}
	if !validJudgeAttributions[v.Attribution] {
		return JudgeVerdict{}, fmt.Errorf("unknown attribution %q", v.Attribution)
	}
	return v, nil
}

func containsNegation(answer string) bool {
	for _, w := range negationWords {
		if strings.Contains(answer, w) {
			return true
		}
	}
	return false
}

// suspectAbsence 答案含"不存在"类结论时，返回轨迹中所有空结果查询，交给 judge 重点核对。
func suspectAbsence(answer string, ts TraceSummary) []TraceCall {
	if !containsNegation(answer) {
		return nil
	}
	var out []TraceCall
	for _, c := range ts.Calls {
		if c.Empty {
			out = append(out, c)
		}
	}
	return out
}

var answerTypeHints = map[string]string{
	"enumerate": "清单",
	"count":     "数字",
	"lookup":    "具体值",
	"diagnose":  "原因及依据",
	"howto":     "操作步骤",
}

func buildJudgePrompt(task Task, answer string, ts TraceSummary) string {
	calls := ts.Calls
	if len(calls) > judgeMaxCalls {
		calls = calls[len(calls)-judgeMaxCalls:]
	}
	callsJSON, _ := json.Marshal(calls)
	var b strings.Builder
	b.WriteString("你是评测打分器。只判断答案的形式是否回应了问题，不判断数值是否正确。\n\n")
	fmt.Fprintf(&b, "问题：%s\n", task.Input)
	fmt.Fprintf(&b, "期望答案类型：%s（%s）\n", task.Expect.AnswerType, answerTypeHints[task.Expect.AnswerType])
	fmt.Fprintf(&b, "必须满足：%s\n", joinOrNone(task.Expect.ShapeMust))
	fmt.Fprintf(&b, "禁止：%s\n\n", joinOrNone(task.Expect.ShapeForbid))
	fmt.Fprintf(&b, "工具调用摘要（JSON，hits=-1 表示无法判断命中数）：\n%s\n\n", callsJSON)
	if sus := suspectAbsence(answer, ts); len(sus) > 0 {
		susJSON, _ := json.Marshal(sus)
		fmt.Fprintf(&b, "注意：答案包含\"没有/不存在\"类结论，而以下查询返回空结果。请判断它们是否为可疑空结果（字段不存在、值是猜的、时间窗过短）：\n%s\n\n", susJSON)
	}
	fmt.Fprintf(&b, "最终答案：\n<<<\n%s\n>>>\n\n", answer)
	b.WriteString(`逐条判断：
1. 答案形式与期望类型一致。
2. 第一段直接回答问题，不先铺垫过程。
3. 满足"必须满足"，不触犯"禁止"，没有其他未被要求的扩展。
4. 凡声称没有/不存在/未发现，对应查询不是可疑空结果；答案没有此类结论时判通过。

任一条失败时给出 attribution：understanding（理解错问题）、harness（被调查流程等外层机制带偏）、tool（工具报错或查不到）、model（其他）。全部通过时 attribution 为空字符串。

只输出 JSON：{"checks":[{"id":1,"pass":true,"reason":"..."},{"id":2,"pass":true,"reason":"..."},{"id":3,"pass":true,"reason":"..."},{"id":4,"pass":true,"reason":"..."}],"attribution":""}`)
	return b.String()
}

func joinOrNone(ss []string) string {
	if len(ss) == 0 {
		return "无"
	}
	return strings.Join(ss, "；")
}
```

测试里断言 prompt 含"可疑空结果"，上面的注意段落和规则 4 文本都包含该词。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add evals/runner/judge.go evals/runner/judge_test.go
git commit -m "feat(evals): add answer-shape judge with rule-4 absence precheck"
```

---

### Task 10: 单次打分、多次合并与 `ShapeSummary`

**Files:**
- Create: `evals/runner/answer_shape.go`
- Modify: `evals/runner/metrics.go`
- Test: `evals/runner/answer_shape_test.go`、`evals/runner/metrics_test.go`

- [ ] **Step 1: 写失败测试**

`answer_shape_test.go`：

```go
package main

import (
	"context"
	"testing"
)

const firstFails = `{"checks":[{"id":1,"pass":false,"reason":"没给清单"},{"id":2,"pass":true,"reason":""},{"id":3,"pass":true,"reason":""},{"id":4,"pass":true,"reason":""}],"attribution":"harness"}`

func TestScoreAnswerShape(t *testing.T) {
	ts := TraceSummary{Calls: []TraceCall{{Tool: "es_log_query", Hits: 3}}}
	r := scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{replies: []string{allPass}}}, shapeTask, "198002", ts)
	if !r.Passed || r.Judge == nil || r.Trace == nil || len(r.ToolsUsed) != 1 {
		t.Fatalf("r=%+v", r)
	}
	r = scoreAnswerShape(context.Background(), &Judge{Model: &stubJudgeModel{replies: []string{firstFails}}}, shapeTask, "根因是…", ts)
	if r.Passed || r.FailureReason != "shape_mismatch" || r.Attribution != "harness" {
		t.Fatalf("r=%+v", r)
	}
	r = scoreAnswerShape(context.Background(), nil, shapeTask, "x", ts)
	if r.FailureReason != "judge_error" {
		t.Fatalf("nil judge must give judge_error, got %+v", r)
	}
}

func TestMergeRuns(t *testing.T) {
	pass := TaskResult{TaskID: "a", Passed: true, Output: "p"}
	fail := TaskResult{TaskID: "a", FailureReason: "shape_mismatch", Output: "f"}
	lost := TaskResult{TaskID: "a", FailureReason: "infra_error"}

	m := mergeRuns([]TaskResult{pass, fail, pass})
	if !m.Passed || m.Runs != 3 || m.Passes != 2 || m.Output != "p" {
		t.Fatalf("2/3 should pass: %+v", m)
	}
	m = mergeRuns([]TaskResult{pass, fail})
	if m.Passed || m.Runs != 2 || m.Passes != 1 || m.Output != "f" {
		t.Fatalf("1/2 is not a majority: %+v", m)
	}
	m = mergeRuns([]TaskResult{lost, pass, pass})
	if !m.Passed || m.Runs != 2 || m.InfraErrors != 1 {
		t.Fatalf("lost runs excluded from runs: %+v", m)
	}
	m = mergeRuns([]TaskResult{lost, lost})
	if m.Passed || m.Runs != 0 || m.InfraErrors != 2 || m.FailureReason != "infra_error" {
		t.Fatalf("all lost: %+v", m)
	}
}
```

追加到 `metrics_test.go`：

```go
func TestComputeSummary_AnswerShapeSeparated(t *testing.T) {
	tasks := []Task{
		{ID: "s1", Category: "answer_shape", Expect: Expectation{AnswerType: "enumerate"}},
		{ID: "s2", Category: "answer_shape", Expect: Expectation{AnswerType: "diagnose"}},
		{ID: "s3", Category: "answer_shape", Expect: Expectation{AnswerType: "count"}},
		{ID: "t1", Category: "single_tool", Expect: Expectation{Tools: []string{"list_tables"}}},
	}
	results := []TaskResult{
		{TaskID: "s1", Category: "answer_shape", Passed: true, Runs: 2, Passes: 2, Steps: 3,
			Trace: &TraceSummary{Calls: []TraceCall{{Tool: "a", Hits: 1}, {Tool: "b", Empty: true}}}},
		{TaskID: "s2", Category: "answer_shape", Runs: 2, Passes: 1, Steps: 5, Attribution: "harness", FailureReason: "shape_mismatch",
			Judge: &JudgeVerdict{Checks: []JudgeCheck{{ID: 1, Pass: true}, {ID: 2, Pass: true}, {ID: 3, Pass: true}, {ID: 4, Pass: false}}},
			Trace: &TraceSummary{Calls: []TraceCall{{Tool: "c", Error: "boom"}}}},
		{TaskID: "s3", Category: "answer_shape", FailureReason: "infra_error", InfraErrors: 2},
		{TaskID: "t1", Category: "single_tool", Passed: true, Steps: 2, ToolsUsed: []string{"list_tables"}},
	}
	s := ComputeSummary(results, tasks)
	if s.Total != 1 || s.CompletionRate != 1 {
		t.Fatalf("answer_shape must not count toward completion_rate: %+v", s)
	}
	a := s.AnswerShape
	if a == nil {
		t.Fatal("missing answer_shape summary")
	}
	if a.Tasks != 3 || a.Runs != 4 || a.Passes != 3 || a.LostRuns != 2 {
		t.Fatalf("counts %+v", a)
	}
	if a.PassRate != 0.75 || a.LostRate != 2.0/6 {
		t.Fatalf("rates pass=%v lost=%v", a.PassRate, a.LostRate)
	}
	if a.ByAnswerType["diagnose"].Total != 2 || a.ByAnswerType["diagnose"].Passed != 1 {
		t.Fatalf("by type %+v", a.ByAnswerType)
	}
	if a.Rule4Failures != 1 || a.Attribution["harness"] != 1 {
		t.Fatalf("rule4=%d attribution=%v", a.Rule4Failures, a.Attribution)
	}
	if a.ToolErrorRate != 1.0/3 || a.EmptyRate != 1.0/3 {
		t.Fatalf("tool rates err=%v empty=%v", a.ToolErrorRate, a.EmptyRate)
	}
}

func TestComputeSummary_OnlyAnswerShapeNoNaN(t *testing.T) {
	s := ComputeSummary([]TaskResult{{TaskID: "s1", Category: "answer_shape", Passed: true, Runs: 1, Passes: 1}}, nil)
	if s.Total != 0 || s.CompletionRate != 0 || s.AnswerShape == nil || s.AnswerShape.PassRate != 1 {
		t.Fatalf("s=%+v", s)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "ScoreAnswerShape|MergeRuns|AnswerShape" -count=1`
Expected: 编译失败

- [ ] **Step 3: 实现 `metrics.go`**

`TaskResult` 增加字段（放在 `Error` 之前），并更新 `FailureReason` 注释：

```go
	FailureReason string   `json:"failure_reason,omitempty"` // wrong_tool|tool_sequence|missing_output|over_steps|refused|model_error|premature_stop|too_few_tool_calls|no_contrast|stopped_at_mechanism|missing_root_cause|shape_mismatch|infra_error|judge_error
	Attribution   string   `json:"attribution,omitempty"`    // prompt|tool|schema|model|harness|understanding
```

```go
	// Runs / Passes answer_shape 多次运行中有效运行数与通过数；InfraErrors 为因 infra_error/judge_error 丢弃的运行数。
	Runs        int           `json:"runs,omitempty"`
	Passes      int           `json:"passes,omitempty"`
	InfraErrors int           `json:"infra_errors,omitempty"`
	Judge       *JudgeVerdict `json:"judge,omitempty"`
	Trace       *TraceSummary `json:"trace,omitempty"`
```

`Summary` 末尾加：

```go
	// AnswerShape answer_shape 任务的端到端汇总；这类任务不计入上面的 Total/CompletionRate。
	AnswerShape *ShapeSummary `json:"answer_shape,omitempty"`
```

新增类型：

```go
// ShapeSummary answer_shape 端到端汇总。ByAnswerType 中 Total/Passed 按运行次数计。
type ShapeSummary struct {
	Tasks         int                     `json:"tasks"`
	Runs          int                     `json:"runs"`
	Passes        int                     `json:"passes"`
	PassRate      float64                 `json:"e2e_pass_rate"`
	LostRuns      int                     `json:"lost_runs"`
	LostRate      float64                 `json:"lost_rate"`
	ByAnswerType  map[string]CategoryStat `json:"by_answer_type"`
	ToolErrorRate float64                 `json:"tool_error_rate"`
	EmptyRate     float64                 `json:"empty_rate"`
	Rule4Failures int                     `json:"rule4_failures"`
	AvgSteps      float64                 `json:"avg_steps"`
	Attribution   map[string]int          `json:"attribution"`
}
```

把 `ComputeSummary` 替换为（主体逻辑不变，只增加 answer_shape 分流与 Total=0 保护）：

```go
func ComputeSummary(results []TaskResult, tasks []Task) Summary {
	s := Summary{
		ByCategory:  map[string]CategoryStat{},
		Attribution: map[string]int{},
	}
	if len(results) == 0 {
		return s
	}

	expectTools := map[string][]string{}
	expectSeq := map[string][]string{}
	for _, t := range tasks {
		if len(t.Expect.ToolSequence) > 0 {
			expectSeq[t.ID] = t.Expect.ToolSequence
		} else if len(t.Expect.Tools) > 0 {
			expectTools[t.ID] = t.Expect.Tools
		}
	}

	var shapeResults []TaskResult
	var stepsSum, costSum, f1Sum, ledgerSum float64
	f1N, criticSum := 0, 0
	for _, r := range results {
		if r.Category == "answer_shape" {
			shapeResults = append(shapeResults, r)
			continue
		}
		s.Total++
		if r.Passed {
			s.Passed++
		}
		stepsSum += float64(r.Steps)
		costSum += r.CostUSD
		criticSum += r.CriticRounds
		if r.HitMaxSteps {
			s.HitMaxSteps++
		}
		if r.LedgerScore != nil {
			s.LedgerScored++
			ledgerSum += *r.LedgerScore
		}

		cs := s.ByCategory[r.Category]
		cs.Total++
		if r.Passed {
			cs.Passed++
		}
		s.ByCategory[r.Category] = cs

		if r.Attribution != "" {
			s.Attribution[r.Attribution]++
		}

		if seq, ok := expectSeq[r.TaskID]; ok {
			if sc := sequenceF1(r.ToolsUsed, seq); sc >= 0 {
				f1Sum += sc
				f1N++
			}
		} else if exp, ok := expectTools[r.TaskID]; ok {
			if sc := setF1(r.ToolsUsed, exp); sc >= 0 {
				f1Sum += sc
				f1N++
			}
		}
	}

	if s.Total > 0 {
		s.CompletionRate = float64(s.Passed) / float64(s.Total)
		s.AvgSteps = stepsSum / float64(s.Total)
		s.AvgCostUSD = costSum / float64(s.Total)
		s.AvgCriticRounds = float64(criticSum) / float64(s.Total)
	}
	if s.LedgerScored > 0 {
		s.AvgLedgerScore = ledgerSum / float64(s.LedgerScored)
	}
	if f1N > 0 {
		s.ToolF1 = f1Sum / float64(f1N)
	}
	for k, cs := range s.ByCategory {
		if cs.Total > 0 {
			cs.Rate = float64(cs.Passed) / float64(cs.Total)
			s.ByCategory[k] = cs
		}
	}
	s.AnswerShape = computeShapeSummary(shapeResults, tasks)
	return s
}
```

- [ ] **Step 4: 实现 `answer_shape.go`**

```go
package main

import "context"

// scoreAnswerShape 用 judge 判定一次运行。judge 未配置或输出不可解析时记 judge_error（不计入通过率）。
func scoreAnswerShape(ctx context.Context, j *Judge, task Task, answer string, ts TraceSummary) TaskResult {
	res := TaskResult{
		TaskID:   task.ID,
		Category: task.Category,
		Output:   answer,
		Steps:    len(ts.Calls) + 1,
		Trace:    &ts,
	}
	seen := map[string]bool{}
	for _, c := range ts.Calls {
		if c.Tool != "" && !seen[c.Tool] {
			seen[c.Tool] = true
			res.ToolsUsed = append(res.ToolsUsed, c.Tool)
		}
	}
	if j == nil || j.Model == nil {
		res.FailureReason = "judge_error"
		res.Error = "judge not configured"
		return res
	}
	v, err := j.Evaluate(ctx, task, answer, ts)
	if err != nil {
		res.FailureReason = "judge_error"
		res.Error = err.Error()
		return res
	}
	res.Judge = &v
	if v.Passed() {
		res.Passed = true
		return res
	}
	res.FailureReason = "shape_mismatch"
	res.Attribution = v.Attribution
	if res.Attribution == "" {
		res.Attribution = "model"
	}
	return res
}

func isLostRun(r TaskResult) bool {
	return r.FailureReason == "infra_error" || r.FailureReason == "judge_error"
}

// mergeRuns 合并同一任务的多次运行：丢失的运行不计入 Runs；Passed 取严格多数（Passes*2 > Runs）。
// 代表结果取与多数结论一致的最后一次有效运行，便于报告展示对应的输出与轨迹。
func mergeRuns(runs []TaskResult) TaskResult {
	var valid []TaskResult
	lost := 0
	for _, r := range runs {
		if isLostRun(r) {
			lost++
			continue
		}
		valid = append(valid, r)
	}
	if len(valid) == 0 {
		out := runs[len(runs)-1]
		out.Runs, out.Passes, out.InfraErrors, out.Passed = 0, 0, lost, false
		return out
	}
	passes := 0
	for _, v := range valid {
		if v.Passed {
			passes++
		}
	}
	passed := passes*2 > len(valid)
	out := valid[len(valid)-1]
	for i := len(valid) - 1; i >= 0; i-- {
		if valid[i].Passed == passed {
			out = valid[i]
			break
		}
	}
	out.Runs, out.Passes, out.InfraErrors, out.Passed = len(valid), passes, lost, passed
	return out
}

// runCounts 兼容未经 mergeRuns 的旧结果（Runs 与 InfraErrors 均为 0）。
func runCounts(r TaskResult) (runs, passes, lost int) {
	if r.Runs > 0 || r.InfraErrors > 0 {
		return r.Runs, r.Passes, r.InfraErrors
	}
	if isLostRun(r) {
		return 0, 0, 1
	}
	if r.Passed {
		return 1, 1, 0
	}
	return 1, 0, 0
}

func computeShapeSummary(results []TaskResult, tasks []Task) *ShapeSummary {
	if len(results) == 0 {
		return nil
	}
	types := map[string]string{}
	for _, t := range tasks {
		types[t.ID] = t.Expect.AnswerType
	}
	ss := &ShapeSummary{ByAnswerType: map[string]CategoryStat{}, Attribution: map[string]int{}}
	calls, errs, empties, steps := 0, 0, 0, 0
	for _, r := range results {
		ss.Tasks++
		runs, passes, lost := runCounts(r)
		ss.Runs += runs
		ss.Passes += passes
		ss.LostRuns += lost
		at := types[r.TaskID]
		if at == "" {
			at = "unknown"
		}
		cs := ss.ByAnswerType[at]
		cs.Total += runs
		cs.Passed += passes
		ss.ByAnswerType[at] = cs
		if !r.Passed && r.Attribution != "" {
			ss.Attribution[r.Attribution]++
		}
		if r.Judge != nil {
			for _, c := range r.Judge.Checks {
				if c.ID == 4 && !c.Pass {
					ss.Rule4Failures++
				}
			}
		}
		if r.Trace != nil {
			calls += len(r.Trace.Calls)
			errs += r.Trace.ErrorCount()
			empties += r.Trace.EmptyCount()
		}
		steps += r.Steps
	}
	if ss.Runs > 0 {
		ss.PassRate = float64(ss.Passes) / float64(ss.Runs)
	}
	if total := ss.Runs + ss.LostRuns; total > 0 {
		ss.LostRate = float64(ss.LostRuns) / float64(total)
	}
	if calls > 0 {
		ss.ToolErrorRate = float64(errs) / float64(calls)
		ss.EmptyRate = float64(empties) / float64(calls)
	}
	ss.AvgSteps = float64(steps) / float64(ss.Tasks)
	for k, cs := range ss.ByAnswerType {
		if cs.Total > 0 {
			cs.Rate = float64(cs.Passed) / float64(cs.Total)
			ss.ByAnswerType[k] = cs
		}
	}
	return ss
}
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add evals/runner/answer_shape.go evals/runner/answer_shape_test.go evals/runner/metrics.go evals/runner/metrics_test.go
git commit -m "feat(evals): score answer_shape runs and summarize end-to-end pass rate"
```

---

### Task 11: 门禁

**Files:**
- Modify: `evals/runner/gate.go`
- Test: `evals/runner/gate_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `gate_test.go`：

```go
func shapeSummary(rate, lost float64) *ShapeSummary {
	return &ShapeSummary{Runs: 20, PassRate: rate, LostRate: lost}
}

func TestEvaluateGate_AnswerShape(t *testing.T) {
	base := &Summary{AnswerShape: shapeSummary(0.70, 0)}
	if d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.65, 0)}, base); !d.GatePassed {
		t.Fatalf("5pt drop is within 8pt threshold: %+v", d.AnswerShape)
	}
	d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.60, 0)}, base)
	if d.GatePassed || d.AnswerShape.GatePassed {
		t.Fatalf("10pt drop must fail: %+v", d.AnswerShape)
	}
	if err := gateError(d); err == nil || !strings.Contains(err.Error(), "e2e_pass_rate") {
		t.Fatalf("gateError=%v", err)
	}
}

func TestEvaluateGate_AnswerShapeInvalidWithoutBaseline(t *testing.T) {
	d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.9, 0.25)}, nil)
	if d.GatePassed || !d.AnswerShape.Invalid {
		t.Fatalf(">20%% lost runs must invalidate the run: %+v", d.AnswerShape)
	}
}

func TestEvaluateGate_ShapeOnlyRunIgnoresCompletionBaseline(t *testing.T) {
	base := &Summary{Total: 48, CompletionRate: 0.9}
	if d := EvaluateGate(Summary{AnswerShape: shapeSummary(0.5, 0)}, base); !d.GatePassed {
		t.Fatalf("run without legacy tasks must not fail the completion gate: %+v", d)
	}
}

func TestTaskRegressions(t *testing.T) {
	base := []TaskResult{{TaskID: "a", Passed: true}, {TaskID: "b", Passed: true}, {TaskID: "c"}}
	cur := []TaskResult{{TaskID: "a", Passed: true}, {TaskID: "b"}, {TaskID: "c"}, {TaskID: "d"}}
	got := TaskRegressions(cur, base)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
	d := &Diff{GatePassed: true}
	d.applyRegressions(got)
	if d.GatePassed || d.AnswerShape == nil || len(d.AnswerShape.Regressed) != 1 {
		t.Fatalf("d=%+v", d)
	}
	if err := gateError(d); err == nil || !strings.Contains(err.Error(), "b") {
		t.Fatalf("gateError=%v", err)
	}
}
```

`gate_test.go` 的 import 改为 `import ("strings"; "testing")`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "EvaluateGate|TaskRegressions" -count=1`
Expected: 编译失败

- [ ] **Step 3: 实现**

`gate.go` 全文替换为：

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// DefaultGateThreshold 完成率相对 baseline 允许的最大下降（pt）。下降超过 2pt 判为回归。
const DefaultGateThreshold = 0.02

// ShapeGateThreshold answer_shape 端到端通过率允许的最大下降；高于真实环境约 5pt 的自然波动。
const ShapeGateThreshold = 0.08

// ShapeMaxLostRate infra_error + judge_error 占比超过该值时本次运行无效。
const ShapeMaxLostRate = 0.20

// Diff baseline 对比结果。
type Diff struct {
	CompletionRateDelta float64    `json:"completion_rate_delta"`
	ToolF1Delta         float64    `json:"tool_selection_f1_delta"`
	GatePassed          bool       `json:"gate_passed"`
	Threshold           float64    `json:"threshold"`
	AnswerShape         *ShapeDiff `json:"answer_shape,omitempty"`
}

// ShapeDiff answer_shape 门禁结果。
type ShapeDiff struct {
	PassRateDelta float64  `json:"e2e_pass_rate_delta"`
	Threshold     float64  `json:"threshold"`
	LostRate      float64  `json:"lost_rate"`
	Invalid       bool     `json:"invalid,omitempty"`
	Regressed     []string `json:"regressed,omitempty"`
	GatePassed    bool     `json:"gate_passed"`
}

// EvaluateGate 对比 baseline 与当前 summary。
// 旧类别：baseline 为 nil 或 Total==0、或本次没有旧类别任务时不设门禁。
// answer_shape：丢失运行占比超限即失败（无需基线）；有基线时比较 e2e_pass_rate。
func EvaluateGate(summary Summary, baseline *Summary) *Diff {
	d := &Diff{Threshold: DefaultGateThreshold, GatePassed: true}
	d.AnswerShape = evaluateShapeGate(summary.AnswerShape, baseline)
	if baseline != nil && baseline.Total > 0 && summary.Total > 0 {
		d.CompletionRateDelta = summary.CompletionRate - baseline.CompletionRate
		d.ToolF1Delta = summary.ToolF1 - baseline.ToolF1
		d.GatePassed = d.CompletionRateDelta >= -DefaultGateThreshold
	}
	if d.AnswerShape != nil && !d.AnswerShape.GatePassed {
		d.GatePassed = false
	}
	return d
}

func evaluateShapeGate(cur *ShapeSummary, baseline *Summary) *ShapeDiff {
	if cur == nil {
		return nil
	}
	sd := &ShapeDiff{Threshold: ShapeGateThreshold, LostRate: cur.LostRate, GatePassed: true}
	if cur.LostRate > ShapeMaxLostRate {
		sd.Invalid = true
		sd.GatePassed = false
		return sd
	}
	if baseline == nil || baseline.AnswerShape == nil || baseline.AnswerShape.Runs == 0 {
		return sd
	}
	sd.PassRateDelta = cur.PassRate - baseline.AnswerShape.PassRate
	sd.GatePassed = sd.PassRateDelta >= -ShapeGateThreshold
	return sd
}

// TaskRegressions 返回基线中通过、本次未通过的任务 ID（CI 单题门禁）。本次没跑到的任务不算回归。
func TaskRegressions(cur, base []TaskResult) []string {
	curByID := make(map[string]TaskResult, len(cur))
	for _, r := range cur {
		curByID[r.TaskID] = r
	}
	var out []string
	for _, b := range base {
		if !b.Passed {
			continue
		}
		if c, ok := curByID[b.TaskID]; ok && !c.Passed {
			out = append(out, b.TaskID)
		}
	}
	return out
}

func (d *Diff) applyRegressions(ids []string) {
	if len(ids) == 0 {
		return
	}
	if d.AnswerShape == nil {
		d.AnswerShape = &ShapeDiff{Threshold: ShapeGateThreshold}
	}
	d.AnswerShape.Regressed = ids
	d.AnswerShape.GatePassed = false
	d.GatePassed = false
}

// gateError 把未通过的门禁转成退出错误；通过时返回 nil。
func gateError(d *Diff) error {
	if d == nil || d.GatePassed {
		return nil
	}
	var parts []string
	if d.CompletionRateDelta < -d.Threshold {
		parts = append(parts, fmt.Sprintf("completion_rate dropped %.1fpt (threshold %.1fpt)", -d.CompletionRateDelta*100, d.Threshold*100))
	}
	if a := d.AnswerShape; a != nil && !a.GatePassed {
		if a.Invalid {
			parts = append(parts, fmt.Sprintf("answer_shape run invalid: %.0f%% runs lost to infra/judge errors (max %.0f%%)", a.LostRate*100, ShapeMaxLostRate*100))
		}
		if a.PassRateDelta < -a.Threshold {
			parts = append(parts, fmt.Sprintf("e2e_pass_rate dropped %.1fpt (threshold %.1fpt)", -a.PassRateDelta*100, a.Threshold*100))
		}
		if len(a.Regressed) > 0 {
			parts = append(parts, "answer_shape regressed tasks: "+strings.Join(a.Regressed, ", "))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "gate failed")
	}
	return fmt.Errorf("regression gate failed: %s", strings.Join(parts, "; "))
}

// LoadBaseline 读取 baseline Summary JSON；path 为空返回 nil（无基线）。
func LoadBaseline(path string) (*Summary, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
```

`main.go` 中三处 `if diff != nil && !diff.GatePassed { return fmt.Errorf("regression gate failed: ...") }`（`runMockMode`、`runLiveMode`、`runReport`）统一改为：

```go
	return gateError(diff)
```

（放在 `writeReport` 之后，替换原有 `if` 块和其后的 `return nil`。）

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`
Expected: PASS（含原有三个 `TestEvaluateGate_*`）

- [ ] **Step 5: 提交**

```bash
git add evals/runner/gate.go evals/runner/gate_test.go evals/runner/main.go
git commit -m "feat(evals): gate answer_shape on e2e pass rate, lost runs and per-task regressions"
```

---

### Task 12: live 模式接入 judge 与 `-repeat`

**Files:**
- Modify: `evals/runner/live.go`
- Modify: `evals/runner/main.go`
- Test: `evals/runner/live_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `live_test.go`：

```go
// toolErrorModel 先调用一个不存在的工具（产生工具错误），再给出最终答案。
type toolErrorModel struct{ calls int }

func (m *toolErrorModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "198002"}, nil
}
func (m *toolErrorModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "198002"}, nil
}
func (m *toolErrorModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (m *toolErrorModel) ChatWithTools(context.Context, []model.Message, *tool.Registry, ...model.Option) (*model.Generation, error) {
	m.calls++
	if m.calls == 1 {
		return &model.Generation{Raw: model.ToolStep{Used: true, ToolCallID: "c1", ToolName: "es_log_query", Arguments: map[string]any{"query": "x"}}}, nil
	}
	return &model.Generation{Text: "198002"}, nil
}

func failingESRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	_ = reg.Register(tool.Tool{
		Name:        "es_log_query",
		Description: "always fails",
		Execute: func(context.Context, map[string]any) (any, error) {
			return nil, errors.New("index is required")
		},
	})
	return reg
}

func TestRunLiveTask_AnswerShapeUsesJudgeEvenWithToolError(t *testing.T) {
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass}}}
	r := runLiveTask(shapeTask, failingESRegistry(), &toolErrorModel{}, liveOptions{Judge: j})
	if r.Trace == nil || r.Trace.ErrorCount() != 1 {
		t.Fatalf("test setup must produce one tool error, trace=%+v", r.Trace)
	}
	if !r.Passed || r.FailureReason != "" || r.Judge == nil {
		t.Fatalf("answer_shape must be judged on the answer, got %+v", r)
	}
}

func TestRunLive_RepeatsAndMergesAnswerShape(t *testing.T) {
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass, firstFails, allPass}}}
	res := runLive([]Task{shapeTask}, &toolErrorModel{}, liveOptions{Judge: j, Repeat: 3})
	if len(res) != 1 || res[0].Runs != 3 || res[0].Passes != 2 || !res[0].Passed {
		t.Fatalf("res=%+v", res)
	}
}
```

`runLiveTask` 在任务没有 fixtures、也没开 `-ledger`/`-timeline` 时直接使用传入的注册表，所以 `failingESRegistry` 能稳定制造一次工具报错。`runLive` 内部固定用 `buildMockRegistry()`，`TestRunLive_RepeatsAndMergesAnswerShape` 只校验合并，不依赖工具报错；`toolErrorModel` 跨重复共享实例，第二次起直接给答案，不影响该测试。`live_test.go` 的 import 改为：

```go
import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)
```

再追加 `buildJudge` 的单测（同文件，需额外 import `"strings"`）：

```go
func TestBuildJudge(t *testing.T) {
	if j, err := buildJudge(judgeFlags{}, "openai", "", "k"); j != nil || err != nil {
		t.Fatalf("empty -judge-model must give nil judge, got %v %v", j, err)
	}
	_, err := buildJudge(judgeFlags{Model: "Qwen-Max", SUTModel: "qwen-max"}, "openai", "http://x", "k")
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("same model (case-insensitive) must be refused, err=%v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "RunLiveTask_AnswerShape|RunLive_Repeats|BuildJudge" -count=1`
Expected: 编译失败（`liveOptions` 无 `Judge`/`Repeat`，`buildJudge` 未定义）

- [ ] **Step 3: 实现 `live.go`**

`liveOptions` 增加：

```go
	// Judge answer_shape 任务的打分器；Repeat 为 answer_shape 每题运行次数（<=1 为 1 次），其他类别不重复。
	Judge  *Judge
	Repeat int
```

`runLive` 改为：

```go
func runLive(tasks []Task, m model.Model, lo liveOptions) []TaskResult {
	reg := buildMockRegistry()
	results := make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		if task.Category != "answer_shape" {
			results = append(results, runLiveTask(task, reg, m, lo))
			continue
		}
		n := lo.Repeat
		if n < 1 {
			n = 1
		}
		runs := make([]TaskResult, 0, n)
		for i := 0; i < n; i++ {
			runs = append(runs, runLiveTask(task, reg, m, lo))
		}
		results = append(results, mergeRuns(runs))
	}
	return results
}
```

`runLiveTask` 中，在 `if task.Category == "investigation" {` 之前插入：

```go
	if task.Category == "answer_shape" {
		shaped := scoreAnswerShape(context.Background(), lo.Judge, task, resp.Text, summarizeRunTrace(tr))
		shaped.StopNudges, shaped.CriticRounds, shaped.HitMaxSteps = res.StopNudges, res.CriticRounds, res.HitMaxSteps
		return shaped
	}
```

- [ ] **Step 4: 实现 `main.go` 参数与 judge 构造**

在 flag 定义区（`casesDir` 之后）加：

```go
	judgeProvider := flag.String("judge-provider", "", "judge model provider (default: -provider)")
	judgeModel := flag.String("judge-model", "", "judge model name; required when tasks include answer_shape")
	judgeBaseURL := flag.String("judge-base-url", "", "judge model base URL (default: -base-url)")
	judgeAPIKey := flag.String("judge-api-key", os.Getenv("SATH_EVAL_JUDGE_API_KEY"), "judge API key (or env SATH_EVAL_JUDGE_API_KEY; default: -api-key)")
	sutModel := flag.String("sut-model", "", "model under test, used to refuse judging with the same model (live mode default: -model)")
	repeat := flag.Int("repeat", 1, "runs per answer_shape task; results are merged by majority")
	baselineResults := flag.String("baseline-results", "", "results JSONL used as per-task baseline (answer_shape CI gate)")
```

新增 `judgeFlags` 与构造函数：

```go
// judgeFlags 为 answer_shape 打分器配置；空字段回落到被测模型的同名配置。
type judgeFlags struct {
	Provider, Model, BaseURL, APIKey, SUTModel string
}

func buildJudge(jf judgeFlags, fallbackProvider, fallbackBaseURL, fallbackAPIKey string) (*Judge, error) {
	name := strings.TrimSpace(jf.Model)
	if name == "" {
		return nil, nil
	}
	if sut := strings.TrimSpace(jf.SUTModel); sut != "" && strings.EqualFold(sut, name) {
		return nil, fmt.Errorf("-judge-model %q must differ from the model under test", name)
	}
	cfg := model.ModelConfig{Provider: jf.Provider, Model: name, BaseURL: jf.BaseURL, APIKey: jf.APIKey}
	if cfg.Provider == "" {
		cfg.Provider = fallbackProvider
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = fallbackBaseURL
	}
	if cfg.APIKey == "" {
		cfg.APIKey = fallbackAPIKey
	}
	m, err := model.NewModelFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("judge model: %w", err)
	}
	return &Judge{Model: m}, nil
}

func hasAnswerShape(tasks []Task) bool {
	for _, t := range tasks {
		if t.Category == "answer_shape" {
			return true
		}
	}
	return false
}
```

`liveFlags` 增加 `Repeat int`、`BaselineResults string`、`Judge judgeFlags`。`main()` 中 live 调用传入：

```go
	jf := judgeFlags{Provider: *judgeProvider, Model: *judgeModel, BaseURL: *judgeBaseURL, APIKey: *judgeAPIKey, SUTModel: *sutModel}
```

```go
	case "live":
		err = runLiveMode(*tasksPath, *baselinePath, *outPath, *resultsOutPath, *baselineOutPath, *provider, *modelName, *baseURL, *apiKey, *stopRulesPath, liveFlags{Ledger: *ledger, Critic: *critic, Timeline: *timeline, ExtraSteps: *extraSteps, CasesDir: *casesDir, MaxOutputTokens: *maxOutputTokens, Repeat: *repeat, BaselineResults: *baselineResults, Judge: jf})
```

`runLiveMode` 中，在构造 `lo` 之后、`runLive` 之前加：

```go
	lo.Repeat = flags.Repeat
	if flags.Judge.SUTModel == "" {
		flags.Judge.SUTModel = modelName
	}
	lo.Judge, err = buildJudge(flags.Judge, provider, baseURL, apiKey)
	if err != nil {
		return err
	}
	if lo.Judge == nil && hasAnswerShape(tasks) {
		return fmt.Errorf("-judge-model is required for answer_shape tasks")
	}
```

把 `runLiveMode` 中 `results := runLive(tasks, m, lo)` 之后的写结果 / 基线 / 门禁 / 报告逻辑整体替换为：

```go
	results := runLive(tasks, m, lo)
	return finishRun("live", tasks, results, finishOpts{
		ResultsOut: resultsOutPath, BaselineOut: baselineOutPath, Baseline: baselinePath,
		BaselineResults: flags.BaselineResults, Out: outPath,
	})
```

并新增：

```go
// finishOpts 为一次运行收尾时的输出与门禁配置。
type finishOpts struct {
	ResultsOut, BaselineOut, Baseline, BaselineResults, Out string
}

// finishRun 写结果与基线，计算门禁（含 answer_shape 单题回归），输出报告。
func finishRun(mode string, tasks []Task, results []TaskResult, o finishOpts) error {
	summary := ComputeSummary(results, tasks)
	if o.ResultsOut != "" {
		if err := writeResults(o.ResultsOut, results); err != nil {
			return err
		}
	}
	if o.BaselineOut != "" {
		if err := writeBaseline(o.BaselineOut, &summary); err != nil {
			return err
		}
	}
	baseline, err := LoadBaseline(o.Baseline)
	if err != nil {
		return err
	}
	var diff *Diff
	if baseline != nil || summary.AnswerShape != nil || o.BaselineResults != "" {
		diff = EvaluateGate(summary, baseline)
	}
	if o.BaselineResults != "" {
		base, err := LoadResults(o.BaselineResults)
		if err != nil {
			return err
		}
		diff.applyRegressions(TaskRegressions(results, base))
	}
	if err := writeReport(o.Out, BuildReport(mode, summary, results, diff)); err != nil {
		return err
	}
	return gateError(diff)
}
```

`runLiveMode` 里原先只服务于收尾的变量（`summary`、`diff`）删掉后，检查 `go vet` 无未使用变量。

- [ ] **Step 5: 运行确认通过**

Run: `go test ./... -count=1 && go vet ./...`
Expected: PASS，无 vet 告警

- [ ] **Step 6: 提交**

```bash
git add evals/runner/live.go evals/runner/live_test.go evals/runner/main.go
git commit -m "feat(evals): judge answer_shape in live mode with repeated runs"
```

---

### Task 13: Portal 客户端

**Files:**
- Create: `evals/runner/portal.go`
- Test: `evals/runner/portal_test.go`

Portal 事实（实施前已核对）：
- 直连 Portal 时公开对话入站默认关闭，POST 返回 403。评测时要么指向 Gateway（路径相同），要么 Portal 以 `SATH_CHAT_PUBLIC_INBOUND_ENABLED=true` 启动。
- 鉴权头 `Authorization: Bearer <token>`，可选 `X-Org-Id`。SSE 路由鉴权失败返回 HTTP 200 + `event: error`。
- `POST /api/v1/agents/{agent}/sessions` body `{"title":...}`，响应 lowerCamel：`{"ret":{"code":0},"id":"..."}`。
- `POST /api/v1/sessions/{id}/messages/stream` body `{"content":...}`（snake_case），事件 `event: X\ndata: {json}\n\n`；`done` 与 `error` 为终止事件，`done` 不带正文。
- `GET /api/v1/sessions/{id}/messages` 响应 `{"items":[{"role","content","metadata":{"timeline":[...]}}]}`；权威终答取最后一条 `role=="assistant"` 的 `content`。

- [ ] **Step 1: 写失败测试**

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePortal struct {
	createStatus int
	streamBody   string
	answer       string
	slowStream   time.Duration
	gotAuth      string
}

func (f *fakePortal) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agents/{agent}/sessions", func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		if f.createStatus != 0 {
			w.WriteHeader(f.createStatus)
			_, _ = w.Write([]byte(`{"ret":{"code":500,"message":"boom"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":{"code":0},"id":"sess-1","agentId":"` + r.PathValue("agent") + `"}`))
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if f.slowStream > 0 {
			time.Sleep(f.slowStream)
		}
		_, _ = w.Write([]byte(f.streamBody))
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ret":{"code":0},"items":[{"role":"user","content":"q"},{"role":"assistant","content":%q,"metadata":{"timeline":[{"kind":"tool","toolName":"es_log_query","phase":"completed","result":{"total":2}}]}}]}`, f.answer)
	})
	return mux
}

const sseDone = "event: chunk\ndata: {\"content\":\"草稿\"}\n\nevent: done\ndata: {\"content\":\"\",\"done\":true}\n\n"

func newTestClient(url string) *PortalClient {
	return &PortalClient{BaseURL: url, Token: "tok", OrgID: "default", HTTP: http.DefaultClient, Timeout: 2 * time.Second, PollInterval: 10 * time.Millisecond}
}

func TestPortalClient_RunTurn(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002\n198065"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "agent-1", "eval-x", "q")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Failed || turn.Answer != "198002\n198065" || len(turn.Timeline) != 1 {
		t.Fatalf("turn=%+v", turn)
	}
	if f.gotAuth != "Bearer tok" {
		t.Fatalf("auth header=%q", f.gotAuth)
	}
}

func TestPortalClient_SSEErrorIsRunFailure(t *testing.T) {
	f := &fakePortal{streamBody: "event: error\ndata: {\"error\":\"model timeout\"}\n\n"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || !turn.Failed || !strings.Contains(turn.Error, "model timeout") {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_StreamCutFallsBackToPolling(t *testing.T) {
	f := &fakePortal{streamBody: "event: chunk\ndata: {\"content\":\"半截\"}\n\n", answer: "最终答案"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	turn, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q")
	if err != nil || turn.Answer != "最终答案" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
}

func TestPortalClient_InfraErrors(t *testing.T) {
	f := &fakePortal{createStatus: http.StatusInternalServerError}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	if _, err := newTestClient(ts.URL).RunTurn(context.Background(), "a", "t", "q"); err == nil {
		t.Fatal("5xx on create session must be an error")
	}

	slow := &fakePortal{streamBody: sseDone, slowStream: 500 * time.Millisecond, answer: "x"}
	ts2 := httptest.NewServer(slow.handler(t))
	defer ts2.Close()
	c := newTestClient(ts2.URL)
	c.Timeout = 100 * time.Millisecond
	if _, err := c.RunTurn(context.Background(), "a", "t", "q"); err == nil {
		t.Fatal("exceeding the turn timeout must be an error")
	}
}

func TestRunPortal_ScoresAndMerges(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "198002"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	j := &Judge{Model: &stubJudgeModel{replies: []string{allPass, allPass}}}
	res := runPortal([]Task{shapeTask}, newTestClient(ts.URL), portalRunOptions{AgentID: "a", RunID: "r1", Judge: j, Repeat: 2, Concurrency: 1})
	if len(res) != 1 || res[0].Runs != 2 || !res[0].Passed || res[0].Trace == nil || len(res[0].Trace.Calls) != 1 {
		t.Fatalf("res=%+v", res)
	}
}
```

`http.ServeMux` 的 `"POST /path/{x}"` 模式与 `r.PathValue` 需要 Go 1.22+；runner 是 Go 1.26，可直接用。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run "PortalClient|RunPortal" -count=1`
Expected: 编译失败，`undefined: PortalClient`

- [ ] **Step 3: 实现**

```go
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PortalClient 通过 Portal（或 Gateway）的对话接口驱动真实 agent。
// 直连 Portal 需以 SATH_CHAT_PUBLIC_INBOUND_ENABLED=true 启动，否则 POST 返回 403。
type PortalClient struct {
	BaseURL      string
	Token        string
	OrgID        string
	HTTP         *http.Client
	Timeout      time.Duration // 单轮从发送到拿到终答的总时限
	PollInterval time.Duration // SSE 中断后轮询消息的间隔
}

// portalTurn 一轮对话的结果。Failed 表示 agent 本轮以 SSE error 结束（计入真实失败，不是 infra 错误）。
type portalTurn struct {
	Answer   string
	Timeline []any
	Failed   bool
	Error    string
}

type portalRet struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RunTurn 新建会话、发送问题、等待结束并取回终答与执行时间线。返回的 error 都视为 infra 错误。
func (c *PortalClient) RunTurn(ctx context.Context, agentID, title, content string) (portalTurn, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	sessionID, err := c.createSession(ctx, agentID, title)
	if err != nil {
		return portalTurn{}, err
	}
	failed, errMsg, completed, err := c.stream(ctx, sessionID, content)
	if err != nil {
		return portalTurn{}, err
	}
	if failed {
		return portalTurn{Failed: true, Error: errMsg}, nil
	}
	for {
		answer, timeline, found, err := c.lastAssistant(ctx, sessionID)
		if err != nil {
			return portalTurn{}, err
		}
		if found {
			return portalTurn{Answer: answer, Timeline: timeline}, nil
		}
		if completed {
			return portalTurn{}, errors.New("portal: stream completed but no assistant message was persisted")
		}
		select {
		case <-ctx.Done():
			return portalTurn{}, fmt.Errorf("portal: waiting for reply: %w", ctx.Err())
		case <-time.After(c.PollInterval):
		}
	}
}

func (c *PortalClient) createSession(ctx context.Context, agentID, title string) (string, error) {
	var out struct {
		Ret *portalRet `json:"ret"`
		ID  string     `json:"id"`
	}
	path := "/api/v1/agents/" + url.PathEscape(agentID) + "/sessions"
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]any{"title": title}, &out); err != nil {
		return "", fmt.Errorf("portal: create session: %w", err)
	}
	if out.ID == "" {
		return "", errors.New("portal: create session returned no id")
	}
	return out.ID, nil
}

// stream 发送消息并读 SSE。completed=true 表示读到了 done；SSE 中途断开时 completed=false、err=nil，由调用方轮询兜底。
func (c *PortalClient) stream(ctx context.Context, sessionID, content string) (failed bool, errMsg string, completed bool, err error) {
	body, _ := json.Marshal(map[string]any{"content": content})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/sessions/"+url.PathEscape(sessionID)+"/messages/stream", bytes.NewReader(body))
	if err != nil {
		return false, "", false, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, "", false, fmt.Errorf("portal: send message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, "", false, fmt.Errorf("portal: send message: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			switch event {
			case "done":
				return false, "", true, nil
			case "error":
				var e struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal([]byte(data), &e)
				if e.Error == "" {
					e.Error = data
				}
				return true, e.Error, true, nil
			}
		}
	}
	if ctx.Err() != nil {
		return false, "", false, fmt.Errorf("portal: stream: %w", ctx.Err())
	}
	return false, "", false, nil
}

func (c *PortalClient) lastAssistant(ctx context.Context, sessionID string) (answer string, timeline []any, found bool, err error) {
	var out struct {
		Ret   *portalRet `json:"ret"`
		Items []struct {
			Role     string         `json:"role"`
			Content  string         `json:"content"`
			Metadata map[string]any `json:"metadata"`
		} `json:"items"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(sessionID)+"/messages", nil, &out); err != nil {
		return "", nil, false, fmt.Errorf("portal: list messages: %w", err)
	}
	for i := len(out.Items) - 1; i >= 0; i-- {
		it := out.Items[i]
		if it.Role != "assistant" {
			continue
		}
		tl, _ := it.Metadata["timeline"].([]any)
		return it.Content, tl, true, nil
	}
	return "", nil, false, nil
}

func (c *PortalClient) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 300))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	var ret struct {
		Ret *portalRet `json:"ret"`
	}
	if json.Unmarshal(raw, &ret) == nil && ret.Ret != nil && ret.Ret.Code != 0 {
		return fmt.Errorf("ret.code=%d: %s", ret.Ret.Code, ret.Ret.Message)
	}
	return nil
}

func (c *PortalClient) setHeaders(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.OrgID != "" {
		req.Header.Set("X-Org-Id", c.OrgID)
	}
}

// portalRunOptions portal 模式运行配置。
type portalRunOptions struct {
	AgentID     string
	RunID       string
	Judge       *Judge
	Repeat      int
	Concurrency int
}

// runPortal 只运行 answer_shape 任务；每题按 Repeat 顺序跑多次后合并，题目之间按 Concurrency 并发。
func runPortal(tasks []Task, c *PortalClient, o portalRunOptions) []TaskResult {
	var shape []Task
	for _, t := range tasks {
		if t.Category == "answer_shape" {
			shape = append(shape, t)
		}
	}
	repeat, conc := o.Repeat, o.Concurrency
	if repeat < 1 {
		repeat = 1
	}
	if conc < 1 {
		conc = 1
	}
	results := make([]TaskResult, len(shape))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i, task := range shape {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, task Task) {
			defer wg.Done()
			defer func() { <-sem }()
			runs := make([]TaskResult, 0, repeat)
			for r := 0; r < repeat; r++ {
				runs = append(runs, runPortalTask(context.Background(), c, o, task, r))
			}
			results[i] = mergeRuns(runs)
		}(i, task)
	}
	wg.Wait()
	return results
}

func runPortalTask(ctx context.Context, c *PortalClient, o portalRunOptions, task Task, attempt int) TaskResult {
	title := fmt.Sprintf("eval-%s-%s-%d", o.RunID, task.ID, attempt+1)
	turn, err := c.RunTurn(ctx, o.AgentID, title, task.Input)
	if err != nil {
		return TaskResult{TaskID: task.ID, Category: task.Category, FailureReason: "infra_error", Error: err.Error()}
	}
	if turn.Failed {
		return TaskResult{TaskID: task.ID, Category: task.Category, FailureReason: "model_error", Attribution: "model", Error: turn.Error}
	}
	return scoreAnswerShape(ctx, o.Judge, task, turn.Answer, summarizeTimeline(turn.Timeline))
}
```

注意 `TestRunPortal_ScoresAndMerges` 中 `stubJudgeModel` 被两个并发 goroutine 共享时会有数据竞争；该测试用 `Concurrency: 1` 且单题顺序重复，不并发。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1 -race`
Expected: PASS，无 race 报告

- [ ] **Step 5: 提交**

```bash
git add evals/runner/portal.go evals/runner/portal_test.go
git commit -m "feat(evals): add portal client to run answer_shape tasks against real agents"
```

---

### Task 14: `-mode portal` 入口

**Files:**
- Modify: `evals/runner/main.go`
- Test: `evals/runner/main_portal_test.go`

- [ ] **Step 1: 写失败测试**

```go
package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPortalMode_RequiresJudgeAndAgent(t *testing.T) {
	dir := t.TempDir()
	tasks := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(tasks, []byte(`{"id":"s","category":"answer_shape","input":"q","max_steps":30,"expect":{"answer_type":"count"}}`+"\n"), 0o644)

	err := runPortalMode(portalModeConfig{TasksPath: tasks, PortalURL: "http://x", AgentID: ""})
	if err == nil || !strings.Contains(err.Error(), "-agent") {
		t.Fatalf("err=%v", err)
	}
	err = runPortalMode(portalModeConfig{TasksPath: tasks, PortalURL: "http://x", AgentID: "a"})
	if err == nil || !strings.Contains(err.Error(), "-judge-model") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunPortalMode_EndToEnd(t *testing.T) {
	f := &fakePortal{streamBody: sseDone, answer: "3 台"}
	ts := httptest.NewServer(f.handler(t))
	defer ts.Close()
	dir := t.TempDir()
	tasks := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(tasks, []byte(`{"id":"s","category":"answer_shape","input":"q","max_steps":30,"expect":{"answer_type":"count"}}`+"\n"), 0o644)
	resultsOut := filepath.Join(dir, "r.jsonl")

	err := runPortalMode(portalModeConfig{
		TasksPath: tasks, PortalURL: ts.URL, AgentID: "a", Token: "tok", Repeat: 1, Concurrency: 1,
		Judge: &Judge{Model: &stubJudgeModel{replies: []string{allPass}}},
		Finish: finishOpts{ResultsOut: resultsOut, Out: filepath.Join(dir, "report.json")},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := LoadResults(resultsOut)
	if err != nil || len(res) != 1 || !res[0].Passed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -run RunPortalMode -count=1`
Expected: 编译失败，`undefined: runPortalMode`

- [ ] **Step 3: 实现**

`main.go` flag 区加：

```go
	portalURL := flag.String("portal-url", "", "Portal or Gateway base URL (portal mode)")
	agentID := flag.String("agent", "", "agent id to evaluate (portal mode)")
	portalToken := flag.String("token", os.Getenv("SATH_EVAL_PORTAL_TOKEN"), "bearer token (portal mode; or env SATH_EVAL_PORTAL_TOKEN)")
	orgID := flag.String("org", "default", "X-Org-Id header (portal mode)")
	concurrency := flag.Int("concurrency", 2, "parallel tasks (portal mode)")
	turnTimeout := flag.Duration("turn-timeout", 10*time.Minute, "per-turn timeout from send to final reply (portal mode)")
```

import 加 `"time"`。更新文件头注释与 `-mode` 帮助文本为 `report|mock|live|portal`，注释补一行：

```go
//   - portal：通过 Portal/Gateway 对话接口驱动真实 agent 跑 answer_shape 任务，judge 打分（nightly 真实环境）。
```

`switch *mode` 增加：

```go
	case "portal":
		var j *Judge
		j, err = buildJudge(jf, *provider, *baseURL, *apiKey)
		if err == nil {
			err = runPortalMode(portalModeConfig{
				TasksPath: *tasksPath, PortalURL: *portalURL, AgentID: *agentID, Token: *portalToken, OrgID: *orgID,
				Repeat: *repeat, Concurrency: *concurrency, TurnTimeout: *turnTimeout, Judge: j,
				Finish: finishOpts{ResultsOut: *resultsOutPath, BaselineOut: *baselineOutPath, Baseline: *baselinePath, BaselineResults: *baselineResults, Out: *outPath},
			})
		}
```

`jf` 的定义需移到 `switch` 之前（Task 12 已在 live 分支前定义则复用）。portal 模式下被测模型未知，`-sut-model` 由使用者显式传入才做同模型检查。

新增：

```go
// portalModeConfig portal 模式参数。
type portalModeConfig struct {
	TasksPath, PortalURL, AgentID, Token, OrgID string
	Repeat, Concurrency                         int
	TurnTimeout                                 time.Duration
	Judge                                       *Judge
	Finish                                      finishOpts
}

func runPortalMode(cfg portalModeConfig) error {
	if cfg.TasksPath == "" {
		return fmt.Errorf("-tasks is required for portal mode")
	}
	if strings.TrimSpace(cfg.PortalURL) == "" {
		return fmt.Errorf("-portal-url is required for portal mode")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return fmt.Errorf("-agent is required for portal mode")
	}
	if cfg.Judge == nil {
		return fmt.Errorf("-judge-model is required for portal mode")
	}
	tasks, err := LoadTasksFromPath(cfg.TasksPath)
	if err != nil {
		return err
	}
	if !hasAnswerShape(tasks) {
		return fmt.Errorf("portal mode runs answer_shape tasks only; none found in %s", cfg.TasksPath)
	}
	timeout := cfg.TurnTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	client := &PortalClient{
		BaseURL:      strings.TrimRight(cfg.PortalURL, "/"),
		Token:        cfg.Token,
		OrgID:        cfg.OrgID,
		HTTP:         &http.Client{},
		Timeout:      timeout,
		PollInterval: 3 * time.Second,
	}
	runID := time.Now().UTC().Format("20060102T150405")
	results := runPortal(tasks, client, portalRunOptions{
		AgentID: cfg.AgentID, RunID: runID, Judge: cfg.Judge, Repeat: cfg.Repeat, Concurrency: cfg.Concurrency,
	})
	return finishRun("portal", tasks, results, cfg.Finish)
}
```

import 加 `"net/http"`。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1 -race && go vet ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add evals/runner/main.go evals/runner/main_portal_test.go
git commit -m "feat(evals): add portal mode entry point"
```

---

### Task 15: 任务集与文档

**Files:**
- Create: `evals/tasks_portal/answer_shape.jsonl`（真实环境题，portal 模式）
- Create: `evals/tasks_ci/answer_shape_ci.jsonl`（带 fixtures 的 CI 题，live 模式）
- Modify: `evals/README.md`

两类题都不放 `evals/tasks/`：现有每晚命令 `-mode=live -tasks ../tasks` 会加载该目录全部 `*.jsonl`，混入 answer_shape 题会因缺 `-judge-model` 退出；runner 也不支持按标签过滤。

- [ ] **Step 1: 写入种子任务**

`evals/tasks_portal/answer_shape.jsonl`（复盘会话的两道失败题）：

```jsonl
# 复盘会话 8a9e9efb 的两道失败题（真实环境 portal 模式）
{"id":"shape-vm-prelaunch-fail-list","category":"answer_shape","max_steps":30,"input":"查看最近一个小时一直预启动失败的 vmid","expect":{"answer_type":"enumerate","shape_must":["开头直接列出 vmid 清单"],"shape_forbid":["未被要求的根因分析或修复建议"]},"source_session":"8a9e9efb-a323-403a-813f-404cea99620d","tags":["vm","prelaunch"]}
{"id":"shape-vm-prelaunch-with-success","category":"answer_shape","max_steps":30,"input":"一直预启动但收到成功事件的 vmid 有哪些","expect":{"answer_type":"enumerate","shape_must":["开头直接列出同时有反复预启动和成功回调的 vmid 清单，或说明查询条件后明确给出空清单"],"shape_forbid":["把问题改成失败 VM 有没有收到成功事件"]},"source_session":"8a9e9efb-a323-403a-813f-404cea99620d","tags":["vm","prelaunch"]}
```

`evals/tasks_ci/answer_shape_ci.jsonl`（fixtures 合成工具返回）：

```jsonl
{"id":"shape-ci-prelaunch-list","category":"answer_shape","max_steps":12,"input":"查看最近一个小时一直预启动失败的 vmid","expect":{"answer_type":"enumerate","shape_must":["开头直接列出 vmid 清单"],"shape_forbid":["未被要求的根因分析或修复建议"]},"tags":["ci"],"fixtures":[{"tool":"es_log_query","response":"{\"ok\":true,\"total\":4,\"hits\":[{\"@timestamp\":\"2026-09-30T10:01:00Z\",\"message\":\"prelaunchFailCallback vmId=198002 err cnt check failed\"},{\"@timestamp\":\"2026-09-30T10:02:00Z\",\"message\":\"prelaunchFailCallback vmId=198065 err cnt check failed\"},{\"@timestamp\":\"2026-09-30T10:03:00Z\",\"message\":\"prelaunchFailCallback vmId=198002 err cnt check failed\"},{\"@timestamp\":\"2026-09-30T10:04:00Z\",\"message\":\"prelaunchFailCallback vmId=203644 err cnt check failed\"}]}"}]}
{"id":"shape-ci-unknown-field-empty","category":"answer_shape","max_steps":12,"input":"查一下最近一小时 flow_id 为 abc123 的日志有哪些","expect":{"answer_type":"enumerate","shape_must":["开头列出匹配的日志或明确说明结果"],"shape_forbid":["仅凭字段不存在导致的空结果就断言没有日志"]},"tags":["ci"],"fixtures":[{"tool":"es_log_query","match":"flow_id","response":"{\"ok\":true,\"total\":0,\"hit_status\":\"suspect\",\"unknown_fields\":[\"flow_id\"],\"field_hints\":[\"message\"]}"},{"tool":"es_log_query","match":"abc123","response":"{\"ok\":true,\"total\":2,\"hits\":[{\"message\":\"flow abc123 start\"},{\"message\":\"flow abc123 done\"}]}"}]}
```

- [ ] **Step 2: 验证可加载**

Run: `go run . -mode=mock -tasks ../tasks_portal -out - && go run . -mode=mock -tasks ../tasks_ci -out -`
Expected: 两次都退出码 0（mock 跳过 answer_shape，只验证文件能通过加载校验；`summary.total` 为 0）。
Run: `go run . -mode=mock -tasks ../tasks -out -`
Expected: 退出码 0，`summary.total` 与改动前一致。

- [ ] **Step 3: 补充其余题目（人工挑选）**

从线上会话中再挑 16–26 题，使总数达到 20–30，满足：
- 五种 `answer_type` 都有，`diagnose` 不少于 5 题。
- 问题文本去掉内部主机名、IP、账号；`source_session` 填会话 ID。
- 每题 `shape_must` / `shape_forbid` 写成 judge 可判断的具体要求，不写数值答案。
- 能用合成数据复现的再往 `evals/tasks_ci/answer_shape_ci.jsonl` 补 3–6 题，使 CI 子集达到 5–8 题。

每加一批运行 Step 2 的命令确认可加载。

- [ ] **Step 4: 更新 README**

在 `evals/README.md` 中：
- 目录结构加 `tasks_portal/`（真实环境 answer_shape 题）与 `tasks_ci/`（CI 用 answer_shape 题），说明它们不放 `tasks/` 的原因；类别表加一行：`answer_shape | answer_type（+ shape_must / shape_forbid） | judge 判定答案形式是否回应问题`。
- 新增"答案对题评测（answer_shape）"一节：

````markdown
## 答案对题评测（answer_shape）

answer_shape 只判答案形式，不判数值是否正确。judge 模型必须与被测模型不同，按四条规则打分：形式与 `answer_type` 一致、第一段直接回答、满足 `shape_must` 且不触犯 `shape_forbid`、"没有/不存在"类结论不是建立在可疑空结果上。

这类任务单独汇总为 `summary.answer_shape`（`e2e_pass_rate` 等），不计入 `completion_rate`；mock 模式跳过它们。

```bash
# 每晚：真实环境（Portal 需 SATH_CHAT_PUBLIC_INBOUND_ENABLED=true，或把 -portal-url 指向 Gateway）
# 首次运行时基线文件还不存在，先去掉 -baseline 跑一次生成基线
mkdir -p ../results/nightly ../reports/history
go run . -mode=portal -tasks ../tasks_portal \
  -portal-url http://<portal-or-gateway> -agent <agent-id> -token "$SATH_EVAL_PORTAL_TOKEN" \
  -judge-model <judge-model> -judge-base-url <url> -judge-api-key "$SATH_EVAL_JUDGE_API_KEY" -sut-model <agent-model> \
  -repeat 2 -baseline ../reports/answer_shape_baseline.json \
  -results-out ../results/nightly/$(date +%F).jsonl -out ../reports/history/answer_shape_$(date +%F).json

# CI：只跑带 ci 标签、有 fixtures 的题，每题 3 次取多数，对比单题基线
go run . -mode=live -tasks ../tasks_ci/answer_shape_ci.jsonl -model <m> -base-url <url> \
  -judge-model <judge-model> -repeat 3 -max-output-tokens 4096 \
  -baseline-results ../baselines/answer_shape_ci.jsonl -results-out ci.jsonl -out ci.json
```

门禁：
- 丢失运行（`infra_error` + `judge_error`）超过 20% 时本次运行无效。
- 每晚：`e2e_pass_rate` 较基线下降超过 8pt 失败（真实环境自然波动约 5pt）。
- CI：基线中通过的题本次未通过即失败。
````

- [ ] **Step 5: 提交**

```bash
git add evals/tasks_portal/answer_shape.jsonl evals/tasks_ci/answer_shape_ci.jsonl evals/README.md
git commit -m "feat(evals): add answer_shape task set and usage docs"
```

---

### Task 16: 产出第一份基线（运行，不改代码）

- [ ] **Step 1: CI 子集基线**

```bash
cd evals/runner
mkdir -p ../baselines
go run . -mode=live -tasks ../tasks_ci/answer_shape_ci.jsonl -model <m> -base-url <url> \
  -judge-model <judge-model> -repeat 3 -max-output-tokens 4096 \
  -results-out ../baselines/answer_shape_ci.jsonl -out -
```

检查每题 `runs=3`、`judge` 字段存在。把 `evals/baselines/answer_shape_ci.jsonl` 提交。

- [ ] **Step 2: 真实环境基线（连续两晚）**

按 README 的每晚命令运行两次（不带 `-baseline`），第二次加 `-baseline-out ../reports/answer_shape_baseline.json`。

验收（对应 spec §7 成功标准 2、3）：
- 两次 `summary.answer_shape.e2e_pass_rate` 相差 ≤5pt；超过则检查 `lost_rate` 与 judge 理由，先修评测本身再定基线。
- `shape-vm-prelaunch-fail-list` 与 `shape-vm-prelaunch-with-success` 判为不通过，归因为 `understanding` 或 `harness`。若判为通过，说明 judge 规则太松，调整 `shape_must` 或提示词后重跑。

- [ ] **Step 3: 提交基线**

```bash
git add evals/baselines/answer_shape_ci.jsonl evals/reports/answer_shape_baseline.json
git commit -m "chore(evals): record first answer_shape baselines"
```

---

## 完成后检查

- [ ] `cd framework && go test ./redact/ ./harness/ ./model/ ./tool/ -count=1` 全绿
- [ ] `cd portal && go test ./internal/service/ -count=1` 全绿
- [ ] `cd evals/runner && go test ./... -count=1 -race && go vet ./...` 全绿
- [ ] `go run . -mode=mock -tasks ../tasks -out -` 结果与改动前一致

`-race` 在 Windows 上需要 `CGO_ENABLED=1` 和可用的 gcc；不满足时去掉 `-race` 运行，并在 Linux CI 上补跑一次带 `-race` 的测试。
