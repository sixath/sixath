# run_result_script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对 `tmp/results/` 下已有 spill 文件跑 Python；大 stdout 再外置为 jsonl + `*QuerySpillStub`，小输出以内联 map 回给模型。

**Architecture:** 与 `result_stats` 一样挂在 `RegisterWorkspaceFileToolsWithConfig`。硬闸在起进程之前：合法 `path` + `code`/`script_path` 互斥。stdout 规范化成 `[]map[string]any` 后走 `spillRowSet`（字节阈值 marshal **行切片**，不要 marshal 含 `output` 的 payload）。`QuerySpillStub` 增加 `ExitCode *int` 与 `TimedOut`，插在 `UniqueCount` 与 `GroupsTruncated` 之间，保证 `sample` 仍靠后。

**Tech Stack:** Go；测试必须 `cd framework`。PowerShell 把下文 `&&` 换成 `;`。

**Spec:** `docs/superpowers/specs/2026-08-29-run-result-script-design.md`

**前置（不做完不要写工具）：** 本功能叠在查询 spill 上。主工作区 `feature/mea-minimal-subset` **没有** `framework/tool/query_spill.go`。在 worktree 实现：

```text
E:\workspace\github\sixath\sixath\.worktrees\query-result-spill
分支 feature/query-result-spill
```

若该分支没有本 spec / 本 plan，从 `feature/mea-minimal-subset` cherry-pick spec 提交 `2acc36e` 以及本 plan 文件。Cursor 对话若还在主仓，用 cursor-app-control 的 `move_agent_to_root` 指到该 worktree。

**不做:** OS 沙箱、确认卡、Node/shell、stdin、门户 UI、改 `terminal`、LLM 裁判、live ES e2e。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/tool/query_spill.go` | `ExitCode`/`TimedOut`；`newSpillNamedFile`；`MaybeSpill` 改调 `spillRowSet`；TTL 保持 |
| `framework/tool/query_spill_test.go` | stub JSON 字段序含 `exit_code`；`spillRowSet` 按 rows 判字节 |
| `framework/tool/run_result_script.go` | 注册、守卫、写 `.py`、起 Python、规范化 stdout、接 spill |
| `framework/tool/run_result_script_test.go` | spec §5 |
| `framework/tool/file_tools.go` | `RegisterWorkspaceFileToolsWithConfig` 末尾挂本工具 |
| `framework/tool/file_tools_test.go` | 注册后能 `Get("run_result_script")` |
| `framework/tool/toolset.go` | `builtinDefaultToolset["run_result_script"]=ToolsetFile` |
| `framework/tool/result_stats.go` | description 补一句 `run_result_script` |
| `framework/tool/es_log_tool.go` | description 补一句 `run_result_script` |

---

### Task 0: 切到 spill worktree

**Files:** 无代码

- [ ] **Step 1: 确认 worktree 有 `query_spill.go`**

```powershell
git -C E:\workspace\github\sixath\sixath\.worktrees\query-result-spill rev-parse --abbrev-ref HEAD
Test-Path E:\workspace\github\sixath\sixath\.worktrees\query-result-spill\framework\tool\query_spill.go
```

Expected: 分支 `feature/query-result-spill`；`True`。

- [ ] **Step 2: 把 spec/plan 接到该分支（若缺失）**

在 worktree 里：

```powershell
git cherry-pick 2acc36e
```

若 plan 只存在于主仓工作区，把 `docs/superpowers/plans/2026-08-29-run-result-script.md` 拷进 worktree 再 `git add` 提交。冲突只保留 spill 规格里已有的 `exit_code` 行即可。

- [ ] **Step 3: 后续所有 `go test` 的 cwd 都是 worktree 的 `framework/`**

---

### Task 1: Stub 字段与 `spillRowSet`

**Files:**
- Modify: `framework/tool/query_spill.go`
- Modify: `framework/tool/query_spill_test.go`

- [ ] **Step 1: 写失败测试**

在 `query_spill_test.go` 追加：

```go
func TestQuerySpillStub_ExitCodeBeforeSample(t *testing.T) {
	zero := 0
	stub := &QuerySpillStub{
		Spilled:    true,
		Path:       "tmp/results/s/out.jsonl",
		Count:      1,
		OK:         true,
		SourcePath: "tmp/results/s/in.jsonl",
		ExitCode:   &zero,
		TimedOut:   true,
		Sample:     []map[string]any{{"line": "x"}},
	}
	b, err := json.Marshal(stub)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	iExit := strings.Index(s, `"exit_code"`)
	iTO := strings.Index(s, `"timed_out"`)
	iSample := strings.Index(s, `"sample"`)
	if iExit < 0 || iTO < 0 || iSample < 0 {
		t.Fatalf("missing keys: %s", s)
	}
	if !(iExit < iTO && iTO < iSample) {
		t.Fatalf("field order: %s", s)
	}
}

