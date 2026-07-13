# RCA 工具链 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `sixath/framework` 的 `tool` 包新增 5 个 RCA 专属原生工具(`jaeger_trace`、`es_log_query`、`rca_grep`、`rca_glob`、`rca_read`),让 Agent 能自主完成 trace→日志→多仓库代码 的根因分析闭环。

**Architecture:** 每个工具遵循框架现有 `RegisterXxxTool(reg *tool.Registry, ...deps) error` 模式,返回结构化 `map[string]any`。代码侧三工具共用"多仓库根"解析与 `ResolveWorkspacePath` 越权守卫,并复用 `file_tools.go` 的 `searchWithRipgrep`/`searchFilesByGlob` 底层。`es_log_query` 复用 `executor.Reader`(ES 只读通道)+ 已注册 ES datasource。`jaeger_trace` 新写一个最简无鉴权 HTTP 客户端。

**Tech Stack:** Go,`net/http`(+`net/http/httptest` 测试),`go-elasticsearch/v8`(经 executor 间接使用),ripgrep(可选,底层已带 Go 回退)。

**依据规范(sixath-framework skill):** 所有 `go` 命令在 `framework/` 下执行;密钥仅走环境变量(本期无新密钥);改 `tool/` 后跑 `go test ./tool/... -v`;匹配现有命名与 Option 模式。

**Spec:** `docs/superpowers/specs/2026-07-07-rca-toolchain-design.md`

---

## File Structure

新增文件(全部在 `framework/tool/`):

| 文件 | 责任 |
|------|------|
| `rca_repos.go` | 多仓库根配置解析 + 共享守卫辅助(`rcaRepoRoots`、`resolveInRepos`);被三个代码工具共用 |
| `rca_code_tools.go` | `rca_grep` / `rca_glob` / `rca_read` 三个工具注册 + `RegisterRCACodeTools` |
| `rca_code_tools_test.go` | 上述三工具单测 |
| `jaeger_tool.go` | `jaeger_trace` 工具 + 最简 Jaeger HTTP 客户端 |
| `jaeger_tool_test.go` | `jaeger_trace` 单测(httptest 打桩) |
| `es_log_tool.go` | `es_log_query` 工具(依赖注入 `executor.Reader`) |
| `es_log_tool_test.go` | `es_log_query` 单测(fake Reader) |

修改文件:
- `framework/tool/toolset.go` — 在 `builtinDefaultToolset` 增加 5 个工具名的 toolset 归类

不改动 `NewRegistry()`(避免全局默认注册需要依赖的工具);5 个工具由**装配处显式注册**,注册函数签名把依赖显式传入。装配接线(templates)不在本计划范围——本计划只交付工具本身 + 注册函数 + 测试。

---

## Task 1: 多仓库根配置解析与共享守卫(`rca_repos.go`)

**Files:**
- Create: `framework/tool/rca_repos.go`
- Test: `framework/tool/rca_code_tools_test.go`(本任务先建文件,放守卫测试)

- [ ] **Step 1: 写失败测试**

创建 `framework/tool/rca_code_tools_test.go`:

```go
package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInRepos_HappyAndTraversal(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	for _, d := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{repoA, repoB}

	// repo 指定 + 合法相对路径
	full, root, err := resolveInRepos(roots, "service-b", "sub/x.go")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if root != repoB {
		t.Fatalf("root = %q, want %q", root, repoB)
	}
	wantFull := filepath.Join(repoB, "sub", "x.go")
	if full != wantFull {
		t.Fatalf("full = %q, want %q", full, wantFull)
	}

	// 路径穿越必须被拒
	if _, _, err := resolveInRepos(roots, "service-a", "../service-b/secret"); err == nil {
		t.Fatal("expected traversal to be rejected, got nil err")
	}

	// 未知 repo 名必须报错
	if _, _, err := resolveInRepos(roots, "unknown", "x.go"); err == nil {
		t.Fatal("expected unknown repo error, got nil err")
	}

	// roots 为空必须报错
	if _, _, err := resolveInRepos(nil, "", "x.go"); err == nil {
		t.Fatal("expected empty roots error, got nil err")
	}
}

func TestRepoNameFromRoot(t *testing.T) {
	if got := repoNameFromRoot("/a/b/service-a"); got != "service-a" {
		t.Fatalf("repoNameFromRoot = %q, want service-a", got)
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestResolveInRepos_HappyAndTraversal|TestRepoNameFromRoot' -v`
Expected: FAIL,编译错误 `undefined: resolveInRepos` / `undefined: repoNameFromRoot`

- [ ] **Step 3: 写最小实现**

创建 `framework/tool/rca_repos.go`:

```go
package tool

import (
	"fmt"
	"path/filepath"
)

// repoNameFromRoot 返回仓库根目录的基名,作为该仓库的逻辑名。
func repoNameFromRoot(root string) string {
	return filepath.Base(filepath.Clean(root))
}

// selectRoots 根据可选 repo 名从 roots 中筛选目标根。
// repo 为空返回全部;否则返回名字匹配的单个根。roots 为空或 repo 未命中时报错。
func selectRoots(roots []string, repo string) ([]string, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("rca: no repository roots configured (rca.repos.roots is empty)")
	}
	if repo == "" {
		return roots, nil
	}
	for _, r := range roots {
		if repoNameFromRoot(r) == repo {
			return []string{r}, nil
		}
	}
	return nil, fmt.Errorf("rca: unknown repo %q; configured repos are %v", repo, repoNames(roots))
}

// repoNames 返回全部仓库逻辑名,用于错误提示。
func repoNames(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, repoNameFromRoot(r))
	}
	return out
}

// resolveInRepos 解析 repo 内相对路径 rel 为绝对路径,并用 ResolveWorkspacePath 拒绝越权。
// repo 必填(用于读单个文件的场景);返回 (绝对路径, 命中的仓库根, error)。
func resolveInRepos(roots []string, repo, rel string) (string, string, error) {
	sel, err := selectRoots(roots, repo)
	if err != nil {
		return "", "", err
	}
	if repo == "" {
		return "", "", fmt.Errorf("rca: repo is required to resolve a specific path")
	}
	root := sel[0]
	full, err := ResolveWorkspacePath(root, rel)
	if err != nil {
		return "", "", err
	}
	return full, root, nil
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestResolveInRepos_HappyAndTraversal|TestRepoNameFromRoot' -v`
Expected: PASS

- [ ] **Step 5: 提交(项目非 git 仓库,跳过 commit;若已初始化 git 则执行)**

```bash
# 若 framework 目录在 git 仓库内:
git add framework/tool/rca_repos.go framework/tool/rca_code_tools_test.go
git commit -m "feat(tool): add multi-repo root resolver for RCA code tools"
```
否则跳过本步(spec 已说明本项目非 git 仓库)。后续每个 Task 的 commit 步骤同此约定。

---

## Task 2: `rca_grep` — 跨多仓库内容正则搜

**Files:**
- Create/Modify: `framework/tool/rca_code_tools.go`
- Test: `framework/tool/rca_code_tools_test.go`

- [ ] **Step 1: 写失败测试**

在 `framework/tool/rca_code_tools_test.go` 追加:

```go
import (
	"context"
	// 已有 os / path/filepath / testing
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRCARegistry(t *testing.T, roots []string) *Registry {
	t.Helper()
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterRCACodeTools(reg, roots); err != nil {
		t.Fatalf("RegisterRCACodeTools: %v", err)
	}
	return reg
}

func TestRCAGrep_MultiRepo(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "a.go"), "package a\n// NullPointer here\n")
	writeFile(t, filepath.Join(repoB, "b.go"), "package b\nvar NullPointer = 1\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, ok := reg.Get("rca_grep")
	if !ok {
		t.Fatal("rca_grep not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "NullPointer"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	matches := m["matches"].([]map[string]any)
	if len(matches) != 2 {
		t.Fatalf("want 2 matches across repos, got %d: %v", len(matches), matches)
	}
	// 每条命中带 repo 字段
	repos := map[string]bool{}
	for _, mm := range matches {
		repos[mm["repo"].(string)] = true
	}
	if !repos["service-a"] || !repos["service-b"] {
		t.Fatalf("expected matches from both repos, got %v", repos)
	}
}

func TestRCAGrep_EmptyRootsError(t *testing.T) {
	reg := newRCARegistry(t, nil)
	tl, _ := reg.Get("rca_grep")
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "x"})
	if _, ok := out.(map[string]any)["error"]; !ok {
		t.Fatal("expected error when roots empty")
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestRCAGrep' -v`
Expected: FAIL,`undefined: RegisterRCACodeTools`

- [ ] **Step 3: 写最小实现**

创建 `framework/tool/rca_code_tools.go`:

```go
package tool

import (
	"context"
	"errors"
	"strings"
)

const (
	rcaMaxResultsDefault = 100
	ToolsetRCA           = "rca"
)

// RegisterRCACodeTools 注册 rca_grep / rca_glob / rca_read 三个多仓库代码检索工具。
// roots 为允许检索的仓库根白名单(pathguard 守卫作用于每个根)。
func RegisterRCACodeTools(reg *Registry, roots []string) error {
	if reg == nil {
		return errors.New("rca code tools: registry is nil")
	}
	if err := registerRCAGrepTool(reg, roots); err != nil {
		return err
	}
	if err := registerRCAGlobTool(reg, roots); err != nil {
		return err
	}
	return registerRCAReadTool(reg, roots)
}

func registerRCAGrepTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name:        "rca_grep",
		Description: "Search source code by regex across all configured RCA repository roots (multi-repo). Optionally limit to one repo. Returns file, line and snippet with the owning repo.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Regex pattern for content search."},
				"repo":        map[string]any{"type": "string", "description": "Optional repo name to limit the search to a single repository root."},
				"glob":        map[string]any{"type": "string", "description": "Optional file glob filter, e.g. '*.go'."},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			pattern, _ := params["pattern"].(string)
			if strings.TrimSpace(pattern) == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}
			repo, _ := params["repo"].(string)
			glob, _ := params["glob"].(string)
			maxResults := intFromParam(params["max_results"], rcaMaxResultsDefault)
			if maxResults <= 0 {
				maxResults = rcaMaxResultsDefault
			}
			sel, err := selectRoots(roots, repo)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			matches := make([]map[string]any, 0, maxResults)
			truncated := false
			for _, root := range sel {
				if len(matches) >= maxResults {
					truncated = true
					break
				}
				remaining := maxResults - len(matches)
				// workspaceRoot==root 使返回的 Path 为仓库内相对路径。
				res, err := searchFileContents(root, root, pattern, glob, remaining+1, 0)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				name := repoNameFromRoot(root)
				for _, cm := range res {
					if len(matches) >= maxResults {
						truncated = true
						break
					}
					matches = append(matches, map[string]any{
						"repo":    name,
						"file":    cm.Path,
						"line":    cm.Line,
						"snippet": cm.Content,
					})
				}
			}
			return map[string]any{"matches": matches, "truncated": truncated}, nil
		},
	})
}
```

> 说明:`searchFileContents(workspaceRoot, root, pattern, fileGlob, limit, offset)` 已存在于 `file_tools.go`,内部优先 ripgrep、失败回退 Go 遍历,返回 `[]contentMatch{Path,Line,Content}`。传 `workspaceRoot==root` 使 `Path` 为仓库内相对路径。`intFromParam` 亦为 `file_tools.go` 现有辅助。

本任务需要 `registerRCAGlobTool` / `registerRCAReadTool` 已声明才能编译;为让本任务独立可编译,先加两个占位注册函数在同文件末尾(Task 3/4 再填实现):

```go
func registerRCAGlobTool(reg *Registry, roots []string) error { return nil }
func registerRCAReadTool(reg *Registry, roots []string) error { return nil }
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestRCAGrep' -v`
Expected: PASS(两个用例)

- [ ] **Step 5: Commit**(约定同 Task 1 Step 5)

```bash
git add framework/tool/rca_code_tools.go framework/tool/rca_code_tools_test.go
git commit -m "feat(tool): add rca_grep multi-repo content search"
```

---

## Task 3: `rca_glob` — 跨多仓库按文件名 glob 找文件

**Files:**
- Modify: `framework/tool/rca_code_tools.go`(替换 `registerRCAGlobTool` 占位)
- Test: `framework/tool/rca_code_tools_test.go`

- [ ] **Step 1: 写失败测试**

追加:

```go
func TestRCAGlob_MultiRepo(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "main.go"), "package a\n")
	writeFile(t, filepath.Join(repoA, "readme.md"), "x\n")
	writeFile(t, filepath.Join(repoB, "util.go"), "package b\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, _ := reg.Get("rca_glob")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	matches := out.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 2 {
		t.Fatalf("want 2 .go files, got %d: %v", len(matches), matches)
	}
	for _, mm := range matches {
		if mm["repo"] == "" || mm["file"] == "" {
			t.Fatalf("match missing repo/file: %v", mm)
		}
	}
}

func TestRCAGlob_RepoScoped(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	repoB := filepath.Join(base, "service-b")
	writeFile(t, filepath.Join(repoA, "main.go"), "package a\n")
	writeFile(t, filepath.Join(repoB, "util.go"), "package b\n")
	reg := newRCARegistry(t, []string{repoA, repoB})

	tl, _ := reg.Get("rca_glob")
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "*.go", "repo": "service-a"})
	matches := out.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["repo"].(string) != "service-a" {
		t.Fatalf("want only service-a match, got %v", matches)
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestRCAGlob' -v`
Expected: FAIL(占位实现未注册 `rca_glob`,`reg.Get("rca_glob")` 返回 ok=false → panic 或断言失败)

- [ ] **Step 3: 写实现**

在 `rca_code_tools.go` 用下面替换 `registerRCAGlobTool` 的占位:

