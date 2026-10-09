# 仓库注册与分组绑定（P1 后端）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 portal 中建立仓库注册表与 agent→仓库/仓库组绑定，运行时按绑定生成带唯一逻辑名的 RCA 根，并修复 RCA 工具 basename 重名读错仓库的问题。

**Architecture:** framework 的 rca_* 工具改为接收 `[]RCARoot{Name, Path}`，旧的 `[]string` 入口保留并自动消歧。portal 新增 `repositories / repo_groups / repo_group_members / agent_repo_bindings / agent_effective_repos` 五张表；`biz.RepoRegistryUsecase` 负责扫描、目录组维护、绑定校验与有效仓库集展开；service 层在构建工具注册表时查询有效仓库集，通过 `RegistryBuildOptions.RCARoots` 传给 `chat`，为 nil（agent 无绑定）时完全沿用旧的 `workspace/code` 逻辑。

**Tech Stack:** Go 1.26、kratos、GORM（MySQL；测试用 sqlite 内存库，需要 CGO）、wire（`wire_gen.go` 手工维护）。

**Spec:** `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md`（§4、§5、§6.1、§6.1.1、§6.3、§11、§14、§15 P1 行）

---

## 实施偏差

实施与评审中对下文任务做了以下调整。下文代码块保留原稿，**以代码和本节为准**：

- **绑定权威**：`RCARootsForAgent` 仅在 agent 没有任何绑定时返回 `nil`；有绑定但有效集为空时返回非 nil 空切片，调用方视为权威（不注册 RCA 代码工具，不回退 `workspace/code`）。查询出错仍 fail-open。
- **使用时校验**：`RCARootsForAgent` 丢弃 code root 已不在配置中、路径已不存在、或解析软链接后逃出 code root（每次重新解析 code root）的条目；读取绑定与有效集时持有 agent 级锁。
- **workspace-link 不再自动绑定**：Task 11 的 `BindFromLegacyLink` 已删除。`POST /agents/{id}/workspace-link` 只建软链接；agent 已有绑定时响应带 `repo_bindings_override: true` 与 `warning`。
- **迁移**：`MigrateLegacyLinks(ctx, apply, allow)` 增加 `allow` 过滤（HTTP 层只处理调用者可编辑的 agent）；分页用 100（`agentRepo.List` 超过 100 会被截成 10）；软链接解析出的真实路径映射回配置的 code root；`bind_group` 仅在目标下仓库全是该组直接成员时自动写入，否则降级为 `manual_multi` 并给出 `reason`；报告项新增 `reason`、`after_roots`。
- **扫描**：code root 统一为绝对路径（Windows 下比较不区分大小写）；读取 git 信息失败时保留旧的 branch/commit/remote；用 `MarkRepositoryMissingIfActive` 标记 missing，不覆盖 `archived`；`refreshScannedRepository` 用 SQL `CASE` 保留 `archived`；已从配置移除的 code root 下的仓库在扫描末尾标记 missing（未配置任何 code root 时跳过）。
- **一致性**：绑定写入与有效集重算按 agent 加进程内锁；有效集未变化时不重写；`PatchRepo` 状态未变时不重算。
- **校验与错误**：`ReplaceBindings` / `CopyBindings` 校验 agent 存在（源和目标），不存在返回 `ErrAgentNotFound`；新增 `ErrInvalidRepo`（`PatchRepo` 非法状态，400）与 `ErrRepoGroupInUse`（删除仍被绑定的组，409 `REPO_GROUP_IN_USE`）；`DeleteGroup` 不再连带删除绑定。
- **agent 删除级联**：`agentRepo.Delete` 在同一事务里删除该 agent 的 `agent_repo_bindings` 与 `agent_effective_repos`。
- **其他**：`agentRepo.List` / `ListByIDs` 排序加 `id DESC` 作为第二键；API 列表接口不分页（设计 §11 已同步）。

---

## 范围说明

本计划只覆盖 P1 的 framework + portal 后端。以下内容**不在本计划内**：

- **Web UI**（仓库管理页、Agent 编辑页"代码仓库"区块）：单独写 P1b 计划，依赖本计划的 API。
- **ACL 资源类型 `repo` / `repo_group`**：本期仓库与组接口只要求登录（`runWithMiddleware`）；agent 绑定接口复用 `agentUC.GetForEdit` 的 agent 编辑权限。
- **tag 组、`repo_selector` 规则绑定、`pending_confirm` 成员确认**：P4。表结构已留字段，本期只写 `dir` / `manual` 组和 `active` 成员。
- **handbook 相关字段**：建表但不读写。
- **扫描间隔配置项**：本期常量 10 分钟（`cron.DefaultRepoScanInterval`）。

与设计文档的一处偏差：`repositories.code_root` 用 `VARCHAR(255)`（设计写 512），否则 `UNIQUE(code_root, rel_path)` 在 utf8mb4 下超过 InnoDB 3072 字节索引上限。

## 运行时行为约定

> 以下为实施后的最终约定（原稿以"有效仓库集是否为空"为分界，已按下方"实施偏差"修正）。

- agent **有绑定** → RCA 根 = 有效仓库（`Name = rel_path`，带 `sub_paths` 时 `Name = rel_path/sub`）；忽略 `workspace/code` 和工具配置里的 `roots`。有效仓库集为空时也以此为准：不注册 `rca_code` / `rca_symbol`，不回退旧逻辑。
- agent **没有任何绑定**（未迁移的老 agent）→ `RCARootsForAgent` 返回 `nil`，行为与现在完全一致。
- 查询出错 → 记 warn，按"没有绑定"处理（fail-open 到旧逻辑）。
- 迁移接口 `apply=true` 只自动写入"精确命中一个仓库"或"精确命中一个目录组且目标下仓库全是直接成员"的 agent；其余只出报告，保持旧逻辑直到人工处理（设计 §14）。

## 文件结构

**framework**

| 文件 | 动作 | 职责 |
|------|------|------|
| `framework/tool/rca_repos.go` | 改 | `RCARoot`、`NamedRCARoots`、`validateRCARoots`；`selectRoots` / `repoNames` / `resolveInRepos` / `repoCheck` 改为 `[]RCARoot` |
| `framework/tool/rca_repos_test.go` | 新建 | 命名与校验单测 |
| `framework/tool/rca_code_tools.go` | 改 | `RegisterRCACodeToolsNamed`；内部用 `root.Name` / `root.Path` |
| `framework/tool/rca_symbol_tool.go` | 改 | `RegisterRCASymbolToolNamed`；内部签名改 `[]RCARoot` |
| `framework/tool/rca_symbol_resolve.go` | 改 | 签名改 `[]RCARoot` |
| `framework/tool/rca_code_tools_test.go` | 改 | 适配 + 重名回归测试 |
| `framework/tool/rca_symbol_tool_test.go` | 改 | 第 625 行适配 |

**portal**

| 文件 | 动作 | 职责 |
|------|------|------|
| `portal/migrations/019_repo_registry.sql` | 新建 | 五张表 |
| `portal/internal/data/model/repo_registry.go` | 新建 | GORM 模型 + JSON 列类型 |
| `portal/internal/data/data.go` | 改 | AutoMigrate 列表、ProviderSet |
| `portal/internal/biz/repo_registry.go` | 新建 | 领域类型、常量、错误、`RepoRegistryRepo` 接口 |
| `portal/internal/biz/repo_scan.go` | 新建 | 纯文件系统：发现 git 根、读 HEAD/remote |
| `portal/internal/biz/repo_binding.go` | 新建 | 纯函数：绑定校验、有效仓库集展开、RCA 根生成、旧链接映射 |
| `portal/internal/biz/repo_registry_usecase.go` | 新建 | 用例：扫描、重算、绑定读写、迁移 |
| `portal/internal/biz/biz.go` | 改 | ProviderSet |
| `portal/internal/data/repo_registry.go` | 新建 | `RepoRegistryRepo` 实现 |
| `portal/internal/data/repo_registry_test.go` | 新建 | repo + usecase 集成测试（sqlite） |
| `portal/internal/chat/agent_builder.go` | 改 | `RegistryBuildOptions.RCARoots` |
| `portal/internal/chat/rca_builder.go` | 改 | 有 `RCARoots` 时走 Named 注册 |
| `portal/internal/service/rca_roots.go` | 新建 | `RCARootResolver` 接口与解析辅助函数 |
| `portal/internal/service/chat.go`、`agent.go` | 改 | resolver setter；三处 `BuildRegistry` 传 `RCARoots` |
| `portal/internal/server/repo_registry.go` | 新建 | HTTP handlers |
| `portal/internal/server/code_roots.go` | 改 | workspace-link POST 同步写绑定 |
| `portal/internal/server/http.go` | 改 | 路由、`NewHTTPServer` 参数 |
| `portal/internal/cron/scheduler.go` | 改 | `repoScanLoop` |
| `portal/cmd/backend/wire_gen.go` | 改（手工） | 构造与注入 |

> `wire_gen.go` 已含 `scheduler.SetEvolutionUsecase(...)` 这类手工 setter 调用，**不要用 `wire` 重新生成**，按 Task 12 手工编辑。

## 命令约定

- framework 测试：`go -C framework test ./tool/ -run <Name> -count=1`
- portal 测试：`go -C portal test ./internal/<pkg>/ -run <Name> -count=1`
- sqlite 测试需要 CGO：PowerShell 先执行 `$env:CGO_ENABLED="1"`（需本机有 gcc）；若本机无 gcc，data 包测试在 Linux/CI 上跑，其余包不受影响。

---

## Task 1: framework — `RCARoot` 与 `NamedRCARoots`

**Files:**
- Modify: `framework/tool/rca_repos.go`
- Create: `framework/tool/rca_repos_test.go`

- [ ] **Step 1: 写失败测试**

`framework/tool/rca_repos_test.go`:

```go
package tool

import (
	"path/filepath"
	"testing"
)

func TestNamedRCARoots_BasenameWhenUnique(t *testing.T) {
	got := NamedRCARoots([]string{"/codes/a/svc-a", "/codes/b/svc-b"})
	want := []RCARoot{
		{Name: "svc-a", Path: filepath.Clean("/codes/a/svc-a")},
		{Name: "svc-b", Path: filepath.Clean("/codes/b/svc-b")},
	}
	assertRCARoots(t, got, want)
}

func TestNamedRCARoots_QualifiesCollidingBasenames(t *testing.T) {
	got := NamedRCARoots([]string{"/codes/cloudgame/gateway", "/codes/migu/gateway", "/codes/x/solo"})
	want := []RCARoot{
		{Name: "cloudgame/gateway", Path: filepath.Clean("/codes/cloudgame/gateway")},
		{Name: "migu/gateway", Path: filepath.Clean("/codes/migu/gateway")},
		{Name: "solo", Path: filepath.Clean("/codes/x/solo")},
	}
	assertRCARoots(t, got, want)
}

func TestNamedRCARoots_QualifiesUntilUnique(t *testing.T) {
	got := NamedRCARoots([]string{"/p/team/gw", "/q/team/gw"})
	if got[0].Name != "p/team/gw" || got[1].Name != "q/team/gw" {
		t.Fatalf("names = %q, %q", got[0].Name, got[1].Name)
	}
}

func TestNamedRCARoots_DropsEmptyAndDuplicatePaths(t *testing.T) {
	got := NamedRCARoots([]string{"", "  ", "/codes/a", "/codes/a/", "/codes/a"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %#v", got)
	}
}

func TestValidateRCARoots(t *testing.T) {
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: "/x/a"}, {Name: "b", Path: "/x/b"}}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: "/x/a"}, {Name: "a", Path: "/y/a"}}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	if err := validateRCARoots([]RCARoot{{Name: "", Path: "/x/a"}}); err == nil {
		t.Fatal("expected empty name error")
	}
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: " "}}); err == nil {
		t.Fatal("expected empty path error")
	}
}

func assertRCARoots(t *testing.T, got, want []RCARoot) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go -C framework test ./tool/ -run "TestNamedRCARoots|TestValidateRCARoots" -count=1`
Expected: 编译失败，`undefined: NamedRCARoots` / `RCARoot` / `validateRCARoots`。

- [ ] **Step 3: 实现**

在 `framework/tool/rca_repos.go` 中：import 增加 `"log/slog"`、`"sort"`、`"strings"`；删除 `repoNameFromRoot`；在 import 块之后加入：

```go
// RCARoot 是带逻辑名的仓库根。Name 在同一组根内唯一，是 rca_* 工具 repo 参数的取值。
type RCARoot struct {
	Name string
	Path string
}

// NamedRCARoots 为路径列表生成逻辑名：默认取 basename；basename 冲突时逐级加上父目录
// （如 cloudgame/gateway 与 migu/gateway），直到唯一。空路径与重复路径被丢弃。
func NamedRCARoots(paths []string) []RCARoot {
	out := make([]RCARoot, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, RCARoot{Name: rcaPathSuffix(p, 1), Path: p})
	}
	for depth := 2; ; depth++ {
		dups := duplicateRCANames(out)
		if len(dups) == 0 {
			return out
		}
		changed := false
		for i := range out {
			if _, ok := dups[out[i].Name]; !ok {
				continue
			}
			if n := rcaPathSuffix(out[i].Path, depth); n != out[i].Name {
				out[i].Name = n
				changed = true
			}
		}
		if !changed {
			slog.Warn("rca: repo names still collide after qualification", "names", sortedNameSet(dups))
			return out
		}
		slog.Warn("rca: repo basenames collide; using parent-qualified names", "names", sortedNameSet(dups))
	}
}

func rcaPathSuffix(p string, n int) string {
	parts := strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return p
	}
	if n > len(parts) {
		n = len(parts)
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

func duplicateRCANames(roots []RCARoot) map[string]struct{} {
	count := make(map[string]int, len(roots))
	for _, r := range roots {
		count[r.Name]++
	}
	dups := map[string]struct{}{}
	for name, c := range count {
		if c > 1 {
			dups[name] = struct{}{}
		}
	}
	return dups
}

func sortedNameSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validateRCARoots 要求每个根都有非空 Name/Path，且 Name 唯一。
func validateRCARoots(roots []RCARoot) error {
	seen := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Path) == "" {
			return fmt.Errorf("rca: root name and path are required (name=%q path=%q)", r.Name, r.Path)
		}
		if _, ok := seen[r.Name]; ok {
			return fmt.Errorf("rca: duplicate repo name %q", r.Name)
		}
		seen[r.Name] = struct{}{}
	}
	return nil
}
```

此时其他文件仍引用 `repoNameFromRoot`，包无法编译——Task 2 一并修复，**本 Task 不单独提交**。

- [ ] **Step 4: 进入 Task 2**（Task 1、2 合并为一次提交）

---

## Task 2: framework — rca_* 工具改用 `[]RCARoot`

**Files:**
- Modify: `framework/tool/rca_repos.go`、`rca_code_tools.go`、`rca_symbol_tool.go`、`rca_symbol_resolve.go`
- Modify: `framework/tool/rca_code_tools_test.go`、`rca_symbol_tool_test.go`

- [ ] **Step 1: 写重名回归测试**

在 `framework/tool/rca_code_tools_test.go`：删除 `TestRepoNameFromRoot`；`TestResolveInRepos_HappyAndTraversal` 中把 `roots := []string{repoA, repoB}` 改为 `roots := NamedRCARoots([]string{repoA, repoB})`（其余断言不变，`root` 返回值仍是路径字符串）。末尾追加：