func TestSpillRowSet_ByteThresholdUsesRows(t *testing.T) {
	ctx, root := spillCtx(t)
	row := map[string]any{"line": strings.Repeat("x", 9000)}
	rows := []map[string]any{row}
	payload := map[string]any{"ok": true} // small; must still spill because rows are huge
	stub, _ := spillRowSet(ctx, "run_result_script", rows, rows, payload, nil)
	if stub == nil {
		t.Fatal("want spill when rows marshal > 8192")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stub.Path))); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd E:\workspace\github\sixath\sixath\.worktrees\query-result-spill\framework
go test ./tool -count=1 -run "TestQuerySpillStub_ExitCodeBeforeSample|TestSpillRowSet_ByteThresholdUsesRows"
```

Expected: FAIL（`ExitCode` 未定义 或 `spillRowSet` 未定义）。

- [ ] **Step 3: 改 `query_spill.go`**

在 `QuerySpillStub` 里，`UniqueCount` 之后、`GroupsTruncated` 之前插入：

```go
	ExitCode        *int            `json:"exit_code,omitempty"`
	TimedOut        bool            `json:"timed_out,omitempty"`
```

查询 / `result_stats` **不要**赋值 `ExitCode`（保持 nil，JSON 省略）。

把 `MaybeSpill` 换成：

```go
func MaybeSpill(ctx context.Context, toolName string, rows []map[string]any, payload map[string]any, refs []EvidenceRef) (*QuerySpillStub, map[string]any) {
	return spillRowSet(ctx, toolName, rows, payload, payload, refs)
}
```

抽出 `spillRowSet`：与现在的 `MaybeSpill` 相同，但 `exceedsSpillThreshold(len(rows), marshalTarget)` 用第四参 `marshalTarget`，**不要**再用 `payload` 做字节判定。`newSpillFilePath` 仍写 `.jsonl`。

把 `newSpillFilePath` 改成：

```go
func newSpillFilePath(ws, sessionID, toolName string) (rel string, full string, err error) {
	return newSpillNamedFile(ws, sessionID, toolName, ".jsonl")
}

func newSpillNamedFile(ws, sessionID, toolName, ext string) (rel string, full string, err error) {
	if ext == "" {
		ext = ".jsonl"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	sess := sanitizeSessionID(sessionID)
	seq := atomic.AddUint64(&spillSeq, 1)
	name := fmt.Sprintf("%d_%s_%d%s", time.Now().UnixMilli(), toolName, seq, ext)
	rel = filepath.ToSlash(filepath.Join("tmp", "results", sess, name))
	return resolveResultsPath(ws, rel)
}
```

`spillRowSet` 函数体从原 `MaybeSpill` 复制，仅替换阈值那一行与 `newSpillFilePath` 调用（仍 `.jsonl`）。

- [ ] **Step 4: 跑测试确认通过**

```powershell
cd E:\workspace\github\sixath\sixath\.worktrees\query-result-spill\framework
go test ./tool -count=1 -run "TestQuerySpillStub_ExitCodeBeforeSample|TestSpillRowSet_ByteThresholdUsesRows|TestMaybeSpill"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/query_spill.go framework/tool/query_spill_test.go
git commit -m "feat(tool): add script spill stub fields and row marshal target"
```

---

### Task 2: stdout 规范化（不起 Python）

**Files:**
- Create: `framework/tool/run_result_script.go`（先只放纯函数）
- Create: `framework/tool/run_result_script_test.go`

- [ ] **Step 1: 写失败测试**

`run_result_script_test.go`：

```go
package tool

import (
	"strings"
	"testing"
)

func TestSplitScriptOutput_DropsBlankAndTrailing(t *testing.T) {
	lines := splitScriptOutput("a\n\nb\n")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("%q", lines)
	}
}