```go
func registerRCAGlobTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name:        "rca_glob",
		Description: "Find files by name glob across all configured RCA repository roots (multi-repo). Optionally limit to one repo. Returns matching file paths with the owning repo.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Glob pattern, e.g. '*.go'."},
				"repo":        map[string]any{"type": "string", "description": "Optional repo name to limit to a single repository root."},
				"max_results": map[string]any{"type": "integer", "description": "Max results (default 100)."},
			},
			"required": []string{"pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			pattern, _ := params["pattern"].(string)
			if strings.TrimSpace(pattern) == "" {
				return map[string]any{"error": "pattern is required"}, nil
			}
			repo, _ := params["repo"].(string)
			maxResults := intFromParam(params["max_results"], rcaMaxResultsDefault)
			if maxResults <= 0 {
				maxResults = rcaMaxResultsDefault
			}
			sel, err := selectRoots(roots, repo)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			matches := make([]map[string]any, 0, maxResults)
			truncated := false
			for _, root := range sel {
				if len(matches) >= maxResults {
					truncated = true
					break
				}
				remaining := maxResults - len(matches)
				res, err := searchFilesByGlob(root, root, pattern, "", remaining+1, 0)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}
				name := repoNameFromRoot(root)
				for _, fm := range res {
					if len(matches) >= maxResults {
						truncated = true
						break
					}
					matches = append(matches, map[string]any{
						"repo": name,
						"file": fm.Path,
					})
				}
			}
			return map[string]any{"matches": matches, "truncated": truncated}, nil
		},
	})
}
```

> 说明:`searchFilesByGlob(workspaceRoot, root, pattern, fileGlob, limit, offset)` 已存在,返回 `[]fileMatch{Path,ModTime}`。传 `workspaceRoot==root` 使 `Path` 为仓库内相对路径。

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestRCAGlob' -v`
Expected: PASS(两个用例)

- [ ] **Step 5: Commit**

```bash
git add framework/tool/rca_code_tools.go framework/tool/rca_code_tools_test.go
git commit -m "feat(tool): add rca_glob multi-repo file search"
```

---

## Task 4: `rca_read` — 读某仓库某文件(带行号)

**Files:**
- Modify: `framework/tool/rca_code_tools.go`(替换 `registerRCAReadTool` 占位)
- Test: `framework/tool/rca_code_tools_test.go`

- [ ] **Step 1: 写失败测试**

追加:

```go
func TestRCARead_HappyAndGuard(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "service-a")
	writeFile(t, filepath.Join(repoA, "svc", "handler.go"), "line1\nline2\nline3\n")
	reg := newRCARegistry(t, []string{repoA})

	tl, ok := reg.Get("rca_read")
	if !ok {
		t.Fatal("rca_read not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{
		"repo": "service-a", "file": "svc/handler.go",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	content := m["content"].(string)
	if !strings.Contains(content, "1|line1") || !strings.Contains(content, "3|line3") {
		t.Fatalf("content missing numbered lines: %q", content)
	}
	if m["repo"].(string) != "service-a" {
		t.Fatalf("repo = %v", m["repo"])
	}

	// 越权路径必须被拒
	out2, _ := tl.Execute(context.Background(), map[string]any{
		"repo": "service-a", "file": "../secret.txt",
	})
	if _, has := out2.(map[string]any)["error"]; !has {
		t.Fatal("expected traversal rejection error")
	}

	// 缺 repo 必须报错
	out3, _ := tl.Execute(context.Background(), map[string]any{"file": "svc/handler.go"})
	if _, has := out3.(map[string]any)["error"]; !has {
		t.Fatal("expected error when repo missing")
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestRCARead' -v`
Expected: FAIL(`rca_read` 未注册)

- [ ] **Step 3: 写实现**

替换 `registerRCAReadTool` 占位:

```go
import (
	// 在文件已有 import 基础上补充:
	"fmt"
	"os"
)

func registerRCAReadTool(reg *Registry, roots []string) error {
	return reg.Register(Tool{
		Name:        "rca_read",
		Description: "Read a source file from a specific RCA repository with line numbers (LINE_NUM|CONTENT). Path is guarded to stay inside the repository root.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":       map[string]any{"type": "string", "description": "Repository name (basename of a configured root)."},
				"file":       map[string]any{"type": "string", "description": "Repo-relative file path."},
				"start_line": map[string]any{"type": "integer", "description": "1-based start line (default 1)."},
				"end_line":   map[string]any{"type": "integer", "description": "Inclusive end line (default end of file)."},
			},
			"required": []string{"repo", "file"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			repo, _ := params["repo"].(string)
			file, _ := params["file"].(string)
			if strings.TrimSpace(repo) == "" {
				return map[string]any{"error": "repo is required"}, nil
			}
			if strings.TrimSpace(file) == "" {
				return map[string]any{"error": "file is required"}, nil
			}
			full, _, err := resolveInRepos(roots, repo, file)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			b, err := os.ReadFile(full)
			if err != nil {
				if os.IsNotExist(err) {
					return map[string]any{"error": "file not found", "repo": repo, "file": file}, nil
				}
				return map[string]any{"error": err.Error()}, nil
			}
			lines := strings.Split(string(b), "\n")
			start := intFromParam(params["start_line"], 1)
			if start < 1 {
				start = 1
			}
			end := intFromParam(params["end_line"], len(lines))
			if end <= 0 || end > len(lines) {
				end = len(lines)
			}
			var out strings.Builder
			for i := start - 1; i < end && i < len(lines); i++ {
				fmt.Fprintf(&out, "%d|%s\n", i+1, lines[i])
			}
			return map[string]any{
				"repo":        repo,
				"file":        file,
				"content":     strings.TrimSuffix(out.String(), "\n"),
				"total_lines": len(lines),
			}, nil
		},
	})
}
```

