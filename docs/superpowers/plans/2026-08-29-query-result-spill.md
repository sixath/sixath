# 查询结果外置（Spill）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `es_log_query` / `execute_read` 成功结果过大时只把 jsonl 写进工作区 `tmp/results/`，模型与落库只看 `*QuerySpillStub`；用 `result_stats` 做 group-by / 去重。

**Architecture:** 工具 `Execute` 在压缩 hits 之后调用 `MaybeSpill`。超阈值则写 NDJSON 并返回 struct（字段声明顺序保证 `spilled`/`path`/`count` 在 JSON 前部，且不经 `rcaOK` 包 map）。`truncated_page_gate` 与 `CollectEvidenceRefs` 通过 `SpillFields` / type switch 读 stub。`result_stats` 挂在 workspace 文件工具注册路径上。

**Tech Stack:** Go；framework 测试必须 `cd framework`；portal 测试 `cd portal`。根目录 `go.mod` 是空 module。

**Spec:** `docs/superpowers/specs/2026-08-29-query-result-spill-design.md`

**不做:** Python 脚本执行；多页 concat；`http_request`/终端溢出；下载按钮；改 `read_file` 硬拦；改 mapping 纠错 / ES 禁 `execute_read`；live ES e2e。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/tool/query_spill.go` | 常量、`QuerySpillStub`、路径守卫、写 jsonl、`MaybeSpill`、`SpillFields`、TTL |
| `framework/tool/query_spill_test.go` | 溢出门、回退、字段序、32MiB 帽（测试注入更小上限） |
| `framework/tool/evidence.go` | `HitContractFromResult` + `CollectEvidenceRefs` 认 stub |
| `framework/tool/es_log_tool.go` | 成功路径接 `MaybeSpill`；spill 时跳过 `rcaOK`；description 提 `result_stats` |
| `framework/tool/es_log_tool_test.go` | 行数/字节溢出、extracted_ids、evidence_refs |
| `framework/agent/truncated_page_gate.go` | `lastTruncatedContinueFrom` 改走 `SpillFields`；文案补 result_stats |
| `framework/agent/truncated_page_gate_test.go` | stub 仍 inject |
| `framework/tool/result_stats.go` | 注册 `result_stats` |
| `framework/tool/result_stats_test.go` | group-by / unique / 互斥 / 逃逸 / 再 spill |
| `framework/tool/file_tools.go` | `RegisterWorkspaceFileToolsWithConfig` 末尾注册 `result_stats` |
| `framework/tool/data/execute_read.go` | 成功路径接 spill |
| `framework/tool/data/execute_read_test.go` | 未 spill 类型不变；>50 行返回 stub |
| `portal/internal/service/chat_stream_toolcall_test.go` | stub 截断后仍含 path |
| `portal/internal/service/timeline_persist.go` | Result 经 JSON round-trip，避免 structpb 丢掉 timeline |
| `portal/internal/service/timeline_persist_test.go` | 落库无完整 hits；stub 可 `structpb.NewStruct` |

---

### Task 1: `MaybeSpill` 核心

**Files:**
- Create: `framework/tool/query_spill.go`
- Create: `framework/tool/query_spill_test.go`

- [ ] **Step 1: 写失败测试**

`framework/tool/query_spill_test.go`：

```go
package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func spillCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-1")
	return ctx, root
}

func TestMaybeSpill_SmallKeepsHits(t *testing.T) {
	ctx, root := spillCtx(t)
	rows := []map[string]any{{"operation": "a"}}
	payload := map[string]any{"hits": rows, "count": 1, "total": 1, "hit_status": HitStatusHits}
	stub, out := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub != nil {
		t.Fatal("small result must not spill")
	}
	if _, err := os.Stat(filepath.Join(root, "tmp")); !os.IsNotExist(err) && err != nil {
		t.Fatal(err)
	}
	if out["hits"] == nil {
		t.Fatal("hits must remain")
	}
}

