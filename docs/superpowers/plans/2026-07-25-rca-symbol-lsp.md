# RCA Symbol LSP (`rca_symbol`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 framework 落地 `rca_symbol`（`definition` / `references`）+ LSP 抽象与 gopls 实现，并经 portal/web 以 `func_path: rca_symbol` 绑定到 Agent。

**Architecture:** `tool/lsp` 提供 `LanguageServer`、per-Registry `Pool`、JSON-RPC 与 gopls adapter；`RegisterRCASymbolTool` 复用 `rca_repos`/`pathguard`/`rcaOK` evidence 契约；portal `registerRCATool` 增加分支，与 `rca_code` 对称（空 roots skip）。

**Tech Stack:** Go 1.22+、LSP over stdio（Content-Length JSON-RPC）、本机 `gopls`（CheckFn 门控）、既有 portal/web RCA 表单。

**依据规范:** 所有 `go` 命令在 `framework/` 或 `portal/` 下执行；改 tool 后 `go test ./tool/... ./tool/lsp/...`；密钥本期无新增。

**Spec:** `docs/superpowers/specs/2026-07-25-rca-symbol-lsp-design.md`

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/tool/lsp/types.go` | `Position`/`Location`/`ServerOpts`/`LanguageServer`/`ServerFactory` |
| `framework/tool/lsp/uri.go` | `file://` ↔ 本地路径（含 Windows） |
| `framework/tool/lsp/jsonrpc.go` | Content-Length 读写 + request/response/notification |
| `framework/tool/lsp/pool.go` | `normalizeRoot`、per-root mutex、Kill、Close |
| `framework/tool/lsp/gopls.go` | gopls 子进程：initialize / didOpen / definition / references |
| `framework/tool/lsp/*_test.go` | URI、RPC、Pool、fake server |
| `framework/tool/rca_symbol_tool.go` | `RegisterRCASymbolTool`、入参、定位、截断 |
| `framework/tool/rca_symbol_resolve.go` | `symbol` → candidates（workspace/symbol 或 `*.go` 启发式） |
| `framework/tool/rca_symbol_tool_test.go` | 工具契约单测（注入 fake factory） |
| `framework/tool/evidence.go` | `deriveEvidenceRefs` 增加 `rca_symbol` |
| `framework/tool/evidence_test.go` | 派生 refs / 消歧无 refs |
| `framework/tool/toolset.go` | `builtinDefaultToolset["rca_symbol"]=ToolsetRCA` |
| `portal/internal/chat/rca_builder.go` | `case "rca_symbol"`（从 `cfg["rca"]` 读配置） |
| `portal/internal/biz/tool.go` | `ValidRCAFuncPath` 加 `rca_symbol` |
| `portal/internal/chat/rca_builder_test.go` | 有/无 roots 注册行为 |
| `portal/internal/chat/rca_binding_acceptance_test.go` | allowlist 加 `rca_symbol` |
| `portal/internal/biz/tool_test.go` | Create/Update 合法 func_path |
| `web/src/api/client.ts` | 类型扩展 |
| `web/src/pages/ToolForm.tsx` | 下拉 + roots + gopls_path + ready/request_timeout_sec |

**跟踪项（非独立任务，实现时记下）:** `BuildRegistry` 重建时旧 Registry 的 LSP Pool `Close()`——尽量在 builder 可观察处 hook；若无法 hook，在 PR 描述注明已知限制（spec §5.2 / §13）。

---

### Task 1: LSP 类型与 URI

**Files:**
- Create: `framework/tool/lsp/types.go`
- Create: `framework/tool/lsp/uri.go`
- Create: `framework/tool/lsp/uri_test.go`

- [x] **Step 1: 写失败测试**

```go
package lsp

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathURIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	uri := PathToURI(file)
	got, err := URIToPath(uri)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(file) {
		t.Fatalf("got %q want %q", got, file)
	}
}

func TestURIToPath_RejectsNonFile(t *testing.T) {
	if _, err := URIToPath("https://example.com/x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPathToURI_WindowsDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	uri := PathToURI(`D:\workspace\foo.go`)
	if uri == "" || uri[:8] != "file:///" {
		t.Fatalf("unexpected uri %q", uri)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./tool/lsp/ -run TestPathURI -count=1
```

Expected: FAIL（package 不存在）

- [ ] **Step 3: 最小实现**