func TestRowsFromScriptLines_AllJSONObjects(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, `{"b":2}`})
	if len(rows) != 2 {
		t.Fatalf("%v", rows)
	}
	if _, ok := rows[0]["line"]; ok {
		t.Fatal("must not wrap pure json objects")
	}
	if rows[0]["a"] != float64(1) {
		t.Fatalf("%v", rows[0])
	}
}

func TestRowsFromScriptLines_WrapsText(t *testing.T) {
	rows := rowsFromScriptLines([]string{"hello", "world"})
	if rows[0]["line"] != "hello" || rows[1]["line"] != "world" {
		t.Fatalf("%v", rows)
	}
}

func TestRowsFromScriptLines_MixedGoesWrap(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, "not-json"})
	if rows[0]["line"] != `{"a":1}` || rows[1]["line"] != "not-json" {
		t.Fatalf("%v", rows)
	}
}

func TestTruncateUTF8Bytes(t *testing.T) {
	s := strings.Repeat("x", 9000)
	got := truncateUTF8Bytes(s, 8192)
	if len(got) != 8192 {
		t.Fatalf("len=%d", len(got))
	}
	s2 := strings.Repeat("你", 100)
	got2 := truncateUTF8Bytes(s2, 4)
	if got2 != "你" {
		t.Fatalf("%q", got2)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd E:\workspace\github\sixath\sixath\.worktrees\query-result-spill\framework
go test ./tool -count=1 -run "TestSplitScriptOutput_|TestRowsFromScriptLines_|TestTruncateUTF8Bytes"
```

Expected: FAIL（函数未定义）。

- [ ] **Step 3: 实现纯函数**

`run_result_script.go`：

```go
package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func splitScriptOutput(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(raw, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func rowsFromScriptLines(lines []string) []map[string]any {
	if len(lines) == 0 {
		return nil
	}
	objs := make([]map[string]any, 0, len(lines))
	allObj := true
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
			allObj = false
			break
		}
		objs = append(objs, m)
	}
	if allObj {
		return objs
	}
	out := make([]map[string]any, len(lines))
	for i, line := range lines {
		out[i] = map[string]any{"line": line}
	}
	return out
}

func truncateUTF8Bytes(s string, n int) string {
	if n < 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
```

JSON array 行（`[1]`）`Unmarshal` 进 `map` 会失败，整份走 `line` 包装，符合 spec。

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./tool -count=1 -run "TestSplitScriptOutput_|TestRowsFromScriptLines_|TestTruncateUTF8Bytes"
```

Expected: PASS。

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/run_result_script.go framework/tool/run_result_script_test.go
git commit -m "feat(tool): normalize run_result_script stdout into jsonl rows"
```

---

### Task 3: 守卫（不起 Python）

**Files:**
- Modify: `framework/tool/run_result_script.go`
- Modify: `framework/tool/run_result_script_test.go`
- Modify: `framework/tool/file_tools.go`
- Modify: `framework/tool/file_tools_test.go`
- Modify: `framework/tool/toolset.go`

- [ ] **Step 1: 写失败测试**

追加到 `run_result_script_test.go`（补 import：`context` `os` `path/filepath` `os/exec` 先不用）：

```go
func scriptTool(t *testing.T) Tool {
	t.Helper()
	reg := NewRegistry()
	if err := RegisterRunResultScriptTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("run_result_script")
	if !ok {
		t.Fatal("tool not registered")
	}
	return tl
}

func TestRunResultScript_NoWorkspace(t *testing.T) {
	tl := scriptTool(t)
	_, err := tl.Execute(context.Background(), map[string]any{
		"path": "tmp/results/s/a.jsonl",
		"code": "print(1)",
	})
	if err == nil || !strings.Contains(err.Error(), "workspace_root_missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunResultScript_PathOutsideResults(t *testing.T) {
	ctx, root := spillCtx(t)
	if err := os.MkdirAll(filepath.Join(root, "tmp", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "other", "x.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": "tmp/other/x.jsonl", "code": "print(1)"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_MissingFile(t *testing.T) {
	ctx, _ := spillCtx(t)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": "tmp/results/sess-1/missing.jsonl", "code": "print(1)"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_CodeAndScriptPath(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path":        data,
		"code":       "print(1)",
		"script_path": data,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_NeitherCodeNorScript(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{"path": data})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_CodeTooLong(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": strings.Repeat("a", 65537),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunResultScript_ScriptNotPy(t *testing.T) {
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	js := filepath.Join(root, "tmp", "results", "sess-1", "x.js")
	if err := os.WriteFile(js, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	_, err := tl.Execute(ctx, map[string]any{
		"path":        data,
		"script_path": "tmp/results/sess-1/x.js",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func writeFixtureJSONL(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "tmp", "results", "sess-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := "tmp/results/sess-1/in.jsonl"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("{\"x\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}
```

`spillCtx` 已在 `query_spill_test.go` 同包，session 为 `sess-1`。

`file_tools_test.go` 追加：

```go
func TestRegisterWorkspaceFileTools_IncludesRunResultScript(t *testing.T) {
	reg := registerFileToolsForTest(t)
	if _, ok := reg.Get("run_result_script"); !ok {
		t.Fatal("run_result_script not registered")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./tool -count=1 -run "TestRunResultScript_|TestRegisterWorkspaceFileTools_IncludesRunResultScript"
```

Expected: FAIL（`RegisterRunResultScriptTool` 未定义）。

- [ ] **Step 3: 实现注册 + 守卫 + 尚未接 Python 时守卫失败就 return**

常量与 lookPath 变量一并加上（下一任务用）：

```go
const (
	runResultScriptMaxCodeBytes = 65536
	runResultScriptName         = "run_result_script"
)

var (
	lookPathFn    = exec.LookPath
	scriptTimeout = 15 * time.Second
)
```

`RegisterRunResultScriptTool`：`Toolset: ToolsetFile`。Description 必须含：先查询工具，再 `result_stats`，最后本工具；用 `sys.argv[1]` 打开数据文件；不要 `read_file` 整份 jsonl。

Parameters：`path` required；`code`、`script_path` 可选。`Execute: executeRunResultScript`。

`executeRunResultScript` 顺序：

1. `workspaceRootFromCtx` → error 包装 `run_result_script: workspace_root_missing`
2. `path` 空 → error
3. `resolveResultsPath(ws, path)`；`os.Stat` 必须是普通文件
4. `code` 与 `script_path` 都非空或都空 → error（`trim space` 后判断）
5. `len(code) > 65536` → error
6. 有 `script_path`：`resolveResultsPath`；`strings.ToLower(filepath.Ext)==".py"`；必须存在且是文件
7. **本任务到此为止若还没实现 Python：不要往下跑。** 实现 Python 也在本函数后半，可以同一提交若测试需要；**不要**在守卫失败后调用 `lookPathFn`。

守卫通过后的执行放到 Task 4。若本步只做守卫，Task 4 的小输出测试会失败——因此 **本任务实现完整 Execute（含 Python）也可以**，但 TDD 要求先让守卫测试绿。推荐：守卫失败路径全部 return；成功路径 `return nil, fmt.Errorf("run_result_script: not implemented")` 仅用于确认守卫测试不走到这。下一任务删掉这句。

`file_tools.go` 的 `RegisterWorkspaceFileToolsWithConfig`：

```go
	if err := RegisterResultStatsTool(reg); err != nil {
		return err
	}
	return RegisterRunResultScriptTool(reg)
```

`toolset.go` 的 `builtinDefaultToolset` 增加：

```go
	"result_stats":       ToolsetFile,
	"run_result_script":  ToolsetFile,
```

（若 `result_stats` 已在 map 里则只加 `run_result_script`。）

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./tool -count=1 -run "TestRunResultScript_|TestRegisterWorkspaceFileTools_IncludesRunResultScript"
```

Expected: 守卫用例 PASS。`TestRegisterWorkspaceFileTools_IncludesRunResultScript` PASS。

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/run_result_script.go framework/tool/run_result_script_test.go framework/tool/file_tools.go framework/tool/file_tools_test.go framework/tool/toolset.go
git commit -m "feat(tool): register run_result_script with tmp/results path gates"
```

---

### Task 4: 起 Python、内联与 spill

**Files:**
- Modify: `framework/tool/run_result_script.go`
- Modify: `framework/tool/run_result_script_test.go`

- [ ] **Step 1: 写失败测试**

```go
func requirePython(t *testing.T) {
	t.Helper()
	if _, err := pythonInterpreter(); err != nil {
		t.Skip("python not on PATH")
	}
}

func TestRunResultScript_SmallInline(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "print('hello')",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("%T", out)
	}
	if m["exit_code"] != 0 && m["exit_code"] != float64(0) {
		// Execute 返回 map[string]any 时数字可能是 int
		if v, ok := m["exit_code"].(int); !ok || v != 0 {
			t.Fatalf("%v", m["exit_code"])
		}
	}
	if !strings.Contains(fmt.Sprint(m["output"]), "hello") {
		t.Fatalf("%v", m)
	}
	if _, ok := m["output"]; !ok {
		t.Fatal("want inline output")
	}
	matches, _ := filepath.Glob(filepath.Join(root, "tmp", "results", "sess-1", "*_run_result_script_*.jsonl"))
	if len(matches) != 0 {
		t.Fatalf("unexpected spill %v", matches)
	}
}

func TestRunResultScript_TextSpillSixtyLines(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "for i in range(60):\n print(i)",
	})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := out.(*QuerySpillStub)
	if !ok {
		t.Fatalf("%T %#v", out, out)
	}
	if stub.SourcePath != data {
		t.Fatalf("source=%s", stub.SourcePath)
	}
	if stub.Count != 60 {
		t.Fatalf("count=%d", stub.Count)
	}
	if stub.ExitCode == nil || *stub.ExitCode != 0 {
		t.Fatalf("exit=%v", stub.ExitCode)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if !strings.Contains(first, `"line"`) {
		t.Fatalf("want wrapped lines, got %s", first)
	}
}

func TestRunResultScript_JSONObjectsPassthrough(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	code := "for i in range(60):\n print('{\"a\":%d}'%i)"
	out, err := tl.Execute(ctx, map[string]any{"path": data, "code": code})
	if err != nil {
		t.Fatal(err)
	}
	stub := out.(*QuerySpillStub)
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stub.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(b)), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["line"]; ok {
		t.Fatalf("must not wrap: %v", row)
	}
	if _, ok := row["a"]; !ok {
		t.Fatalf("%v", row)
	}
}

func TestRunResultScript_ReadsArgv1(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	pyRel := "tmp/results/sess-1/count.py"
	py := "import sys\nprint(sum(1 for _ in open(sys.argv[1], encoding='utf-8')))\n"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(pyRel)), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{"path": data, "script_path": pyRel})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if !strings.Contains(fmt.Sprint(m["output"]), "1") {
		t.Fatalf("%v", m)
	}
}

func TestRunResultScript_ExitOneNotExecuteError(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "import sys\nprint('boom')\nsys.exit(1)",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	code, _ := m["exit_code"].(int)
	if code != 1 {
		t.Fatalf("%v", m["exit_code"])
	}
}

func TestRunResultScript_TimeoutWithOutput(t *testing.T) {
	requirePython(t)
	old := scriptTimeout
	scriptTimeout = 200 * time.Millisecond
	defer func() { scriptTimeout = old }()
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "import time\nprint('before', flush=True)\ntime.sleep(5)\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	switch v := out.(type) {
	case map[string]any:
		if v["timed_out"] != true {
			t.Fatalf("%v", v)
		}
	case *QuerySpillStub:
		if !v.TimedOut {
			t.Fatal("want timed_out")
		}
	default:
		t.Fatalf("%T", out)
	}
}

func TestRunResultScript_ByteSpillFewHugeLines(t *testing.T) {
	requirePython(t)
	ctx, root := spillCtx(t)
	data := writeFixtureJSONL(t, root)
	tl := scriptTool(t)
	out, err := tl.Execute(ctx, map[string]any{
		"path": data,
		"code": "print('x'*9000)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(*QuerySpillStub); !ok {
		t.Fatalf("want stub, got %T %#v", out, out)
	}
}
```

补 import：`encoding/json` `fmt` `time`。

`exit_code` 比较请写成 helper，避免 `float64`/`int` 混用：

```go
func exitCodeOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return -999
	}
}
```

`TestRunResultScript_SmallInline` 用 `exitCodeOf(m["exit_code"]) != 0`。

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./tool -count=1 -run "TestRunResultScript_SmallInline|TestRunResultScript_TextSpillSixtyLines|TestRunResultScript_JSONObjectsPassthrough|TestRunResultScript_ReadsArgv1|TestRunResultScript_ExitOneNotExecuteError|TestRunResultScript_TimeoutWithOutput|TestRunResultScript_ByteSpillFewHugeLines"
```

Expected: FAIL（`not implemented` 或无进程）。

- [ ] **Step 3: 实现进程与结果组装**

`pythonInterpreter`：

```go
func pythonInterpreter() (string, error) {
	if p, err := lookPathFn("python"); err == nil {
		return p, nil
	}
	return lookPathFn("python3")
}
```

守卫通过后：

1. 解析脚本绝对路径：有 `code` 则 `newSpillNamedFile(ws, sessionID, "run_result_script", ".py")`；`os.MkdirAll`；`os.WriteFile` UTF-8；失败 return error。成功则 `expireSessionResults(filepath.Dir(full), time.Now())`。若 `full` 等于数据文件绝对路径，再调一次 `newSpillNamedFile`。
2. `interp, err := pythonInterpreter()`；失败：`run_result_script: python 3 required on PATH`
3. `dataAbs` = `resolveResultsPath` 得到的绝对路径
4. `runCtx, cancel := context.WithTimeout(ctx, scriptTimeout)`；`defer cancel()`
5. `cmd := exec.CommandContext(runCtx, interp, scriptAbs, dataAbs)`；`cmd.Dir = filepath.Dir(dataAbs)`
6. 环境：继承 `os.Environ()`，去掉已有 `PYTHONNOUSERSITE`/`PYTHONUNBUFFERED` 再追加 `PYTHONNOUSERSITE=1`、`PYTHONUNBUFFERED=1`（超时用例依赖 flush）。不要设 `workspace_root`。
7. `out, err := cmd.CombinedOutput()`；`raw := string(out)`
8. `timedOut := runCtx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded)`
9. `exitCode := 0`；若 `cmd.ProcessState != nil` 用 `ExitCode()`；否则若 timedOut 则为 `-1`
10. 若 `timedOut && strings.TrimSpace(raw)==""` → `return nil, fmt.Errorf("run_result_script: timed out")`
11. `lines := splitScriptOutput(raw)`；`rows := rowsFromScriptLines(lines)`
12. 若 `len(lines)==0`：返回 map：`ok:true`（bool）、`exit_code:exitCode`（int）、`path:relOut`、`line_count:0`、`output:raw`；仅 `timedOut` 时设 `timed_out:true`
13. `meta := map[string]any{"ok": true}` — **不要**把 `output` 放进 meta
14. `stub, fallback := spillRowSet(ctx, "run_result_script", rows, rows, meta, nil)`
15. 若 `stub != nil`：`stub.SourcePath = relOut`；`ec := exitCode`；`stub.ExitCode = &ec`；`stub.TimedOut = timedOut`；`return stub, nil`
16. 若 `fallback["spill_error"] != nil`（本应 spill 但写失败）：`output = truncateUTF8Bytes(raw, spillByteThreshold)`，返回 map 并带 `spill_error` 短类名（`fmt.Sprint(fallback["spill_error"])`），**不要**把 OS 绝对路径放进 error 字符串
17. 未 spill：map 含 `ok`、`exit_code`、`path`（数据文件相对路径）、`line_count:len(lines)`、`output:raw`；`timed_out` 仅超时

非 0 退出：`Execute` 的 error 仍为 **nil**。

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./tool -count=1 -run "TestRunResultScript_"
go test ./tool -count=1
```

Expected: `TestRunResultScript_*` PASS；`./tool` 全绿（含原 MaybeSpill / result_stats）。

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/run_result_script.go framework/tool/run_result_script_test.go
git commit -m "feat(tool): run Python on spilled result files and spill large stdout"
```

---

### Task 5: 工具描述

**Files:**
- Modify: `framework/tool/result_stats.go`（`Description`）
- Modify: `framework/tool/es_log_tool.go`（`Description`）

- [ ] **Step 1: 写失败测试**

`result_stats_test.go` 追加（文件已存在）：

```go
func TestResultStatsDescriptionMentionsRunResultScript(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterResultStatsTool(reg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("result_stats")
	if !strings.Contains(tl.Description, "run_result_script") {
		t.Fatalf("%s", tl.Description)
	}
}
```

`es_log_tool_test.go` 追加（复用文件里已有的 `fakeReader`，不要 `executor.NewReader()`）：

```go
func TestESLogQueryDescriptionMentionsRunResultScript(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{DatasourceID: "es-logs"}); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("es_log_query")
	if !strings.Contains(tl.Description, "run_result_script") {
		t.Fatalf("%s", tl.Description)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./tool -count=1 -run "TestResultStatsDescriptionMentionsRunResultScript|TestESLogQueryDescriptionMentionsRunResultScript"
```

Expected: FAIL（description 无该字符串）。

- [ ] **Step 3: 改描述**

`result_stats`：在现有句子后追加 ` For transforms result_stats cannot do, use run_result_script on the same path.`

`es_log_query`：在现有 `result_stats` 句后追加 ` Complex transforms: run_result_script (not read_file).`

`run_result_script` 的 Description（若 Task 3 已写全可跳过）必须含 `result_stats` 与 `sys.argv[1]`。

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./tool -count=1 -run "TestResultStatsDescriptionMentionsRunResultScript|TestESLogQueryDescriptionMentionsRunResultScript"
go test ./tool ./tool/data ./agent -count=1
```

Expected: PASS。

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/result_stats.go framework/tool/result_stats_test.go framework/tool/es_log_tool.go framework/tool/es_log_tool_test.go
git commit -m "docs(tool): point result_stats and es_log_query at run_result_script"
```

---

## Self-review（对照 spec）

| Spec | 任务 |
|------|------|
| 无合法 path 不起 Python | Task 3 |
| code XOR script_path、64KiB、`.py` | Task 3 |
| python 然后 python3、argv 绝对路径、cwd=数据目录、15s、合并输出、PYTHONNOUSERSITE | Task 4 |
| 空行丢弃、全 object 原样 / 否则 `line` | Task 2 |
| 50 行或 8KB → stub；空输出不 spill | Task 1 marshal rows + Task 4 |
| stub `source_path`/`exit_code`/`timed_out` 在 sample 前 | Task 1 |
| 非 0 不是 Execute error；超时无输出是 error | Task 4 |
| 写 jsonl 失败截断 8192 | Task 4 步骤 16 |
| 注册与 file 工具一起 | Task 3 |
| 描述优先级 | Task 3+5 |
| 32MiB | 复用 `writeJSONL`，不单开 e2e |
| 不测 OS 沙箱 / 门户 / ES | 无对应任务 |

无 TBD。类型：`ExitCode *int` 全计划一致。