func TestMaybeSpill_RowCountWritesJSONL(t *testing.T) {
	ctx, root := spillCtx(t)
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	rows[6] = map[string]any{"args": map[string]any{"flowIds": []any{"flow-late"}}}
	payload := map[string]any{
		"hits": rows, "count": 51, "total": 51, "hit_status": HitStatusHits,
		"queried_index": "idx", "has_more": true, "continue_from": 51,
		"extracted_ids": extractIDsFromHits(rows),
	}
	refs := deriveESLogRefs(payload)
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, refs)
	if stub == nil || !stub.Spilled || stub.Count != 51 {
		t.Fatalf("stub=%#v", stub)
	}
	if stub.HitStatus != HitStatusHits || stub.HasMore != true || stub.ContinueFrom != 51 {
		t.Fatalf("meta=%#v", stub)
	}
	b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if n := strings.Count(string(b), "\n"); n != 51 {
		t.Fatalf("jsonl lines=%d", n)
	}
	found := false
	for _, id := range stub.ExtractedIDs {
		if id == "flow-late" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extracted_ids=%v", stub.ExtractedIDs)
	}
	raw, _ := json.Marshal(stub)
	head := string(raw[:min(2048, len(raw))])
	if !strings.Contains(head, `"spilled"`) || !strings.Contains(head, `"path"`) || !strings.Contains(head, `"count"`) {
		t.Fatalf("key order head=%s", head)
	}
	if strings.Contains(string(raw), `"hits"`) {
		t.Fatal("stub must not contain hits")
	}
}

func TestMaybeSpill_ByteThreshold(t *testing.T) {
	ctx, _ := spillCtx(t)
	fat := strings.Repeat("x", 9000)
	rows := []map[string]any{{"message": fat}}
	payload := map[string]any{"hits": rows, "count": 1, "total": 1, "hit_status": HitStatusHits}
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub == nil {
		t.Fatal("fat single row must spill")
	}
}

func TestMaybeSpill_NoWorkspaceFallback(t *testing.T) {
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	stub, out := MaybeSpill(context.Background(), "es_log_query", rows, payload, nil)
	if stub != nil {
		t.Fatal("must not spill")
	}
	if out["spill_error"] != "workspace_root_missing" || out["hits"] == nil {
		t.Fatalf("%#v", out)
	}
}

func TestMaybeSpill_FileCap(t *testing.T) {
	old := spillFileMaxBytes
	spillFileMaxBytes = 64
	t.Cleanup(func() { spillFileMaxBytes = old })
	ctx, _ := spillCtx(t)
	rows := make([]map[string]any, 20)
	for i := range rows {
		rows[i] = map[string]any{"message": strings.Repeat("y", 20)}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil)
	if stub == nil || !stub.FileTruncated || stub.Count <= 0 || stub.Count >= 20 {
		t.Fatalf("file cap stub=%#v", stub)
	}
}

func TestMaybeSpill_TTLOnlySessionDir(t *testing.T) {
	ctx, root := spillCtx(t)
	other := filepath.Join(root, "tmp", "results", "other-sess")
	_ = os.MkdirAll(other, 0o755)
	oldf := filepath.Join(other, "old.jsonl")
	if err := os.WriteFile(oldf, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(oldf, past, past)

	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}
	payload := map[string]any{"hits": rows, "hit_status": HitStatusHits}
	if stub, _ := MaybeSpill(ctx, "es_log_query", rows, payload, nil); stub == nil {
		t.Fatal("expected spill")
	}
	if _, err := os.Stat(oldf); err != nil {
		t.Fatalf("must not delete other session: %v", err)
	}
}

func TestResolveResultsPath_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, _, err := resolveResultsPath(root, "../secret.json"); err == nil {
		t.Fatal("expected reject")
	}
	if _, _, err := resolveResultsPath(root, "tmp/other/a.jsonl"); err == nil {
		t.Fatal("expected reject")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./tool -count=1 -run "TestMaybeSpill_|TestResolveResultsPath_"
```

Expected: FAIL（`MaybeSpill` undefined）。

- [ ] **Step 3: 最小实现**

`framework/tool/query_spill.go` 必须包含（字段**按此顺序**声明）：

```go
package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	spillRowThreshold   = 50
	spillByteThreshold  = 8192
	spillSampleRows     = 5
	spillSampleRowBytes = 512
	spillTTL            = 24 * time.Hour
)