```go
func TestRCARead_BasenameCollisionReadsCorrectRepo(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "cloudgame", "gateway")
	b := filepath.Join(base, "migu", "gateway")
	writeFile(t, filepath.Join(a, "main.go"), "package a\n// from cloudgame\n")
	writeFile(t, filepath.Join(b, "main.go"), "package b\n// from migu\n")
	reg := newRCARegistry(t, []string{a, b})

	tl, ok := reg.Get("rca_read")
	if !ok {
		t.Fatal("rca_read not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"repo": "migu/gateway", "file": "main.go"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	content, _ := out.(map[string]any)["content"].(string)
	if !strings.Contains(content, "from migu") {
		t.Fatalf("read wrong repo, content = %q", content)
	}

	out, _ = tl.Execute(context.Background(), map[string]any{"repo": "gateway", "file": "main.go"})
	if _, hasErr := out.(map[string]any)["error"]; !hasErr {
		t.Fatalf("ambiguous basename must be rejected, got %#v", out)
	}
}

func TestRCAGrep_CollisionUsesQualifiedRepoNames(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "cloudgame", "gateway")
	b := filepath.Join(base, "migu", "gateway")
	writeFile(t, filepath.Join(a, "x.go"), "package a\n// Marker\n")
	writeFile(t, filepath.Join(b, "x.go"), "package b\n// Marker\n")
	reg := newRCARegistry(t, []string{a, b})

	tl, _ := reg.Get("rca_grep")
	out, err := tl.Execute(context.Background(), map[string]any{"pattern": "Marker"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	repos := map[string]bool{}
	for _, m := range out.(map[string]any)["matches"].([]map[string]any) {
		repos[m["repo"].(string)] = true
	}
	if !repos["cloudgame/gateway"] || !repos["migu/gateway"] {
		t.Fatalf("repos = %v", repos)
	}
}

func TestRegisterRCACodeToolsNamed_UsesGivenNames(t *testing.T) {
	root := filepath.Join(t.TempDir(), "svc-a")
	writeFile(t, filepath.Join(root, "a.go"), "package a\n// Needle\n")
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	if err := RegisterRCACodeToolsNamed(reg, []RCARoot{{Name: "cloudgame/svc-a", Path: root}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	tl, _ := reg.Get("rca_grep")
	out, _ := tl.Execute(context.Background(), map[string]any{"pattern": "Needle", "repo": "cloudgame/svc-a"})
	matches := out.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["repo"] != "cloudgame/svc-a" {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestRegisterRCACodeToolsNamed_RejectsDuplicateNames(t *testing.T) {
	reg := &Registry{tools: map[string]Tool{}, mcpServerIDs: map[string]struct{}{}}
	err := RegisterRCACodeToolsNamed(reg, []RCARoot{{Name: "a", Path: "/x/a"}, {Name: "a", Path: "/y/a"}})
	if err == nil {
		t.Fatal("expected duplicate name error")
	}
}
```

在 `framework/tool/rca_symbol_tool_test.go` 第 625 行把 `resolveSymbolCandidates([]string{repo}, ...)` 改为 `resolveSymbolCandidates(NamedRCARoots([]string{repo}), ...)`。

- [ ] **Step 2: 改 `rca_repos.go` 的四个辅助函数**

替换 `selectRoots`、`repoNames`、`resolveInRepos`、`repoCheck`：

```go
// selectRoots 根据可选 repo 名从 roots 中筛选目标根。
// repo 为空返回全部;否则返回逻辑名匹配的单个根。roots 为空或 repo 未命中时报错。
func selectRoots(roots []RCARoot, repo string) ([]RCARoot, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("rca: no repository roots configured (rca.repos.roots is empty)")
	}
	if repo == "" {
		return roots, nil
	}
	for _, r := range roots {
		if r.Name == repo {
			return []RCARoot{r}, nil
		}
	}
	return nil, fmt.Errorf("rca: unknown repo %q; configured repos are %v", repo, repoNames(roots))
}

// repoNames 返回全部仓库逻辑名,用于错误提示。
func repoNames(roots []RCARoot) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, r.Name)
	}
	return out
}

// resolveInRepos 解析 repo 内相对路径 rel 为绝对路径,并用 ResolveWorkspacePath 拒绝越权。
// repo 必填(用于读单个文件的场景);返回 (绝对路径, 命中的仓库根路径, error)。
func resolveInRepos(roots []RCARoot, repo, rel string) (string, string, error) {
	if repo == "" {
		return "", "", fmt.Errorf("rca: repo is required to resolve a specific path")
	}
	sel, err := selectRoots(roots, repo)
	if err != nil {
		return "", "", err
	}
	root := sel[0].Path
	full, err := fwws.ResolveWorkspacePath(root, rel)
	if err != nil {
		return "", "", err
	}
	return full, root, nil
}

// repoCheck 是 rca_* 工具 repo 参数的候选规则。
func repoCheck(roots []RCARoot) OneOf {
	return OneOf{
		Param: "repo",
		Source: func(context.Context, map[string]any) ([]string, error) {
			return repoNames(roots), nil
		},
		Hint: "repo must be one of the configured repositories (the repo field returned by rca_grep / rca_glob)",
	}
}
```

- [ ] **Step 3: 改 `rca_code_tools.go`**

常量：

```go
	rcaRequiredRepoDesc = "Repository name (one of the configured repos, as returned in the repo field of rca_grep / rca_glob)."
```

入口：

```go
// RegisterRCACodeTools 注册 rca_grep / rca_glob / rca_read 三个多仓库代码检索工具。
// roots 为允许检索的仓库根白名单；逻辑名取 basename，重名时自动加父目录消歧。
func RegisterRCACodeTools(reg *Registry, roots []string) error {
	return RegisterRCACodeToolsNamed(reg, NamedRCARoots(roots))
}

// RegisterRCACodeToolsNamed 与 RegisterRCACodeTools 相同，但由调用方指定每个根的逻辑名（必须唯一）。
func RegisterRCACodeToolsNamed(reg *Registry, roots []RCARoot) error {
	if reg == nil {
		return errors.New("rca code tools: registry is nil")
	}
	if err := validateRCARoots(roots); err != nil {
		return err
	}
	if err := registerRCAGrepTool(reg, roots); err != nil {
		return err
	}
	if err := registerRCAGlobTool(reg, roots); err != nil {
		return err
	}
	return registerRCAReadTool(reg, roots)
}
```

三个 `register*Tool` 的参数改为 `roots []RCARoot`。函数体内（行号为改前）：

| 行 | 改前 | 改后 |
|----|------|------|
| 82 | `searchRCAFileContents(root, ...)` | `searchRCAFileContents(root.Path, ...)` |
| 86 | `attachRCAGrepContext(root, res, contextLines)` | `attachRCAGrepContext(root.Path, res, contextLines)` |
| 87 | `name := repoNameFromRoot(root)` | `name := root.Name` |
| 103 | `topRepo = repoNameFromRoot(sel[0])` | `topRepo = sel[0].Name` |
| 166 | `os.Stat(root)` | `os.Stat(root.Path)` |
| 167 | `repoNameFromRoot(root)` | `root.Name` |
| 174 | `searchFilesByGlob(root, root, ...)` | `searchFilesByGlob(root.Path, root.Path, ...)` |
| 178 | `name := repoNameFromRoot(root)` | `name := root.Name` |

`rca_read` 中 `resolveInRepos` 返回的 `root` 仍是路径字符串，第 238、247 行不用改。

- [ ] **Step 4: 改 `rca_symbol_tool.go` 与 `rca_symbol_resolve.go`**

`rca_symbol_tool.go` 入口：

```go
// RegisterRCASymbolTool registers source-symbol navigation through gopls.
// roots may be empty: the tool remains registered, while calls fail permanently.
func RegisterRCASymbolTool(reg *Registry, roots []string, opts RCASymbolOpts) error {
	return RegisterRCASymbolToolNamed(reg, NamedRCARoots(roots), opts)
}

// RegisterRCASymbolToolNamed is RegisterRCASymbolTool with caller-chosen unique repo names.
func RegisterRCASymbolToolNamed(reg *Registry, roots []RCARoot, opts RCASymbolOpts) error {
	if reg == nil {
		return errors.New("rca symbol tool: registry is nil")
	}
	if err := validateRCARoots(roots); err != nil {
		return err
	}
	// ……原函数体从 `command := strings.TrimSpace(opts.GoplsPath)` 起原样保留……
}
```

以下函数参数 `roots []string` → `roots []RCARoot`，函数体不变：`executeRCASymbol`、`filterRCASymbolLocations`、`rcaSymbolReferencesOK`、`rcaSymbolGrepFallback`、`grepSymbolCallers`、`appendCrossRepoGrepCallers`（`len(roots)` 照常可用）。

`grepSymbolCallersOtherRoots` 改为：

```go
func grepSymbolCallersOtherRoots(roots []RCARoot, skipRepo, name string, limit int) ([]lsp.Location, error) {
	var out []lsp.Location
	remaining := limit
	for _, root := range roots {
		if root.Name == skipRepo {
			continue
		}
		if remaining <= 0 {
			break
		}
		locs, err := grepSymbolCallers(roots, root.Name, root.Path, name, remaining)
		if err != nil {
			continue
		}
		out = append(out, locs...)
		remaining = limit - len(out)
	}
	return out, nil
}
```

`rca_symbol_resolve.go`：`resolveSymbolCandidates(roots []RCARoot, ...)`、`resolveRCASymbolRoot(roots []RCARoot, ...)`，函数体不变。

- [ ] **Step 5: 编译 + 运行全部 tool 测试**

Run: `go -C framework vet ./tool/ ; go -C framework test ./tool/ -count=1`
Expected: PASS。如有遗漏的 `repoNameFromRoot` 引用，`vet` 会指出。

- [ ] **Step 6: 跑 framework 全量，确认 `templates/rca_wiring.go` 等单根调用方不受影响**

Run: `go -C framework test ./... -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add framework/tool
git commit -m "fix(rca): unique repo logical names; add Named register APIs"
```

---

## Task 3: portal — 建表与 GORM 模型

**Files:**
- Create: `portal/migrations/019_repo_registry.sql`
- Create: `portal/internal/data/model/repo_registry.go`
- Modify: `portal/internal/data/data.go`

- [ ] **Step 1: 迁移 SQL**

`portal/migrations/019_repo_registry.sql`:

```sql
-- Repository registry: repos discovered under code roots, groups, and agent bindings.
-- Design: docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md §4.

CREATE TABLE IF NOT EXISTS repositories (
    id               VARCHAR(36)   NOT NULL PRIMARY KEY,
    code_root        VARCHAR(255)  NOT NULL,
    rel_path         VARCHAR(512)  NOT NULL,
    name             VARCHAR(256)  NOT NULL DEFAULT '',
    description      TEXT          NULL,
    tags             JSON          NULL,
    git_remote       VARCHAR(512)  NOT NULL DEFAULT '',
    git_branch       VARCHAR(256)  NOT NULL DEFAULT '',
    head_commit      VARCHAR(64)   NOT NULL DEFAULT '',
    sync_mode        VARCHAR(16)   NOT NULL DEFAULT 'registry_only',
    status           VARCHAR(16)   NOT NULL DEFAULT 'active',
    handbook_status  VARCHAR(16)   NOT NULL DEFAULT 'none',
    handbook_commit  VARCHAR(64)   NOT NULL DEFAULT '',
    handbook_version INT           NOT NULL DEFAULT 0,
    handbook_stats   JSON          NULL,
    owner_id         VARCHAR(36)   NOT NULL DEFAULT '',
    last_scanned_at  DATETIME(3)   NULL,
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_repo_root_rel (code_root, rel_path),
    INDEX idx_repo_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS repo_groups (
    id               VARCHAR(36)   NOT NULL PRIMARY KEY,
    name             VARCHAR(256)  NOT NULL,
    kind             VARCHAR(16)   NOT NULL,
    rule             JSON          NULL,
    auto_apply_new   TINYINT(1)    NOT NULL DEFAULT 1,
    handbook_status  VARCHAR(16)   NOT NULL DEFAULT 'none',
    handbook_version INT           NOT NULL DEFAULT 0,
    owner_id         VARCHAR(36)   NOT NULL DEFAULT '',
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    INDEX idx_rg_kind (kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS repo_group_members (
    group_id    VARCHAR(36)  NOT NULL,
    repo_id     VARCHAR(36)  NOT NULL,
    source      VARCHAR(16)  NOT NULL,
    state       VARCHAR(16)  NOT NULL DEFAULT 'active',
    created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (group_id, repo_id),
    INDEX idx_rgm_repo (repo_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_repo_bindings (
    agent_id     VARCHAR(36)   NOT NULL,
    target_kind  VARCHAR(16)   NOT NULL,
    target_id    VARCHAR(256)  NOT NULL,
    mode         VARCHAR(16)   NOT NULL DEFAULT 'include',
    sub_paths    JSON          NULL,
    rule         JSON          NULL,
    priority     INT           NOT NULL DEFAULT 0,
    created_by   VARCHAR(128)  NOT NULL DEFAULT '',
    created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (agent_id, target_kind, target_id),
    INDEX idx_arb_target (target_kind, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_effective_repos (
    agent_id     VARCHAR(36)  NOT NULL,
    repo_id      VARCHAR(36)  NOT NULL,
    via          JSON         NULL,
    sub_paths    JSON         NULL,
    computed_at  DATETIME(3)  NOT NULL,
    PRIMARY KEY (agent_id, repo_id),
    INDEX idx_aer_repo (repo_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

- [ ] **Step 2: GORM 模型**

`portal/internal/data/model/repo_registry.go`:

```go
package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Repository is one git repository discovered under a code root.
type Repository struct {
	ID              string      `gorm:"column:id;primaryKey;size:36"`
	CodeRoot        string      `gorm:"column:code_root;size:255;not null;uniqueIndex:uk_repo_root_rel"`
	RelPath         string      `gorm:"column:rel_path;size:512;not null;uniqueIndex:uk_repo_root_rel"`
	Name            string      `gorm:"column:name;size:256;not null;default:''"`
	Description     string      `gorm:"column:description;type:text"`
	Tags            JSONStrings `gorm:"column:tags;type:json"`
	GitRemote       string      `gorm:"column:git_remote;size:512;not null;default:''"`
	GitBranch       string      `gorm:"column:git_branch;size:256;not null;default:''"`
	HeadCommit      string      `gorm:"column:head_commit;size:64;not null;default:''"`
	SyncMode        string      `gorm:"column:sync_mode;size:16;not null;default:registry_only"`
	Status          string      `gorm:"column:status;size:16;not null;default:active;index:idx_repo_status"`
	HandbookStatus  string      `gorm:"column:handbook_status;size:16;not null;default:none"`
	HandbookCommit  string      `gorm:"column:handbook_commit;size:64;not null;default:''"`
	HandbookVersion int         `gorm:"column:handbook_version;not null;default:0"`
	HandbookStats   JSONObject  `gorm:"column:handbook_stats;type:json"`
	OwnerID         string      `gorm:"column:owner_id;size:36;not null;default:''"`
	LastScannedAt   *time.Time  `gorm:"column:last_scanned_at"`
	CreatedAt       time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time   `gorm:"column:updated_at;not null"`
}

func (Repository) TableName() string { return "repositories" }

// RepoGroup is a bindable set of repositories (dir / tag / manual).
type RepoGroup struct {
	ID              string         `gorm:"column:id;primaryKey;size:36"`
	Name            string         `gorm:"column:name;size:256;not null"`
	Kind            string         `gorm:"column:kind;size:16;not null;index:idx_rg_kind"`
	Rule            *RepoGroupRule `gorm:"column:rule;type:json"`
	AutoApplyNew    bool           `gorm:"column:auto_apply_new;not null;default:true"`
	HandbookStatus  string         `gorm:"column:handbook_status;size:16;not null;default:none"`
	HandbookVersion int            `gorm:"column:handbook_version;not null;default:0"`
	OwnerID         string         `gorm:"column:owner_id;size:36;not null;default:''"`
	CreatedAt       time.Time      `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;not null"`
}

func (RepoGroup) TableName() string { return "repo_groups" }

// RepoGroupRule is the JSON rule of dir / tag groups.
type RepoGroupRule struct {
	CodeRoot  string   `json:"code_root,omitempty"`
	RelPrefix string   `json:"rel_prefix,omitempty"`
	AllOf     []string `json:"all_of,omitempty"`
	AnyOf     []string `json:"any_of,omitempty"`
}

func (r RepoGroupRule) Value() (driver.Value, error) { return json.Marshal(r) }
func (r *RepoGroupRule) Scan(v any) error          { return scanJSON(v, r) }