`types.go`：导出 `Position`（0-based）、内部用 location 结构、`ServerOpts`、`LanguageServer` 接口、`ServerFactory`（见 spec §5.1）。  
`uri.go`：`PathToURI` / `URIToPath`（可用 `net/url` + `path/filepath`；Windows 注意 `file:///D:/...`）。

- [ ] **Step 4: 测试通过**

```bash
cd framework
go test ./tool/lsp/ -run "TestPathURI|TestURIToPath" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**（若用户要求提交再执行；默认可跳过）

---

### Task 2: JSON-RPC framing

**Files:**
- Create: `framework/tool/lsp/jsonrpc.go`
- Create: `framework/tool/lsp/jsonrpc_test.go`

- [ ] **Step 1: 写失败测试**

覆盖：写一条 `Content-Length` 消息再读回；粘包（一次 write 两条）；半包（分两次 read）。

- [ ] **Step 2: 跑测确认失败 → 实现 `Conn`（`io.ReadWriter`）`WriteMessage` / `ReadMessage` → 再跑通**

```bash
cd framework
go test ./tool/lsp/ -run TestJSONRPC -count=1
```

---

### Task 3: Pool + normalizeRoot

**Files:**
- Create: `framework/tool/lsp/pool.go`
- Create: `framework/tool/lsp/pool_test.go`

- [ ] **Step 1: Fake LanguageServer**

在测试文件内实现 `fakeServer`：记录 `EnsureReady` 次数、`Close` 次数；`Definition` 返回固定 location。

- [ ] **Step 2: 测试**

- 同 root 两次 `Get` → 只 `EnsureReady` 一次（或 factory 只调一次）。  
- Windows：`D:\foo` 与 `D:/foo`（用 TempDir 拼两种写法）命中同一 key（可导出 `normalizeRoot` 做单测）。  
- factory 失败 / `MarkDead` 后下次重建。  
- `Close` 后 `Get` 报错。

- [ ] **Step 3: 实现 `Pool`**

```go
type Pool struct {
    factory ServerFactory
    opts    ServerOpts
    // mu + map[string]*entry
}

func (p *Pool) Get(ctx context.Context, root string) (LanguageServer, error)
func (p *Pool) Close(ctx context.Context) error
```

`normalizeRoot`: Clean + EvalSymlinks（失败则 Clean）+ Windows ToLower。

- [ ] **Step 4: 测试通过**

```bash
cd framework
go test ./tool/lsp/ -run TestPool -count=1
```

---

### Task 4: gopls adapter（可单测协议，集成可选）

**Files:**
- Create: `framework/tool/lsp/gopls.go`
- Create: `framework/tool/lsp/gopls_test.go`
- Optional: `framework/tool/lsp/gopls_integration_test.go`（`//go:build gopls_integration`）

- [ ] **Step 1: 用假子进程测协议**

测试启动 `go test` 辅助：`TestMain` 或 `exec.Command` 跑一个小 Go 程序读 stdin 写固定 LSP initialize/definition 响应（或用 `net.Pipe` + 手写 framing 注入，不真启 gopls）。

验证：`Definition` / `References` 解析 `Location | Location[] | LocationLink[]`；root 外 URI 由上层过滤（本层可原样返回 path）。

- [ ] **Step 2: 实现 `GoplsServer`**

- `StartGopls(ctx, root, opts)` → initialize + initialized  
- `didOpen` 读盘  
- `textDocument/definition`、`textDocument/references`  
- `Close` → shutdown/exit + Kill  
- initialize 结果缺少 `definitionProvider` / `referencesProvider` → 后续请求返回可映射为工具层 **`permanent`** 的错误（spec §5.1）

- [ ] **Step 3: 单元测通过；集成测（有 gopls 时）**

```bash
cd framework
go test ./tool/lsp/ -count=1
# 可选：
go test ./tool/lsp/ -tags=gopls_integration -count=1
```

---

### Task 5: `RegisterRCASymbolTool` — file+line 路径

**Files:**
- Create: `framework/tool/rca_symbol_tool.go`
- Create: `framework/tool/rca_symbol_tool_test.go`
- Modify: `framework/tool/toolset.go`（加 `"rca_symbol": ToolsetRCA`）

- [ ] **Step 1: 写失败测试**

参考 `rca_code_tools_test.go` 建双仓 TempDir；注入 `RCASymbolOpts{Factory: fake}`：