var (
	spillFileMaxBytes int64 = 32 << 20
	spillSeq          uint64
)

type QuerySpillStub struct {
	Spilled         bool             `json:"spilled"`
	Path            string           `json:"path"`
	Count           int              `json:"count"`
	OK              bool            `json:"ok"`
	HitStatus       string           `json:"hit_status,omitempty"`
	QueriedIndex    string           `json:"queried_index,omitempty"`
	HasMore         bool            `json:"has_more,omitempty"`
	ContinueFrom    int             `json:"continue_from,omitempty"`
	NextFrom        int             `json:"next_from,omitempty"`
	From            int             `json:"from,omitempty"`
	Returned        int             `json:"returned,omitempty"`
	Truncated       bool            `json:"truncated,omitempty"`
	Total           int             `json:"total,omitempty"`
	Columns         []string        `json:"columns,omitempty"`
	ExtractedIDs    []string        `json:"extracted_ids,omitempty"`
	EvidenceRefs    []EvidenceRef   `json:"evidence_refs,omitempty"`
	SourcePath      string           `json:"source_path,omitempty"`
	UniqueCount     int             `json:"unique_count,omitempty"`
	GroupsTruncated bool            `json:"groups_truncated,omitempty"`
	FileTruncated   bool            `json:"file_truncated,omitempty"`
	Sample          []map[string]any `json:"sample"`
	UnknownFields   any             `json:"unknown_fields,omitempty"`
	SimilarFields   any             `json:"similar_fields,omitempty"`
	MappingError    string           `json:"mapping_error,omitempty"`
	QueryRewritten  bool            `json:"query_rewritten,omitempty"`
	FieldHints      any             `json:"field_hints,omitempty"`
	TraceID         string           `json:"trace_id,omitempty"`
	SpillError      string           `json:"spill_error,omitempty"`
	SkippedBadLines int             `json:"skipped_bad_lines,omitempty"`
}

type SpillView struct {
	Spilled      bool
	HasMore      bool
	Truncated    bool
	ContinueFrom int
	NextFrom     int
	HitStatus    string
	QueriedIndex string
}

// MaybeSpill 超阈值则写 jsonl 并返回 stub；否则 stub=nil，返回原 payload（可能带 spill_error）。
func MaybeSpill(ctx context.Context, toolName string, rows []map[string]any, payload map[string]any, refs []EvidenceRef) (*QuerySpillStub, map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if len(rows) == 0 {
		return nil, payload
	}
	if !exceedsSpillThreshold(len(rows), payload) {
		return nil, payload
	}
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	if strings.TrimSpace(ws) == "" {
		payload["spill_error"] = "workspace_root_missing"
		return nil, payload
	}
	sess, _ := ctx.Value(ContextKeySessionID).(string)
	rel, full, err := newSpillFilePath(ws, sess, toolName)
	if err != nil {
		payload["spill_error"] = "path_rejected"
		return nil, payload
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		payload["spill_error"] = "mkdir_failed"
		return nil, payload
	}
	n, fileTrunc, sample, err := writeJSONL(full, rows)
	if err != nil {
		payload["spill_error"] = "write_failed"
		return nil, payload
	}
	expireSessionResults(filepath.Dir(full), time.Now())
	stub := stubFromPayload(payload, rel, n, sample, refs)
	stub.FileTruncated = fileTrunc
	stub.OK = true
	stub.Spilled = true
	if len(stub.ExtractedIDs) == 0 {
		stub.ExtractedIDs = extractIDsFromHits(rows)
	}
	if len(stub.Columns) == 0 {
		stub.Columns = columnsFromRows(rows)
	}
	return stub, payload
}