type RepoGroupMember struct {
	GroupID   string    `gorm:"column:group_id;primaryKey;size:36"`
	RepoID    string    `gorm:"column:repo_id;primaryKey;size:36;index:idx_rgm_repo"`
	Source    string    `gorm:"column:source;size:16;not null"`
	State     string    `gorm:"column:state;size:16;not null;default:active"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (RepoGroupMember) TableName() string { return "repo_group_members" }

type AgentRepoBinding struct {
	AgentID    string      `gorm:"column:agent_id;primaryKey;size:36"`
	TargetKind string      `gorm:"column:target_kind;primaryKey;size:16;index:idx_arb_target,priority:1"`
	TargetID   string      `gorm:"column:target_id;primaryKey;size:256;index:idx_arb_target,priority:2"`
	Mode       string      `gorm:"column:mode;size:16;not null;default:include"`
	SubPaths   JSONStrings `gorm:"column:sub_paths;type:json"`
	Rule       JSONObject  `gorm:"column:rule;type:json"`
	Priority   int         `gorm:"column:priority;not null;default:0"`
	CreatedBy  string      `gorm:"column:created_by;size:128;not null;default:''"`
	CreatedAt  time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time   `gorm:"column:updated_at;not null"`
}

func (AgentRepoBinding) TableName() string { return "agent_repo_bindings" }

type AgentEffectiveRepo struct {
	AgentID    string          `gorm:"column:agent_id;primaryKey;size:36"`
	RepoID     string          `gorm:"column:repo_id;primaryKey;size:36;index:idx_aer_repo"`
	Via        RepoBindingRefs `gorm:"column:via;type:json"`
	SubPaths   JSONStrings     `gorm:"column:sub_paths;type:json"`
	ComputedAt time.Time       `gorm:"column:computed_at;not null"`
}

func (AgentEffectiveRepo) TableName() string { return "agent_effective_repos" }

// RepoBindingRef records which binding made a repo effective.
type RepoBindingRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type RepoBindingRefs []RepoBindingRef

func (r RepoBindingRefs) Value() (driver.Value, error) { return jsonValue(r) }
func (r *RepoBindingRefs) Scan(v any) error           { return scanJSON(v, r) }

// JSONStrings is a JSON array of strings; nil is stored as NULL.
type JSONStrings []string

func (s JSONStrings) Value() (driver.Value, error) { return jsonValue(s) }
func (s *JSONStrings) Scan(v any) error           { return scanJSON(v, s) }

// JSONObject is a free-form JSON object; nil is stored as NULL.
type JSONObject map[string]any

func (o JSONObject) Value() (driver.Value, error) { return jsonValue(o) }
func (o *JSONObject) Scan(v any) error           { return scanJSON(v, o) }

func jsonValue[T any](v T) (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return string(b), nil
}

func scanJSON(value any, dst any) error {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return json.Unmarshal(v, dst)
	case string:
		if v == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), dst)
	default:
		return fmt.Errorf("scan json: unsupported type %T", value)
	}
}
```

- [ ] **Step 3: 加入 AutoMigrate**

`portal/internal/data/data.go` 的 AutoMigrate 列表，在 `&EvolutionProposal{},` 后追加：

```go
		&model.Repository{}, &model.RepoGroup{}, &model.RepoGroupMember{},
		&model.AgentRepoBinding{}, &model.AgentEffectiveRepo{},
```

- [ ] **Step 4: 编译**

Run: `go -C portal build ./...`
Expected: 成功。

- [ ] **Step 5: Commit**

```bash
git add portal/migrations/019_repo_registry.sql portal/internal/data/model/repo_registry.go portal/internal/data/data.go
git commit -m "feat(portal): repo registry tables and models"
```

---

## Task 4: biz — 领域类型与仓储接口

**Files:**
- Create: `portal/internal/biz/repo_registry.go`

- [ ] **Step 1: 写类型（无逻辑，不单独测试）**

```go
package biz

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

const (
	RepoStatusActive   = "active"
	RepoStatusMissing  = "missing"
	RepoStatusArchived = "archived"

	RepoSyncRegistryOnly = "registry_only"
	HandbookStatusNone   = "none"

	RepoGroupDir    = "dir"
	RepoGroupTag    = "tag"
	RepoGroupManual = "manual"

	RepoMemberSourceManual = "manual"
	RepoMemberSourceRule   = "rule"
	RepoMemberActive       = "active"

	RepoTargetRepo  = "repo"
	RepoTargetGroup = "repo_group"

	RepoBindingInclude = "include"
	RepoBindingExclude = "exclude"
)

var (
	ErrRepoNotFound       = errors.New("repo registry: not found")
	ErrInvalidRepoBinding = errors.New("repo registry: invalid binding")
	ErrInvalidRepoGroup   = errors.New("repo registry: invalid group")
	ErrRepoScanRunning    = errors.New("repo registry: scan already running")
)

type Repository struct {
	ID             string     `json:"id"`
	CodeRoot       string     `json:"code_root"`
	RelPath        string     `json:"rel_path"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Tags           []string   `json:"tags"`
	GitRemote      string     `json:"git_remote"`
	GitBranch      string     `json:"git_branch"`
	HeadCommit     string     `json:"head_commit"`
	SyncMode       string     `json:"sync_mode"`
	Status         string     `json:"status"`
	HandbookStatus string     `json:"handbook_status"`
	OwnerID        string     `json:"owner_id"`
	LastScannedAt  *time.Time `json:"last_scanned_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// AbsPath is the repository root on disk.
func (r *Repository) AbsPath() string {
	return filepath.Join(r.CodeRoot, filepath.FromSlash(r.RelPath))
}

type RepoGroupRule struct {
	CodeRoot  string   `json:"code_root,omitempty"`
	RelPrefix string   `json:"rel_prefix,omitempty"`
	AllOf     []string `json:"all_of,omitempty"`
	AnyOf     []string `json:"any_of,omitempty"`
}

type RepoGroup struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	Rule         *RepoGroupRule `json:"rule,omitempty"`
	AutoApplyNew bool           `json:"auto_apply_new"`
	OwnerID      string         `json:"owner_id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// DirPath is the absolute directory of a dir group; empty for other kinds.
func (g *RepoGroup) DirPath() string {
	if g.Kind != RepoGroupDir || g.Rule == nil {
		return ""
	}
	return filepath.Join(g.Rule.CodeRoot, filepath.FromSlash(g.Rule.RelPrefix))
}

type AgentRepoBinding struct {
	AgentID    string   `json:"agent_id"`
	TargetKind string   `json:"target_kind"`
	TargetID   string   `json:"target_id"`
	Mode       string   `json:"mode"`
	SubPaths   []string `json:"sub_paths,omitempty"`
	Priority   int      `json:"priority"`
	CreatedBy  string   `json:"created_by,omitempty"`
}

type BindingRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type AgentEffectiveRepo struct {
	AgentID    string       `json:"agent_id"`
	RepoID     string       `json:"repo_id"`
	Via        []BindingRef `json:"via"`
	SubPaths   []string     `json:"sub_paths,omitempty"`
	ComputedAt time.Time    `json:"computed_at"`
}

type RepoFilter struct {
	CodeRoot string
	Status   string
	Query    string
	GroupID  string
}

type RepoMetaPatch struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	OwnerID     *string   `json:"owner_id"`
	Status      *string   `json:"status"`
}

// RepoRegistryRepo persists repositories, groups and agent bindings.
type RepoRegistryRepo interface {
	// UpsertScannedRepository inserts by (code_root, rel_path) or refreshes git fields and
	// last_scanned_at; user-edited fields are kept, and archived repos stay archived.
	UpsertScannedRepository(ctx context.Context, r *Repository) (*Repository, error)
	ListRepositories(ctx context.Context, f RepoFilter) ([]*Repository, error)
	GetRepositoriesByIDs(ctx context.Context, ids []string) (map[string]*Repository, error)
	SetRepositoryStatus(ctx context.Context, id, status string) error
	UpdateRepositoryMeta(ctx context.Context, id string, p RepoMetaPatch) (*Repository, error)

	// UpsertDirGroup returns the dir group for (codeRoot, relPrefix), creating it with a stable id.
	UpsertDirGroup(ctx context.Context, codeRoot, relPrefix string) (*RepoGroup, error)
	CreateGroup(ctx context.Context, g *RepoGroup) (*RepoGroup, error)
	ListGroups(ctx context.Context, kind string) ([]*RepoGroup, error)
	GetGroupsByIDs(ctx context.Context, ids []string) (map[string]*RepoGroup, error)
	// DeleteGroup removes the group, its members and bindings that target it.
	DeleteGroup(ctx context.Context, id string) error
	ReplaceGroupMembers(ctx context.Context, groupID, source string, repoIDs []string) error
	ListActiveGroupMembers(ctx context.Context, groupIDs []string) (map[string][]string, error)

	ListAgentBindings(ctx context.Context, agentID string) ([]*AgentRepoBinding, error)
	ReplaceAgentBindings(ctx context.Context, agentID string, bs []*AgentRepoBinding) error
	ListAgentIDsWithBindings(ctx context.Context) ([]string, error)
	ListAgentIDsBoundToGroup(ctx context.Context, groupID string) ([]string, error)
	ReplaceEffectiveRepos(ctx context.Context, agentID string, rows []*AgentEffectiveRepo) error
	ListEffectiveRepos(ctx context.Context, agentID string) ([]*AgentEffectiveRepo, error)
	ListAgentIDsByEffectiveRepo(ctx context.Context, repoID string) ([]string, error)
}
```

- [ ] **Step 2: 编译**

Run: `go -C portal build ./internal/biz/`
Expected: 成功。

- [ ] **Step 3: Commit**

```bash
git add portal/internal/biz/repo_registry.go
git commit -m "feat(portal): repo registry domain types"
```

---

## Task 5: biz — 文件系统扫描（发现 git 根、读 HEAD）

**Files:**
- Create: `portal/internal/biz/repo_scan.go`
- Create: `portal/internal/biz/repo_scan_test.go`

- [ ] **Step 1: 写失败测试**

```go
package biz

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkGitRepo(t *testing.T, dir, head string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverGitRepos_StopsAtGitRootAndSkipsNoise(t *testing.T) {
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-a"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-b"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "solo"), "ref: refs/heads/main\n")
	// nested repo inside svc-a must not be reported
	mkGitRepo(t, filepath.Join(root, "cloudgame", "svc-a", "third_party", "x"), "ref: refs/heads/main\n")
	mkGitRepo(t, filepath.Join(root, "web", "node_modules", "pkg"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "notes", "readme.md"), "x")

	got, err := DiscoverGitRepos(root, 32)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cloudgame/svc-a", "cloudgame/svc-b", "solo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverGitRepos_RespectsDepth(t *testing.T) {
	root := t.TempDir()
	mkGitRepo(t, filepath.Join(root, "a", "b", "c"), "ref: refs/heads/main\n")
	got, _ := DiscoverGitRepos(root, 2)
	if len(got) != 0 {
		t.Fatalf("depth 2 should not reach a/b/c, got %v", got)
	}
	got, _ = DiscoverGitRepos(root, 3)
	if !reflect.DeepEqual(got, []string{"a/b/c"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiscoverGitRepos_GitFileCountsAsRepo(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "wt", ".git"), "gitdir: ../main/.git/worktrees/wt\n")
	got, _ := DiscoverGitRepos(root, 32)
	if !reflect.DeepEqual(got, []string{"wt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestReadGitInfo_LooseRef(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/feature/x\n")
	mustWrite(t, filepath.Join(repo, ".git", "refs", "heads", "feature", "x"), "abc123\n")
	mustWrite(t, filepath.Join(repo, ".git", "config"),
		"[core]\n\tbare = false\n[remote \"origin\"]\n\turl = https://bot:s3cret@git.example.com/g/r.git\n")
	info, err := ReadGitInfo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "feature/x" || info.Commit != "abc123" {
		t.Fatalf("info = %#v", info)
	}
	if info.Remote != "https://git.example.com/g/r.git" {
		t.Fatalf("remote must drop credentials, got %q", info.Remote)
	}
}

func TestReadGitInfo_PackedRef(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(repo, ".git", "packed-refs"),
		"# pack-refs with: peeled\ndef456 refs/heads/main\n^aaa\n")
	info, _ := ReadGitInfo(repo)
	if info.Commit != "def456" {
		t.Fatalf("commit = %q", info.Commit)
	}
}

func TestReadGitInfo_DetachedHead(t *testing.T) {
	repo := t.TempDir()
	mkGitRepo(t, repo, "0123abcd\n")
	info, _ := ReadGitInfo(repo)
	if info.Branch != "" || info.Commit != "0123abcd" {
		t.Fatalf("info = %#v", info)
	}
}

func TestReadGitInfo_WorktreeGitFile(t *testing.T) {
	base := t.TempDir()
	mainGit := filepath.Join(base, "main", ".git")
	wtGit := filepath.Join(mainGit, "worktrees", "wt")
	mustWrite(t, filepath.Join(wtGit, "HEAD"), "ref: refs/heads/wt-branch\n")
	mustWrite(t, filepath.Join(wtGit, "commondir"), "../..\n")
	mustWrite(t, filepath.Join(mainGit, "refs", "heads", "wt-branch"), "fff000\n")
	mustWrite(t, filepath.Join(base, "wt", ".git"), "gitdir: "+wtGit+"\n")

	info, err := ReadGitInfo(filepath.Join(base, "wt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != "wt-branch" || info.Commit != "fff000" {
		t.Fatalf("info = %#v", info)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go -C portal test ./internal/biz/ -run "TestDiscoverGitRepos|TestReadGitInfo" -count=1`
Expected: 编译失败，`undefined: DiscoverGitRepos` / `ReadGitInfo`。

- [ ] **Step 3: 实现**

`portal/internal/biz/repo_scan.go`:

```go
package biz

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepoScanMaxDepth matches chat.MaxCodeBrowseDepth.
const RepoScanMaxDepth = 32

// DiscoverGitRepos returns slash-separated paths (relative to root) of git repositories.
// A directory containing .git (dir or file) is a repository and is not descended into.
// Symlinked directories, node_modules and vendor are skipped; root itself is never reported.
func DiscoverGitRepos(root string, maxDepth int) ([]string, error) {
	root = filepath.Clean(root)
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("code root %q is not a directory", root)
	}
	var out []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root {
				return walkErr
			}
			return nil
		}
		if p == root || !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "vendor":
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			out = append(out, rel)
			return filepath.SkipDir
		}
		if strings.Count(rel, "/")+1 >= maxDepth {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// GitInfo is read directly from .git files (no git binary).
type GitInfo struct {
	Branch string
	Commit string
	Remote string
}

// ReadGitInfo reads branch, HEAD commit and origin URL (credentials stripped).
// Unborn branches yield an empty Commit without error.
func ReadGitInfo(repoDir string) (GitInfo, error) {
	var info GitInfo
	gitDir, err := resolveGitDir(repoDir)
	if err != nil {
		return info, err
	}
	commonDir := resolveCommonDir(gitDir)
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return info, err
	}
	s := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(s, "ref: "); ok {
		info.Branch = strings.TrimPrefix(ref, "refs/heads/")
		info.Commit, err = readGitRef(gitDir, commonDir, ref)
		if err != nil {
			return info, err
		}
	} else {
		info.Commit = s
	}
	info.Remote = readOriginURL(filepath.Join(commonDir, "config"))
	return info, nil
}

func resolveGitDir(repoDir string) (string, error) {
	p := filepath.Join(repoDir, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return p, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok {
		return "", fmt.Errorf("unrecognized .git file in %q", repoDir)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoDir, dir)
	}
	return filepath.Clean(dir), nil
}

func resolveCommonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	c := strings.TrimSpace(string(b))
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitDir, c)
	}
	return filepath.Clean(c)
}

func readGitRef(gitDir, commonDir, ref string) (string, error) {
	for _, dir := range []string{gitDir, commonDir} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref))); err == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	packed, err := os.ReadFile(filepath.Join(commonDir, "packed-refs"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		if sha, name, ok := strings.Cut(line, " "); ok && name == ref {
			return sha, nil
		}
	}
	return "", nil
}

func readOriginURL(configPath string) string {
	f, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "url" {
			return stripURLCredentials(strings.TrimSpace(v))
		}
	}
	return ""
}

func stripURLCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go -C portal test ./internal/biz/ -run "TestDiscoverGitRepos|TestReadGitInfo" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/repo_scan.go portal/internal/biz/repo_scan_test.go
git commit -m "feat(portal): discover git repos and read HEAD without git binary"
```

---

## Task 6: biz — 纯函数：绑定校验、展开、RCA 根、旧链接映射

**Files:**
- Create: `portal/internal/biz/repo_binding.go`
- Create: `portal/internal/biz/repo_binding_test.go`

- [ ] **Step 1: 写失败测试**

```go
package biz

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sixath/framework/tool"
)

func repoFixture(id, root, rel, status string) *Repository {
	return &Repository{ID: id, CodeRoot: root, RelPath: rel, Status: status}
}