```go
func TestRCASymbol_DefinitionByLine(t *testing.T) {
	// RegisterRCASymbolTool(reg, roots, opts with fake returning one location)
	// Execute action=definition repo=... file=a.go line=3
	// assert ok, locations[0].line==…, evidence_refs non-empty via Normalize
}
func TestRCASymbol_EmptyRootsRuntime(t *testing.T) {
	// Register with nil roots; Execute → ok=false permanent
}
func TestRCASymbol_UnknownRepo(t *testing.T) { /* permanent */ }
func TestRCASymbol_FiltersOutOfRootLocations(t *testing.T) { /* fake returns escape path; filtered */ }
func TestRCASymbol_CheckFnMissingGopls(t *testing.T) {
	// opts.GoplsPath = nonexistent; ListForAPI 不含 rca_symbol
}
func TestRCASymbol_ReferencesByLine(t *testing.T) {
	// action=references + include_declaration；assert locations / truncated
}
func TestRCASymbol_ParamValidation(t *testing.T) {
	// 非法 action、缺 repo、有 file 无 line → permanent
}
func TestRCASymbol_EmptyLocationsPermanent(t *testing.T) {
	// fake 返回空 locations → ok=false permanent
}
```

- [ ] **Step 2: 实现注册与 Execute（仅 file+line）**

要点（spec §4 / §7）：

- 入参 line 1-based → `lsp.Position{Line: line-1, Character: character}`  
- 出参 line +1  
- `RequiresSequential: true`  
- `CheckFn`: `exec.LookPath` / 绝对路径 `Stat`  
- 成功走 `rcaOK`；失败 `rcaErr`  
- `max_results` 截断 `locations`  

- [ ] **Step 3: 测试**

```bash
cd framework
go test ./tool/ -run TestRCASymbol_ -count=1
```

Expected: PASS（Task 5 范围；symbol 路径可先跳过或 t.Skip）

---

### Task 6: symbol 解析与消歧

**Files:**
- Create: `framework/tool/rca_symbol_resolve.go`
- Modify: `framework/tool/rca_symbol_tool.go`
- Modify: `framework/tool/rca_symbol_tool_test.go`

- [ ] **Step 1: 测试消歧契约**

```go
func TestRCASymbol_SymbolDisambiguation(t *testing.T) {
	// 两处 func Foo；action=definition symbol=Foo
	// ok=true, needs_disambiguation=true, candidates len>=2
	// 无 locations、无 evidence_refs
}
func TestRCASymbol_SymbolUniqueThenDefinition(t *testing.T) {
	// 唯一声明 → 调用 fake Definition 一次
}
```

- [ ] **Step 2: 实现 `resolveSymbolCandidates`**

按 spec §7.4：仅 `*.go`、整词、T1–T4 tier、`max_results`。  
一期可先做启发式（不强制 workspace/symbol）；若接 gopls `workspace/symbol`，失败时 fallback 启发式。

- [ ] **Step 3: 测试通过**

```bash
cd framework
go test ./tool/ -run "TestRCASymbol_Symbol" -count=1
```

---

### Task 7: Evidence 派生

**Files:**
- Modify: `framework/tool/evidence.go`（`deriveEvidenceRefs` switch）
- Modify: `framework/tool/evidence_test.go`

- [ ] **Step 1: 测试**

```go
func TestDeriveEvidenceRefs_RCASymbol(t *testing.T) {
	payload := map[string]any{
		"locations": []any{
			map[string]any{"repo": "svc", "file": "a.go", "line": 9, "name": "Foo"},
		},
	}
	refs := deriveEvidenceRefs("rca_symbol", payload)
	// kind 钉死为 "rca_symbol"（对齐 rca_grep 用 tool 名作 kind）
	// path=a.go line=9；无 character 要求
}
```

- [ ] **Step 2: 实现 `case "rca_symbol":` 从 `locations` 派生（无 locations 则 nil）**

- [ ] **Step 3:**

```bash
cd framework
go test ./tool/ -run "Evidence|Derive" -count=1
go test ./tool/ ./tool/lsp/ -count=1
```

---

### Task 8: Portal 绑定