func exceedsSpillThreshold(n int, marshalTarget any) bool {
	if n > spillRowThreshold {
		return true
	}
	b, err := json.Marshal(marshalTarget)
	return err == nil && len(b) > spillByteThreshold
}
```

其余实现要点（写在同一文件，不要另发明名字）：

- `newSpillFilePath`：`tmp/results/{sanitizeSession}/{unix_ms}_{tool}_{seq}.jsonl`，`sanitizeSession` 只留 `[A-Za-z0-9._-]`，空则 `_nosession`。`path` 用正斜杠。`seq` 用 `atomic.AddUint64(&spillSeq, 1)`。
- `resolveResultsPath(ws, rel)`：`ResolveWorkspacePath` 之后 `filepath.Rel`，`filepath.ToSlash` 必须带前缀 `tmp/results/`。
- `writeJSONL`：逐行 `json.Marshal` + `\n`，累计字节（含换行）≥ `spillFileMaxBytes` 则停并 `fileTruncated`。`sample` 取已写前 5 行；单行 marshal > 512 则改成 `map[string]any{"_truncated": true}` 加一个短 preview 字符串。
- `stubFromPayload`：从 payload 拷贝 `hit_status`、`queried_index`、`has_more`、`continue_from`、`next_from`、`from`、`returned`、`truncated`、`total`、`extracted_ids`、`unknown_fields`、`trace_id` 等。`count` 用写入行数，不要用 payload 的 `count`（那是 ES total）。
- `SpillFields(v any)`：`*QuerySpillStub` / `QuerySpillStub` / `map[string]any`。map 分支用 `truthy` 等价（bool 或 `"true"`/`"1"`）读 `has_more`/`truncated`；`continue_from`/`next_from` 用与 agent `anyToInt` 相同的 int/int64/float64。
- `expireSessionResults(dir, now)`：只 `ReadDir` 该目录，mtime 超过 24h 则删。错误忽略。
- `columnsFromRows`：按行内键**首次出现**顺序追加到 `[]string`（用 seen set）。不要 `for k := range row` 当输出顺序。`es_log_query` 接线时若有 `QueryResult.Columns` 可传入覆盖。

`MaybeSpill` 写失败不得删除已有 hits。

- [ ] **Step 4: 再跑测试**

```
cd framework
go test ./tool -count=1 -run "TestMaybeSpill_|TestResolveResultsPath_"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```
git add framework/tool/query_spill.go framework/tool/query_spill_test.go
git commit -m "feat(tool): spill oversized query rows to workspace jsonl"
```

---

### Task 2: 证据抽取认 stub

**Files:**
- Modify: `framework/tool/evidence.go`（`HitContractFromResult`、`collectEvidenceRefsFromValue`）
- Modify: `framework/tool/evidence_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestHitContractFromResult_SpillStub(t *testing.T) {
	st, idx, _ := HitContractFromResult(&QuerySpillStub{
		HitStatus: HitStatusHits, QueriedIndex: "backend-cgsession-*",
	})
	if st != HitStatusHits || idx != "backend-cgsession-*" {
		t.Fatalf("%q %q", st, idx)
	}
}

func TestCollectEvidenceRefs_SpillStub(t *testing.T) {
	stub := &QuerySpillStub{
		Spilled: true, Path: "tmp/results/s/1.jsonl", Count: 51, OK: true,
		HitStatus: HitStatusHits,
		EvidenceRefs: []EvidenceRef{{Kind: "es_log_query", TraceID: "t1"}},
		Sample: []map[string]any{{"i": 1}},
	}
	refs := CollectEvidenceRefs(stub)
	if len(refs) != 1 || refs[0].Kind != "es_log_query" || refs[0].Summary == "no hits" {
		t.Fatalf("%#v", refs)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./tool -count=1 -run "TestHitContractFromResult_SpillStub|TestCollectEvidenceRefs_SpillStub"
```

Expected: FAIL 或 stub 的 refs 长度为 0。

- [ ] **Step 3: 实现**

`HitContractFromResult` 增加：

```go
case *QuerySpillStub:
	if x == nil {
		return "", "", ""
	}
	return hitStatusString(x.HitStatus), x.QueriedIndex, ""
case QuerySpillStub:
	return hitStatusString(x.HitStatus), x.QueriedIndex, ""
```