func TestNormalizeRepoBindings(t *testing.T) {
	in := []*AgentRepoBinding{
		{TargetKind: RepoTargetGroup, TargetID: "g1"},
		{TargetKind: RepoTargetRepo, TargetID: "r1", SubPaths: []string{"/svc/x/", "svc/x", "a"}},
		{TargetKind: RepoTargetRepo, TargetID: "r2", Mode: RepoBindingExclude},
	}
	got, err := normalizeRepoBindings("agent-1", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Mode != RepoBindingInclude || got[0].AgentID != "agent-1" || got[0].CreatedBy != "alice" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if !reflect.DeepEqual(got[1].SubPaths, []string{"a", "svc/x"}) {
		t.Fatalf("sub_paths = %v", got[1].SubPaths)
	}

	bad := [][]*AgentRepoBinding{
		{{TargetKind: "repo_selector", TargetID: "x"}},
		{{TargetKind: RepoTargetRepo, TargetID: ""}},
		{{TargetKind: RepoTargetGroup, TargetID: "g", Mode: RepoBindingExclude}},
		{{TargetKind: RepoTargetGroup, TargetID: "g", SubPaths: []string{"a"}}},
		{{TargetKind: RepoTargetRepo, TargetID: "r", SubPaths: []string{"../etc"}}},
		{{TargetKind: RepoTargetRepo, TargetID: "r", Mode: "weird"}},
		{{TargetKind: RepoTargetRepo, TargetID: "r"}, {TargetKind: RepoTargetRepo, TargetID: "r", Mode: RepoBindingExclude}},
	}
	for i, b := range bad {
		if _, err := normalizeRepoBindings("a", "", b); !errors.Is(err, ErrInvalidRepoBinding) {
			t.Fatalf("case %d: err = %v, want ErrInvalidRepoBinding", i, err)
		}
	}
}

func TestExpandRepoBindings(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/c", "cg/a", RepoStatusActive),
		"r2": repoFixture("r2", "/c", "cg/b", RepoStatusActive),
		"r3": repoFixture("r3", "/c", "cg/c", RepoStatusMissing),
		"r4": repoFixture("r4", "/c", "solo", RepoStatusActive),
	}
	members := map[string][]string{"g1": {"r1", "r2", "r3"}}
	bs := []*AgentRepoBinding{
		{TargetKind: RepoTargetGroup, TargetID: "g1", Mode: RepoBindingInclude},
		{TargetKind: RepoTargetRepo, TargetID: "r2", Mode: RepoBindingExclude},
		{TargetKind: RepoTargetRepo, TargetID: "r4", Mode: RepoBindingInclude, SubPaths: []string{"x"}},
		{TargetKind: RepoTargetRepo, TargetID: "r1", Mode: RepoBindingInclude, SubPaths: []string{"only"}},
	}
	now := time.Unix(100, 0)
	got := ExpandRepoBindings("ag", bs, members, repos, now)

	if len(got) != 2 {
		t.Fatalf("want r1 + r4, got %#v", got)
	}
	if got[0].RepoID != "r1" || got[1].RepoID != "r4" {
		t.Fatalf("order by rel_path: %s, %s", got[0].RepoID, got[1].RepoID)
	}
	if got[0].SubPaths != nil {
		t.Fatalf("whole-repo via group must win over sub_paths, got %v", got[0].SubPaths)
	}
	if len(got[0].Via) != 2 {
		t.Fatalf("r1 via = %#v", got[0].Via)
	}
	if !reflect.DeepEqual(got[1].SubPaths, []string{"x"}) {
		t.Fatalf("r4 sub_paths = %v", got[1].SubPaths)
	}
	if got[0].AgentID != "ag" || !got[0].ComputedAt.Equal(now) {
		t.Fatalf("meta = %#v", got[0])
	}
}