**Files:**
- Modify: `portal/internal/biz/tool.go` — `ValidRCAFuncPath` 增加 `"rca_symbol"`
- Modify: `portal/internal/chat/rca_builder.go` — `switch` 增加 `case "rca_symbol":`（**从 `rcaMap` 读字段，勿用顶层 `cfg`**）
- Modify: `portal/internal/chat/rca_builder_test.go`
- Modify: `portal/internal/chat/rca_binding_acceptance_test.go` — allowlist 含 `rca_symbol`
- Modify: `portal/internal/biz/tool_test.go`（若有合法 func_path 表）

- [ ] **Step 1: 测试**

- `ValidRCAFuncPath("rca_symbol") == true`  
- `registerRCATool(reg, map[string]any{"rca": map[string]any{"func_path":"rca_symbol","roots":[]any{"/repos/a"}}}, nil)` → registry 含 `rca_symbol`  
- 无 roots → 不注册、不 panic  
- acceptance allowlist 循环加入 `"rca_symbol"`

- [ ] **Step 2: 实现 case（对齐现有 `rca_code` 读法）**

```go
case "rca_symbol":
	roots := stringSliceFromAny(rcaMap["roots"])
	if len(roots) == 0 {
		slog.Warn("rca: rca_symbol has no roots, skip")
		return
	}
	goplsPath, _ := rcaMap["gopls_path"].(string)
	opts := tool.RCASymbolOpts{GoplsPath: goplsPath}
	if v, ok := rcaMap["ready_timeout_sec"].(float64); ok && v > 0 {
		opts.ReadyTimeout = time.Duration(v) * time.Second
	}
	if v, ok := rcaMap["request_timeout_sec"].(float64); ok && v > 0 {
		opts.RequestTimeout = time.Duration(v) * time.Second
	}
	_ = tool.RegisterRCASymbolTool(reg, roots, opts)
```

注意：`registerRCATool` 入口已是 `rcaMap, _ := cfg["rca"].(map[string]interface{})`，新分支必须读 **`rcaMap`**。

- [ ] **Step 3:**

```bash
cd portal
go test ./internal/biz/ ./internal/chat/ -count=1
go build ./...
```

---

### Task 9: Web 表单

**Files:**
- Modify: `web/src/api/client.ts` — `func_path` 联合类型加 `'rca_symbol'`；`gopls_path?`、`ready_timeout_sec?`、`request_timeout_sec?`
- Modify: `web/src/pages/ToolForm.tsx` — option + 字段展示

- [ ] **Step 1: 类型与下拉**

```tsx
<option value="rca_symbol">符号导航 (definition/references)</option>
```

当 `func_path === 'rca_symbol'`（或与 `rca_code` 共用 roots 条件）显示：

- roots textarea（与 `rca_code` 共用）
- 可选 `gopls_path` input
- 可选 `ready_timeout_sec` / `request_timeout_sec` number input（spec §8.3；写入 `config.rca`）

建议条件：

```tsx
{((config.rca?.func_path || 'rca_code') === 'rca_code' || config.rca?.func_path === 'rca_symbol') && (
  /* roots textarea */
)}
{config.rca?.func_path === 'rca_symbol' && (
  <>
    {/* gopls_path */}
    {/* ready_timeout_sec */}
    {/* request_timeout_sec */}
  </>
)}
```

同步所有 `as 'rca_code' | 'jaeger_trace' | 'es_log_query'` 联合类型为含 `'rca_symbol'`。

- [ ] **Step 2: 构建**

```bash
cd web
npm run build
```

Expected: 成功

---

### Task 10: 回归与手工验收清单

- [ ] **Step 1: 全量相关测试**

```bash
cd framework && go test ./tool/... ./tool/lsp/... -count=1
cd portal && go test ./internal/biz/ ./internal/chat/ -count=1 && go build ./...
cd web && npm run build
```

- [ ] **Step 2: 手工（有 gopls + 真实 Go 仓）**

按 spec §11.3：definition / references / 消歧 / 无 gopls 隐藏 / 与 rca_code 并存。

- [ ] **Step 3: PR 描述写明 BuildRegistry Pool Close 跟踪项状态**

---

## 执行备注

- TDD：每个 Task 先红后绿。  
- 不要把 gopls 集成测默认开进 CI。  
- 不修改 `RegisterRCACodeTools` 签名；不并入 `rca_code`。  
- Description 文案写清：`rca_grep` 取 line → `rca_symbol` → `rca_read`；character 常可省略。