`collectEvidenceRefsFromValue` 增加：

```go
case *QuerySpillStub:
	if x != nil {
		*out = append(*out, x.EvidenceRefs...)
	}
case QuerySpillStub:
	*out = append(*out, x.EvidenceRefs...)
```

- [ ] **Step 4: 再跑**

```
cd framework
go test ./tool -count=1 -run "TestHitContractFromResult_|TestCollectEvidenceRefs_|TestNormalize"
```

Expected: PASS（含旧测试）。

- [ ] **Step 5: Commit**

```
git add framework/tool/evidence.go framework/tool/evidence_test.go
git commit -m "feat(tool): collect evidence refs from query spill stubs"
```

---

### Task 3: 接线 `es_log_query`

**Files:**
- Modify: `framework/tool/es_log_tool.go`（成功 return 前；description）
- Modify: `framework/tool/es_log_tool_test.go`

- [ ] **Step 1: 写失败测试**

在 `es_log_tool_test.go` 追加（复用现有 `fakeReader`）。构造 51 行，第 7 行才有 `args.flowIds`。ctx 注入 workspace + session。

```go
func TestESLogQuery_SpillsOverFiftyRows(t *testing.T) {
	rows := make([][]any, 51)
	cols := []string{"operation", "args"}
	for i := 0; i < 51; i++ {
		rows[i] = []any{"DiscardUserArchive", nil}
	}
	rows[6] = []any{"DiscardUserArchive", map[string]any{"flowIds": []any{"flow-late"}}}
	fr := &fakeReader{result: &executor.QueryResult{Columns: cols, Rows: rows, Truncated: true, EstimatedTotal: 400}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "backend-cgsession-*", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	root := t.TempDir()
	ctx := context.WithValue(context.Background(), ContextKeyWorkspaceRoot, root)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess-es")
	out, err := tl.Execute(ctx, map[string]any{"query": "operation:DiscardUserArchive", "limit": 51})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := out.(*QuerySpillStub)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if !stub.HasMore || stub.Count != 51 {
		t.Fatalf("%#v", stub)
	}
	if refs := CollectEvidenceRefs(stub); len(refs) == 0 || refs[0].Kind != "es_log_query" || refs[0].Summary == "no hits" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestESLogQuery_SmallPageUnchangedType(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns: []string{"message"}, Rows: [][]any{{"a"}},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, fr, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, err := tl.Execute(context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %T", out)
	}
	if m["hits"] == nil || m["count"] != m["total"] {
		t.Fatalf("non-spill count must stay equal total: %#v", m)
	}
}
```