func TestBuildRCARoots(t *testing.T) {
	repos := map[string]*Repository{
		"r1": repoFixture("r1", "/codes", "cg/gateway", RepoStatusActive),
		"r2": repoFixture("r2", "/codes", "migu/gateway", RepoStatusActive),
		"r3": repoFixture("r3", "/codes", "solo", RepoStatusActive),
		"r4": repoFixture("r4", "/other", "cg/gateway", RepoStatusActive),
		"r5": repoFixture("r5", "/codes", "gone", RepoStatusMissing),
	}
	eff := []*AgentEffectiveRepo{
		{RepoID: "r1"}, {RepoID: "r2"}, {RepoID: "r3", SubPaths: []string{"svc/x"}}, {RepoID: "r4"}, {RepoID: "r5"},
	}
	got := buildRCARoots(eff, repos)
	want := []tool.RCARoot{
		{Name: "codes/cg/gateway", Path: filepath.Join("/codes", "cg", "gateway")},
		{Name: "migu/gateway", Path: filepath.Join("/codes", "migu", "gateway")},
		{Name: "solo/svc/x", Path: filepath.Join("/codes", "solo", "svc", "x")},
		{Name: "other/cg/gateway", Path: filepath.Join("/other", "cg", "gateway")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestPlanLegacyBinding(t *testing.T) {
	root := filepath.FromSlash("/codes")
	repos := []*Repository{
		repoFixture("r1", root, "cg/a", RepoStatusActive),
		repoFixture("r2", root, "cg/b", RepoStatusActive),
		repoFixture("r3", root, "solo", RepoStatusActive),
	}
	groups := []*RepoGroup{{ID: "g1", Kind: RepoGroupDir, Rule: &RepoGroupRule{CodeRoot: root, RelPrefix: "cg"}}}

	cases := []struct {
		target, action string
		wantKinds      []string
		wantSub        []string
	}{
		{filepath.Join(root, "solo"), LegacyActionBindRepo, []string{RepoTargetRepo}, nil},
		{filepath.Join(root, "cg"), LegacyActionBindGroup, []string{RepoTargetGroup}, nil},
		{root, LegacyActionManualMulti, []string{RepoTargetRepo, RepoTargetRepo, RepoTargetRepo}, nil},
		{filepath.Join(root, "solo", "svc", "x"), LegacyActionManualSubdir, []string{RepoTargetRepo}, []string{"svc/x"}},
		{filepath.FromSlash("/elsewhere"), LegacyActionUnresolved, nil, nil},
	}
	for _, c := range cases {
		p := planLegacyBinding(c.target, repos, groups)
		if p.Action != c.action {
			t.Fatalf("%s: action = %s, want %s", c.target, p.Action, c.action)
		}
		var kinds []string
		for _, b := range p.Bindings {
			kinds = append(kinds, b.TargetKind)
		}
		if !reflect.DeepEqual(kinds, c.wantKinds) {
			t.Fatalf("%s: kinds = %v", c.target, kinds)
		}
		if c.wantSub != nil && !reflect.DeepEqual(p.Bindings[0].SubPaths, c.wantSub) {
			t.Fatalf("%s: sub = %v", c.target, p.Bindings[0].SubPaths)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go -C portal test ./internal/biz/ -run "TestNormalizeRepoBindings|TestExpandRepoBindings|TestBuildRCARoots|TestPlanLegacyBinding" -count=1`
Expected: 编译失败（未定义）。

- [ ] **Step 3: 实现**

`portal/internal/biz/repo_binding.go`:

```go
package biz

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/tool"
)

// normalizeRepoBindings validates and canonicalizes bindings for one agent.
func normalizeRepoBindings(agentID, actor string, in []*AgentRepoBinding) ([]*AgentRepoBinding, error) {
	out := make([]*AgentRepoBinding, 0, len(in))
	seen := map[string]struct{}{}
	for i, b := range in {
		if b == nil {
			continue
		}
		kind := strings.TrimSpace(b.TargetKind)
		id := strings.TrimSpace(b.TargetID)
		mode := strings.TrimSpace(b.Mode)
		if mode == "" {
			mode = RepoBindingInclude
		}
		if kind != RepoTargetRepo && kind != RepoTargetGroup {
			return nil, fmt.Errorf("%w: [%d] target_kind %q not supported", ErrInvalidRepoBinding, i, kind)
		}
		if id == "" {
			return nil, fmt.Errorf("%w: [%d] target_id required", ErrInvalidRepoBinding, i)
		}
		if mode != RepoBindingInclude && mode != RepoBindingExclude {
			return nil, fmt.Errorf("%w: [%d] mode %q invalid", ErrInvalidRepoBinding, i, mode)
		}
		if mode == RepoBindingExclude && kind != RepoTargetRepo {
			return nil, fmt.Errorf("%w: [%d] exclude is only allowed for repo", ErrInvalidRepoBinding, i)
		}
		subs, err := normalizeSubPaths(b.SubPaths)
		if err != nil {
			return nil, fmt.Errorf("%w: [%d] %v", ErrInvalidRepoBinding, i, err)
		}
		if len(subs) > 0 && (kind != RepoTargetRepo || mode != RepoBindingInclude) {
			return nil, fmt.Errorf("%w: [%d] sub_paths only allowed for repo include", ErrInvalidRepoBinding, i)
		}
		key := kind + "\x00" + id
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: [%d] duplicate target %s/%s", ErrInvalidRepoBinding, i, kind, id)
		}
		seen[key] = struct{}{}
		out = append(out, &AgentRepoBinding{
			AgentID: agentID, TargetKind: kind, TargetID: id, Mode: mode,
			SubPaths: subs, Priority: b.Priority, CreatedBy: actor,
		})
	}
	return out, nil
}

func normalizeSubPaths(in []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, p := range in {
		s := strings.Trim(filepath.ToSlash(strings.TrimSpace(p)), "/")
		if s == "" {
			continue
		}
		c := path.Clean(s)
		if c == "." || c == ".." || strings.HasPrefix(c, "../") || strings.Contains(c, ":") {
			return nil, fmt.Errorf("sub_path %q must be a relative path inside the repo", p)
		}
		set[c] = struct{}{}
	}
	if len(set) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// ExpandRepoBindings computes the effective repo set (design §4.3). groupMembers holds
// active member repo ids per group; repos must contain every referenced repo id.
func ExpandRepoBindings(agentID string, bindings []*AgentRepoBinding, groupMembers map[string][]string, repos map[string]*Repository, now time.Time) []*AgentEffectiveRepo {
	type acc struct {
		via   []BindingRef
		subs  []string
		whole bool
	}
	accs := map[string]*acc{}
	add := func(repoID string, ref BindingRef, subs []string) {
		a := accs[repoID]
		if a == nil {
			a = &acc{}
			accs[repoID] = a
		}
		a.via = append(a.via, ref)
		if len(subs) == 0 {
			a.whole = true
		} else {
			a.subs = append(a.subs, subs...)
		}
	}
	excluded := map[string]bool{}
	for _, b := range bindings {
		ref := BindingRef{Kind: b.TargetKind, ID: b.TargetID}
		switch {
		case b.TargetKind == RepoTargetRepo && b.Mode == RepoBindingExclude:
			excluded[b.TargetID] = true
		case b.TargetKind == RepoTargetRepo:
			add(b.TargetID, ref, b.SubPaths)
		case b.TargetKind == RepoTargetGroup:
			for _, id := range groupMembers[b.TargetID] {
				add(id, ref, nil)
			}
		}
	}
	out := make([]*AgentEffectiveRepo, 0, len(accs))
	for id, a := range accs {
		r := repos[id]
		if excluded[id] || r == nil || r.Status != RepoStatusActive {
			continue
		}
		var subs []string
		if !a.whole {
			subs, _ = normalizeSubPaths(a.subs)
		}
		out = append(out, &AgentEffectiveRepo{AgentID: agentID, RepoID: id, Via: a.via, SubPaths: subs, ComputedAt: now})
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := repos[out[i].RepoID], repos[out[j].RepoID]
		if ri.RelPath != rj.RelPath {
			return ri.RelPath < rj.RelPath
		}
		return ri.CodeRoot < rj.CodeRoot
	})
	return out
}

// buildRCARoots maps effective repos to RCA roots named by rel_path (rel_path/sub for
// sub_paths). Names repeated across code roots are prefixed with the code root basename.
func buildRCARoots(eff []*AgentEffectiveRepo, repos map[string]*Repository) []tool.RCARoot {
	type item struct {
		root     tool.RCARoot
		codeRoot string
	}
	var items []item
	for _, e := range eff {
		r := repos[e.RepoID]
		if r == nil || r.Status != RepoStatusActive {
			continue
		}
		base := r.AbsPath()
		if len(e.SubPaths) == 0 {
			items = append(items, item{tool.RCARoot{Name: r.RelPath, Path: base}, r.CodeRoot})
			continue
		}
		for _, sp := range e.SubPaths {
			items = append(items, item{tool.RCARoot{
				Name: r.RelPath + "/" + sp,
				Path: filepath.Join(base, filepath.FromSlash(sp)),
			}, r.CodeRoot})
		}
	}
	count := map[string]int{}
	for _, it := range items {
		count[it.root.Name]++
	}
	out := make([]tool.RCARoot, 0, len(items))
	for _, it := range items {
		if count[it.root.Name] > 1 {
			it.root.Name = filepath.Base(it.codeRoot) + "/" + it.root.Name
		}
		out = append(out, it.root)
	}
	return out
}

const (
	LegacyActionBindRepo     = "bind_repo"
	LegacyActionBindGroup    = "bind_group"
	LegacyActionManualMulti  = "manual_multi"
	LegacyActionManualSubdir = "manual_subdir"
	LegacyActionUnresolved   = "unresolved"
)

type legacyPlan struct {
	Action   string
	Bindings []*AgentRepoBinding
}

// AutoApply reports whether the plan reproduces the old link exactly.
func (p legacyPlan) AutoApply() bool {
	return p.Action == LegacyActionBindRepo || p.Action == LegacyActionBindGroup
}

// planLegacyBinding maps an old workspace/code target to bindings (design §14 step 2).
func planLegacyBinding(target string, repos []*Repository, groups []*RepoGroup) legacyPlan {
	target = filepath.Clean(target)
	sep := string(filepath.Separator)
	for _, r := range repos {
		if r.Status == RepoStatusActive && filepath.Clean(r.AbsPath()) == target {
			return legacyPlan{LegacyActionBindRepo, []*AgentRepoBinding{{TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude}}}
		}
	}
	for _, g := range groups {
		if d := g.DirPath(); d != "" && filepath.Clean(d) == target {
			return legacyPlan{LegacyActionBindGroup, []*AgentRepoBinding{{TargetKind: RepoTargetGroup, TargetID: g.ID, Mode: RepoBindingInclude}}}
		}
	}
	var under []*AgentRepoBinding
	for _, r := range repos {
		abs := filepath.Clean(r.AbsPath())
		if r.Status != RepoStatusActive {
			continue
		}
		if strings.HasPrefix(abs, target+sep) {
			under = append(under, &AgentRepoBinding{TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude})
			continue
		}
		if strings.HasPrefix(target, abs+sep) {
			rel, _ := filepath.Rel(abs, target)
			return legacyPlan{LegacyActionManualSubdir, []*AgentRepoBinding{{
				TargetKind: RepoTargetRepo, TargetID: r.ID, Mode: RepoBindingInclude,
				SubPaths: []string{filepath.ToSlash(rel)},
			}}}
		}
	}
	if len(under) > 0 {
		return legacyPlan{LegacyActionManualMulti, under}
	}
	return legacyPlan{Action: LegacyActionUnresolved}
}
```

注意：`TestPlanLegacyBinding` 中 `manual_multi` 的三个绑定按 `repos` 切片顺序产出；`ListRepositories` 按 `code_root, rel_path` 排序，生产环境顺序稳定。

- [ ] **Step 4: 运行确认通过**

Run: `go -C portal test ./internal/biz/ -run "TestNormalizeRepoBindings|TestExpandRepoBindings|TestBuildRCARoots|TestPlanLegacyBinding" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/repo_binding.go portal/internal/biz/repo_binding_test.go
git commit -m "feat(portal): repo binding validation, expansion and RCA root naming"
```

---

## Task 7: data — `RepoRegistryRepo` 实现

**Files:**
- Create: `portal/internal/data/repo_registry.go`
- Create: `portal/internal/data/repo_registry_test.go`
- Modify: `portal/internal/data/data.go`（ProviderSet）

- [ ] **Step 1: 写失败测试**

`portal/internal/data/repo_registry_test.go`:

```go
package data

import (
	"context"
	"reflect"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openRepoRegistryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Repository{}, &model.RepoGroup{}, &model.RepoGroupMember{},
		&model.AgentRepoBinding{}, &model.AgentEffectiveRepo{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newRepoRegistryRepoForTest(t *testing.T) biz.RepoRegistryRepo {
	return NewRepoRegistryRepo(&Data{db: openRepoRegistryTestDB(t)}, log.DefaultLogger)
}

func TestRepoRegistryRepo_UpsertKeepsUserFieldsAndArchived(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	desc := "gateway"
	if _, err := r.UpdateRepositoryMeta(ctx, a.ID, biz.RepoMetaPatch{Description: &desc, Tags: &[]string{"go"}}); err != nil {
		t.Fatal(err)
	}
	b, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "ignored", HeadCommit: "222", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.HeadCommit != "222" || b.Description != "gateway" || b.Name != "a" || !reflect.DeepEqual(b.Tags, []string{"go"}) {
		t.Fatalf("after rescan: %#v", b)
	}
	if err := r.SetRepositoryStatus(ctx, a.ID, biz.RepoStatusArchived); err != nil {
		t.Fatal(err)
	}
	c, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", LastScannedAt: &now})
	if c.Status != biz.RepoStatusArchived {
		t.Fatalf("archived must stay archived, got %s", c.Status)
	}
}

func TestRepoRegistryRepo_ListFilters(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	a, _ := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a"})
	_, _ = r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "solo", Name: "solo"})
	g, _ := r.UpsertDirGroup(ctx, "/c", "cg")
	if err := r.ReplaceGroupMembers(ctx, g.ID, biz.RepoMemberSourceRule, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.ListRepositories(ctx, biz.RepoFilter{GroupID: g.ID})
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("group filter: %#v", got)
	}
	got, _ = r.ListRepositories(ctx, biz.RepoFilter{Query: "sol"})
	if len(got) != 1 || got[0].RelPath != "solo" {
		t.Fatalf("query filter: %#v", got)
	}
}

func TestRepoRegistryRepo_DirGroupIDStable(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	g1, err := r.UpsertDirGroup(ctx, "/c", "cg")
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := r.UpsertDirGroup(ctx, "/c", "cg")
	if g1.ID != g2.ID || g1.Kind != biz.RepoGroupDir || g1.Rule.RelPrefix != "cg" || g1.Name != "cg" {
		t.Fatalf("g1 = %#v, g2 = %#v", g1, g2)
	}
}

func TestRepoRegistryRepo_BindingsAndEffective(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	bs := []*biz.AgentRepoBinding{
		{AgentID: "ag", TargetKind: biz.RepoTargetGroup, TargetID: "g1", Mode: biz.RepoBindingInclude},
		{AgentID: "ag", TargetKind: biz.RepoTargetRepo, TargetID: "r1", Mode: biz.RepoBindingInclude, SubPaths: []string{"x"}},
	}
	if err := r.ReplaceAgentBindings(ctx, "ag", bs); err != nil {
		t.Fatal(err)
	}
	got, _ := r.ListAgentBindings(ctx, "ag")
	if len(got) != 2 || !reflect.DeepEqual(got[1].SubPaths, []string{"x"}) {
		t.Fatalf("bindings = %#v", got)
	}
	ids, _ := r.ListAgentIDsBoundToGroup(ctx, "g1")
	if !reflect.DeepEqual(ids, []string{"ag"}) {
		t.Fatalf("bound to group = %v", ids)
	}
	if err := r.ReplaceAgentBindings(ctx, "ag", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListAgentBindings(ctx, "ag"); len(got) != 0 {
		t.Fatalf("replace with nil must clear, got %#v", got)
	}

	now := time.Now()
	rows := []*biz.AgentEffectiveRepo{{AgentID: "ag", RepoID: "r1", Via: []biz.BindingRef{{Kind: "repo", ID: "r1"}}, ComputedAt: now}}
	if err := r.ReplaceEffectiveRepos(ctx, "ag", rows); err != nil {
		t.Fatal(err)
	}
	eff, _ := r.ListEffectiveRepos(ctx, "ag")
	if len(eff) != 1 || eff[0].Via[0].ID != "r1" {
		t.Fatalf("effective = %#v", eff)
	}
	agents, _ := r.ListAgentIDsByEffectiveRepo(ctx, "r1")
	if !reflect.DeepEqual(agents, []string{"ag"}) {
		t.Fatalf("agents by repo = %v", agents)
	}
}

func TestRepoRegistryRepo_DeleteGroupCascades(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	g, _ := r.CreateGroup(ctx, &biz.RepoGroup{Name: "m", Kind: biz.RepoGroupManual})
	_ = r.ReplaceGroupMembers(ctx, g.ID, biz.RepoMemberSourceManual, []string{"r1"})
	_ = r.ReplaceAgentBindings(ctx, "ag", []*biz.AgentRepoBinding{{AgentID: "ag", TargetKind: biz.RepoTargetGroup, TargetID: g.ID, Mode: biz.RepoBindingInclude}})
	if err := r.DeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if bs, _ := r.ListAgentBindings(ctx, "ag"); len(bs) != 0 {
		t.Fatalf("bindings to deleted group must be removed: %#v", bs)
	}
	if m, _ := r.ListActiveGroupMembers(ctx, []string{g.ID}); len(m[g.ID]) != 0 {
		t.Fatalf("members must be removed: %#v", m)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run（PowerShell）: `$env:CGO_ENABLED="1"; go -C portal test ./internal/data/ -run TestRepoRegistryRepo -count=1`
Expected: 编译失败，`undefined: NewRepoRegistryRepo`。

- [ ] **Step 3: 实现**

`portal/internal/data/repo_registry.go`:

```go
package data

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.RepoRegistryRepo = (*repoRegistryRepo)(nil)

// dirGroupNamespace derives stable dir-group ids from (code_root, rel_prefix).
var dirGroupNamespace = uuid.MustParse("6f1f0f2e-8a7c-4c1e-9b7d-3c2a1e5d4f60")

type repoRegistryRepo struct {
	db  *gorm.DB
	log *log.Helper
}

func NewRepoRegistryRepo(data *Data, logger log.Logger) biz.RepoRegistryRepo {
	if data == nil || data.db == nil {
		panic("NewRepoRegistryRepo: Data.db is nil")
	}
	return &repoRegistryRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *repoRegistryRepo) UpsertScannedRepository(ctx context.Context, in *biz.Repository) (*biz.Repository, error) {
	var out *biz.Repository
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Repository
		err := tx.Where("code_root = ? AND rel_path = ?", in.CodeRoot, in.RelPath).First(&m).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			m = model.Repository{
				ID: uuid.NewString(), CodeRoot: in.CodeRoot, RelPath: in.RelPath, Name: in.Name,
				GitRemote: in.GitRemote, GitBranch: in.GitBranch, HeadCommit: in.HeadCommit,
				SyncMode: biz.RepoSyncRegistryOnly, Status: biz.RepoStatusActive,
				HandbookStatus: biz.HandbookStatusNone, LastScannedAt: in.LastScannedAt,
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			updates := map[string]any{
				"git_remote": in.GitRemote, "git_branch": in.GitBranch,
				"head_commit": in.HeadCommit, "last_scanned_at": in.LastScannedAt,
			}
			if m.Status != biz.RepoStatusArchived {
				updates["status"] = biz.RepoStatusActive
			}
			if err := tx.Model(&m).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", m.ID).First(&m).Error; err != nil {
				return err
			}
		}
		out = repositoryToBiz(&m)
		return nil
	})
	return out, err
}

func (r *repoRegistryRepo) ListRepositories(ctx context.Context, f biz.RepoFilter) ([]*biz.Repository, error) {
	q := r.db.WithContext(ctx).Model(&model.Repository{})
	if f.CodeRoot != "" {
		q = q.Where("code_root = ?", f.CodeRoot)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		like := "%" + s + "%"
		q = q.Where("(rel_path LIKE ? OR name LIKE ?)", like, like)
	}
	if f.GroupID != "" {
		sub := r.db.Model(&model.RepoGroupMember{}).Select("repo_id").
			Where("group_id = ? AND state = ?", f.GroupID, biz.RepoMemberActive)
		q = q.Where("id IN (?)", sub)
	}
	var ms []model.Repository
	if err := q.Order("code_root, rel_path").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.Repository, 0, len(ms))
	for i := range ms {
		out = append(out, repositoryToBiz(&ms[i]))
	}
	return out, nil
}

func (r *repoRegistryRepo) GetRepositoriesByIDs(ctx context.Context, ids []string) (map[string]*biz.Repository, error) {
	out := map[string]*biz.Repository{}
	if len(ids) == 0 {
		return out, nil
	}
	var ms []model.Repository
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&ms).Error; err != nil {
		return nil, err
	}
	for i := range ms {
		out[ms[i].ID] = repositoryToBiz(&ms[i])
	}
	return out, nil
}

func (r *repoRegistryRepo) SetRepositoryStatus(ctx context.Context, id, status string) error {
	res := r.db.WithContext(ctx).Model(&model.Repository{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return biz.ErrRepoNotFound
	}
	return nil
}

func (r *repoRegistryRepo) UpdateRepositoryMeta(ctx context.Context, id string, p biz.RepoMetaPatch) (*biz.Repository, error) {
	updates := map[string]any{}
	if p.Name != nil {
		updates["name"] = strings.TrimSpace(*p.Name)
	}
	if p.Description != nil {
		updates["description"] = *p.Description
	}
	if p.Tags != nil {
		updates["tags"] = model.JSONStrings(*p.Tags)
	}
	if p.OwnerID != nil {
		updates["owner_id"] = *p.OwnerID
	}
	if p.Status != nil {
		updates["status"] = *p.Status
	}
	db := r.db.WithContext(ctx)
	if len(updates) > 0 {
		if err := db.Model(&model.Repository{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	var m model.Repository
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, biz.ErrRepoNotFound
		}
		return nil, err
	}
	return repositoryToBiz(&m), nil
}

func (r *repoRegistryRepo) UpsertDirGroup(ctx context.Context, codeRoot, relPrefix string) (*biz.RepoGroup, error) {
	id := uuid.NewSHA1(dirGroupNamespace, []byte(codeRoot+"\x00"+relPrefix)).String()
	m := model.RepoGroup{
		ID: id, Name: relPrefix, Kind: biz.RepoGroupDir, AutoApplyNew: true,
		HandbookStatus: biz.HandbookStatusNone,
		Rule:           &model.RepoGroupRule{CodeRoot: codeRoot, RelPrefix: relPrefix},
	}
	if err := r.db.WithContext(ctx).Where("id = ?", id).FirstOrCreate(&m).Error; err != nil {
		return nil, err
	}
	return repoGroupToBiz(&m), nil
}

func (r *repoRegistryRepo) CreateGroup(ctx context.Context, g *biz.RepoGroup) (*biz.RepoGroup, error) {
	m := model.RepoGroup{
		ID: uuid.NewString(), Name: g.Name, Kind: g.Kind, AutoApplyNew: g.AutoApplyNew,
		HandbookStatus: biz.HandbookStatusNone, OwnerID: g.OwnerID, Rule: ruleToModel(g.Rule),
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return repoGroupToBiz(&m), nil
}

func (r *repoRegistryRepo) ListGroups(ctx context.Context, kind string) ([]*biz.RepoGroup, error) {
	q := r.db.WithContext(ctx).Model(&model.RepoGroup{})
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var ms []model.RepoGroup
	if err := q.Order("kind, name").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.RepoGroup, 0, len(ms))
	for i := range ms {
		out = append(out, repoGroupToBiz(&ms[i]))
	}
	return out, nil
}

func (r *repoRegistryRepo) GetGroupsByIDs(ctx context.Context, ids []string) (map[string]*biz.RepoGroup, error) {
	out := map[string]*biz.RepoGroup{}
	if len(ids) == 0 {
		return out, nil
	}
	var ms []model.RepoGroup
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&ms).Error; err != nil {
		return nil, err
	}
	for i := range ms {
		out[ms[i].ID] = repoGroupToBiz(&ms[i])
	}
	return out, nil
}

func (r *repoRegistryRepo) DeleteGroup(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&model.RepoGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target_kind = ? AND target_id = ?", biz.RepoTargetGroup, id).Delete(&model.AgentRepoBinding{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&model.RepoGroup{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return biz.ErrRepoNotFound
		}
		return nil
	})
}

func (r *repoRegistryRepo) ReplaceGroupMembers(ctx context.Context, groupID, source string, repoIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ? AND source = ?", groupID, source).Delete(&model.RepoGroupMember{}).Error; err != nil {
			return err
		}
		if len(repoIDs) == 0 {
			return nil
		}
		now := time.Now()
		rows := make([]model.RepoGroupMember, 0, len(repoIDs))
		for _, id := range dedupeStrings(repoIDs) {
			rows = append(rows, model.RepoGroupMember{GroupID: groupID, RepoID: id, Source: source, State: biz.RepoMemberActive, CreatedAt: now})
		}
		return tx.Create(&rows).Error
	})
}

func (r *repoRegistryRepo) ListActiveGroupMembers(ctx context.Context, groupIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	var ms []model.RepoGroupMember
	if err := r.db.WithContext(ctx).Where("group_id IN ? AND state = ?", groupIDs, biz.RepoMemberActive).
		Order("group_id, repo_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	for _, m := range ms {
		out[m.GroupID] = append(out[m.GroupID], m.RepoID)
	}
	return out, nil
}

func (r *repoRegistryRepo) ListAgentBindings(ctx context.Context, agentID string) ([]*biz.AgentRepoBinding, error) {
	var ms []model.AgentRepoBinding
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).
		Order("priority, target_kind, target_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.AgentRepoBinding, 0, len(ms))
	for _, m := range ms {
		out = append(out, &biz.AgentRepoBinding{
			AgentID: m.AgentID, TargetKind: m.TargetKind, TargetID: m.TargetID, Mode: m.Mode,
			SubPaths: []string(m.SubPaths), Priority: m.Priority, CreatedBy: m.CreatedBy,
		})
	}
	return out, nil
}

func (r *repoRegistryRepo) ReplaceAgentBindings(ctx context.Context, agentID string, bs []*biz.AgentRepoBinding) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentRepoBinding{}).Error; err != nil {
			return err
		}
		if len(bs) == 0 {
			return nil
		}
		rows := make([]model.AgentRepoBinding, 0, len(bs))
		for _, b := range bs {
			rows = append(rows, model.AgentRepoBinding{
				AgentID: agentID, TargetKind: b.TargetKind, TargetID: b.TargetID, Mode: b.Mode,
				SubPaths: model.JSONStrings(b.SubPaths), Priority: b.Priority, CreatedBy: b.CreatedBy,
			})
		}
		return tx.Create(&rows).Error
	})
}

func (r *repoRegistryRepo) ListAgentIDsWithBindings(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentRepoBinding{}).Distinct().Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func (r *repoRegistryRepo) ListAgentIDsBoundToGroup(ctx context.Context, groupID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentRepoBinding{}).
		Where("target_kind = ? AND target_id = ?", biz.RepoTargetGroup, groupID).
		Distinct().Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func (r *repoRegistryRepo) ReplaceEffectiveRepos(ctx context.Context, agentID string, rows []*biz.AgentEffectiveRepo) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentEffectiveRepo{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ms := make([]model.AgentEffectiveRepo, 0, len(rows))
		for _, e := range rows {
			via := make(model.RepoBindingRefs, 0, len(e.Via))
			for _, v := range e.Via {
				via = append(via, model.RepoBindingRef{Kind: v.Kind, ID: v.ID})
			}
			ms = append(ms, model.AgentEffectiveRepo{
				AgentID: agentID, RepoID: e.RepoID, Via: via,
				SubPaths: model.JSONStrings(e.SubPaths), ComputedAt: e.ComputedAt,
			})
		}
		return tx.Create(&ms).Error
	})
}

func (r *repoRegistryRepo) ListEffectiveRepos(ctx context.Context, agentID string) ([]*biz.AgentEffectiveRepo, error) {
	var ms []model.AgentEffectiveRepo
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("repo_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.AgentEffectiveRepo, 0, len(ms))
	for _, m := range ms {
		via := make([]biz.BindingRef, 0, len(m.Via))
		for _, v := range m.Via {
			via = append(via, biz.BindingRef{Kind: v.Kind, ID: v.ID})
		}
		out = append(out, &biz.AgentEffectiveRepo{
			AgentID: m.AgentID, RepoID: m.RepoID, Via: via,
			SubPaths: []string(m.SubPaths), ComputedAt: m.ComputedAt,
		})
	}
	return out, nil
}

func (r *repoRegistryRepo) ListAgentIDsByEffectiveRepo(ctx context.Context, repoID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentEffectiveRepo{}).Where("repo_id = ?", repoID).
		Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func repositoryToBiz(m *model.Repository) *biz.Repository {
	return &biz.Repository{
		ID: m.ID, CodeRoot: m.CodeRoot, RelPath: m.RelPath, Name: m.Name, Description: m.Description,
		Tags: []string(m.Tags), GitRemote: m.GitRemote, GitBranch: m.GitBranch, HeadCommit: m.HeadCommit,
		SyncMode: m.SyncMode, Status: m.Status, HandbookStatus: m.HandbookStatus, OwnerID: m.OwnerID,
		LastScannedAt: m.LastScannedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func repoGroupToBiz(m *model.RepoGroup) *biz.RepoGroup {
	g := &biz.RepoGroup{
		ID: m.ID, Name: m.Name, Kind: m.Kind, AutoApplyNew: m.AutoApplyNew,
		OwnerID: m.OwnerID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.Rule != nil {
		g.Rule = &biz.RepoGroupRule{CodeRoot: m.Rule.CodeRoot, RelPrefix: m.Rule.RelPrefix, AllOf: m.Rule.AllOf, AnyOf: m.Rule.AnyOf}
	}
	return g
}

func ruleToModel(r *biz.RepoGroupRule) *model.RepoGroupRule {
	if r == nil {
		return nil
	}
	return &model.RepoGroupRule{CodeRoot: r.CodeRoot, RelPrefix: r.RelPrefix, AllOf: r.AllOf, AnyOf: r.AnyOf}
}

func dedupeStrings(in []string) []string {
	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := set[s]; ok || s == "" {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
```

若 data 包已有同名的 `dedupeStrings`，改名为 `dedupeRepoIDs`（先 `rg "func dedupeStrings" portal/internal/data` 确认）。

`portal/internal/data/data.go` 的 `ProviderSet` 末尾追加 `NewRepoRegistryRepo`。

- [ ] **Step 4: 运行确认通过**

Run: `$env:CGO_ENABLED="1"; go -C portal test ./internal/data/ -run TestRepoRegistryRepo -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/data/repo_registry.go portal/internal/data/repo_registry_test.go portal/internal/data/data.go
git commit -m "feat(portal): repo registry gorm repository"
```

---

## Task 8: biz — `RepoRegistryUsecase`

**Files:**
- Create: `portal/internal/biz/repo_registry_usecase.go`
- Modify: `portal/internal/biz/biz.go`
- Test: 追加到 `portal/internal/data/repo_registry_test.go`（用真实 sqlite repo 做集成测试）

- [ ] **Step 1: 写失败测试**

追加到 `portal/internal/data/repo_registry_test.go`（import 增加 `os`、`path/filepath`）：

```go
type stubAgentRepo struct {
	biz.AgentRepo
	agents []*biz.AgentMeta
}

func (s *stubAgentRepo) List(_ context.Context, page, pageSize int32) ([]*biz.AgentMeta, int, error) {
	start := int((page - 1) * pageSize)
	if start >= len(s.agents) {
		return nil, len(s.agents), nil
	}
	end := start + int(pageSize)
	if end > len(s.agents) {
		end = len(s.agents)
	}
	return s.agents[start:end], len(s.agents), nil
}

func mkRepoDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newUsecaseForTest(t *testing.T, codeRoot string, agents ...*biz.AgentMeta) (*biz.RepoRegistryUsecase, biz.RepoRegistryRepo) {
	repo := newRepoRegistryRepoForTest(t)
	uc := biz.NewRepoRegistryUsecase(repo, &stubAgentRepo{agents: agents}, []string{codeRoot}, log.DefaultLogger)
	return uc, repo
}

func TestRepoRegistryUsecase_ScanBindAndRoots(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "gateway"))
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-a"))
	mkRepoDir(t, filepath.Join(codeRoot, "migu", "gateway"))
	uc, _ := newUsecaseForTest(t, codeRoot)

	rep, err := uc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Found != 3 || rep.Added != 3 {
		t.Fatalf("report = %#v", rep)
	}
	groups, err := uc.ListGroups(ctx, biz.RepoGroupDir)
	if err != nil {
		t.Fatal(err)
	}
	var cg *biz.RepoGroupView
	for _, g := range groups {
		if g.Name == "cloudgame" {
			cg = g
		}
	}
	if cg == nil || len(cg.RepoIDs) != 2 {
		t.Fatalf("cloudgame dir group = %#v", cg)
	}
	repos, _ := uc.ListRepos(ctx, biz.RepoFilter{Query: "migu/gateway"})
	if len(repos) != 1 {
		t.Fatalf("migu repos = %#v", repos)
	}

	_, err = uc.ReplaceBindings(ctx, "ag", []*biz.AgentRepoBinding{
		{TargetKind: biz.RepoTargetGroup, TargetID: cg.ID},
		{TargetKind: biz.RepoTargetRepo, TargetID: repos[0].ID},
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := uc.RCARootsForAgent(ctx, "ag")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range roots {
		names[r.Name] = r.Path
	}
	if len(roots) != 3 || names["migu/gateway"] != filepath.Join(codeRoot, "migu", "gateway") || names["cloudgame/gateway"] == "" {
		t.Fatalf("roots = %#v", roots)
	}

	// a repo disappearing drops out of the effective set after rescan
	if err := os.RemoveAll(filepath.Join(codeRoot, "cloudgame", "svc-a")); err != nil {
		t.Fatal(err)
	}
	rep, _ = uc.Scan(ctx)
	if rep.Missing != 1 {
		t.Fatalf("report = %#v", rep)
	}
	roots, _ = uc.RCARootsForAgent(ctx, "ag")
	if len(roots) != 2 {
		t.Fatalf("after missing: %#v", roots)
	}
}

func TestRepoRegistryUsecase_ReplaceBindingsRejectsUnknownTarget(t *testing.T) {
	uc, _ := newUsecaseForTest(t, t.TempDir())
	_, err := uc.ReplaceBindings(context.Background(), "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: "nope"}}, "")
	if !errors.Is(err, biz.ErrInvalidRepoBinding) {
		t.Fatalf("err = %v", err)
	}
}

func TestRepoRegistryUsecase_NoBindingsMeansNoRoots(t *testing.T) {
	uc, _ := newUsecaseForTest(t, t.TempDir())
	roots, err := uc.RCARootsForAgent(context.Background(), "ag")
	if err != nil || roots != nil {
		t.Fatalf("roots = %#v, err = %v", roots, err)
	}
}

func TestRepoRegistryUsecase_MigrateLegacyLinks(t *testing.T) {
	ctx := context.Background()
	codeRoot := t.TempDir()
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-a"))
	mkRepoDir(t, filepath.Join(codeRoot, "solo"))

	mkLinkedWorkspace := func(target string) string {
		ws := t.TempDir()
		if err := os.Symlink(target, filepath.Join(ws, "code")); err != nil {
			t.Skipf("symlink not permitted: %v", err)
		}
		return ws
	}
	agents := []*biz.AgentMeta{
		{ID: "a-repo", Workspace: mkLinkedWorkspace(filepath.Join(codeRoot, "solo"))},
		{ID: "a-group", Workspace: mkLinkedWorkspace(filepath.Join(codeRoot, "cloudgame"))},
		{ID: "a-multi", Workspace: mkLinkedWorkspace(codeRoot)},
		{ID: "a-none", Workspace: t.TempDir()},
	}
	uc, repo := newUsecaseForTest(t, codeRoot, agents...)
	if _, err := uc.Scan(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := uc.MigrateLegacyLinks(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, it := range items {
		actions[it.AgentID] = it.Action
		if it.Applied {
			t.Fatalf("dry run must not apply: %#v", it)
		}
	}
	if actions["a-repo"] != biz.LegacyActionBindRepo || actions["a-group"] != biz.LegacyActionBindGroup ||
		actions["a-multi"] != biz.LegacyActionManualMulti {
		t.Fatalf("actions = %v", actions)
	}
	if _, ok := actions["a-none"]; ok {
		t.Fatal("agents without workspace/code are not reported")
	}

	if _, err := uc.MigrateLegacyLinks(ctx, true); err != nil {
		t.Fatal(err)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-repo"); len(bs) != 1 {
		t.Fatalf("a-repo bindings = %#v", bs)
	}
	if bs, _ := repo.ListAgentBindings(ctx, "a-multi"); len(bs) != 0 {
		t.Fatalf("manual cases must not be applied: %#v", bs)
	}
	items, _ = uc.MigrateLegacyLinks(ctx, true)
	for _, it := range items {
		if it.AgentID == "a-repo" && it.Action != biz.LegacyActionSkipHasBindings {
			t.Fatalf("second run must skip bound agents: %#v", it)
		}
	}
}
```

import 需加 `"errors"`。

- [ ] **Step 2: 运行确认失败**

Run: `$env:CGO_ENABLED="1"; go -C portal test ./internal/data/ -run TestRepoRegistryUsecase -count=1`
Expected: 编译失败，`undefined: biz.NewRepoRegistryUsecase` 等。

- [ ] **Step 3: 实现**

`portal/internal/biz/repo_registry_usecase.go`:

```go
package biz

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
	fwws "github.com/sixath/framework/workspace"
)

// RepoRegistryUsecase maintains the repository registry and agent repo bindings.
type RepoRegistryUsecase struct {
	repo      RepoRegistryRepo
	agents    AgentRepo
	codeRoots []string
	scanMu    sync.Mutex
	log       *log.Helper
}

func NewRepoRegistryUsecase(repo RepoRegistryRepo, agents AgentRepo, codeRoots []string, logger log.Logger) *RepoRegistryUsecase {
	return &RepoRegistryUsecase{repo: repo, agents: agents, codeRoots: cleanCodeRoots(codeRoots), log: log.NewHelper(logger)}
}

func cleanCodeRoots(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

type RepoScanReport struct {
	Roots    int      `json:"roots"`
	Found    int      `json:"found"`
	Added    int      `json:"added"`
	Restored int      `json:"restored"`
	Missing  int      `json:"missing"`
	Errors   []string `json:"errors,omitempty"`
}

// Scan discovers repositories under every code root, refreshes dir groups and recomputes
// effective repo sets. An unreadable code root is reported and its repos are left untouched.
func (uc *RepoRegistryUsecase) Scan(ctx context.Context) (*RepoScanReport, error) {
	if !uc.scanMu.TryLock() {
		return nil, ErrRepoScanRunning
	}
	defer uc.scanMu.Unlock()
	rep := &RepoScanReport{}
	for _, root := range uc.codeRoots {
		if err := uc.scanRoot(ctx, root, rep); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", root, err))
		}
	}
	if err := uc.RecomputeAll(ctx); err != nil {
		return rep, err
	}
	return rep, nil
}

func (uc *RepoRegistryUsecase) scanRoot(ctx context.Context, root string, rep *RepoScanReport) error {
	rels, unreadable, err := DiscoverGitRepos(root, RepoScanMaxDepth)
	if err != nil {
		return err
	}
	rep.Roots++
	for _, u := range unreadable {
		rep.Errors = append(rep.Errors, fmt.Sprintf("%s: unreadable subtree %q", root, u))
	}
	existing, err := uc.repo.ListRepositories(ctx, RepoFilter{CodeRoot: root})
	if err != nil {
		return err
	}
	byRel := make(map[string]*Repository, len(existing))
	for _, r := range existing {
		byRel[r.RelPath] = r
	}
	now := time.Now()
	seen := make(map[string]bool, len(rels))
	dirMembers := map[string][]string{}
	for _, rel := range rels {
		seen[rel] = true
		info, gerr := ReadGitInfo(filepath.Join(root, filepath.FromSlash(rel)))
		if gerr != nil {
			uc.log.Warnf("repo scan: read git info %s/%s: %v", root, rel, gerr)
		}
		saved, err := uc.repo.UpsertScannedRepository(ctx, &Repository{
			CodeRoot: root, RelPath: rel, Name: path.Base(rel),
			GitRemote: info.Remote, GitBranch: info.Branch, HeadCommit: info.Commit, LastScannedAt: &now,
		})
		if err != nil {
			return err
		}
		rep.Found++
		switch prev := byRel[rel]; {
		case prev == nil:
			rep.Added++
		case prev.Status == RepoStatusMissing:
			rep.Restored++
		}
		if parent := path.Dir(rel); parent != "." {
			dirMembers[parent] = append(dirMembers[parent], saved.ID)
		}
	}
	for rel, r := range byRel {
		if seen[rel] || r.Status != RepoStatusActive {
			continue
		}
		if underAnyPrefix(rel, unreadable) {
			// 子树读不到时保持原状，并保留其目录组成员身份，避免绑定该组的 agent 丢仓库。
			if parent := path.Dir(rel); parent != "." {
				dirMembers[parent] = append(dirMembers[parent], r.ID)
			}
			continue
		}
		if err := uc.repo.SetRepositoryStatus(ctx, r.ID, RepoStatusMissing); err != nil {
			return err
		}
		rep.Missing++
	}
	return uc.syncDirGroups(ctx, root, dirMembers)
}

func (uc *RepoRegistryUsecase) syncDirGroups(ctx context.Context, root string, members map[string][]string) error {
	touched := map[string]bool{}
	for prefix, ids := range members {
		g, err := uc.repo.UpsertDirGroup(ctx, root, prefix)
		if err != nil {
			return err
		}
		touched[g.ID] = true
		if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceRule, ids); err != nil {
			return err
		}
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Rule != nil && g.Rule.CodeRoot == root && !touched[g.ID] {
			if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceRule, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecomputeAgent rebuilds agent_effective_repos for one agent.
func (uc *RepoRegistryUsecase) RecomputeAgent(ctx context.Context, agentID string) ([]*AgentEffectiveRepo, error) {
	bs, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	var groupIDs, repoIDs []string
	for _, b := range bs {
		switch b.TargetKind {
		case RepoTargetGroup:
			groupIDs = append(groupIDs, b.TargetID)
		case RepoTargetRepo:
			repoIDs = append(repoIDs, b.TargetID)
		}
	}
	members, err := uc.repo.ListActiveGroupMembers(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, ids := range members {
		repoIDs = append(repoIDs, ids...)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	eff := ExpandRepoBindings(agentID, bs, members, repos, time.Now())
	if err := uc.repo.ReplaceEffectiveRepos(ctx, agentID, eff); err != nil {
		return nil, err
	}
	return eff, nil
}

// RecomputeAll recomputes every agent that has bindings; errors are joined, not fatal.
func (uc *RepoRegistryUsecase) RecomputeAll(ctx context.Context) error {
	ids, err := uc.repo.ListAgentIDsWithBindings(ctx)
	if err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, ids)
}

func (uc *RepoRegistryUsecase) recomputeAgents(ctx context.Context, ids []string) error {
	var errs []error
	for _, id := range ids {
		if _, err := uc.RecomputeAgent(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("agent %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// RCARootsForAgent returns RCA roots for the agent's effective repos; nil means the agent
// has no repo bindings and callers should fall back to the legacy workspace/code link.
func (uc *RepoRegistryUsecase) RCARootsForAgent(ctx context.Context, agentID string) ([]tool.RCARoot, error) {
	eff, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil || len(eff) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(eff))
	for _, e := range eff {
		ids = append(ids, e.RepoID)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	sort.Slice(eff, func(i, j int) bool {
		ri, rj := repos[eff[i].RepoID], repos[eff[j].RepoID]
		if ri == nil || rj == nil {
			return ri != nil
		}
		return ri.RelPath < rj.RelPath
	})
	return buildRCARoots(eff, repos), nil
}

// ---- repositories ----

func (uc *RepoRegistryUsecase) ListRepos(ctx context.Context, f RepoFilter) ([]*Repository, error) {
	return uc.repo.ListRepositories(ctx, f)
}

type RepoDetail struct {
	Repository *Repository `json:"repository"`
	AgentIDs   []string    `json:"agent_ids"`
}

func (uc *RepoRegistryUsecase) GetRepo(ctx context.Context, id string) (*RepoDetail, error) {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	r := m[id]
	if r == nil {
		return nil, ErrRepoNotFound
	}
	agents, err := uc.repo.ListAgentIDsByEffectiveRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RepoDetail{Repository: r, AgentIDs: agents}, nil
}

// PatchRepo updates user-editable fields; status may only be set to active or archived.
func (uc *RepoRegistryUsecase) PatchRepo(ctx context.Context, id string, p RepoMetaPatch) (*Repository, error) {
	if p.Status != nil && *p.Status != RepoStatusActive && *p.Status != RepoStatusArchived {
		return nil, fmt.Errorf("%w: status must be active or archived", ErrInvalidRepoBinding)
	}
	r, err := uc.repo.UpdateRepositoryMeta(ctx, id, p)
	if err != nil {
		return nil, err
	}
	if p.Status != nil {
		agents, err := uc.repo.ListAgentIDsByEffectiveRepo(ctx, id)
		if err != nil {
			return nil, err
		}
		if *p.Status == RepoStatusActive {
			if agents, err = uc.repo.ListAgentIDsWithBindings(ctx); err != nil {
				return nil, err
			}
		}
		if err := uc.recomputeAgents(ctx, agents); err != nil {
			uc.log.Warnf("recompute after repo status change: %v", err)
		}
	}
	return r, nil
}

// ---- groups ----

type RepoGroupView struct {
	*RepoGroup
	RepoIDs []string `json:"repo_ids"`
}

func (uc *RepoRegistryUsecase) ListGroups(ctx context.Context, kind string) ([]*RepoGroupView, error) {
	groups, err := uc.repo.ListGroups(ctx, kind)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	members, err := uc.repo.ListActiveGroupMembers(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*RepoGroupView, 0, len(groups))
	for _, g := range groups {
		out = append(out, &RepoGroupView{RepoGroup: g, RepoIDs: members[g.ID]})
	}
	return out, nil
}

func (uc *RepoRegistryUsecase) CreateManualGroup(ctx context.Context, name string, repoIDs []string, owner string) (*RepoGroupView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name required", ErrInvalidRepoGroup)
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoGroup); err != nil {
		return nil, err
	}
	g, err := uc.repo.CreateGroup(ctx, &RepoGroup{Name: name, Kind: RepoGroupManual, OwnerID: owner})
	if err != nil {
		return nil, err
	}
	if err := uc.repo.ReplaceGroupMembers(ctx, g.ID, RepoMemberSourceManual, repoIDs); err != nil {
		return nil, err
	}
	return &RepoGroupView{RepoGroup: g, RepoIDs: repoIDs}, nil
}

func (uc *RepoRegistryUsecase) SetManualGroupMembers(ctx context.Context, groupID string, repoIDs []string) error {
	if _, err := uc.requireGroup(ctx, groupID, RepoGroupManual); err != nil {
		return err
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoGroup); err != nil {
		return err
	}
	if err := uc.repo.ReplaceGroupMembers(ctx, groupID, RepoMemberSourceManual, repoIDs); err != nil {
		return err
	}
	agents, err := uc.repo.ListAgentIDsBoundToGroup(ctx, groupID)
	if err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, agents)
}

// DeleteGroup deletes a manual group and the bindings that reference it.
func (uc *RepoRegistryUsecase) DeleteGroup(ctx context.Context, groupID string) error {
	if _, err := uc.requireGroup(ctx, groupID, RepoGroupManual); err != nil {
		return err
	}
	agents, err := uc.repo.ListAgentIDsBoundToGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if err := uc.repo.DeleteGroup(ctx, groupID); err != nil {
		return err
	}
	return uc.recomputeAgents(ctx, agents)
}

func (uc *RepoRegistryUsecase) requireGroup(ctx context.Context, id, kind string) (*RepoGroup, error) {
	m, err := uc.repo.GetGroupsByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	g := m[id]
	if g == nil {
		return nil, ErrRepoNotFound
	}
	if kind != "" && g.Kind != kind {
		return nil, fmt.Errorf("%w: group %s is %s, only %s groups can be edited", ErrInvalidRepoGroup, id, g.Kind, kind)
	}
	return g, nil
}

func (uc *RepoRegistryUsecase) requireRepos(ctx context.Context, ids []string, kindErr error) error {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if m[id] == nil {
			return fmt.Errorf("%w: unknown repo %s", kindErr, id)
		}
	}
	return nil
}

// ---- agent bindings ----

type EffectiveRepoView struct {
	*AgentEffectiveRepo
	Repository *Repository `json:"repository,omitempty"`
}

type AgentRepoBindingsView struct {
	Bindings  []*AgentRepoBinding  `json:"bindings"`
	Effective []*EffectiveRepoView `json:"effective"`
}

func (uc *RepoRegistryUsecase) GetBindings(ctx context.Context, agentID string) (*AgentRepoBindingsView, error) {
	bs, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	eff, err := uc.repo.ListEffectiveRepos(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return uc.bindingsView(ctx, bs, eff)
}

// ReplaceBindings validates and replaces all bindings of an agent, then recomputes its effective set.
func (uc *RepoRegistryUsecase) ReplaceBindings(ctx context.Context, agentID string, in []*AgentRepoBinding, actor string) (*AgentRepoBindingsView, error) {
	bs, err := normalizeRepoBindings(agentID, actor, in)
	if err != nil {
		return nil, err
	}
	var repoIDs, groupIDs []string
	for _, b := range bs {
		if b.TargetKind == RepoTargetRepo {
			repoIDs = append(repoIDs, b.TargetID)
		} else {
			groupIDs = append(groupIDs, b.TargetID)
		}
	}
	if err := uc.requireRepos(ctx, repoIDs, ErrInvalidRepoBinding); err != nil {
		return nil, err
	}
	groups, err := uc.repo.GetGroupsByIDs(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range groupIDs {
		if groups[id] == nil {
			return nil, fmt.Errorf("%w: unknown repo group %s", ErrInvalidRepoBinding, id)
		}
	}
	if err := uc.repo.ReplaceAgentBindings(ctx, agentID, bs); err != nil {
		return nil, err
	}
	eff, err := uc.RecomputeAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return uc.bindingsView(ctx, bs, eff)
}

func (uc *RepoRegistryUsecase) CopyBindings(ctx context.Context, fromAgentID, toAgentID, actor string) (*AgentRepoBindingsView, error) {
	src, err := uc.repo.ListAgentBindings(ctx, fromAgentID)
	if err != nil {
		return nil, err
	}
	return uc.ReplaceBindings(ctx, toAgentID, src, actor)
}

func (uc *RepoRegistryUsecase) bindingsView(ctx context.Context, bs []*AgentRepoBinding, eff []*AgentEffectiveRepo) (*AgentRepoBindingsView, error) {
	ids := make([]string, 0, len(eff))
	for _, e := range eff {
		ids = append(ids, e.RepoID)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	view := &AgentRepoBindingsView{Bindings: bs, Effective: make([]*EffectiveRepoView, 0, len(eff))}
	for _, e := range eff {
		view.Effective = append(view.Effective, &EffectiveRepoView{AgentEffectiveRepo: e, Repository: repos[e.RepoID]})
	}
	return view, nil
}

// ---- legacy workspace/code migration (design §14) ----

const LegacyActionSkipHasBindings = "skip_has_bindings"

type LegacyLinkMigrationItem struct {
	AgentID  string              `json:"agent_id"`
	Target   string              `json:"target"`
	Action   string              `json:"action"`
	Bindings []*AgentRepoBinding `json:"bindings,omitempty"`
	Applied  bool                `json:"applied"`
	Error    string              `json:"error,omitempty"`
}

// MigrateLegacyLinks maps each agent's workspace/code link to repo bindings. With apply=false
// it only reports; with apply=true it writes bindings only for exact repo / dir-group matches.
func (uc *RepoRegistryUsecase) MigrateLegacyLinks(ctx context.Context, apply bool) ([]*LegacyLinkMigrationItem, error) {
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return nil, err
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return nil, err
	}
	bound, err := uc.repo.ListAgentIDsWithBindings(ctx)
	if err != nil {
		return nil, err
	}
	hasBindings := make(map[string]bool, len(bound))
	for _, id := range bound {
		hasBindings[id] = true
	}
	var items []*LegacyLinkMigrationItem
	const pageSize = 200
	for page := int32(1); ; page++ {
		agents, _, err := uc.agents.List(ctx, page, pageSize)
		if err != nil {
			return items, err
		}
		for _, a := range agents {
			target := fwws.ResolveCodeMount(a.Workspace)
			if target == "" {
				continue
			}
			item := &LegacyLinkMigrationItem{AgentID: a.ID, Target: target}
			items = append(items, item)
			if hasBindings[a.ID] {
				item.Action = LegacyActionSkipHasBindings
				continue
			}
			plan := planLegacyBinding(target, repos, groups)
			item.Action, item.Bindings = plan.Action, plan.Bindings
			if !apply || !plan.AutoApply() {
				continue
			}
			if _, err := uc.ReplaceBindings(ctx, a.ID, plan.Bindings, "legacy-migration"); err != nil {
				item.Error = err.Error()
				continue
			}
			item.Applied = true
		}
		if len(agents) < pageSize {
			return items, nil
		}
	}
}

// BindFromLegacyLink keeps the old workspace-link API in sync: when the agent has no
// bindings and target maps exactly to a repo or dir group, the binding is written.
func (uc *RepoRegistryUsecase) BindFromLegacyLink(ctx context.Context, agentID, target, actor string) (bool, error) {
	existing, err := uc.repo.ListAgentBindings(ctx, agentID)
	if err != nil || len(existing) > 0 {
		return false, err
	}
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return false, err
	}
	groups, err := uc.repo.ListGroups(ctx, RepoGroupDir)
	if err != nil {
		return false, err
	}
	plan := planLegacyBinding(target, repos, groups)
	if !plan.AutoApply() {
		return false, nil
	}
	if _, err := uc.ReplaceBindings(ctx, agentID, plan.Bindings, actor); err != nil {
		return false, err
	}
	return true, nil
}
```

`PatchRepo` 中状态改回 `active` 时，可能让该仓库重新进入任意 agent 的有效集，所以重算全部有绑定的 agent；改为 `archived` 只需重算当前包含它的 agent。

`portal/internal/biz/biz.go` 的 `ProviderSet` 追加 `NewRepoRegistryUsecase`。

- [ ] **Step 4: 运行确认通过**

Run: `$env:CGO_ENABLED="1"; go -C portal test ./internal/data/ -run "TestRepoRegistry" -count=1`
Expected: PASS（Windows 无符号链接权限时 `TestRepoRegistryUsecase_MigrateLegacyLinks` 会 SKIP，需在 Linux 上补跑）。

Run: `go -C portal test ./internal/biz/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/repo_registry_usecase.go portal/internal/biz/biz.go portal/internal/data/repo_registry_test.go
git commit -m "feat(portal): repo registry usecase (scan, bindings, legacy migration)"
```

---

## Task 9: chat — `RegistryBuildOptions.RCARoots`

**Files:**
- Modify: `portal/internal/chat/agent_builder.go`
- Modify: `portal/internal/chat/rca_builder.go`
- Test: `portal/internal/chat/rca_builder_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `portal/internal/chat/rca_builder_test.go`（按需补 import `context`、`os`、`path/filepath`、`strings`、`tool`）：

```go
func TestRegisterRCATool_NamedRootsOverrideWorkspace(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "cloudgame", "gateway")
	b := filepath.Join(base, "migu", "gateway")
	for dir, body := range map[string]string{a: "from cloudgame", b: "from migu"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n// "+body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := tool.NewRegistry()
	cfg := map[string]interface{}{"rca": map[string]interface{}{"func_path": "rca_code"}}
	registerRCATool(reg, cfg, "", RegistryBuildOptions{RCARoots: []tool.RCARoot{
		{Name: "cloudgame/gateway", Path: a},
		{Name: "migu/gateway", Path: b},
	}})

	tl, ok := reg.Get("rca_read")
	if !ok {
		t.Fatal("rca_read not registered")
	}
	out, err := tl.Execute(context.Background(), map[string]any{"repo": "cloudgame/gateway", "file": "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := out.(map[string]any)["content"].(string); !strings.Contains(content, "from cloudgame") {
		t.Fatalf("content = %q", content)
	}
}

func TestRegisterRCATool_NamedRootsForSymbol(t *testing.T) {
	reg := tool.NewRegistry()
	cfg := map[string]interface{}{"rca": map[string]interface{}{"func_path": "rca_symbol"}}
	registerRCATool(reg, cfg, "", RegistryBuildOptions{RCARoots: []tool.RCARoot{{Name: "cg/a", Path: t.TempDir()}}})
	if _, ok := reg.Get("rca_symbol"); !ok {
		t.Fatal("rca_symbol not registered with named roots")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go -C portal test ./internal/chat/ -run "TestRegisterRCATool_NamedRoots" -count=1`
Expected: 编译失败，`unknown field RCARoots`。

- [ ] **Step 3: 实现**

`agent_builder.go` 的 `RegistryBuildOptions` 末尾追加：

```go
	// RCARoots are the agent's bound repositories with unique logical names. When
	// non-empty they replace workspace/code and configured rca roots.
	RCARoots []tool.RCARoot
```

（若 `agent_builder.go` 尚未 import `github.com/sixath/framework/tool`，补上。）

`rca_builder.go` 中 `rca_code` / `rca_symbol` 两个分支改为：

```go
	case "rca_code":
		if len(o.RCARoots) > 0 {
			if err := tool.RegisterRCACodeToolsNamed(reg, o.RCARoots); err != nil {
				slog.Warn("rca: rca_code named roots rejected", "err", err)
			}
			return
		}
		roots := MergeRCARoots(workspace, stringSliceFromAny(rcaMap["roots"]))
		if len(roots) == 0 {
			slog.Warn("rca: rca_code has no roots, skip")
			return
		}
		_ = tool.RegisterRCACodeTools(reg, roots)
	case "rca_symbol":
		goplsPath, _ := rcaMap["gopls_path"].(string)
		symOpts := tool.RCASymbolOpts{GoplsPath: goplsPath}
		if readyTimeout, ok := rcaTimeoutSeconds(rcaMap["ready_timeout_sec"]); ok {
			symOpts.ReadyTimeout = readyTimeout
		}
		if requestTimeout, ok := rcaTimeoutSeconds(rcaMap["request_timeout_sec"]); ok {
			symOpts.RequestTimeout = requestTimeout
		}
		if len(o.RCARoots) > 0 {
			if err := tool.RegisterRCASymbolToolNamed(reg, o.RCARoots, symOpts); err != nil {
				slog.Warn("rca: rca_symbol named roots rejected", "err", err)
			}
			return
		}
		roots := MergeRCARoots(workspace, stringSliceFromAny(rcaMap["roots"]))
		if len(roots) == 0 {
			slog.Warn("rca: rca_symbol has no roots, skip")
			return
		}
		_ = tool.RegisterRCASymbolTool(reg, roots, symOpts)
```

（把原来遮蔽外层 `opts` 的局部变量改名为 `symOpts`。）

- [ ] **Step 4: 运行 chat 包全量测试**

Run: `go -C portal test ./internal/chat/ -count=1`
Expected: PASS（老用例不传 `RCARoots`，行为不变）。

- [ ] **Step 5: Commit**

```bash
git add portal/internal/chat/agent_builder.go portal/internal/chat/rca_builder.go portal/internal/chat/rca_builder_test.go
git commit -m "feat(portal): rca tools prefer bound repo roots"
```

---

## Task 10: service — 在构建注册表时注入 RCA 根

**Files:**
- Create: `portal/internal/service/rca_roots.go`
- Create: `portal/internal/service/rca_roots_test.go`
- Modify: `portal/internal/service/chat.go`（字段、setter、第 424、728 行）
- Modify: `portal/internal/service/agent.go`（字段、setter、第 306 行）

- [ ] **Step 1: 写失败测试**

```go
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

type fakeRCARootResolver struct {
	roots []tool.RCARoot
	err   error
	got   string
}

func (f *fakeRCARootResolver) RCARootsForAgent(_ context.Context, agentID string) ([]tool.RCARoot, error) {
	f.got = agentID
	return f.roots, f.err
}

func TestResolveRCARoots(t *testing.T) {
	logger := log.NewHelper(log.DefaultLogger)
	if got := resolveRCARoots(context.Background(), nil, "a", logger); got != nil {
		t.Fatalf("nil resolver: %v", got)
	}
	f := &fakeRCARootResolver{roots: []tool.RCARoot{{Name: "cg/a", Path: "/c/cg/a"}}}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); len(got) != 1 || f.got != "agent-1" {
		t.Fatalf("got %v, agent %q", got, f.got)
	}
	f = &fakeRCARootResolver{err: errors.New("db down")}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); got != nil {
		t.Fatalf("error must fall back to nil, got %v", got)
	}
	if got := resolveRCARoots(context.Background(), f, "", logger); got != nil {
		t.Fatalf("empty agent id: %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go -C portal test ./internal/service/ -run TestResolveRCARoots -count=1`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`portal/internal/service/rca_roots.go`:

```go
package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

// RCARootResolver returns the RCA roots of an agent's bound repositories. An empty result
// means the agent has no repo bindings and the legacy workspace/code link applies.
type RCARootResolver interface {
	RCARootsForAgent(ctx context.Context, agentID string) ([]tool.RCARoot, error)
}

// resolveRCARoots fails open: lookup errors fall back to the legacy roots.
func resolveRCARoots(ctx context.Context, r RCARootResolver, agentID string, logger *log.Helper) []tool.RCARoot {
	if r == nil || agentID == "" {
		return nil
	}
	roots, err := r.RCARootsForAgent(ctx, agentID)
	if err != nil {
		logger.Warnf("resolve rca roots for agent %s: %v", agentID, err)
		return nil
	}
	return roots
}
```

`chat.go`：`ChatService` 结构体加字段 `rcaRoots RCARootResolver`，并在 `SetCodeRoots` 之后加：

```go
// SetRCARootResolver wires repo-binding based RCA roots.
func (s *ChatService) SetRCARootResolver(r RCARootResolver) {
	if s == nil {
		return
	}
	s.rcaRoots = r
}
```

第 424、728 行两处 `chat.RegistryBuildOptions{...}` 都追加一行：

```go
		RCARoots:     resolveRCARoots(ctx, s.rcaRoots, agentMeta.ID, s.log),
```

`agent.go`：`AgentService` 加字段 `rcaRoots RCARootResolver` 与同样的 `SetRCARootResolver`；第 306 行同样追加 `RCARoots`。

> `RCARootResolver` 是接口，nil 指针要传真正的 nil。wire_gen 里传的是非 nil 的 `*biz.RepoRegistryUsecase`，没有 typed-nil 风险。

- [ ] **Step 4: 运行 service 包测试**

Run: `go -C portal test ./internal/service/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/service/rca_roots.go portal/internal/service/rca_roots_test.go portal/internal/service/chat.go portal/internal/service/agent.go
git commit -m "feat(portal): inject bound repo roots into agent tool registry"
```

---

## Task 11: server — HTTP API 与 workspace-link 兼容

**Files:**
- Create: `portal/internal/server/repo_registry.go`
- Modify: `portal/internal/server/code_roots.go`
- Modify: `portal/internal/server/http.go`

- [ ] **Step 1: Handlers**

`portal/internal/server/repo_registry.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// RepoRegistryHandlers serves /api/v1/repos, /api/v1/repo-groups and agent repo bindings.
type RepoRegistryHandlers struct {
	uc      *biz.RepoRegistryUsecase
	agentUC *biz.AgentUsecase
}

func NewRepoRegistryHandlers(uc *biz.RepoRegistryUsecase, agentUC *biz.AgentUsecase) *RepoRegistryHandlers {
	return &RepoRegistryHandlers{uc: uc, agentUC: agentUC}
}

func repoRegistryErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, biz.ErrInvalidRepoBinding), errors.Is(err, biz.ErrInvalidRepoGroup):
		return kratosErrors.BadRequest("INVALID_ARGUMENT", err.Error())
	case errors.Is(err, biz.ErrRepoNotFound):
		return kratosErrors.NotFound("NOT_FOUND", err.Error())
	case errors.Is(err, biz.ErrRepoScanRunning):
		return kratosErrors.Conflict("REPO_SCAN_RUNNING", err.Error())
	default:
		// agentUC.GetForEdit 等返回的错误交给现有中间件编码，保持与 workspace-link 接口一致的状态码。
		return err
	}
}

func decodeJSONBody(ctx kratoshttp.Context, dst any) error {
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "read body failed")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
	}
	return nil
}

func requestActor(ctx kratoshttp.Context) string {
	if v := strings.TrimSpace(ctx.Request().Header.Get("X-User-Email")); v != "" {
		return v
	}
	return "unknown"
}

func (h *RepoRegistryHandlers) serve(ctx kratoshttp.Context, fn func(context.Context) (any, error)) error {
	out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
		v, err := fn(c)
		return v, repoRegistryErr(err)
	})
	if err != nil {
		return err
	}
	return ctx.JSON(200, out)
}

// GET /api/v1/repos?status=&q=&group_id=&code_root=
func (h *RepoRegistryHandlers) ListRepos() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		q := ctx.Query()
		f := biz.RepoFilter{
			Status: strings.TrimSpace(q.Get("status")), Query: strings.TrimSpace(q.Get("q")),
			GroupID: strings.TrimSpace(q.Get("group_id")), CodeRoot: strings.TrimSpace(q.Get("code_root")),
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			items, err := h.uc.ListRepos(c, f)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []*biz.Repository{}
			}
			return map[string]any{"items": items, "total": len(items)}, nil
		})
	}
}

// GET /api/v1/repos/{id}
func (h *RepoRegistryHandlers) GetRepo() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.GetRepo(c, id) })
	}
}

// PATCH /api/v1/repos/{id}
func (h *RepoRegistryHandlers) PatchRepo() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var p biz.RepoMetaPatch
		if err := decodeJSONBody(ctx, &p); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.PatchRepo(c, id, p) })
	}
}

// POST /api/v1/repos/scan
func (h *RepoRegistryHandlers) Scan() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.Scan(c) })
	}
}

// POST /api/v1/repos/migrate-legacy-links?apply=true (default: dry run)
func (h *RepoRegistryHandlers) MigrateLegacyLinks() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		apply := strings.EqualFold(strings.TrimSpace(ctx.Query().Get("apply")), "true")
		return h.serve(ctx, func(c context.Context) (any, error) {
			items, err := h.uc.MigrateLegacyLinks(c, apply)
			if err != nil {
				return nil, err
			}
			return map[string]any{"apply": apply, "items": items}, nil
		})
	}
}

// GET /api/v1/repo-groups?kind=
func (h *RepoRegistryHandlers) ListGroups() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		kind := strings.TrimSpace(ctx.Query().Get("kind"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			items, err := h.uc.ListGroups(c, kind)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []*biz.RepoGroupView{}
			}
			return map[string]any{"items": items}, nil
		})
	}
}

// POST /api/v1/repo-groups {"name":"...","repo_ids":[...]} — manual groups only.
func (h *RepoRegistryHandlers) CreateGroup() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var req struct {
			Name    string   `json:"name"`
			RepoIDs []string `json:"repo_ids"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		actor := requestActor(ctx)
		return h.serve(ctx, func(c context.Context) (any, error) {
			return h.uc.CreateManualGroup(c, req.Name, req.RepoIDs, actor)
		})
	}
}

// PUT /api/v1/repo-groups/{id}/members {"repo_ids":[...]}
func (h *RepoRegistryHandlers) SetGroupMembers() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var req struct {
			RepoIDs []string `json:"repo_ids"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			return map[string]any{"ok": true}, h.uc.SetManualGroupMembers(c, id, req.RepoIDs)
		})
	}
}

// DELETE /api/v1/repo-groups/{id}
func (h *RepoRegistryHandlers) DeleteGroup() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			return map[string]any{"ok": true}, h.uc.DeleteGroup(c, id)
		})
	}
}

// GET /api/v1/agents/{agent_id}/repo-bindings
func (h *RepoRegistryHandlers) GetBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			return h.uc.GetBindings(c, agentID)
		})
	}
}

// PUT /api/v1/agents/{agent_id}/repo-bindings {"bindings":[...]}
func (h *RepoRegistryHandlers) PutBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		var req struct {
			Bindings []*biz.AgentRepoBinding `json:"bindings"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		actor := requestActor(ctx)
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			return h.uc.ReplaceBindings(c, agentID, req.Bindings, actor)
		})
	}
}

// POST /api/v1/agents/{agent_id}/repo-bindings/copy-from/{other_id}
func (h *RepoRegistryHandlers) CopyBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		otherID := strings.TrimSpace(ctx.Vars().Get("other_id"))
		actor := requestActor(ctx)
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			if _, err := h.agentUC.GetForEdit(c, otherID); err != nil {
				return nil, err
			}
			return h.uc.CopyBindings(c, otherID, agentID, actor)
		})
	}
}
```

复制绑定要求对源 agent 也有编辑权限，避免借此读到无权查看的 agent 的仓库配置。如果 `AgentUsecase` 有只读权限检查方法，可替换成只读检查（实现时 `rg "func \(uc \*AgentUsecase\) Get" portal/internal/biz` 确认）。

- [ ] **Step 2: workspace-link 兼容**

`code_roots.go` 中 `AgentWorkspaceLinkHandler` 签名加 `repoUC *biz.RepoRegistryUsecase`，`runWithMiddleware` 回调改为：

```go
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			agent, err := agentUC.GetForEdit(c, agentID)
			if err != nil {
				return nil, err
			}
			res, err := linkWorkspaceCode(agent.Workspace, req.Target, codeRoots)
			if err != nil || repoUC == nil {
				return res, err
			}
			m := res.(map[string]any)
			bound, bindErr := repoUC.BindFromLegacyLink(c, agentID, m["target"].(string), requestActor(ctx))
			if bindErr != nil {
				m["repo_binding_error"] = bindErr.Error()
			}
			m["repo_binding_created"] = bound
			return m, nil
		})