> 说明:行号格式 `LINE_NUM|CONTENT` 与现有 `read_file` 一致。`resolveInRepos` 来自 Task 1,内部用 `ResolveWorkspacePath` 拒绝越权。

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestRCARead' -v`
Expected: PASS

- [ ] **Step 5: 全代码工具回归**

Run: `cd framework && go test ./tool/ -run 'TestRCA|TestResolveInRepos|TestRepoName' -v`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add framework/tool/rca_code_tools.go framework/tool/rca_code_tools_test.go
git commit -m "feat(tool): add rca_read guarded per-repo file read"
```

---

## Task 5: `jaeger_trace` — Jaeger 链路查询(无鉴权 HTTP)

**Files:**
- Create: `framework/tool/jaeger_tool.go`
- Test: `framework/tool/jaeger_tool_test.go`

Jaeger Query API 返回结构(简化,取本工具需要的字段):
```json
{"data":[{"traceID":"abc","spans":[
  {"traceID":"abc","spanID":"s1","operationName":"GET /x","duration":1200,"startTime":1710000000000000,
   "processID":"p1","tags":[{"key":"error","type":"bool","value":true}]}],
  "processes":{"p1":{"serviceName":"service-a"}}}]}
```
- `duration` 单位微秒 → `duration_ms = duration/1000`。
- span 的服务名来自 `processes[processID].serviceName`。
- error span:tags 中存在 `key=="error"` 且 `value==true`。

- [ ] **Step 1: 写失败测试**

创建 `framework/tool/jaeger_tool_test.go`:

```go
package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const jaegerTraceBody = `{"data":[{"traceID":"abc","spans":[
  {"traceID":"abc","spanID":"s1","operationName":"GET /x","duration":1200,"startTime":1710000000000000,"processID":"p1","tags":[{"key":"error","type":"bool","value":true}]},
  {"traceID":"abc","spanID":"s2","operationName":"db.query","duration":800,"startTime":1710000000500000,"processID":"p2","tags":[]}],
  "processes":{"p1":{"serviceName":"service-a"},"p2":{"serviceName":"service-b"}}}]}`