若现有测试用 `&Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}`，本任务照抄，不要改用 `NewRegistry()`（避免和 HTTP/MCP 注册缠在一起）。

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./tool -count=1 -run "TestESLogQuery_SpillsOverFiftyRows|TestESLogQuery_SmallPageUnchangedType"
```

Expected: FAIL（仍返回 map）。

- [ ] **Step 3: 接线**

`es_log_tool.go` 在 `StampHitContract` **之后**、`return rcaOK` **之前**：

```go
refs := deriveESLogRefs(payload) // 必须在仍含 hits 时调用
if stub, fallback := MaybeSpill(ctx, toolName, hits, payload, refs); stub != nil {
	return stub, nil
}
payload = fallback
return rcaOK(toolName, payload), nil
```

`deriveESLogRefs` 已有：有 `hits` 且非空则 Summary 不是 `no hits`。禁止对 stub 再调它。

Description 追加一句：`Large pages are written to workspace tmp/results/*.jsonl; use result_stats on path instead of read_file.`

- [ ] **Step 4: 再跑**

```
cd framework
go test ./tool -count=1 -run "TestESLogQuery_|TestEvalGolden_|TestMaybeSpill_"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```
git add framework/tool/es_log_tool.go framework/tool/es_log_tool_test.go
git commit -m "feat(tool): spill es_log_query pages instead of stuffing hits"
```

---

### Task 4: 翻页闸读 stub

**Files:**
- Modify: `framework/agent/truncated_page_gate.go`
- Modify: `framework/agent/truncated_page_gate_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestEvaluateTruncatedPageGate_SpillStub(t *testing.T) {
	q := "查询全部 DiscardUserArchive 并解析"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result: &tool.QuerySpillStub{
			Spilled: true, Path: "tmp/results/s/1.jsonl", Count: 50, OK: true,
			HasMore: true, ContinueFrom: 50, HitStatus: "hits",
		},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if got.Allow || !strings.Contains(got.Prompt, "50") {
		t.Fatalf("%#v", got)
	}
	if !strings.Contains(got.Prompt, "result_stats") {
		t.Fatalf("prompt=%q", got.Prompt)
	}
}

func TestEvaluateTruncatedPageGate_SpillWithoutMore(t *testing.T) {
	q := "查询全部"
	tr := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query",
		Result:   &tool.QuerySpillStub{Spilled: true, Count: 10, OK: true},
	}}}
	got := EvaluateTruncatedPageGate(tr, q)
	if !got.Allow {
		t.Fatalf("spilled complete page must allow, %#v", got)
	}
}
```

保留现有 map 回归测试。

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./agent -count=1 -run TestEvaluateTruncatedPageGate_
```

Expected: FAIL（type assert map 失败，Allow=true）。

- [ ] **Step 3: 实现**

`lastTruncatedContinueFrom` 不要 `rec.Result.(map[string]any)`。改为：

```go
view := tool.SpillFields(rec.Result)
more := view.HasMore || view.Truncated
if !more {
	return -1
}
if view.ContinueFrom > 0 {
	return view.ContinueFrom
}
if view.NextFrom > 0 {
	return view.NextFrom
}
return 0
```

inject 文案补：`上一页已 spill 到 path 时用 result_stats 做统计/去重，翻页请继续 from=`（具体中文与 spec §2.6 一致即可）。

- [ ] **Step 4: 再跑**

```
cd framework
go test ./agent -count=1 -run TestEvaluateTruncatedPageGate_
```

Expected: PASS。

- [ ] **Step 5: Commit**

```
git add framework/agent/truncated_page_gate.go framework/agent/truncated_page_gate_test.go
git commit -m "fix(agent): truncated page gate reads spilled es_log_query stubs"
```

---

### Task 5: `result_stats`

**Files:**
- Create: `framework/tool/result_stats.go`
- Create: `framework/tool/result_stats_test.go`
- Modify: `framework/tool/file_tools.go`（`RegisterWorkspaceFileToolsWithConfig` 在 `registerSearchFilesTool` 之后 `RegisterResultStatsTool`）

- [ ] **Step 1: 写失败测试**

准备：在 temp workspace 写 `tmp/results/sess/a.jsonl` 三行：

```
{"operation":"A","args":{"flowIds":["f1","f2"]}}
{"operation":"A","args":{"flowIds":["f1"]}}
{"operation":"B","args":{"flowIds":["f3"]}}
```

覆盖：

1. `group_by=operation` → 未 spill，`groups` 含 A:2、B:1，`path` 为入参。
2. `unique=args.flowIds` → `unique_values` 为首次出现顺序 `f1,f2,f3`（展开一层；用 `[]string` 插序，禁止 `range map`）。
3. 同时传 group_by+unique → error，不读盘。
4. `path=../x` 与 `tmp/other/a.jsonl` → error。
5. 缺文件 → error 含 `re-query`。
6. 80 个 unique：写 80 行不同 id 的 jsonl；返回 `*QuerySpillStub`，`unique_count=80`，`source_path` 为入参，统计文件 80 行，JSON 无 `unique_values`。
7. 10001 个不同 operation：`groups_truncated=true`，统计文件 ≤10000 行。
8. 损坏 jsonl：三行里中间一行不是 JSON → 仍统计其余两行，结果带 `skipped_bad_lines=1`。三行全坏 → 错误（不返回空统计冒充成功）。

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./tool -count=1 -run TestResultStats_
```

Expected: FAIL。

- [ ] **Step 3: 实现 `RegisterResultStatsTool`**

- Name: `result_stats`；Toolset: `ToolsetFile`。
- 参数：`path` required；`group_by`；`unique`。
- 互斥：两者都非空 → `return map[string]any{"error": "group_by and unique are mutually exclusive"}, nil`（与 `read_file` 一样用 payload error，不要 Go error，除非本目录其它只读工具用 error——**跟 `read_file` 一致：map error**）。测试按实际返回断言。
- 都空：数行数返回 `map[string]any{"path": rel, "count": n}`。
- 点分路径：`strings.Split(path, ".")` 逐层取 map；array 展开一层做键；object 跳过；标量 `fmt.Sprint`。
- 聚合：`map[string]int` + 另存 `[]string` 插入序（unique 输出即此顺序）。第 10001 个新键：设 `groups_truncated`，**停止读文件**。
- 扫描：逐行 `json.Unmarshal`；失败则 `skipped++` 继续。扫完后若 `count==0 && skipped>0`（没有任何合法行）→ 返回 error。否则把 `skipped_bad_lines` 写进内联 map 或 stub 的 `SkippedBadLines`。
- `group_by` 输出按 count 降序（`sort.Slice`，count 相同按 value 字符串升序，保证测试稳）。
- 内联阈值：`len(groups|unique_values) > 50` 或 marshal > 8192 → 把统计项变成 `[]map[string]any` 调 `MaybeSpill`；spill 后设 `SourcePath`、`UniqueCount`、`GroupsTruncated`。
- Description：`Aggregate a spilled query jsonl under tmp/results/. Do not read_file the whole file.`
- `RegisterWorkspaceFileToolsWithConfig`：search_files 成功后 `return RegisterResultStatsTool(reg)`。

- [ ] **Step 4: 再跑**

```
cd framework
go test ./tool -count=1 -run "TestResultStats_|TestMaybeSpill_|TestESLogQuery_Spills"
```

Expected: PASS。顺带跑一条现有 `TestReadFile` 确认注册链没断：

```
cd framework
go test ./tool -count=1 -run "TestReadFile_|TestWorkspace"
```

- [ ] **Step 5: Commit**

```
git add framework/tool/result_stats.go framework/tool/result_stats_test.go framework/tool/file_tools.go
git commit -m "feat(tool): add result_stats for spilled query jsonl"
```

---

### Task 6: `execute_read`

**Files:**
- Modify: `framework/tool/data/execute_read.go`
- Modify: `framework/tool/data/execute_read_test.go`

- [ ] **Step 1: 写失败测试**

未 spill：现有 `TestExecuteRead_Basic` 必须仍是 `*executor.QueryResult`。

新增：`Rows` 长度 51，ctx 带 workspace；断言 `*tool.QuerySpillStub`，`Count==51`，用 `%+v`/`json.Marshal` 确认没有 `Rows` 全表。

ES 拒绝测试不改。

- [ ] **Step 2: 跑测试确认失败**

```
cd framework
go test ./tool/data -count=1 -run TestExecuteRead_
```

Expected: 新测试 FAIL。

- [ ] **Step 3: 接线**

`return res, nil` 之前：

```go
rows := queryResultRows(res) // columns → map
payload := map[string]any{
	"hits": rows, "truncated": res.Truncated, "hit_status": res.HitStatus, "queried_index": res.QueriedIndex,
}
if stub, _ := tool.MaybeSpill(ctx, "execute_read", rows, payload, nil); stub != nil {
	stub.Truncated = res.Truncated
	return stub, nil
}
return res, nil
```

`queryResultRows` 放在 `execute_read.go`（data 包），不要改 `executor.QueryResult` 结构。

- [ ] **Step 4: 再跑**

```
cd framework
go test ./tool/data -count=1
```

Expected: PASS。

- [ ] **Step 5: Commit**

```
git add framework/tool/data/execute_read.go framework/tool/data/execute_read_test.go
git commit -m "feat(data): spill oversized execute_read row sets"
```

---

### Task 7: 门户 SSE / 时间线

**Files:**
- Modify: `portal/internal/service/chat_stream_toolcall_test.go`
- Modify: `portal/internal/service/timeline_persist_test.go`

前端不渲染 `hits` 字段（当前 web 无此绑定）。**不要**加下载 API。

- [ ] **Step 1: 写失败测试**

`chat_stream_toolcall_test.go`：

```go
func TestToolCallPayloadFromRecord_SpillStubKeepsPathWhenTruncated(t *testing.T) {
	sample := make([]map[string]any, 5)
	for i := range sample {
		sample[i] = map[string]any{"blob": strings.Repeat("z", 3000)}
	}
	stub := &tool.QuerySpillStub{
		Spilled: true,
		Path:    "tmp/results/sess/1_es_log_query_1.jsonl",
		Count:   51,
		OK:      true,
		Sample:  sample,
	}
	rec := agent.ToolCallRecord{ToolCallID: "c", ToolName: "es_log_query", Result: stub}
	p := toolCallPayloadFromRecord(rec, "completed")
	s, _ := p.Result.(string)
	if !p.Truncated {
		t.Fatal("expected truncation of fat sample")
	}
	if !strings.Contains(s, "tmp/results/sess") {
		t.Fatalf("path lost after truncate: %s", s[:min(200, len(s))])
	}
}
```

`timeline_persist_test.go`：

1. ApplyToolCall completed，`Result` 为含 `path` 的 stub；`Finalize` 后 `json.Marshal` 节点，断言含 `spilled`/`path`，**不含** `"hits"` 数组。
2. **必须** `structpb.NewStruct(MetadataWithTimeline(...))` 成功。`truncateField` 在未超 8KB 时原样返回 `*QuerySpillStub`；`timelineNodeToMap` 若把 struct 指针塞进 `result`，`SaveAssistantMessage` 的 `structpb.NewStruct` 会失败并**丢掉** metadata。

- [ ] **Step 2: 跑测试**

```
cd portal
go test ./internal/service -count=1 -run "TestToolCallPayloadFromRecord_|TestTimelineAccumulator_|TestMetadataWithTimeline_"
```

Expected: structpb 测试 FAIL（`invalid type: *tool.QuerySpillStub`）。

- [ ] **Step 3: 实现**

`timelineNodeToMap`：对 `n.Result` 和 `n.Arguments` 做 JSON round-trip 成 `map/[]/scalar` 再放入 metadata（`json.Marshal` → `json.Unmarshal` 到 `any`）。不要改 `truncateField` 的未截断返回值（SSE 再 marshal stub 时要保住字段序）。

若 round-trip 后测试仍红，再查 `chat_stream.go` 是否把 Result 转成了 map。

- [ ] **Step 4: 再跑**

```
cd portal
go test ./internal/service -count=1 -run "TestToolCallPayloadFromRecord_|TestTimelineAccumulator_|TestMetadataWithTimeline_"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```
git add portal/internal/service/chat_stream_toolcall_test.go portal/internal/service/timeline_persist.go portal/internal/service/timeline_persist_test.go
git commit -m "test(portal): keep spill path through SSE truncation and timeline"
```

---

## 手工验收（不进 CI）

重启已加载 framework 的 portal。对「查全部 DiscardUserArchive 并解析」：tool 结果应有 `spilled`/`path`，无整页 hits；模型可 `result_stats`；要全量时翻页闸仍能 nudge。

---

## 完成定义

- `cd framework && go test ./tool ./tool/data ./agent -count=1` 绿。
- `cd portal && go test ./internal/service -count=1 -run "TestToolCallPayloadFromRecord_|TestTimelineAccumulator_|TestMetadataWithTimeline_"` 绿。
- 非溢出 `es_log_query` 仍返回 map，`count==total`。
- spill 的 `es_log_query` 返回 `*QuerySpillStub`，`CollectEvidenceRefs` 含 `Kind=es_log_query` 且 Summary 不是 `no hits`。