```

- [ ] **Step 3: 路由与签名**

`http.go`：`NewHTTPServer` 参数列表末尾追加 `repoUC *biz.RepoRegistryUsecase`；第 111 行改为 `AgentWorkspaceLinkHandler(agentUC, codeRoots, repoUC)`；在 evolution 路由块之后加：

```go
	repoH := NewRepoRegistryHandlers(repoUC, agentUC)
	r.GET("/api/v1/repos", repoH.ListRepos())
	r.POST("/api/v1/repos/scan", repoH.Scan())
	r.POST("/api/v1/repos/migrate-legacy-links", repoH.MigrateLegacyLinks())
	r.GET("/api/v1/repos/{id}", repoH.GetRepo())
	r.PATCH("/api/v1/repos/{id}", repoH.PatchRepo())
	r.GET("/api/v1/repo-groups", repoH.ListGroups())
	r.POST("/api/v1/repo-groups", repoH.CreateGroup())
	r.PUT("/api/v1/repo-groups/{id}/members", repoH.SetGroupMembers())
	r.DELETE("/api/v1/repo-groups/{id}", repoH.DeleteGroup())
	r.GET("/api/v1/agents/{agent_id}/repo-bindings", repoH.GetBindings())
	r.PUT("/api/v1/agents/{agent_id}/repo-bindings", repoH.PutBindings())
	r.POST("/api/v1/agents/{agent_id}/repo-bindings/copy-from/{other_id}", repoH.CopyBindings())