func TestJaegerTrace_ByTraceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/traces/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jaegerTraceBody))
	}))
	defer srv.Close()

	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterJaegerTool(reg, srv.URL); err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, ok := reg.Get("jaeger_trace")
	if !ok {
		t.Fatal("jaeger_trace not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)

	spans := m["spans"].([]map[string]any)
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
	if spans[0]["service"].(string) != "service-a" || spans[0]["duration_ms"].(float64) != 1.2 {
		t.Fatalf("span0 mapping wrong: %v", spans[0])
	}

	errs := m["errors"].([]map[string]any)
	if len(errs) != 1 || errs[0]["service"].(string) != "service-a" {
		t.Fatalf("want 1 error span from service-a, got %v", errs)
	}

	services := m["services"].([]string)
	if len(services) != 2 {
		t.Fatalf("want 2 distinct services, got %v", services)
	}
}

func TestJaegerTrace_RequiresParam(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterJaegerTool(reg, "http://unused")
	tl, _ := reg.Get("jaeger_trace")
	out, _ := tl.Execute(context.Background(), map[string]any{})
	if _, has := out.(map[string]any)["error"]; !has {
		t.Fatal("expected error when neither trace_id nor service provided")
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestJaegerTrace' -v`
Expected: FAIL,`undefined: RegisterJaegerTool`

- [ ] **Step 3: 写实现**

创建 `framework/tool/jaeger_tool.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// jaegerAPIResponse 映射 Jaeger Query API 的最小子集。
type jaegerAPIResponse struct {
	Data []struct {
		TraceID   string `json:"traceID"`
		Spans     []jaegerSpan `json:"spans"`
		Processes map[string]struct {
			ServiceName string `json:"serviceName"`
		} `json:"processes"`
	} `json:"data"`
}

type jaegerSpan struct {
	SpanID        string `json:"spanID"`
	OperationName string `json:"operationName"`
	Duration      int64  `json:"duration"`  // microseconds
	StartTime     int64  `json:"startTime"` // microseconds since epoch
	ProcessID     string `json:"processID"`
	Tags          []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	} `json:"tags"`
}

// RegisterJaegerTool 注册 jaeger_trace 工具。queryURL 为 Jaeger Query 基址(无鉴权)。
func RegisterJaegerTool(reg *Registry, queryURL string) error {
	if reg == nil {
		return errors.New("jaeger tool: registry is nil")
	}
	base := strings.TrimRight(queryURL, "/")
	return reg.Register(Tool{
		Name:        "jaeger_trace",
		Description: "Fetch a Jaeger trace by trace_id (or search by service/operation) and return structured spans, error spans and involved services.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id":  map[string]any{"type": "string", "description": "Exact trace ID to fetch the full chain."},
				"service":   map[string]any{"type": "string", "description": "Search mode: service name."},
				"operation": map[string]any{"type": "string", "description": "Search mode: operation name."},
				"limit":     map[string]any{"type": "integer", "description": "Search mode: max traces (default 20)."},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			traceID, _ := params["trace_id"].(string)
			service, _ := params["service"].(string)
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(service) == "" {
				return map[string]any{"error": "either trace_id or service is required"}, nil
			}

			var endpoint string
			if strings.TrimSpace(traceID) != "" {
				endpoint = base + "/api/traces/" + url.PathEscape(traceID)
			} else {
				operation, _ := params["operation"].(string)
				limit := intFromParam(params["limit"], 20)
				q := url.Values{}
				q.Set("service", service)
				if operation != "" {
					q.Set("operation", operation)
				}
				q.Set("limit", fmt.Sprintf("%d", limit))
				endpoint = base + "/api/traces?" + q.Encode()
			}

			body, err := jaegerGET(ctx, endpoint)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			var parsed jaegerAPIResponse
			if err := json.Unmarshal(body, &parsed); err != nil {
				return map[string]any{"error": fmt.Sprintf("decode jaeger response: %v", err)}, nil
			}
			return summarizeJaeger(parsed), nil
		},
	})
}

func jaegerGET(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jaeger returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func summarizeJaeger(parsed jaegerAPIResponse) map[string]any {
	spans := []map[string]any{}
	errs := []map[string]any{}
	serviceSet := map[string]struct{}{}

	for _, tr := range parsed.Data {
		for _, sp := range tr.Spans {
			svc := ""
			if p, ok := tr.Processes[sp.ProcessID]; ok {
				svc = p.ServiceName
			}
			if svc != "" {
				serviceSet[svc] = struct{}{}
			}
			isErr := false
			for _, tg := range sp.Tags {
				if tg.Key == "error" {
					if bv, ok := tg.Value.(bool); ok && bv {
						isErr = true
					}
				}
			}
			row := map[string]any{
				"service":     svc,
				"operation":   sp.OperationName,
				"start":       sp.StartTime,
				"duration_ms": float64(sp.Duration) / 1000.0,
				"error":       isErr,
			}
			spans = append(spans, row)
			if isErr {
				errs = append(errs, map[string]any{
					"service":   svc,
					"operation": sp.OperationName,
				})
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for s := range serviceSet {
		services = append(services, s)
	}
	sort.Strings(services)

	return map[string]any{
		"spans":    spans,
		"errors":   errs,
		"services": services,
	}
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestJaegerTrace' -v`
Expected: PASS(两个用例)

- [ ] **Step 5: Commit**

```bash
git add framework/tool/jaeger_tool.go framework/tool/jaeger_tool_test.go
git commit -m "feat(tool): add jaeger_trace tool"
```

---

## Task 6: `es_log_query` — ELK 日志查询(复用 executor.Reader)

**Files:**
- Create: `framework/tool/es_log_tool.go`
- Test: `framework/tool/es_log_tool_test.go`

工具依赖注入 `executor.Reader`(装配时传 `executor.NewBundle(dsReg).Reader`)。DSL 为 ES Search 请求体 JSON,index 经 `QueryOptions.Extras["index"]` 传入(与 `executor/elasticsearch.go` 一致)。

- [ ] **Step 1: 写失败测试**

创建 `framework/tool/es_log_tool_test.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sixath/framework/executor"
)

// fakeReader 实现 executor.Reader,记录收到的 DSL / 数据源 / index。
type fakeReader struct {
	gotDatasource string
	gotDSL        string
	gotIndex      string
	result        *executor.QueryResult
}

func (f *fakeReader) Query(ctx context.Context, datasourceID string, dsl string, opts executor.QueryOptions) (*executor.QueryResult, error) {
	f.gotDatasource = datasourceID
	f.gotDSL = dsl
	if opts.Extras != nil {
		if v, ok := opts.Extras["index"].(string); ok {
			f.gotIndex = v
		}
	}
	return f.result, nil
}

func TestESLogQuery_ByTraceID(t *testing.T) {
	fr := &fakeReader{result: &executor.QueryResult{
		Columns: []string{"@timestamp", "level", "service", "message"},
		Rows: [][]any{
			{"2026-07-07T10:00:00Z", "ERROR", "service-a", "NPE at Foo.bar"},
		},
	}}
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterESLogTool(reg, fr, ESLogConfig{
		DatasourceID: "es-logs", DefaultIndex: "app-logs-*", TraceIDField: "trace_id",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, ok := reg.Get("es_log_query")
	if !ok {
		t.Fatal("es_log_query not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"trace_id": "abc"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	hits := m["hits"].([]map[string]any)
	if len(hits) != 1 || hits[0]["service"] != "service-a" {
		t.Fatalf("hits mapping wrong: %v", hits)
	}
	if fr.gotDatasource != "es-logs" || fr.gotIndex != "app-logs-*" {
		t.Fatalf("datasource/index wrong: %q %q", fr.gotDatasource, fr.gotIndex)
	}
	// DSL 必须是合法 JSON 且包含对 trace_id 的匹配
	var dsl map[string]any
	if err := json.Unmarshal([]byte(fr.gotDSL), &dsl); err != nil {
		t.Fatalf("DSL not valid JSON: %v (%s)", err, fr.gotDSL)
	}
	if !strings.Contains(fr.gotDSL, "trace_id") || !strings.Contains(fr.gotDSL, "abc") {
		t.Fatalf("DSL missing trace_id match: %s", fr.gotDSL)
	}
}

func TestESLogQuery_RequiresParam(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	_ = RegisterESLogTool(reg, &fakeReader{}, ESLogConfig{DatasourceID: "es", DefaultIndex: "i", TraceIDField: "trace_id"})
	tl, _ := reg.Get("es_log_query")
	out, _ := tl.Execute(context.Background(), map[string]any{})
	if _, has := out.(map[string]any)["error"]; !has {
		t.Fatal("expected error when neither trace_id nor query provided")
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestESLogQuery' -v`
Expected: FAIL,`undefined: RegisterESLogTool` / `undefined: ESLogConfig`

- [ ] **Step 3: 写实现**

创建 `framework/tool/es_log_tool.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sixath/framework/executor"
)

// ESLogConfig 为 es_log_query 的静态配置。
type ESLogConfig struct {
	DatasourceID string // 指向已注册的 ES datasource
	DefaultIndex string // 默认业务日志索引
	TraceIDField string // 日志中关联 trace 的字段名(如 trace_id)
}

const esLogDefaultLimit = 50

// RegisterESLogTool 注册 es_log_query 工具,复用只读 executor.Reader。
func RegisterESLogTool(reg *Registry, reader executor.Reader, cfg ESLogConfig) error {
	if reg == nil {
		return errors.New("es log tool: registry is nil")
	}
	if reader == nil {
		return errors.New("es log tool: reader is nil")
	}
	if cfg.DatasourceID == "" {
		return errors.New("es log tool: datasource id is empty")
	}
	if cfg.TraceIDField == "" {
		cfg.TraceIDField = "trace_id"
	}
	return reg.Register(Tool{
		Name:        "es_log_query",
		Description: "Query ELK application logs by trace_id (preferred) or keyword within a time window. Returns matching log lines. Read-only.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id": map[string]any{"type": "string", "description": "Correlate logs by trace id (matched on the configured trace id field)."},
				"query":    map[string]any{"type": "string", "description": "Keyword/full-text query when trace_id is not used."},
				"index":    map[string]any{"type": "string", "description": "Override the default log index/pattern."},
				"limit":    map[string]any{"type": "integer", "description": "Max hits (default 50)."},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			traceID, _ := params["trace_id"].(string)
			query, _ := params["query"].(string)
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(query) == "" {
				return map[string]any{"error": "either trace_id or query is required"}, nil
			}
			limit := intFromParam(params["limit"], esLogDefaultLimit)
			if limit <= 0 {
				limit = esLogDefaultLimit
			}
			index := cfg.DefaultIndex
			if v, _ := params["index"].(string); strings.TrimSpace(v) != "" {
				index = v
			}

			// 构造只读 Search DSL。
			var inner map[string]any
			if strings.TrimSpace(traceID) != "" {
				inner = map[string]any{"term": map[string]any{cfg.TraceIDField: traceID}}
			} else {
				inner = map[string]any{"query_string": map[string]any{"query": query}}
			}
			dslObj := map[string]any{"size": limit, "query": inner}
			dslBytes, err := json.Marshal(dslObj)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}

			res, err := reader.Query(ctx, cfg.DatasourceID, string(dslBytes), executor.QueryOptions{
				MaxRows: limit,
				Extras:  map[string]any{"index": index},
			})
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return map[string]any{
				"hits":  rowsToHits(res),
				"total": totalFromResult(res),
			}, nil
		},
	})
}

// rowsToHits 把列式 QueryResult 转成 [{col:val}] 便于模型阅读。
func rowsToHits(res *executor.QueryResult) []map[string]any {
	hits := []map[string]any{}
	if res == nil {
		return hits
	}
	for _, row := range res.Rows {
		h := make(map[string]any, len(res.Columns))
		for i, col := range res.Columns {
			if i < len(row) {
				h[col] = row[i]
			}
		}
		hits = append(hits, h)
	}
	return hits
}

func totalFromResult(res *executor.QueryResult) int {
	if res == nil {
		return 0
	}
	if res.EstimatedTotal > 0 {
		return int(res.EstimatedTotal)
	}
	return len(res.Rows)
}

var _ = fmt.Sprintf // keep fmt import if unused after edits
```

> 说明:`executor.Reader`、`executor.QueryOptions{Extras}`、`executor.QueryResult{Columns,Rows,EstimatedTotal}` 均为 `executor` 包现有类型。装配时 reader 用 `executor.NewBundle(dsReg).Reader`,dsReg 需已 `datasource.RegisterElasticsearch` 且注册目标 ES 数据源(参照 `templates/dataquery.go`)。若最终 `fmt` 未被使用,删掉最后一行与 import 中的 `fmt`。

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestESLogQuery' -v`
Expected: PASS(两个用例)

- [ ] **Step 5: Commit**

```bash
git add framework/tool/es_log_tool.go framework/tool/es_log_tool_test.go
git commit -m "feat(tool): add es_log_query tool over executor.Reader"
```

---

## Task 7: toolset 归类

**Files:**
- Modify: `framework/tool/toolset.go`(`builtinDefaultToolset` map)

- [ ] **Step 1: 写失败测试**

在 `framework/tool/rca_code_tools_test.go` 追加:

```go
func TestRCAToolsetDefaults(t *testing.T) {
	for _, name := range []string{"rca_grep", "rca_glob", "rca_read", "jaeger_trace", "es_log_query"} {
		if got := builtinDefaultToolset[name]; got != ToolsetRCA {
			t.Fatalf("toolset[%s] = %q, want %q", name, got, ToolsetRCA)
		}
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `cd framework && go test ./tool/ -run 'TestRCAToolsetDefaults' -v`
Expected: FAIL(map 中无这些键,返回空串)

- [ ] **Step 3: 写实现**

在 `framework/tool/toolset.go` 的 `builtinDefaultToolset` map 里,`"cronjob": ToolsetCronjob,` 之后加入:

```go
	"rca_grep":     ToolsetRCA,
	"rca_glob":     ToolsetRCA,
	"rca_read":     ToolsetRCA,
	"jaeger_trace": ToolsetRCA,
	"es_log_query": ToolsetRCA,
```

> `ToolsetRCA` 常量已在 Task 2 的 `rca_code_tools.go` 定义。

- [ ] **Step 4: 运行测试,确认通过**

Run: `cd framework && go test ./tool/ -run 'TestRCAToolsetDefaults' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add framework/tool/toolset.go framework/tool/rca_code_tools_test.go
git commit -m "feat(tool): classify RCA tools under rca toolset"
```

---

## Task 8: 全量回归

- [ ] **Step 1: tool 包全量测试**

Run: `cd framework && go test ./tool/... -v`
Expected: 全部 PASS(含既有测试无回归)

- [ ] **Step 2: 全仓库构建 + 测试**

Run: `cd framework && go build ./... && go test ./...`
Expected: 构建成功;测试全部 PASS

- [ ] **Step 3: 最终 commit(如有未提交项)**

```bash
git add -A
git commit -m "test: full regression for RCA toolchain"
```

---

## 装配提示(超出本计划范围,交接给接线方)

本计划交付 5 个工具 + 注册函数 + 测试,但**未接入任何 Handler**。接线方需在装配处(参照 `templates/dataquery.go` / `templates/skills_handler.go`)按需调用:

```go
_ = RegisterRCACodeTools(reg, cfg.RCA.Repos.Roots)
_ = RegisterJaegerTool(reg, cfg.RCA.Jaeger.QueryURL)

dsReg := datasource.NewRegistry()
datasource.RegisterElasticsearch(dsReg)
// ...注册 cfg.RCA.ES.DatasourceID 对应的 ES 数据源...
bundle := executor.NewBundle(dsReg)
_ = RegisterESLogTool(reg, bundle.Reader, ESLogConfig{
	DatasourceID: cfg.RCA.ES.DatasourceID,
	DefaultIndex: cfg.RCA.ES.DefaultIndex,
	TraceIDField: cfg.RCA.ES.TraceIDField,
})
```

并在 `config.Config` 增加 `RCA` 配置节(`jaeger.query_url`、`es.datasource_id/default_index/trace_id_field`、`repos.roots`)。这部分建议作为**后续接线计划**单独进行。