```

- [ ] **Step 4: 编译 server 包**

Run: `go -C portal build ./internal/server/`
Expected: 成功（`cmd/backend` 在 Task 12 修完前编译失败，属预期）。

- [ ] **Step 5: 进入 Task 12**（Task 11、12 合并为一次提交，保证每次提交都能完整编译）

---

## Task 12: wire_gen 与定时扫描

**Files:**
- Modify: `portal/internal/cron/scheduler.go`
- Modify: `portal/cmd/backend/wire_gen.go`

- [ ] **Step 1: Scheduler**

`scheduler.go`：结构体加字段

```go
	repoUC           *biz.RepoRegistryUsecase
	repoScanInterval time.Duration
```

加常量、setter 和循环：

```go
// DefaultRepoScanInterval is how often code roots are rescanned for repositories.
const DefaultRepoScanInterval = 10 * time.Minute

// SetRepoRegistry enables periodic repository scans.
func (s *Scheduler) SetRepoRegistry(uc *biz.RepoRegistryUsecase, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultRepoScanInterval
	}
	s.repoUC = uc
	s.repoScanInterval = interval
}

// repoScanLoop scans once at startup, then every repoScanInterval.
func (s *Scheduler) repoScanLoop(ctx context.Context) {
	s.runRepoScan(ctx)
	ticker := time.NewTicker(s.repoScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRepoScan(ctx)
		}
	}
}

func (s *Scheduler) runRepoScan(ctx context.Context) {
	rep, err := s.repoUC.Scan(ctx)
	if err != nil {
		s.log.Warnf("repo scan: %v", err)
		return
	}
	s.log.Infof("repo scan: roots=%d found=%d added=%d restored=%d missing=%d errors=%v",
		rep.Roots, rep.Found, rep.Added, rep.Restored, rep.Missing, rep.Errors)
}
```

`Start` 中 `go s.evolutionCleanupLoop(ctx)` 之后加：

```go
	if s.repoUC != nil {
		go s.repoScanLoop(ctx)
	}
```

- [ ] **Step 2: wire_gen.go（手工）**

在第 58 行 `evolutionProposalRepo := ...` 之后插入：

```go
	repoRegistryRepo := data.NewRepoRegistryRepo(dataData, logger)
	repoRegistryUsecase := biz.NewRepoRegistryUsecase(repoRegistryRepo, agentRepo, v, logger)
	agentService.SetRCARootResolver(repoRegistryUsecase)
```

在第 59 行 `chatService := ...` 之后插入：

```go
	chatService.SetRCARootResolver(repoRegistryUsecase)
```

第 83 行 `server.NewHTTPServer(...)` 参数末尾追加 `, repoRegistryUsecase`。

第 86 行 `scheduler.SetEvolutionUsecase(evolutionUsecase)` 之后插入：

```go
	scheduler.SetRepoRegistry(repoRegistryUsecase, cron.DefaultRepoScanInterval)
```

- [ ] **Step 3: 全量编译 + vet**

Run: `go -C portal build ./... ; go -C portal vet ./internal/... ./cmd/backend/`
Expected: 成功。

- [ ] **Step 4: Commit**

```bash
git add portal/internal/server/repo_registry.go portal/internal/server/code_roots.go portal/internal/server/http.go portal/internal/cron/scheduler.go portal/cmd/backend/wire_gen.go
git commit -m "feat(portal): repo registry HTTP API, periodic scan and wiring"
```

---

## Task 13: 全量验证与冒烟

- [ ] **Step 1: framework 全量测试**

Run: `go -C framework test ./... -count=1`
Expected: PASS

- [ ] **Step 2: portal 全量测试**

Run: `$env:CGO_ENABLED="1"; go -C portal test ./... -count=1`
Expected: PASS。Windows 下符号链接相关用例可能 SKIP，需在 Linux（CI 或 dev 容器）补跑 `go -C portal test ./internal/data/ -run TestRepoRegistryUsecase -count=1`。

- [ ] **Step 3: 本地冒烟（dev 环境，portal 已启动且挂载了 /mnt/codes）**

```bash
curl -s -X POST http://localhost:<port>/api/v1/repos/scan -H "Authorization: Bearer <token>"
curl -s "http://localhost:<port>/api/v1/repo-groups?kind=dir" -H "Authorization: Bearer <token>"
curl -s -X POST "http://localhost:<port>/api/v1/repos/migrate-legacy-links" -H "Authorization: Bearer <token>"
```

检查：
- scan 报告的 `found` 与 `/mnt/codes` 下 git 仓库数一致；
- dir 组按一级父目录生成；
- 迁移 dry-run 报告中每个已有 `workspace/code` 的 agent 都有 action，且 `applied=false`。

- [ ] **Step 4: 运行时冒烟**

挑一个测试 agent：
1. `PUT /api/v1/agents/{id}/repo-bindings`，绑定一个 dir 组并排除其中一个仓库；
2. 在 web 发起一次 RCA 对话，确认 `rca_grep` 结果的 `repo` 字段形如 `cloudgame/svc-a`，且被排除仓库不出现；
3. 用 `PUT` 清空绑定，确认该 agent 回到旧的 `workspace/code` 行为。

- [ ] **Step 5: 迁移上线顺序（写进 PR 描述）**

1. 部署后定时扫描自动建立 `repositories` 与 dir 组（启动即扫一次）；
2. 先 `POST /api/v1/repos/migrate-legacy-links` dry-run，核对报告；
3. 再 `?apply=true`，只自动迁移精确命中的 agent；
4. `manual_multi` / `manual_subdir` / `unresolved` 的 agent 等 P1b UI 上线后人工处理，期间保持旧行为。

---

## 自检记录

- 设计 §6.1.1 逻辑名：Task 1–2（framework），Task 6 `buildRCARoots`（portal 命名 = `rel_path`，跨 code root 重名加前缀）。
- 设计 §4.3 展开规则：Task 6 `ExpandRepoBindings`（exclude、missing 剔除、整仓优先于 sub_paths）。
- 设计 §5 扫描：Task 5、8（不下钻嵌套仓库、无 git 可执行文件、只建直接父目录组、code root 不可读不标 missing）。
- 设计 §6.1 三级优先：Task 9（有 `RCARoots` 即覆盖）+ Task 10（为空/出错回落旧逻辑）。
- 设计 §6.3、§14 兼容与迁移：Task 8 `MigrateLegacyLinks` / `BindFromLegacyLink`，Task 11 workspace-link。
- 设计 §11 API：P1 覆盖 repos 列表/详情/PATCH/scan、组列表/创建/成员/删除、绑定 GET/PUT/copy；`handbook/*`、`batch`、`members/confirm` 留到后续期。
