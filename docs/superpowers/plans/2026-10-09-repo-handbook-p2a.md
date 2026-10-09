# 仓库 Handbook P2a（确定性生成）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为每个已登记仓库生成不依赖 LLM 的 handbook Skill（目录分区、Go 包与符号、表/路由/topic/缓存键读写位置），按 agent 的有效仓库集注入 `code-map` 入口与各仓库 handbook，HEAD 变化后随扫描自动重建，并在仓库页展示状态、浏览与手动重建。

**Architecture:** 新包 `portal/internal/handbook` 是纯函数管线（遍历 → Go AST/正则抽取事实 → 渲染 Markdown → 版本化落盘），不访问 DB。`biz.HandbookUsecase` 负责租约、版本号、失败记录和按 agent 组装 skill 目录；`cron` 在每次仓库扫描后触发过期重建；service 层在 `sharedSkillDirs` 末尾追加 handbook 目录（fail-open）。framework `skills` 新增 `hidden_from_summary` / `summary_pinned`，让 30 个仓库 handbook 不挤占摘要、不参与自动路由，只有 `code-map` 置顶进摘要。

**Tech Stack:** Go 1.26（`go/parser`、`regexp`、GORM + MySQL/SQLite 测试）、kratos HTTP、React 19 + TypeScript + Vite、node:test、Playwright。

> **实施后说明：** 评审修复（提交 1f011bc、a76262f、612da25、736a8d3）偏离了下文任务中的代码，以设计文档为准（§7.2"P2a 实现 / P2a 渲染"、§8.2"P2a 实现"、§11、§12）。主要差异：`GeneratorVersion` 为 `p2a-2`；skill 名在 slug 有损或被截断时追加 `-<sha256(rel_path) 前 8 位>`（总长 ≤ 60）；每页（含 `index.md`）≤ 48KB 分页、超大节截断、寄存器名 > 200 字节丢弃；`ReadSkillFile` 按段白名单 + `filepath.IsLocal` + `os.OpenInRoot` 读取并拒绝目录；租约增加 `handbook_lease_token`，Finish/Release 校验 token（`ErrHandbookLeaseLost`），单次构建 25 分钟超时记为失败，父 context 取消时释放租约并恢复原状态，租约过期的 `building` 由 `RebuildStale` 重新认领，全局并发 2（手动重建忙时 409），认领后跳过已归档仓库，`Publish` 写唯一临时目录且从不覆盖已有版本（顺延最多 3 次）；前端构建中仍可重建（409 内联显示）、有活跃构建时每 3 秒轮询、弹窗有加载/错误状态并支持 Esc；设计文档中 `rca_feedback` 迁移改为 `021`。

**设计文档:** `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md` §6.2、§7、§8、§15。P2b（LLM 文件卡片、行为阶段、总览、增量刷新与骨架重建、冻结）另立计划。

---

## 约定（每个任务都适用）

- 工作分支：`main`，每个任务一个提交。**只 `git add` 本任务列出的文件，绝不暂存 `evals/`。**
- Portal 构建与测试必须 `-p 1`，**禁止** `go build ./...`（会 OOM）。涉及 SQLite 的测试（`portal/internal/data`）需要 CGO：

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"
```

- Framework 测试：`cd framework; go test -p 1 ./skills/... ./tool/skillops/...`
- Web：`npm --prefix web test`（node:test，被测模块只能 `import type`）、`npm --prefix web run build`、e2e `cd web; npx playwright test e2e/repo-registry.spec.ts`。
- 已知无关失败：`portal/internal/chat` 的 `TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook`，忽略。

## 范围说明（相对设计文档的取舍）

| 设计条目 | P2a 做法 |
|---|---|
| Phase I 确定性事实 | 做：文件清单 + sha256、Go 包/符号/函数 body 指纹、`go.mod` 直接依赖、寄存器候选（表/路由/topic/缓存键） |
| `.gitignore` | 不解析；固定跳过隐藏目录、`vendor`、`node_modules`、`third_party`、`testdata`、`dist`、`build`、`target`、`out`，以及生成代码、二进制、>512KB 文件 |
| 包级调用图 | 不做（`coverage.graph = "none"`），P2b 再评估 |
| stage | P2a 用**目录分区**（`references/areas/<id>.md`），P2b 的 LLM 行为阶段写到 `references/stages/` |
| 冻结 | 不做。P2a 全量重建很便宜，HEAD 一变就重建；冻结只对 P2b 的 LLM 内容有意义 |
| 并发 | DB 租约 `handbook_lease_until`（migration `020_repo_handbook.sql`），崩溃后租约过期可抢占 |
| 版本 | `handbooks/repos/<id>/v<N>/`，`current.json` 原子切换，保留最近 2 版 |
| code-map | 每个 agent 一份 `handbooks/agents/<agent_id>/code-map/SKILL.md`，按 `path.Dir(rel_path)` 分组，`summary_pinned: true` |
| 保留名 | `skill_manage` 拒绝 `code-map` 与 `handbook-*` |
| UI | 仓库表新增 Handbook 列、查看弹窗、重建按钮 |

## 文件结构

| 操作 | 文件 | 职责 |
|---|---|---|
| MODIFY | `framework/skills/meta.go` | `HiddenFromSummary`、`SummaryPinned`、`VisibleSkills`、`IsReservedSkillName` |
| MODIFY | `framework/skills/index.go` | 解析两个新 frontmatter 字段 |
| MODIFY | `framework/skills/prompt.go` | 摘要过滤隐藏、置顶优先 |
| MODIFY | `framework/skills/route.go`、`embed_route.go` | 路由跳过隐藏 |
| MODIFY | `framework/tool/skillops/skill_tools.go` | `skills_list` 跳过隐藏 |
| MODIFY | `framework/tool/skillops/skill_manager_tool.go` | 保留名拒写 |
| MODIFY | `portal/internal/chat/skills_catalog.go`、`skill_router.go` | 目录提示与路由缓存键跳过隐藏 |
| CREATE | `portal/internal/handbook/facts.go` | 遍历仓库、过滤、哈希、汇总事实 |
| CREATE | `portal/internal/handbook/gosyms.go` | Go 符号、包注释、`TableName()`、`go.mod` |
| CREATE | `portal/internal/handbook/registers.go` | 寄存器候选正则抽取 |
| CREATE | `portal/internal/handbook/render.go` | SKILL.md 与 references 页面、分页 |
| CREATE | `portal/internal/handbook/codemap.go` | agent 的 `code-map` Skill |
| CREATE | `portal/internal/handbook/store.go` | 版本目录、`current.json`、清理、安全读取 |
| CREATE | `portal/internal/handbook/builder.go` | `Build`：事实 + 渲染 + manifest |
| CREATE | `portal/internal/handbook/*_test.go` | 单元测试 |
| CREATE | `portal/migrations/020_repo_handbook.sql` | `handbook_lease_until` 列 |
| MODIFY | `portal/internal/data/model/repo_registry.go` | 模型加租约列 |
| MODIFY | `portal/internal/biz/repo_registry.go` | `Repository` 加 handbook 字段、状态常量、`HandbookBuildResult`、仓储接口两个方法 |
| MODIFY | `portal/internal/data/repo_registry.go` | `ClaimHandbookBuild`、`FinishHandbookBuild`、映射新字段 |
| MODIFY | `portal/internal/biz/repo_registry_usecase.go` | `EffectiveRepositories`、`ResolveRepoPath` |
| CREATE | `portal/internal/biz/handbook.go` | `HandbookUsecase` |
| CREATE | `portal/internal/data/repo_handbook_test.go` | 仓储与 usecase 集成测试（SQLite） |
| MODIFY | `portal/internal/biz/biz.go`、`portal/cmd/backend/wire_gen.go` | DI |
| MODIFY | `portal/internal/cron/scheduler.go` | 扫描后触发重建 |
| CREATE | `portal/internal/service/handbook_dirs.go`（+ test） | `HandbookSkillDirResolver`、`appendHandbookDirs` |
| MODIFY | `portal/internal/service/agent.go`、`chat.go` | 注入 handbook 目录；`ListSkills` 过滤隐藏 |
| MODIFY | `portal/internal/server/repo_registry.go`、`http.go`（+ test） | handbook 三个接口 |
| MODIFY | `web/src/api/repoRegistryTypes.ts`、`api/repoRegistry.ts`、`utils/repoRegistry.ts` | 类型、API、纯函数 |
| CREATE | `web/src/components/HandbookDialog.tsx` | handbook 浏览弹窗 |
| MODIFY | `web/src/pages/RepoListPage.tsx`、`RepoRegistry.css` | Handbook 列与操作 |
| MODIFY | `web/tests/repoRegistry.test.ts`、`web/e2e/repo-registry.spec.ts` | 测试 |
| MODIFY | 设计文档 | 同步 P2a 落地细节 |

---

### Task 1: Skill 元数据 `hidden_from_summary` / `summary_pinned`

**Files:**
- Modify: `framework/skills/meta.go`
- Modify: `framework/skills/index.go:216-271`
- Modify: `framework/skills/prompt.go:10-19`
- Modify: `framework/skills/route.go:38-43`
- Modify: `framework/skills/embed_route.go:51`
- Test: `framework/skills/prompt_test.go`、`index_test.go`、`route_test.go`、`embed_route_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `framework/skills/prompt_test.go`：

```go
func TestBuildSkillsSummary_hiddenAndPinned(t *testing.T) {
	all := []SkillMeta{
		{Name: "a", Description: "A"},
		{Name: "b", Description: "B"},
		{Name: "handbook-x", Description: "H", HiddenFromSummary: true},
		{Name: "code-map", Description: "M", SummaryPinned: true},
	}
	out := BuildSkillsSummary(all, 2)
	if strings.Contains(out, "handbook-x") {
		t.Fatalf("hidden skill listed: %s", out)
	}
	if !strings.Contains(out, "- code-map：M") || !strings.Contains(out, "- a：A") || strings.Contains(out, "- b：B") {
		t.Fatalf("pinned skill must come first within the cap: %s", out)
	}
	if all[0].Name != "a" || all[3].Name != "code-map" {
		t.Fatal("input slice must not be reordered")
	}
}

func TestBuildSkillsSummary_allHidden(t *testing.T) {
	if got := BuildSkillsSummary([]SkillMeta{{Name: "h", HiddenFromSummary: true}}, 8); got != "" {
		t.Fatalf("all hidden: got %q", got)
	}
}
```

追加到 `framework/skills/index_test.go`（文件已有 `package skills` 与 `testing` 导入）：

```go
func TestParseSkillFrontmatter_summaryFlags(t *testing.T) {
	meta, ok, err := parseSkillFrontmatterContent("---\nname: handbook-x\ndescription: d\nhidden_from_summary: true\nsummary_pinned: true\n---\nbody", "SKILL.md")
	if err != nil || !ok {
		t.Fatalf("parse: ok=%v err=%v", ok, err)
	}
	if !meta.HiddenFromSummary || !meta.SummaryPinned {
		t.Fatalf("flags not parsed: %#v", meta)
	}
}

func TestIsReservedSkillName(t *testing.T) {
	for name, want := range map[string]bool{
		"code-map": true, "handbook-cloudgame-svc-a": true, "Handbook-X": true,
		"code-mapper": false, "my-handbook": false, "": false,
	} {
		if got := IsReservedSkillName(name); got != want {
			t.Fatalf("IsReservedSkillName(%q) = %v, want %v", name, got, want)
		}
	}
}
```

追加到 `framework/skills/route_test.go`：

```go
func TestRoute_skipsHidden(t *testing.T) {
	visible := SkillMeta{Name: "archive-move-ops", Description: "archive migration logs"}
	if got := Route("help me with archive-move-ops flow", []SkillMeta{visible}, RouteOptions{}); len(got) != 1 {
		t.Fatalf("control: want 1 match, got %#v", got)
	}
	hidden := visible
	hidden.HiddenFromSummary = true
	if got := Route("help me with archive-move-ops flow", []SkillMeta{hidden}, RouteOptions{}); len(got) != 0 {
		t.Fatalf("hidden skill routed: %#v", got)
	}
}
```

追加到 `framework/skills/embed_route_test.go`：

```go
func TestNewEmbedRouter_skipsHidden(t *testing.T) {
	calls := 0
	idx := testIndexFromMetas(
		SkillMeta{Name: "alpha-skill", Description: "handles alpha", HiddenFromSummary: true},
		SkillMeta{Name: "beta-skill", Description: "handles beta"},
	)
	r, err := NewEmbedRouter(context.Background(), idx, fakeEmbedByKeyword(&calls), 0.5)
	if err != nil || r == nil {
		t.Fatalf("build: err=%v r=%v", err, r)
	}
	if calls != 1 {
		t.Fatalf("hidden skill must not be embedded, embedded %d texts", calls)
	}
	if _, _, ok := r.Route(context.Background(), "please do alpha thing"); ok {
		t.Fatal("hidden skill must not be routed")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework; go test -p 1 ./skills/...`
Expected: 编译失败（`HiddenFromSummary`、`SummaryPinned`、`IsReservedSkillName` 未定义）。

- [ ] **Step 3: 实现**

`framework/skills/meta.go` 中 `SkillMeta` 末尾（`MCPTools` 之后）追加字段，并在文件末尾追加函数（需要 `import "strings"`）：

```go
	// HiddenFromSummary 为 true 时不进系统提示摘要、不参与自动路由和 skills_list，
	// 只能按名字 load_skill / skill_view / read_skill_file（仓库 handbook 使用）。
	HiddenFromSummary bool
	// SummaryPinned 为 true 时在系统提示摘要中排在最前，不会被条数上限截掉。
	SummaryPinned bool
```

```go
// VisibleSkills returns the skills not marked hidden_from_summary, in their original order.
func VisibleSkills(all []SkillMeta) []SkillMeta {
	out := make([]SkillMeta, 0, len(all))
	for _, m := range all {
		if !m.HiddenFromSummary {
			out = append(out, m)
		}
	}
	return out
}

// IsReservedSkillName reports whether name belongs to generated repository handbooks
// ("code-map" and the "handbook-" prefix); user-authored skills must not use it.
func IsReservedSkillName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "code-map" || strings.HasPrefix(n, "handbook-")
}
```

`framework/skills/index.go` 的 `frontmatter` 结构体追加：

```go
		HiddenFromSummary bool        `yaml:"hidden_from_summary"`
		SummaryPinned     bool        `yaml:"summary_pinned"`
```

返回值追加：

```go
		HiddenFromSummary: fm.HiddenFromSummary,
		SummaryPinned:     fm.SummaryPinned,
```

`framework/skills/prompt.go`：`BuildSkillsSummary` 开头改为（加 `import "sort"`）：

```go
func BuildSkillsSummary(all []SkillMeta, maxCount int) string {
	all = VisibleSkills(all)
	if len(all) == 0 {
		return ""
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].SummaryPinned && !all[j].SummaryPinned })
	if maxCount <= 0 {
		maxCount = 8
	}
	if len(all) > maxCount {
		all = all[:maxCount]
	}
```

（`VisibleSkills` 返回新切片，排序不影响调用方。）

`framework/skills/route.go` 的 `Route` 循环开头：

```go
	for _, m := range metas {
		if m.HiddenFromSummary {
			continue
		}
		sc := scoreSkill(q, qTokens, m)
```

`framework/skills/embed_route.go:51`：

```go
	metas := VisibleSkills(idx.All())
```

- [ ] **Step 4: 运行确认通过**

Run: `cd framework; go test -p 1 ./skills/...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/skills/meta.go framework/skills/index.go framework/skills/prompt.go framework/skills/route.go framework/skills/embed_route.go framework/skills/prompt_test.go framework/skills/index_test.go framework/skills/route_test.go framework/skills/embed_route_test.go
git commit -m "feat(skills): hidden_from_summary and summary_pinned frontmatter"
```

---

### Task 2: 隐藏 Skill 的下游过滤与保留名

**Files:**
- Modify: `framework/tool/skillops/skill_tools.go:436`
- Modify: `framework/tool/skillops/skill_manager_tool.go:194,333`
- Modify: `portal/internal/chat/skills_catalog.go:26`
- Modify: `portal/internal/chat/skill_router.go:35,92`
- Test: `framework/tool/skillops/skill_manager_tool_test.go`、`framework/tool/skillops/skill_tools_test.go`（若不存在则新建）、`portal/internal/chat/skills_catalog_test.go`（若不存在则新建）

- [ ] **Step 1: 写失败测试**

追加到 `framework/tool/skillops/skill_manager_tool_test.go`：

```go
func TestSkillManage_RejectsReservedNames(t *testing.T) {
	for _, requireConfirm := range []bool{false, true} {
		root := t.TempDir()
		tl := registerSkillManageForTest(t, skillManageTestConfig(nil, requireConfirm))
		for _, name := range []string{"code-map", "handbook-cloudgame-svc-a"} {
			res, err := tl.Execute(skillManageTestCtx(root), map[string]any{
				"action":  "create",
				"name":    name,
				"content": "---\nname: " + name + "\ndescription: d\n---\n# x",
			})
			if err != nil {
				t.Fatal(err)
			}
			m := res.(map[string]any)
			if m["error"] != "skill_name_reserved" {
				t.Fatalf("confirm=%v name=%s: %#v", requireConfirm, name, m)
			}
			if _, err := os.Stat(filepath.Join(root, "skills", name)); !os.IsNotExist(err) {
				t.Fatalf("reserved skill written to disk: %v", err)
			}
		}
	}
}
```

在 `framework/tool/skillops/` 新建（或追加到已有的）`skill_tools_hidden_test.go`：

```go
package toolskill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sixath/framework/skills"
	core "github.com/sixath/framework/tool"
)

func TestSkillsList_SkipsHidden(t *testing.T) {
	dir := t.TempDir()
	write := func(name, extra string) {
		p := filepath.Join(dir, name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: d\n"+extra+"---\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("visible", "")
	write("handbook-x", "hidden_from_summary: true\n")
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := core.NewRegistry()
	if err := registerSkillsListTool(reg, idx); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("skills_list")
	res, err := tl.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	items := res.(map[string]any)["skills"].([]map[string]any)
	if len(items) != 1 || items[0]["name"] != "visible" {
		t.Fatalf("skills_list = %#v", items)
	}
}
```

在 `portal/internal/chat/` 新建 `skills_catalog_hidden_test.go`：

```go
package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

func TestSkillsCatalogProvider_SkipsHidden(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "handbook-x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nname: handbook-x\ndescription: secret map\nhidden_from_summary: true\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := (&SkillsCatalogProvider{Index: idx}).Enrich(context.Background(), []tool.ToolCatalogEntry{{Name: "load_skill"}})
	if strings.Contains(strings.Join(out[0].SearchHints, " "), "handbook-x") {
		t.Fatalf("hidden skill leaked into hints: %#v", out[0].SearchHints)
	}
}
```

> 若 `tool.ToolCatalogEntry.SearchHints` 不是 `[]string`，按实际类型调整断言（读 `framework/tool` 中该结构体定义）。

- [ ] **Step 2: 运行确认失败**

Run: `cd framework; go test -p 1 ./tool/skillops/ -run "ReservedNames|SkipsHidden"`
Run: `cd portal; go test -p 1 ./internal/chat/ -run SkillsCatalogProvider_SkipsHidden`
Expected: FAIL（保留名被写入、隐藏 skill 被列出）。

- [ ] **Step 3: 实现**

`framework/tool/skillops/skill_tools.go` 的 `skills_list` Execute：

```go
			metas := skills.VisibleSkills(idx.All())
```

`framework/tool/skillops/skill_manager_tool.go`：新增函数，并在 `proposeSkillManage`（`skillManageScanParams` 之后、`IsSkillPinned` 之前）与 `applySkillManage`（`skillManageScanParams` 之后、`if isSkillManageWriteAction(action)` 之前）各调用一次：

```go
// reservedSkillNameResult rejects writes to names owned by generated repository handbooks.
func reservedSkillNameResult(action, name string) map[string]any {
	if !isSkillManageWriteAction(action) || !skills.IsReservedSkillName(name) {
		return nil
	}
	return map[string]any{
		"error": "skill_name_reserved",
		"hint":  "code-map and handbook-* are generated from the repository registry; pick another name",
		"name":  name,
	}
}
```

```go
	if r := reservedSkillNameResult(action, name); r != nil {
		return r, nil
	}
```

`portal/internal/chat/skills_catalog.go:26`：

```go
	metas := skills.VisibleSkills(p.Index.All())
```

`portal/internal/chat/skill_router.go`：`SkillEmbedRouterFor` 开头与 `skillRouterCacheKey` 循环都改用可见列表：

```go
	if idx == nil || len(skills.VisibleSkills(idx.All())) == 0 {
		return nil
	}
```

```go
	for _, m := range skills.VisibleSkills(idx.All()) {
```

- [ ] **Step 4: 运行确认通过**

Run: `cd framework; go test -p 1 ./tool/skillops/...`
Run: `cd portal; go test -p 1 ./internal/chat/ -run "SkillsCatalog|SkillRouter|SkillEmbed"`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/skillops/skill_tools.go framework/tool/skillops/skill_manager_tool.go framework/tool/skillops/skill_manager_tool_test.go framework/tool/skillops/skill_tools_hidden_test.go portal/internal/chat/skills_catalog.go portal/internal/chat/skill_router.go portal/internal/chat/skills_catalog_hidden_test.go
git commit -m "feat(skills): hide handbook skills downstream and reserve handbook names"
```

---

### Task 3: handbook 事实采集（遍历、过滤、哈希）

**Files:**
- Create: `portal/internal/handbook/facts.go`
- Create: `portal/internal/handbook/gosyms.go`（本任务只放 `GoModule`/`parseGoMod`/`Symbol`/`parseGoFile` 的最小桩，Task 4 补全测试）
- Create: `portal/internal/handbook/registers.go`（本任务只放类型与空的 `scanRegisters`，Task 5 实现）
- Test: `portal/internal/handbook/facts_test.go`

为了让 Task 3 可独立编译，先建 `gosyms.go` 和 `registers.go` 的完整版本（下面 Task 4/5 的代码），但 Task 3 只测遍历行为。若按顺序执行，可以直接把 Task 4、5 的实现代码一起放进来，测试仍按任务拆分。

- [ ] **Step 1: 写失败测试**

`portal/internal/handbook/facts_test.go`：

```go
package handbook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const orderStoreGo = `// Package order persists orders.
package order

import "database/sql"

type Store struct{ db *sql.DB }

type Order struct{ ID int64 }

func (Order) TableName() string { return "orders" }

func (s *Store) Get(id int64) error {
	_, err := s.db.Query("SELECT id FROM orders WHERE id = ?", id)
	return err
}

func (s *Store) MarkPaid(id int64) error {
	_, err := s.db.Exec("UPDATE orders SET status = 'paid' WHERE id = ?", id)
	return err
}
`

func sampleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":                       "module example.com/svc\n\ngo 1.22\n\nrequire (\n\tgithub.com/a/b v1.0.0\n\tgithub.com/c/d v1.0.0 // indirect\n)\n",
		"go.sum":                       "ignored\n",
		"cmd/server/main.go":           "package main\n\nfunc main() {}\n",
		"internal/order/store.go":      orderStoreGo,
		"internal/order/store_test.go": "package order\n\nfunc TestX() {}\n",
		"vendor/x/x.go":                "package x\n",
		".github/ci.yml":               "on: push\n",
		"api/pb/order.pb.go":           "// Code generated by protoc-gen-go. DO NOT EDIT.\npackage pb\n",
		"assets/logo.png":              "\x89PNG\x00\x00binary",
		"big.txt":                      strings.Repeat("x", MaxFileBytes+1),
		"README.md":                    "# svc\n",
	})
	return root
}

func TestCollectFacts_FiltersAndHashes(t *testing.T) {
	f, err := CollectFacts(context.Background(), sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range f.Files {
		paths = append(paths, file.Path)
		if len(file.Hash) != 64 {
			t.Fatalf("hash of %s = %q", file.Path, file.Hash)
		}
	}
	want := "README.md,cmd/server/main.go,go.mod,internal/order/store.go,internal/order/store_test.go"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("files = %s\nwant    %s", got, want)
	}
	c := f.Coverage
	if c.SkippedBinary != 1 || c.SkippedGenerated != 1 || c.SkippedLarge != 1 || c.Truncated || c.Graph != "none" {
		t.Fatalf("coverage = %#v", c)
	}
	for _, file := range f.Files {
		if file.Path == "internal/order/store_test.go" && !file.Test {
			t.Fatal("store_test.go should be marked as test")
		}
		if file.Path == "internal/order/store.go" && (file.Lang != "go" || file.Lines != 20) {
			t.Fatalf("store.go = %#v", file)
		}
	}
}

func TestCollectFacts_GoPackagesAndModule(t *testing.T) {
	f, err := CollectFacts(context.Background(), sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Module == nil || f.Module.Path != "example.com/svc" || strings.Join(f.Module.Requires, ",") != "github.com/a/b" {
		t.Fatalf("module = %#v", f.Module)
	}
	if len(f.Packages) != 2 {
		t.Fatalf("packages = %#v", f.Packages)
	}
	main, order := f.Packages[0], f.Packages[1]
	if main.Dir != "cmd/server" || !main.Main || order.Dir != "internal/order" || order.Name != "order" || order.Doc != "Package order persists orders." || order.Files != 1 {
		t.Fatalf("packages = %#v", f.Packages)
	}
	if len(f.Symbols["internal/order/store.go"]) == 0 || len(f.Symbols["internal/order/store_test.go"]) != 0 {
		t.Fatalf("symbols = %#v", f.Symbols)
	}
}

func TestCollectFacts_TruncatesAtMaxFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 5; i++ {
		files[filepath.ToSlash(filepath.Join("d", string(rune('a'+i))+".txt"))] = "x\n"
	}
	writeTree(t, root, files)
	f, err := collectFacts(context.Background(), root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Files) != 3 || !f.Coverage.Truncated {
		t.Fatalf("files=%d truncated=%v", len(f.Files), f.Coverage.Truncated)
	}
}

func TestCollectFacts_MissingRoot(t *testing.T) {
	if _, err := CollectFacts(context.Background(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing root must fail")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: 编译失败（包不存在）。

- [ ] **Step 3: 实现**

`portal/internal/handbook/facts.go`：

```go
// Package handbook builds deterministic repository handbooks: facts gathered by walking the
// repository (file inventory, Go symbols, register candidates) rendered as an agent Skill.
package handbook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// MaxFileBytes skips larger files; they are almost always data or generated code.
	MaxFileBytes = 512 << 10
	// MaxFiles caps the walk so one huge repository cannot stall the rebuild loop.
	MaxFiles   = 20000
	sniffBytes = 8000
)

var skipDirNames = map[string]bool{
	"vendor": true, "node_modules": true, "third_party": true, "testdata": true,
	"dist": true, "build": true, "target": true, "out": true,
}

var skipFileNames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
}

var langByExt = map[string]string{
	".go": "go", ".proto": "proto", ".sql": "sql", ".py": "python", ".java": "java",
	".js": "javascript", ".jsx": "javascript", ".ts": "typescript", ".tsx": "typescript",
	".sh": "shell", ".lua": "lua", ".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp",
	".rs": "rust", ".php": "php", ".rb": "ruby", ".kt": "kotlin", ".cs": "csharp", ".scala": "scala",
	".yaml": "yaml", ".yml": "yaml", ".json": "json", ".toml": "toml", ".xml": "xml",
	".md": "markdown", ".html": "html", ".css": "css", ".vue": "vue",
}

// File is one source file kept in the handbook.
type File struct {
	Path  string `json:"path"` // slash-separated, relative to the repository root
	Lang  string `json:"lang"`
	Size  int64  `json:"size"`
	Lines int    `json:"lines"`
	Hash  string `json:"hash"` // sha256 of the content, hex
	Test  bool   `json:"test,omitempty"`
}

// GoPackage summarizes one Go package directory.
type GoPackage struct {
	Dir   string `json:"dir"`
	Name  string `json:"name"`
	Doc   string `json:"doc,omitempty"`
	Files int    `json:"files"`
	Main  bool   `json:"main,omitempty"`
}

// Coverage records what the walk skipped or could not analyze.
type Coverage struct {
	SkippedLarge     int      `json:"skipped_large"`
	SkippedBinary    int      `json:"skipped_binary"`
	SkippedGenerated int      `json:"skipped_generated"`
	Truncated        bool     `json:"truncated"`
	Unreadable       []string `json:"unreadable,omitempty"`
	GoParseErrors    []string `json:"go_parse_errors,omitempty"`
	Graph            string   `json:"graph"`
}

// Facts are the deterministic facts of one repository checkout.
type Facts struct {
	Files     []File              `json:"files"`
	Packages  []GoPackage         `json:"packages"`
	Symbols   map[string][]Symbol `json:"symbols"`
	Registers []RegisterHit       `json:"registers"`
	Module    *GoModule           `json:"module,omitempty"`
	Coverage  Coverage            `json:"coverage"`
}

// CollectFacts walks root once and gathers the facts of every kept file.
func CollectFacts(ctx context.Context, root string) (*Facts, error) {
	return collectFacts(ctx, root, MaxFiles)
}

func collectFacts(ctx context.Context, root string, maxFiles int) (*Facts, error) {
	f := &Facts{Symbols: map[string][]Symbol{}, Coverage: Coverage{Graph: "none"}}
	pkgs := map[string]*GoPackage{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if err != nil {
			if rel == "." {
				return err
			}
			f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || skipDirNames[d.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") || skipFileNames[d.Name()] {
			return nil
		}
		if len(f.Files) >= maxFiles {
			f.Coverage.Truncated = true
			return fs.SkipAll
		}
		if len(f.Files)%200 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return f.addFile(p, rel, d, pkgs)
	})
	if err != nil {
		return nil, err
	}
	for _, pk := range pkgs {
		f.Packages = append(f.Packages, *pk)
	}
	sort.Slice(f.Packages, func(i, j int) bool { return f.Packages[i].Dir < f.Packages[j].Dir })
	f.Registers = dedupeRegisters(f.Registers)
	sortRegisters(f.Registers)
	return f, nil
}

func (f *Facts) addFile(abs, rel string, d fs.DirEntry, pkgs map[string]*GoPackage) error {
	info, err := d.Info()
	if err != nil {
		f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
		return nil
	}
	if info.Size() > MaxFileBytes {
		f.Coverage.SkippedLarge++
		return nil
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
		return nil
	}
	if bytes.IndexByte(src[:min(len(src), sniffBytes)], 0) >= 0 {
		f.Coverage.SkippedBinary++
		return nil
	}
	if isGenerated(rel, src) {
		f.Coverage.SkippedGenerated++
		return nil
	}
	sum := sha256.Sum256(src)
	file := File{
		Path: rel, Lang: langOf(rel), Size: int64(len(src)), Lines: countLines(src),
		Hash: hex.EncodeToString(sum[:]), Test: isTestFile(rel),
	}
	f.Files = append(f.Files, file)
	if rel == "go.mod" {
		f.Module = parseGoMod(src)
	}
	if file.Test {
		return nil
	}
	if file.Lang == "go" {
		gi, err := parseGoFile(rel, src)
		if err != nil {
			f.Coverage.GoParseErrors = append(f.Coverage.GoParseErrors, rel)
		} else {
			if len(gi.Symbols) > 0 {
				f.Symbols[rel] = gi.Symbols
			}
			f.Registers = append(f.Registers, gi.Tables...)
			dir := path.Dir(rel)
			pk := pkgs[dir]
			if pk == nil {
				pk = &GoPackage{Dir: dir, Name: gi.Package}
				pkgs[dir] = pk
			}
			pk.Files++
			if pk.Doc == "" {
				pk.Doc = gi.Doc
			}
			if gi.Package == "main" {
				pk.Main = true
			}
		}
	}
	f.Registers = append(f.Registers, scanRegisters(rel, file.Lang, src)...)
	return nil
}

func isGenerated(rel string, src []byte) bool {
	base := path.Base(rel)
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_gen.go") ||
		strings.HasPrefix(base, "zz_generated") || strings.HasSuffix(base, ".min.js") {
		return true
	}
	head := src[:min(len(src), 2048)]
	return bytes.Contains(head, []byte("Code generated")) && bytes.Contains(head, []byte("DO NOT EDIT"))
}

func isTestFile(rel string) bool {
	base := path.Base(rel)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_")
}

func langOf(rel string) string {
	if l, ok := langByExt[strings.ToLower(path.Ext(rel))]; ok {
		return l
	}
	return "other"
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
```

同时创建 Task 4 的 `gosyms.go` 与 Task 5 的 `registers.go`（完整代码见对应任务），使包可编译。

- [ ] **Step 4: 运行确认通过**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run CollectFacts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/handbook/facts.go portal/internal/handbook/gosyms.go portal/internal/handbook/registers.go portal/internal/handbook/facts_test.go
git commit -m "feat(handbook): collect deterministic repository facts"
```

---

### Task 4: Go 符号、包注释与 go.mod

**Files:**
- Create/complete: `portal/internal/handbook/gosyms.go`
- Test: `portal/internal/handbook/gosyms_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/handbook/gosyms_test.go`：

```go
package handbook

import (
	"strings"
	"testing"
)

func TestParseGoFile_SymbolsAndTables(t *testing.T) {
	src := `// Package repo stores things. Second sentence.
package repo

type Box[T any] struct{ v T }

func (b *Box[T]) Put(v T) { b.v = v }

func (Row) TableName() string { return "rows" }

type Row struct{}

func helper() int {
	return 1
}
`
	gi, err := parseGoFile("repo/box.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if gi.Package != "repo" || gi.Doc != "Package repo stores things." {
		t.Fatalf("package = %q doc = %q", gi.Package, gi.Doc)
	}
	var names []string
	for _, s := range gi.Symbols {
		names = append(names, s.Kind+":"+s.Name)
	}
	if got := strings.Join(names, ","); got != "type:Box,method:(*Box).Put,method:(Row).TableName,type:Row,func:helper" {
		t.Fatalf("symbols = %s", got)
	}
	h := gi.Symbols[4]
	if h.Line != 12 || h.EndLine != 14 || len(h.BodyHash) != 12 {
		t.Fatalf("helper = %#v", h)
	}
	if len(gi.Tables) != 1 || gi.Tables[0].Name != "rows" || gi.Tables[0].Kind != RegTable || gi.Tables[0].Line != 8 {
		t.Fatalf("tables = %#v", gi.Tables)
	}
}

func TestParseGoFile_SyntaxError(t *testing.T) {
	if _, err := parseGoFile("x.go", []byte("package x\nfunc (")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestParseGoMod(t *testing.T) {
	m := parseGoMod([]byte("module \"example.com/a\"\n\nrequire github.com/x/y v1.0.0\nrequire (\n\t// comment\n\tgithub.com/p/q v0.1.0\n\tgithub.com/z/z v1.0.0 // indirect\n)\n"))
	if m.Path != "example.com/a" || strings.Join(m.Requires, ",") != "github.com/x/y,github.com/p/q" {
		t.Fatalf("mod = %#v", m)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := map[string]string{
		"Package a does x. And y.": "Package a does x.",
		"包 a 处理订单。其余说明。":          "包 a 处理订单。",
		"no period":                "no period",
		strings.Repeat("长", 200):   strings.Repeat("长", 120) + "…",
	}
	for in, want := range cases {
		if got := firstSentence(in); got != want {
			t.Fatalf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "ParseGo|FirstSentence"`
Expected: FAIL（若 Task 3 已放入完整实现则直接 PASS，此时检查断言确实覆盖了实现，再继续）。

- [ ] **Step 3: 实现**

`portal/internal/handbook/gosyms.go`：

```go
package handbook

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// Symbol is one top-level Go declaration.
type Symbol struct {
	Name     string `json:"name"` // "Func", "(*T).Method" or "T"
	Kind     string `json:"kind"` // func | method | type
	Line     int    `json:"line"`
	EndLine  int    `json:"end_line"`
	BodyHash string `json:"body_hash,omitempty"` // first 12 hex chars of sha256 of the declaration
}

// GoModule is the module path and direct requirements of go.mod.
type GoModule struct {
	Path     string   `json:"path"`
	Requires []string `json:"requires,omitempty"`
}

type goFileInfo struct {
	Package string
	Doc     string
	Symbols []Symbol
	Tables  []RegisterHit
}

func parseGoFile(rel string, src []byte) (*goFileInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	info := &goFileInfo{Package: f.Name.Name}
	if f.Doc != nil {
		info.Doc = firstSentence(f.Doc.Text())
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start, end := fset.Position(d.Pos()), fset.Position(d.End())
			s := Symbol{Name: d.Name.Name, Kind: "func", Line: start.Line, EndLine: end.Line, BodyHash: shortHash(src[start.Offset:end.Offset])}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind = "method"
				s.Name = "(" + recvString(d.Recv.List[0].Type) + ")." + d.Name.Name
				if d.Name.Name == "TableName" {
					if lit := returnedStringLiteral(d); lit != "" {
						info.Tables = append(info.Tables, RegisterHit{Kind: RegTable, Name: lit, Access: AccessRef, Path: rel, Line: start.Line})
					}
				}
			}
			info.Symbols = append(info.Symbols, s)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, sp := range d.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				start, end := fset.Position(ts.Pos()), fset.Position(ts.End())
				info.Symbols = append(info.Symbols, Symbol{Name: ts.Name.Name, Kind: "type", Line: start.Line, EndLine: end.Line})
			}
		}
	}
	return info, nil
}

func recvString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + recvString(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvString(t.X)
	case *ast.IndexListExpr:
		return recvString(t.X)
	case *ast.ParenExpr:
		return recvString(t.X)
	}
	return "?"
}

func returnedStringLiteral(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return ""
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// firstSentence collapses whitespace and keeps the first sentence, capped at 120 runes.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if i := strings.Index(s, "。"); i >= 0 {
		s = s[:i+len("。")]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func parseGoMod(src []byte) *GoModule {
	m := &GoModule{}
	inRequire := false
	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "module "):
			m.Path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), `"`)
		case strings.HasPrefix(line, "require") && strings.HasSuffix(line, "("):
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire || strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
			if line == "" || strings.HasPrefix(line, "//") || strings.Contains(line, "// indirect") {
				continue
			}
			if fields := strings.Fields(line); len(fields) > 0 {
				m.Requires = append(m.Requires, fields[0])
			}
		}
	}
	return m
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/handbook/gosyms.go portal/internal/handbook/gosyms_test.go
git commit -m "feat(handbook): go symbols, package docs and go.mod facts"
```

---

### Task 5: 寄存器候选抽取

**Files:**
- Create/complete: `portal/internal/handbook/registers.go`
- Test: `portal/internal/handbook/registers_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/handbook/registers_test.go`：

```go
package handbook

import (
	"fmt"
	"strings"
	"testing"
)

func hitsString(hs []RegisterHit) string {
	var out []string
	for _, h := range hs {
		out = append(out, fmt.Sprintf("%s/%s/%s@%d", h.Kind, h.Name, h.Access, h.Line))
	}
	return strings.Join(out, " ")
}

func TestScanRegisters_GoSource(t *testing.T) {
	src := strings.Join([]string{
		`q := "SELECT o.id FROM orders o JOIN users u ON u.id = o.uid"`, // 1
		`db.Exec("INSERT INTO order_logs (id) VALUES (?)")`,             // 2
		`db.Exec("UPDATE orders SET status = ?")`,                       // 3
		`db.Exec("DELETE FROM carts WHERE id = ?")`,                     // 4
		`db.Table("coupons").Where("id = ?", id)`,                       // 5
		`r.GET("/api/v1/orders/:id", h.Get)`,                            // 6
		`mux.HandleFunc("/healthz", health)`,                            // 7
		`const OrderTopic = "order-events"`,                             // 8
		`rdb.Set(ctx, fmt.Sprintf("order:detail:%d", id), v, ttl)`,     // 9
		`val, _ := rdb.Get(ctx, "order:detail:"+id).Result()`,          // 10
		`log.Printf("read from file")`,                                  // 11
		`url := "http://example.com:8080/x"`,                            // 12
		`layout := "15:04:05"`,                                          // 13
	}, "\n")
	got := hitsString(dedupeRegisters(scanRegisters("svc/order.go", "go", []byte(src))))
	want := "table/orders/read@1 table/users/read@1 table/order_logs/write@2 table/orders/write@3 table/carts/write@4 table/coupons/ref@5 " +
		"route//api/v1/orders/:id/serve@6 route//healthz/serve@7 topic/order-events/ref@8 " +
		"cache_key/order:detail:/write@9 cache_key/order:detail:/read@10"
	if got != want {
		t.Fatalf("hits:\n got %s\nwant %s", got, want)
	}
}

func TestScanRegisters_SQLProtoYAML(t *testing.T) {
	sql := "select * from payments;\ninsert into refunds values (1);\n"
	if got := hitsString(scanRegisters("db/q.sql", "sql", []byte(sql))); got != "table/payments/read@1 table/refunds/write@2" {
		t.Fatalf("sql: %s", got)
	}
	proto := "option (google.api.http) = { get: \"/v1/orders/{id}\" };\n"
	if got := hitsString(scanRegisters("api/o.proto", "proto", []byte(proto))); got != "route//v1/orders/{id}/serve@1" {
		t.Fatalf("proto: %s", got)
	}
	yaml := "kafka:\n  order_topic: order-events\n  brokers: a:9092\n"
	if got := hitsString(scanRegisters("conf/app.yaml", "yaml", []byte(yaml))); got != "topic/order-events/ref@2" {
		t.Fatalf("yaml: %s", got)
	}
	if got := scanRegisters("README.md", "markdown", []byte("SELECT * FROM orders")); len(got) != 0 {
		t.Fatalf("markdown must not be scanned: %v", got)
	}
}

func TestSortRegisters_WritesFirst(t *testing.T) {
	hs := []RegisterHit{
		{Kind: RegTable, Name: "orders", Access: AccessRead, Path: "a.go", Line: 1},
		{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "z.go", Line: 9},
		{Kind: RegRoute, Name: "/a", Access: AccessServe, Path: "r.go", Line: 1},
	}
	sortRegisters(hs)
	if got := hitsString(hs); got != "route//a/serve@1 table/orders/write@9 table/orders/read@1" {
		t.Fatalf("sorted: %s", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "Registers"`
Expected: FAIL（若 `registers.go` 仍是桩）。

- [ ] **Step 3: 实现**

`portal/internal/handbook/registers.go`：

```go
package handbook

import (
	"regexp"
	"sort"
	"strings"
)

const (
	RegTable    = "table"
	RegRoute    = "route"
	RegTopic    = "topic"
	RegCacheKey = "cache_key"

	AccessWrite = "write"
	AccessServe = "serve"
	AccessRead  = "read"
	AccessRef   = "ref"
)

// RegisterHit is one literal reference to shared state (table, route, topic, cache key).
type RegisterHit struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Access string `json:"access"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

type sqlPatterns struct{ insert, update, del, read *regexp.Regexp }

const sqlIdent = "`?([A-Za-z_][A-Za-z0-9_.]*)`?"

func compileSQL(flags string) sqlPatterns {
	return sqlPatterns{
		insert: regexp.MustCompile(flags + `\bINSERT\s+(?:IGNORE\s+)?INTO\s+` + sqlIdent),
		update: regexp.MustCompile(flags + `\bUPDATE\s+` + sqlIdent + `\s+SET\b`),
		del:    regexp.MustCompile(flags + `\bDELETE\s+FROM\s+` + sqlIdent),
		read:   regexp.MustCompile(flags + `\b(?:FROM|JOIN)\s+` + sqlIdent),
	}
}

var (
	// Source code only matches upper-case SQL keywords; English "from" in strings is common.
	sqlUpper   = compileSQL("")
	sqlAnyCase = compileSQL("(?i)")

	gormTableRe  = regexp.MustCompile(`\.Table\(\s*"([A-Za-z_][A-Za-z0-9_]*)"`)
	routeRe      = regexp.MustCompile(`\.(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any|Handle|HandleFunc)\(\s*"(/[^"]*)"`)
	protoRouteRe = regexp.MustCompile(`\b(?:get|post|put|delete|patch)\s*:\s*"(/[^"]+)"`)
	topicRe      = regexp.MustCompile(`(?i)\b\w*topic\w*"?\s*(?::=|=|:)\s*"([A-Za-z0-9][\w.\-]*)"`)
	yamlTopicRe  = regexp.MustCompile(`(?i)^\s*\w*topic\w*\s*:\s*["']?([A-Za-z0-9][\w.\-]*)["']?\s*$`)
	cacheKeyRe   = regexp.MustCompile(`"([A-Za-z][\w\-]*:[\w\-:%{}]*)"`)
	cacheCallRe  = regexp.MustCompile(`\.(\w+)\(`)
	cacheCtxRe   = regexp.MustCompile(`(?i)redis|cache|rdb|\bkey`)
)

var codeLangs = setOf("go", "java", "python", "javascript", "typescript", "php", "ruby", "kotlin", "csharp", "rust", "lua", "c", "cpp", "scala")

var cacheWrites = setOf("Set", "SetNX", "SetEX", "SetEx", "MSet", "Del", "Unlink", "HSet", "HMSet", "HDel", "HIncrBy",
	"Incr", "IncrBy", "Decr", "DecrBy", "Expire", "ExpireAt", "LPush", "RPush", "LPop", "RPop", "SAdd", "SRem",
	"ZAdd", "ZRem", "ZIncrBy", "Append", "GetSet", "GetDel")

var cacheReads = setOf("Get", "MGet", "HGet", "HGetAll", "HMGet", "HExists", "Exists", "TTL", "PTTL", "LRange", "LLen",
	"SMembers", "SIsMember", "SCard", "ZRange", "ZRangeByScore", "ZRevRange", "ZScore", "ZCard", "Scan", "Keys")

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func scanRegisters(rel, lang string, src []byte) []RegisterHit {
	var out []RegisterHit
	for i, line := range strings.Split(string(src), "\n") {
		n := i + 1
		add := func(kind, name, access string) {
			out = append(out, RegisterHit{Kind: kind, Name: name, Access: access, Path: rel, Line: n})
		}
		switch {
		case lang == "sql":
			scanSQL(sqlAnyCase, line, add)
		case lang == "proto":
			for _, m := range protoRouteRe.FindAllStringSubmatch(line, -1) {
				add(RegRoute, m[1], AccessServe)
			}
		case lang == "yaml":
			if m := yamlTopicRe.FindStringSubmatch(line); m != nil {
				add(RegTopic, m[1], AccessRef)
			}
		case codeLangs[lang]:
			scanSQL(sqlUpper, line, add)
			for _, m := range gormTableRe.FindAllStringSubmatch(line, -1) {
				add(RegTable, m[1], AccessRef)
			}
			for _, m := range routeRe.FindAllStringSubmatch(line, -1) {
				add(RegRoute, m[1], AccessServe)
			}
			for _, m := range topicRe.FindAllStringSubmatch(line, -1) {
				add(RegTopic, m[1], AccessRef)
			}
			scanCacheKeys(line, add)
		}
	}
	return out
}

// scanSQL records writes first and blanks them out so "DELETE FROM t" is not also a read.
func scanSQL(p sqlPatterns, line string, add func(kind, name, access string)) {
	for _, re := range []*regexp.Regexp{p.insert, p.update, p.del} {
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			add(RegTable, m[1], AccessWrite)
		}
		line = re.ReplaceAllString(line, " ")
	}
	for _, m := range p.read.FindAllStringSubmatch(line, -1) {
		add(RegTable, m[1], AccessRead)
	}
}

func scanCacheKeys(line string, add func(kind, name, access string)) {
	keys := cacheKeyRe.FindAllStringSubmatch(line, -1)
	if len(keys) == 0 || !cacheCtxRe.MatchString(line) {
		return
	}
	access := AccessRef
	for _, m := range cacheCallRe.FindAllStringSubmatch(line, -1) {
		if cacheWrites[m[1]] {
			access = AccessWrite
			break
		}
		if cacheReads[m[1]] {
			access = AccessRead
		}
	}
	for _, m := range keys {
		name := m[1]
		if i := strings.IndexAny(name, "%{"); i >= 0 {
			name = name[:i]
		}
		if strings.Contains(name, ":") {
			add(RegCacheKey, name, access)
		}
	}
}

var accessRank = map[string]int{AccessWrite: 0, AccessServe: 1, AccessRead: 2, AccessRef: 3}

// sortRegisters orders by kind and name, then writes before reads so renderers keep writers.
func sortRegisters(hs []RegisterHit) {
	sort.Slice(hs, func(i, j int) bool {
		a, b := hs[i], hs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if accessRank[a.Access] != accessRank[b.Access] {
			return accessRank[a.Access] < accessRank[b.Access]
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
}

func dedupeRegisters(hs []RegisterHit) []RegisterHit {
	seen := make(map[RegisterHit]bool, len(hs))
	out := hs[:0]
	for _, h := range hs {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
```

> 若测试 `TestScanRegisters_GoSource` 的期望顺序与实现的行内顺序不一致（同一行内按正则调用顺序：SQL 写 → SQL 读 → `.Table` → 路由 → topic → 缓存），以实现顺序为准修正**期望字符串**，但不得删掉任何一条期望命中，也不得出现第 11–13 行的命中。

- [ ] **Step 4: 运行确认通过**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/handbook/registers.go portal/internal/handbook/registers_test.go
git commit -m "feat(handbook): extract table/route/topic/cache-key register candidates"
```

---

### Task 6: 渲染 Skill 页面

**Files:**
- Create: `portal/internal/handbook/render.go`
- Test: `portal/internal/handbook/render_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/handbook/render_test.go`：

```go
package handbook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/skills"
)

func renderSample(t *testing.T) map[string]string {
	t.Helper()
	f, err := CollectFacts(context.Background(), sampleRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	return Render(RenderMeta{RelPath: "cloudgame/svc-a", Commit: "0123456789abcdef", GeneratedAt: time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)}, f)
}

func TestRender_PagesAndContent(t *testing.T) {
	pages := renderSample(t)
	for _, p := range []string{"SKILL.md", "references/overview.md", "references/index.md", "references/registers.md",
		"references/areas/root.md", "references/areas/cmd-server.md", "references/areas/internal-order.md"} {
		if _, ok := pages[p]; !ok {
			t.Fatalf("missing page %s; have %v", p, keysOf(pages))
		}
	}
	checks := map[string][]string{
		"references/overview.md":            {"cloudgame/svc-a 概览", "`0123456789ab`", "Go module `example.com/svc`", "`cmd/server`", "`github.com/a/b`", "数据表 1 个"},
		"references/index.md":               {"| internal/order | 2 | `references/areas/internal-order.md` |", "`internal/order`（package order，1 个文件）：Package order persists orders."},
		"references/registers.md":           {"### `orders`", "- 写 `internal/order/store.go:18`", "- 读 `internal/order/store.go:13`", "- 引用 `internal/order/store.go:10`"},
		"references/areas/internal-order.md": {"## `internal/order`（package order）", "- `internal/order/store.go`（go，20 行）", "  - method `(*Store).MarkPaid` L17-20", "，测试）"},
	}
	for page, needles := range checks {
		for _, n := range needles {
			if !strings.Contains(pages[page], n) {
				t.Fatalf("%s missing %q:\n%s", page, n, pages[page])
			}
		}
	}
}

func TestRender_SkillFrontmatterParses(t *testing.T) {
	pages := renderSample(t)
	dir := filepath.Join(t.TempDir(), "skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(pages["SKILL.md"]), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := idx.GetByName("handbook-cloudgame-svc-a")
	if !ok || !m.HiddenFromSummary || !strings.Contains(m.Description, "cloudgame/svc-a 的代码地图") {
		t.Fatalf("meta = %#v ok=%v", m, ok)
	}
}

func TestPaginate(t *testing.T) {
	big := strings.Repeat("x", MaxPageBytes/2+1)
	pages := paginate("references/areas/a", "# 分区 a", []string{big, big, big})
	if len(pages) != 3 {
		t.Fatalf("pages = %v", keysOf(pages))
	}
	first := pages["references/areas/a.md"]
	if !strings.Contains(first, "（第 1/3 页）") || !strings.Contains(first, "`references/areas/a.p2.md`、`references/areas/a.p3.md`") {
		t.Fatalf("first page header:\n%s", first[:200])
	}
	if empty := paginate("references/registers", "# 寄存器", nil); !strings.Contains(empty["references/registers.md"], "（无）") {
		t.Fatalf("empty = %v", empty)
	}
}

func TestSlugAndSkillName(t *testing.T) {
	cases := map[string]string{"cloudgame/svc-a": "handbook-cloudgame-svc-a", "Infra/Common_Lib": "handbook-infra-common-lib", "///": "handbook-root"}
	for in, want := range cases {
		if got := SkillName(in); got != want {
			t.Fatalf("SkillName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := areaOfDir("internal/order/sub"); got != "internal/order" {
		t.Fatalf("areaOfDir = %q", got)
	}
	if got := areaOfDir("."); got != "(root)" {
		t.Fatalf("areaOfDir(.) = %q", got)
	}
	if got := areaOfDir("docs/a"); got != "docs" {
		t.Fatalf("areaOfDir(docs/a) = %q", got)
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

> 行号对应 Task 3 的 `orderStoreGo`：TableName 第 10 行，SELECT 第 13 行，`MarkPaid` 第 17–20 行（UPDATE 在第 18 行）。

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "Render|Paginate|Slug"`
Expected: 编译失败（`Render` 未定义）。

- [ ] **Step 3: 实现**

`portal/internal/handbook/render.go`：

```go
package handbook

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxPageBytes keeps each page small enough to read in one read_skill_file call.
const MaxPageBytes = 48 << 10

const (
	maxSymbolsPerFile  = 60
	maxPackagesPerArea = 50
	maxLocationsPerReg = 30
	maxRequires        = 40
)

// RenderMeta identifies the repository and commit a handbook was built from.
type RenderMeta struct {
	RelPath     string
	Commit      string
	GeneratedAt time.Time
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "root"
	}
	return s
}

// SkillName is the handbook skill name for a repository logical name (rel_path).
func SkillName(relPath string) string { return "handbook-" + slug(relPath) }

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

var areaPrefixes = map[string]bool{
	"internal": true, "pkg": true, "cmd": true, "app": true, "apps": true,
	"src": true, "service": true, "services": true, "api": true,
}

// areaOfDir groups directories by their first segment, or first two under common prefixes.
func areaOfDir(dir string) string {
	if dir == "." || dir == "" {
		return "(root)"
	}
	parts := strings.Split(dir, "/")
	if areaPrefixes[parts[0]] && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

type area struct {
	Name  string
	ID    string
	Files []File
}

func buildAreas(f *Facts) []area {
	byName := map[string]*area{}
	var names []string
	for _, file := range f.Files {
		n := areaOfDir(path.Dir(file.Path))
		a := byName[n]
		if a == nil {
			a = &area{Name: n}
			byName[n] = a
			names = append(names, n)
		}
		a.Files = append(a.Files, file)
	}
	sort.Strings(names)
	used := map[string]int{}
	out := make([]area, 0, len(names))
	for _, n := range names {
		a := byName[n]
		id := slug(n)
		used[id]++
		if used[id] > 1 {
			id = fmt.Sprintf("%s-%d", id, used[id])
		}
		a.ID = id
		sort.SliceStable(a.Files, func(i, j int) bool {
			di, dj := path.Dir(a.Files[i].Path), path.Dir(a.Files[j].Path)
			if di != dj {
				return di < dj
			}
			return a.Files[i].Path < a.Files[j].Path
		})
		out = append(out, *a)
	}
	return out
}

// Render returns the skill pages keyed by path relative to the skill directory.
func Render(meta RenderMeta, f *Facts) map[string]string {
	areas := buildAreas(f)
	pages := map[string]string{}
	for p, c := range paginate("references/registers", "# "+meta.RelPath+" 寄存器", registerSections(f.Registers)) {
		pages[p] = c
	}
	for _, a := range areas {
		for p, c := range paginate("references/areas/"+a.ID, "# 分区 "+a.Name, areaSections(a, f)) {
			pages[p] = c
		}
	}
	pages["references/index.md"] = renderIndex(meta, areas, f)
	pages["references/overview.md"] = renderOverview(meta, f, areas)
	pages["SKILL.md"] = renderSkill(meta)
	return pages
}

// paginate splits sections into pages of at most MaxPageBytes without splitting a section.
// The first page is <base>.md and lists continuation pages <base>.p2.md, <base>.p3.md, ...
func paginate(base, title string, sections []string) map[string]string {
	var chunks [][]string
	var cur []string
	size := 0
	for _, s := range sections {
		if len(cur) > 0 && size+len(s) > MaxPageBytes {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		cur = append(cur, s)
		size += len(s)
	}
	if len(cur) > 0 || len(chunks) == 0 {
		chunks = append(chunks, cur)
	}
	name := func(i int) string {
		if i == 0 {
			return base + ".md"
		}
		return fmt.Sprintf("%s.p%d.md", base, i+1)
	}
	out := make(map[string]string, len(chunks))
	for i, c := range chunks {
		var b strings.Builder
		b.WriteString(title)
		if len(chunks) > 1 {
			fmt.Fprintf(&b, "（第 %d/%d 页）", i+1, len(chunks))
		}
		b.WriteString("\n\n")
		if i == 0 && len(chunks) > 1 {
			b.WriteString("续页：")
			for j := 1; j < len(chunks); j++ {
				if j > 1 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", name(j))
			}
			b.WriteString("\n\n")
		}
		if len(c) == 0 {
			b.WriteString("（无）\n")
		}
		for _, s := range c {
			b.WriteString(s)
		}
		out[name(i)] = b.String()
	}
	return out
}

var registerKinds = []struct{ kind, title string }{
	{RegTable, "数据表"}, {RegRoute, "HTTP 路由"}, {RegTopic, "MQ topic"}, {RegCacheKey, "缓存键前缀"},
}

var accessLabels = map[string]string{AccessRead: "读", AccessWrite: "写", AccessRef: "引用", AccessServe: "提供"}

// registerSections expects hits sorted by sortRegisters (writes first within a name).
func registerSections(hits []RegisterHit) []string {
	var out []string
	for _, k := range registerKinds {
		byName := map[string][]RegisterHit{}
		var names []string
		for _, h := range hits {
			if h.Kind != k.kind {
				continue
			}
			if _, ok := byName[h.Name]; !ok {
				names = append(names, h.Name)
			}
			byName[h.Name] = append(byName[h.Name], h)
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf("## %s（%d 个）\n\n", k.title, len(names)))
		for _, n := range names {
			hs := byName[n]
			var b strings.Builder
			fmt.Fprintf(&b, "### `%s`\n\n", n)
			for i, h := range hs {
				if i == maxLocationsPerReg {
					fmt.Fprintf(&b, "- …另有 %d 处，用 rca_grep 搜索 `%s`\n", len(hs)-i, n)
					break
				}
				fmt.Fprintf(&b, "- %s `%s:%d`\n", accessLabels[h.Access], h.Path, h.Line)
			}
			b.WriteString("\n")
			out = append(out, b.String())
		}
	}
	return out
}

func areaSections(a area, f *Facts) []string {
	pkgs := map[string]GoPackage{}
	for _, p := range f.Packages {
		pkgs[p.Dir] = p
	}
	var out []string
	lastDir := ""
	for _, file := range a.Files {
		var b strings.Builder
		if dir := path.Dir(file.Path); dir != lastDir {
			lastDir = dir
			fmt.Fprintf(&b, "## `%s`", dir)
			if p, ok := pkgs[dir]; ok {
				fmt.Fprintf(&b, "（package %s）", p.Name)
				if p.Doc != "" {
					b.WriteString("\n\n" + p.Doc)
				}
			}
			b.WriteString("\n\n")
		}
		mark := ""
		if file.Test {
			mark = "，测试"
		}
		fmt.Fprintf(&b, "- `%s`（%s，%d 行%s）\n", file.Path, file.Lang, file.Lines, mark)
		syms := f.Symbols[file.Path]
		for i, s := range syms {
			if i == maxSymbolsPerFile {
				fmt.Fprintf(&b, "  - …另有 %d 个符号\n", len(syms)-i)
				break
			}
			fmt.Fprintf(&b, "  - %s `%s` L%d-%d\n", s.Kind, s.Name, s.Line, s.EndLine)
		}
		out = append(out, b.String())
	}
	return out
}

func renderIndex(meta RenderMeta, areas []area, f *Facts) string {
	pkgsByArea := map[string][]GoPackage{}
	for _, p := range f.Packages {
		n := areaOfDir(p.Dir)
		pkgsByArea[n] = append(pkgsByArea[n], p)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 分区索引\n\n按目录分区。分区页列出文件和符号；表、路由、topic、缓存键见 `references/registers.md`。\n\n", meta.RelPath)
	b.WriteString("| 分区 | 文件数 | 页面 |\n|---|---|---|\n")
	for _, a := range areas {
		fmt.Fprintf(&b, "| %s | %d | `references/areas/%s.md` |\n", a.Name, len(a.Files), a.ID)
	}
	for _, a := range areas {
		pk := pkgsByArea[a.Name]
		if len(pk) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", a.Name)
		for i, p := range pk {
			if i == maxPackagesPerArea {
				fmt.Fprintf(&b, "- …另有 %d 个包，见分区页\n", len(pk)-i)
				break
			}
			fmt.Fprintf(&b, "- `%s`（package %s，%d 个文件）", p.Dir, p.Name, p.Files)
			if p.Doc != "" {
				b.WriteString("：" + p.Doc)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderOverview(meta RenderMeta, f *Facts, areas []area) string {
	langs := map[string]int{}
	tests := 0
	for _, file := range f.Files {
		langs[file.Lang]++
		if file.Test {
			tests++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 概览\n\n", meta.RelPath)
	fmt.Fprintf(&b, "- commit `%s`，生成于 %s（静态分析，无 LLM）\n", shortCommit(meta.Commit), meta.GeneratedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- 文件 %d 个（测试 %d），Go 包 %d 个，分区 %d 个\n", len(f.Files), tests, len(f.Packages), len(areas))
	if f.Module != nil && f.Module.Path != "" {
		fmt.Fprintf(&b, "- Go module `%s`\n", f.Module.Path)
	}
	b.WriteString("\n## 语言\n\n| 语言 | 文件数 |\n|---|---|\n")
	for _, l := range keysByCount(langs) {
		fmt.Fprintf(&b, "| %s | %d |\n", l, langs[l])
	}
	var mains []string
	for _, p := range f.Packages {
		if p.Main {
			mains = append(mains, p.Dir)
		}
	}
	if len(mains) > 0 {
		b.WriteString("\n## 入口（package main）\n\n")
		for _, m := range mains {
			fmt.Fprintf(&b, "- `%s`\n", m)
		}
	}
	if f.Module != nil && len(f.Module.Requires) > 0 {
		b.WriteString("\n## 直接依赖（go.mod）\n\n")
		for i, r := range f.Module.Requires {
			if i == maxRequires {
				fmt.Fprintf(&b, "- …另有 %d 个\n", len(f.Module.Requires)-i)
				break
			}
			fmt.Fprintf(&b, "- `%s`\n", r)
		}
	}
	names := map[string]map[string]bool{}
	for _, h := range f.Registers {
		if names[h.Kind] == nil {
			names[h.Kind] = map[string]bool{}
		}
		names[h.Kind][h.Name] = true
	}
	b.WriteString("\n## 寄存器\n\n")
	for _, k := range registerKinds {
		fmt.Fprintf(&b, "- %s %d 个\n", k.title, len(names[k.kind]))
	}
	b.WriteString("\n详见 `references/registers.md`。\n\n## 覆盖说明\n\n")
	c := f.Coverage
	fmt.Fprintf(&b, "- 跳过：大文件 %d，二进制 %d，生成代码 %d；不读 vendor、node_modules、隐藏目录等\n", c.SkippedLarge, c.SkippedBinary, c.SkippedGenerated)
	if c.Truncated {
		fmt.Fprintf(&b, "- 文件数超过 %d，清单已截断\n", MaxFiles)
	}
	if len(c.GoParseErrors) > 0 {
		fmt.Fprintf(&b, "- %d 个 Go 文件解析失败，未列符号\n", len(c.GoParseErrors))
	}
	b.WriteString("- 不含调用图；非 Go 文件只列清单和寄存器\n")
	return b.String()
}

func keysByCount(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

func renderSkill(meta RenderMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: >-\n  %s 的代码地图（静态生成，commit %s）：目录分区、Go 包与符号、数据表/路由/topic/缓存键的读写位置。RCA 定位时按需下钻，结论以 rca_read 读到的源码为准。\nhidden_from_summary: true\n---\n\n",
		SkillName(meta.RelPath), meta.RelPath, shortCommit(meta.Commit))
	fmt.Fprintf(&b, "# %s Handbook\n\n仓库逻辑名 `%s`（rca_* 工具的 repo 参数）。commit `%s`，生成于 %s。\n\n",
		meta.RelPath, meta.RelPath, shortCommit(meta.Commit), meta.GeneratedAt.Format("2006-01-02 15:04"))
	b.WriteString("## 文件\n\n" +
		"- `references/overview.md` —— 语言、规模、入口、直接依赖、覆盖说明。先读。\n" +
		"- `references/index.md` —— 目录分区及其中的 Go 包和包说明。\n" +
		"- `references/registers.md` —— 数据表、HTTP 路由、MQ topic、缓存键前缀的读写位置（path:line）。\n" +
		"- `references/areas/<分区>.md` —— 分区内每个文件的函数、方法、类型及行号。\n\n" +
		"页面过长时拆成续页（`*.p2.md` …），首页列出续页。用 `read_skill_file` 读取上述路径。\n\n")
	fmt.Fprintf(&b, "## 用法（RCA）\n\n"+
		"1. 读 overview 和 index，确定与现象相关的分区和寄存器，不要过早收窄到一个分区。\n"+
		"2. 涉及共享状态（表、缓存、topic、接口）时读 registers.md，记下所有写入点——异常状态往往在远处被写坏。\n"+
		"3. 打开相关分区页找候选文件和函数。\n"+
		"4. 用 `rca_read`（repo=`%s`）读真实源码确认。handbook 可能落后于代码，以源码为准；找不到时改用 `rca_grep`。\n", meta.RelPath)
	return b.String()
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/handbook/render.go portal/internal/handbook/render_test.go
git commit -m "feat(handbook): render handbook skill pages"
```

---

### Task 7: code-map、版本存储与 Build

**Files:**
- Create: `portal/internal/handbook/codemap.go`
- Create: `portal/internal/handbook/store.go`
- Create: `portal/internal/handbook/builder.go`
- Test: `portal/internal/handbook/codemap_test.go`、`store_test.go`、`builder_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/handbook/codemap_test.go`：

```go
package handbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/skills"
)

func TestRenderCodeMap(t *testing.T) {
	out := RenderCodeMap([]CodeMapEntry{
		{RelPath: "cloudgame/svc-a", Description: "网关\n第二行", SkillName: "handbook-cloudgame-svc-a"},
		{RelPath: "cloudgame/svc-b", SubPaths: []string{"internal/x"}},
		{RelPath: "infra"},
	})
	for _, n := range []string{
		"## cloudgame（2 个）",
		"- `cloudgame/svc-a`：网关 —— skill_view(\"handbook-cloudgame-svc-a\")",
		"- `cloudgame/svc-b`（仅子目录，repo 参数用 `cloudgame/svc-b/internal/x`） —— 暂无 handbook，直接用 rca_grep",
		"## （顶层）（1 个）",
	} {
		if !strings.Contains(out, n) {
			t.Fatalf("missing %q:\n%s", n, out)
		}
	}
	dir := filepath.Join(t.TempDir(), "code-map")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := skills.NewIndex([]string{dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := idx.GetByName(CodeMapSkillName)
	if !ok || !m.SummaryPinned || m.HiddenFromSummary || !strings.Contains(m.Description, "3 个代码仓库") {
		t.Fatalf("meta = %#v ok=%v", m, ok)
	}
	if empty := RenderCodeMap(nil); !strings.Contains(empty, "当前没有可访问的仓库") {
		t.Fatalf("empty code-map:\n%s", empty)
	}
}
```

`portal/internal/handbook/store_test.go`：

```go
package handbook

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_PublishPruneRead(t *testing.T) {
	s := Store{Root: t.TempDir()}
	for v := 1; v <= 3; v++ {
		files := map[string][]byte{"skill/SKILL.md": []byte("v" + string(rune('0'+v))), "skill/references/index.md": []byte("idx")}
		if err := s.Publish("r1", v, files, 2); err != nil {
			t.Fatal(err)
		}
	}
	if cur, err := s.Current("r1"); err != nil || cur != 3 {
		t.Fatalf("current = %d err=%v", cur, err)
	}
	if _, err := os.Stat(s.VersionDir("r1", 1)); !os.IsNotExist(err) {
		t.Fatal("v1 should be pruned")
	}
	if _, err := os.Stat(s.VersionDir("r1", 2)); err != nil {
		t.Fatal("v2 should be kept")
	}
	pages, err := s.ListSkillFiles("r1", 3)
	if err != nil || strings.Join(pages, ",") != "SKILL.md,references/index.md" {
		t.Fatalf("pages = %v err=%v", pages, err)
	}
	b, err := s.ReadSkillFile("r1", 3, "SKILL.md")
	if err != nil || string(b) != "v3" {
		t.Fatalf("read = %q err=%v", b, err)
	}
	for _, bad := range []string{"", "../manifest.json", "..", "/etc/passwd", `..\x`, "references/../../x"} {
		if _, err := s.ReadSkillFile("r1", 3, bad); !errors.Is(err, ErrBadPath) {
			t.Fatalf("ReadSkillFile(%q) err = %v, want ErrBadPath", bad, err)
		}
	}
	if _, err := s.ReadSkillFile("r1", 3, "nope.md"); !os.IsNotExist(err) {
		t.Fatalf("missing page err = %v", err)
	}
}

func TestStore_CurrentMissing(t *testing.T) {
	if v, err := (Store{Root: t.TempDir()}).Current("nope"); err != nil || v != 0 {
		t.Fatalf("v=%d err=%v", v, err)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b.txt")
	if err := WriteFileAtomic(p, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("2")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "2" {
		t.Fatalf("content = %q", b)
	}
}
```

`portal/internal/handbook/builder_test.go`：

```go
package handbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	out, err := Build(context.Background(), BuildInput{RepoID: "r1", RelPath: "cloudgame/svc-a", Root: sampleRepo(t), Commit: "abc", Now: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"skill/SKILL.md", "skill/references/overview.md", "manifest.json", "coverage.json", "facts/files.json", "facts/symbols.json", "facts/registers.json", "facts/packages.json"} {
		if _, ok := out.Files[p]; !ok {
			t.Fatalf("missing %s", p)
		}
	}
	var man Manifest
	if err := json.Unmarshal(out.Files["manifest.json"], &man); err != nil {
		t.Fatal(err)
	}
	if man.Commit != "abc" || man.GeneratorVersion != GeneratorVersion || man.LeafMode != "file" || man.Stats.Files != 5 {
		t.Fatalf("manifest = %#v", man)
	}
	s := out.Stats
	if s.GoFiles != 3 || s.Packages != 2 || s.Areas != 3 || s.Registers != 1 || s.Symbols == 0 {
		t.Fatalf("stats = %#v", s)
	}
	m := s.Map()
	if m["generator_version"] != GeneratorVersion || m["files"] != float64(5) {
		t.Fatalf("stats map = %#v", m)
	}
	if !strings.Contains(string(out.Files["skill/SKILL.md"]), "name: handbook-cloudgame-svc-a") {
		t.Fatal("skill name")
	}
}
```

> 分区为 `(root)`（README.md、go.mod）、`cmd/server`、`internal/order` 共 3 个；`GoFiles` 含测试文件；`Registers` 是去重后的寄存器名数（只有表 `orders`）。

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`portal/internal/handbook/codemap.go`：

```go
package handbook

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// CodeMapSkillName is the per-agent entry skill listing its repositories and handbooks.
const CodeMapSkillName = "code-map"

// CodeMapEntry is one effective repository of an agent.
type CodeMapEntry struct {
	RelPath     string
	Description string
	SubPaths    []string
	SkillName   string // empty when the repository has no handbook yet
}

// RenderCodeMap renders the code-map SKILL.md; entries should be sorted by RelPath.
func RenderCodeMap(entries []CodeMapEntry) string {
	groups := map[string][]CodeMapEntry{}
	var keys []string
	for _, e := range entries {
		k := path.Dir(e.RelPath)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], e)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: 你可访问的 %d 个代码仓库及其 handbook。排查与代码相关的问题时先读它，再按仓库查看 handbook。\nsummary_pinned: true\n---\n\n", CodeMapSkillName, len(entries))
	b.WriteString("# 代码仓库目录\n\nrca_* 工具的 repo 参数使用下面的仓库逻辑名。有 handbook 的仓库先 `skill_view` 它，再用 `rca_read` 核实源码。\n")
	if len(entries) == 0 {
		b.WriteString("\n当前没有可访问的仓库。\n")
		return b.String()
	}
	for _, k := range keys {
		title := k
		if k == "." {
			title = "（顶层）"
		}
		es := groups[k]
		fmt.Fprintf(&b, "\n## %s（%d 个）\n\n", title, len(es))
		for _, e := range es {
			fmt.Fprintf(&b, "- `%s`", e.RelPath)
			if d := firstLine(e.Description); d != "" {
				b.WriteString("：" + d)
			}
			if len(e.SubPaths) > 0 {
				names := make([]string, len(e.SubPaths))
				for i, sp := range e.SubPaths {
					names[i] = "`" + e.RelPath + "/" + sp + "`"
				}
				b.WriteString("（仅子目录，repo 参数用 " + strings.Join(names, "、") + "）")
			}
			if e.SkillName != "" {
				fmt.Fprintf(&b, " —— skill_view(\"%s\")\n", e.SkillName)
			} else {
				b.WriteString(" —— 暂无 handbook，直接用 rca_grep\n")
			}
		}
	}
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}
```

`portal/internal/handbook/store.go`：

```go
package handbook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrBadPath rejects page paths that are empty, absolute or escape the skill directory.
var ErrBadPath = errors.New("handbook: invalid page path")

const maxReadBytes = 256 << 10

// Store lays out handbook versions under Root (normally {data_root}/handbooks):
// repos/<id>/v<N>/... and repos/<id>/current.json.
type Store struct{ Root string }

type currentPointer struct {
	Version int `json:"version"`
}

func (s Store) repoDir(id string) string { return filepath.Join(s.Root, "repos", id) }

// VersionDir is the directory of one published version.
func (s Store) VersionDir(id string, v int) string {
	return filepath.Join(s.repoDir(id), "v"+strconv.Itoa(v))
}

// SkillDir is the agent-facing skill directory of one version.
func (s Store) SkillDir(id string, v int) string { return filepath.Join(s.VersionDir(id, v), "skill") }

// Publish writes files into v<version>, points current.json at it and keeps the newest keep
// versions. Older versions stay readable until pruned so in-flight chats are not broken.
func (s Store) Publish(id string, version int, files map[string][]byte, keep int) error {
	if version <= 0 {
		return fmt.Errorf("handbook: invalid version %d", version)
	}
	final := s.VersionDir(id, version)
	tmp := final + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	for rel, b := range files {
		p := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	ptr, err := json.Marshal(currentPointer{Version: version})
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(filepath.Join(s.repoDir(id), "current.json"), ptr); err != nil {
		return err
	}
	s.prune(id, version, keep)
	return nil
}

// Current returns the version current.json points at, or 0 when nothing is published.
func (s Store) Current(id string) (int, error) {
	b, err := os.ReadFile(filepath.Join(s.repoDir(id), "current.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var p currentPointer
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, err
	}
	return p.Version, nil
}

func (s Store) prune(id string, current, keep int) {
	if keep < 1 {
		keep = 1
	}
	ents, err := os.ReadDir(s.repoDir(id))
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "v") {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "v"))
		if err == nil && n <= current-keep {
			_ = os.RemoveAll(filepath.Join(s.repoDir(id), name))
		}
	}
}

// ListSkillFiles lists the files of a version's skill directory as sorted slash paths.
func (s Store) ListSkillFiles(id string, v int) ([]string, error) {
	root := s.SkillDir(id, v)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

// ReadSkillFile reads one page of a version's skill directory, capped at 256 KiB.
func (s Store) ReadSkillFile(id string, v int, rel string) ([]byte, error) {
	clean := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	if strings.TrimSpace(rel) == "" || path.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || filepath.VolumeName(filepath.FromSlash(clean)) != "" {
		return nil, ErrBadPath
	}
	f, err := os.Open(filepath.Join(s.SkillDir(id, v), filepath.FromSlash(clean)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReadBytes))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// WriteFileAtomic writes b to a temp file next to p and renames it over p.
func WriteFileAtomic(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
```

`portal/internal/handbook/builder.go`：

```go
package handbook

import (
	"context"
	"encoding/json"
	"time"
)

// GeneratorVersion changes whenever the output format changes; repos built by an older
// generator are rebuilt on the next pass.
const GeneratorVersion = "p2a-1"

// BuildInput identifies the checkout to build from.
type BuildInput struct {
	RepoID  string
	RelPath string
	Root    string // absolute repository root with symlinks resolved
	Commit  string
	Now     time.Time
}

// Stats is stored in repositories.handbook_stats.
type Stats struct {
	GeneratorVersion string `json:"generator_version"`
	BuiltAt          string `json:"built_at"`
	Files            int    `json:"files"`
	GoFiles          int    `json:"go_files"`
	Packages         int    `json:"packages"`
	Areas            int    `json:"areas"`
	Symbols          int    `json:"symbols"`
	Registers        int    `json:"registers"`
	Truncated        bool   `json:"truncated"`
}

// Map converts stats to the JSON object stored in the database.
func (s Stats) Map() map[string]any {
	b, _ := json.Marshal(s)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// Manifest describes one published version.
type Manifest struct {
	RelPath          string    `json:"rel_path"`
	Commit           string    `json:"commit"`
	GeneratorVersion string    `json:"generator_version"`
	LeafMode         string    `json:"leaf_mode"`
	GeneratedAt      time.Time `json:"generated_at"`
	Stats            Stats     `json:"stats"`
}

// Output is the file set of one version, keyed by slash path relative to v<N>/.
type Output struct {
	Files map[string][]byte
	Stats Stats
}

// Build collects facts from in.Root and renders the handbook files.
func Build(ctx context.Context, in BuildInput) (*Output, error) {
	facts, err := CollectFacts(ctx, in.Root)
	if err != nil {
		return nil, err
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	meta := RenderMeta{RelPath: in.RelPath, Commit: in.Commit, GeneratedAt: in.Now}
	out := &Output{Files: map[string][]byte{}}
	for p, c := range Render(meta, facts) {
		out.Files["skill/"+p] = []byte(c)
	}
	regNames := map[[2]string]bool{}
	for _, h := range facts.Registers {
		regNames[[2]string{h.Kind, h.Name}] = true
	}
	stats := Stats{
		GeneratorVersion: GeneratorVersion, BuiltAt: in.Now.UTC().Format(time.RFC3339),
		Files: len(facts.Files), Packages: len(facts.Packages), Areas: len(buildAreas(facts)),
		Registers: len(regNames), Truncated: facts.Coverage.Truncated,
	}
	for _, f := range facts.Files {
		if f.Lang == "go" {
			stats.GoFiles++
		}
	}
	for _, s := range facts.Symbols {
		stats.Symbols += len(s)
	}
	docs := map[string]any{
		"facts/files.json":     facts.Files,
		"facts/symbols.json":   facts.Symbols,
		"facts/registers.json": facts.Registers,
		"facts/packages.json":  facts.Packages,
		"coverage.json":        facts.Coverage,
		"manifest.json": Manifest{
			RelPath: in.RelPath, Commit: in.Commit, GeneratorVersion: GeneratorVersion,
			LeafMode: "file", GeneratedAt: in.Now, Stats: stats,
		},
	}
	for p, v := range docs {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		out.Files[p] = b
	}
	out.Stats = stats
	return out, nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd portal; go test -p 1 ./internal/handbook/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/handbook/codemap.go portal/internal/handbook/store.go portal/internal/handbook/builder.go portal/internal/handbook/codemap_test.go portal/internal/handbook/store_test.go portal/internal/handbook/builder_test.go
git commit -m "feat(handbook): code-map skill, versioned store and build entry"
```

---

### Task 8: 仓储层：handbook 字段与构建租约

**Files:**
- Create: `portal/migrations/020_repo_handbook.sql`
- Modify: `portal/internal/data/model/repo_registry.go:23-26`
- Modify: `portal/internal/biz/repo_registry.go`
- Modify: `portal/internal/data/repo_registry.go`
- Test: `portal/internal/data/repo_handbook_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/data/repo_handbook_test.go`：

```go
package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
)

func TestRepoRegistryRepo_HandbookClaimAndFinish(t *testing.T) {
	ctx := context.Background()
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	a, err := r.UpsertScannedRepository(ctx, &biz.Repository{CodeRoot: "/c", RelPath: "cg/a", Name: "a", HeadCommit: "111", LastScannedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	reload := func() *biz.Repository {
		m, err := r.GetRepositoriesByIDs(ctx, []string{a.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m[a.ID]
	}

	if ok, err := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if got := reload(); got.HandbookStatus != biz.HandbookStatusBuilding {
		t.Fatalf("status = %s", got.HandbookStatus)
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now, now.Add(time.Minute)); ok {
		t.Fatal("second claim must fail while the lease is live")
	}
	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(2*time.Minute), now.Add(3*time.Minute)); !ok {
		t.Fatal("an expired lease must be claimable")
	}

	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusReady, Commit: "111", Version: 1, Stats: map[string]any{"files": 3},
	}); err != nil {
		t.Fatal(err)
	}
	g := reload()
	if g.HandbookStatus != biz.HandbookStatusReady || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["files"] != float64(3) {
		t.Fatalf("after ready: %#v", g)
	}

	if ok, _ := r.ClaimHandbookBuild(ctx, a.ID, now.Add(5*time.Minute), now.Add(6*time.Minute)); !ok {
		t.Fatal("a finished build must release the lease")
	}
	if err := r.FinishHandbookBuild(ctx, a.ID, biz.HandbookBuildResult{
		Status: biz.HandbookStatusFailed, Stats: map[string]any{"last_error": "boom"},
	}); err != nil {
		t.Fatal(err)
	}
	g = reload()
	if g.HandbookStatus != biz.HandbookStatusFailed || g.HandbookCommit != "111" || g.HandbookVersion != 1 || g.HandbookStats["last_error"] != "boom" {
		t.Fatalf("a failed build must keep commit and version: %#v", g)
	}
}

func TestRepoRegistryRepo_ClaimUnknownRepo(t *testing.T) {
	r := newRepoRegistryRepoForTest(t)
	now := time.Now()
	if ok, err := r.ClaimHandbookBuild(context.Background(), "nope", now, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run（PowerShell）：

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"; cd portal; go test -p 1 ./internal/data/ -run "Handbook|ClaimUnknown"
```

Expected: 编译失败（方法和字段未定义）。

- [ ] **Step 3: 实现**

`portal/migrations/020_repo_handbook.sql`：

```sql
-- Repository handbook build lease (P2a).
-- Design: docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md §8.2.
ALTER TABLE repositories ADD COLUMN handbook_lease_until DATETIME(3) NULL AFTER handbook_stats;
```

`portal/internal/data/model/repo_registry.go`：`HandbookStats` 之后追加：

```go
	HandbookLeaseUntil *time.Time `gorm:"column:handbook_lease_until"`
```

`portal/internal/biz/repo_registry.go`：

1. 常量块 `HandbookStatusNone` 之后追加：

```go
	HandbookStatusBuilding = "building"
	HandbookStatusReady    = "ready"
	HandbookStatusFailed   = "failed"
```

2. `Repository` 结构体 `HandbookStatus` 之后追加：

```go
	HandbookCommit  string         `json:"handbook_commit"`
	HandbookVersion int            `json:"handbook_version"`
	HandbookStats   map[string]any `json:"handbook_stats,omitempty"`
```

3. `RepoMetaPatch` 之后追加：

```go
// HandbookBuildResult is the outcome of one handbook build. Commit and Version are written
// only when Status is ready; a failed build keeps serving the previous version.
type HandbookBuildResult struct {
	Status  string
	Commit  string
	Version int
	Stats   map[string]any
}
```

4. `RepoRegistryRepo` 接口末尾追加（需要 `time` 已导入，文件已有）：

```go
	// ClaimHandbookBuild marks the repo as building until leaseUntil unless another build
	// holds a lease that has not expired at now; it reports whether the claim succeeded.
	ClaimHandbookBuild(ctx context.Context, id string, now, leaseUntil time.Time) (bool, error)
	// FinishHandbookBuild releases the lease and records the outcome.
	FinishHandbookBuild(ctx context.Context, id string, res HandbookBuildResult) error
```

`portal/internal/data/repo_registry.go`：

```go
func (r *repoRegistryRepo) ClaimHandbookBuild(ctx context.Context, id string, now, leaseUntil time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Repository{}).
		Where("id = ? AND (handbook_status <> ? OR handbook_lease_until IS NULL OR handbook_lease_until < ?)",
			id, biz.HandbookStatusBuilding, now).
		Updates(map[string]any{"handbook_status": biz.HandbookStatusBuilding, "handbook_lease_until": leaseUntil})
	return res.RowsAffected > 0, res.Error
}

func (r *repoRegistryRepo) FinishHandbookBuild(ctx context.Context, id string, res biz.HandbookBuildResult) error {
	updates := map[string]any{
		"handbook_status":      res.Status,
		"handbook_lease_until": nil,
		"handbook_stats":       model.JSONObject(res.Stats),
	}
	if res.Status == biz.HandbookStatusReady {
		updates["handbook_commit"] = res.Commit
		updates["handbook_version"] = res.Version
	}
	return r.db.WithContext(ctx).Model(&model.Repository{}).Where("id = ?", id).Updates(updates).Error
}
```

`repositoryToBiz` 追加映射：

```go
		HandbookCommit: m.HandbookCommit, HandbookVersion: m.HandbookVersion, HandbookStats: map[string]any(m.HandbookStats),
```

- [ ] **Step 4: 运行确认通过**

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"; cd portal; go test -p 1 ./internal/data/ ./internal/biz/
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/migrations/020_repo_handbook.sql portal/internal/data/model/repo_registry.go portal/internal/biz/repo_registry.go portal/internal/data/repo_registry.go portal/internal/data/repo_handbook_test.go
git commit -m "feat(repo-registry): handbook fields and build lease"
```

---

### Task 9: `HandbookUsecase`

**Files:**
- Modify: `portal/internal/biz/repo_registry_usecase.go`（在 `RCARootsForAgent` 之后加两个方法）
- Create: `portal/internal/biz/handbook.go`
- Test: `portal/internal/data/repo_handbook_test.go`（追加）

- [ ] **Step 1: 写失败测试**

追加到 `portal/internal/data/repo_handbook_test.go`（补充 import：`errors`、`os`、`path/filepath`、`strings`、`backend/internal/handbook`、`github.com/go-kratos/kratos/v2/log`、`github.com/sixath/framework/skills`）：

```go
func setRepoHead(t *testing.T, dir, sha string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shaOf(c byte) string { return strings.Repeat(string(c), 40) }

type handbookFixture struct {
	ctx      context.Context
	codeRoot string
	repoDir  string
	reg      *biz.RepoRegistryUsecase
	repo     biz.RepoRegistryRepo
	hb       *biz.HandbookUsecase
	repoID   string
}

func newHandbookFixture(t *testing.T) *handbookFixture {
	t.Helper()
	ctx := context.Background()
	codeRoot := t.TempDir()
	repoDir := filepath.Join(codeRoot, "cloudgame", "svc-a")
	setRepoHead(t, repoDir, shaOf('a'))
	if err := os.MkdirAll(filepath.Join(repoDir, "internal", "order"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "internal", "order", "store.go"),
		[]byte("// Package order stores orders.\npackage order\n\nfunc Get() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkRepoDir(t, filepath.Join(codeRoot, "cloudgame", "svc-b"))
	reg, repo := newUsecaseForTest(t, codeRoot, &biz.AgentMeta{ID: "ag"}, &biz.AgentMeta{ID: "other"})
	if _, err := reg.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	repos, err := reg.ListRepos(ctx, biz.RepoFilter{Query: "svc-a"})
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos = %v err=%v", repos, err)
	}
	hb := biz.NewHandbookUsecase(repo, reg, t.TempDir(), log.DefaultLogger)
	return &handbookFixture{ctx: ctx, codeRoot: codeRoot, repoDir: repoDir, reg: reg, repo: repo, hb: hb, repoID: repos[0].ID}
}

func (f *handbookFixture) get(t *testing.T) *biz.Repository {
	t.Helper()
	m, err := f.repo.GetRepositoriesByIDs(f.ctx, []string{f.repoID})
	if err != nil {
		t.Fatal(err)
	}
	return m[f.repoID]
}

func TestHandbookUsecase_RebuildStaleAndSkillDirs(t *testing.T) {
	f := newHandbookFixture(t)
	n, err := f.hb.RebuildStale(f.ctx)
	if err != nil || n != 1 {
		t.Fatalf("first pass n=%d err=%v (svc-b has no commit and must be skipped)", n, err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusReady || r.HandbookCommit != shaOf('a') || r.HandbookVersion != 1 ||
		r.HandbookStats["generator_version"] != handbook.GeneratorVersion {
		t.Fatalf("after build: %#v", r)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatalf("second pass rebuilt %d repos", n)
	}

	if dirs, err := f.hb.SkillDirsForAgent(f.ctx, "ag"); err != nil || dirs != nil {
		t.Fatalf("unbound agent: dirs=%v err=%v", dirs, err)
	}
	if _, err := f.reg.ReplaceBindings(f.ctx, "ag", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: f.repoID}}, "alice"); err != nil {
		t.Fatal(err)
	}
	dirs, err := f.hb.SkillDirsForAgent(f.ctx, "ag")
	if err != nil || len(dirs) != 2 {
		t.Fatalf("dirs = %v err=%v", dirs, err)
	}
	idx, err := skills.NewIndex(dirs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cm, ok := idx.GetByName("code-map")
	if !ok || !cm.SummaryPinned {
		t.Fatalf("code-map = %#v ok=%v", cm, ok)
	}
	body, _ := idx.LoadSkillBody("code-map")
	if !strings.Contains(body, `skill_view("handbook-cloudgame-svc-a")`) {
		t.Fatalf("code-map body:\n%s", body)
	}
	if hbMeta, ok := idx.GetByName("handbook-cloudgame-svc-a"); !ok || !hbMeta.HiddenFromSummary {
		t.Fatalf("handbook skill = %#v ok=%v", hbMeta, ok)
	}

	setRepoHead(t, f.repoDir, shaOf('b'))
	if _, err := f.reg.Scan(f.ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 1 {
		t.Fatalf("HEAD change must trigger a rebuild, n=%d", n)
	}
	if r := f.get(t); r.HandbookVersion != 2 || r.HandbookCommit != shaOf('b') {
		t.Fatalf("after head change: %#v", r)
	}
}

func TestHandbookUsecase_FailureIsNotRetriedUntilHeadMoves(t *testing.T) {
	f := newHandbookFixture(t)
	f.hb.SetBuilder(func(context.Context, handbook.BuildInput) (*handbook.Output, error) {
		return nil, errors.New("boom")
	})
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.get(t)
	if r.HandbookStatus != biz.HandbookStatusFailed || r.HandbookStats["failed_commit"] != shaOf('a') ||
		!strings.Contains(r.HandbookStats["last_error"].(string), "boom") || r.HandbookVersion != 0 {
		t.Fatalf("after failure: %#v", r)
	}
	if n, _ := f.hb.RebuildStale(f.ctx); n != 0 {
		t.Fatal("a failed commit must not be retried automatically")
	}

	f.hb.SetBuilder(handbook.Build)
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); err != nil {
		t.Fatal(err)
	}
	f.hb.Wait()
	if r := f.get(t); r.HandbookStatus != biz.HandbookStatusReady || r.HandbookVersion != 1 {
		t.Fatalf("manual rebuild: %#v", r)
	}
}

func TestHandbookUsecase_RequestRebuildConflictsAndValidates(t *testing.T) {
	f := newHandbookFixture(t)
	now := time.Now()
	if ok, _ := f.repo.ClaimHandbookBuild(f.ctx, f.repoID, now, now.Add(time.Hour)); !ok {
		t.Fatal("claim")
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrHandbookBuilding) {
		t.Fatalf("err = %v, want ErrHandbookBuilding", err)
	}
	if err := f.hb.RequestRebuild(f.ctx, "nope"); !errors.Is(err, biz.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	archived := biz.RepoStatusArchived
	if _, err := f.reg.PatchRepo(f.ctx, f.repoID, biz.RepoMetaPatch{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if err := f.hb.RequestRebuild(f.ctx, f.repoID); !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("err = %v, want ErrInvalidRepo", err)
	}
}

func TestHandbookUsecase_ViewAndPages(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "SKILL.md"); !errors.Is(err, biz.ErrHandbookNotFound) {
		t.Fatalf("before build err = %v", err)
	}
	if _, err := f.hb.RebuildStale(f.ctx); err != nil {
		t.Fatal(err)
	}
	v, err := f.hb.GetHandbook(f.ctx, f.repoID)
	if err != nil || v.Version != 1 || v.HeadCommit != shaOf('a') {
		t.Fatalf("view = %#v err=%v", v, err)
	}
	joined := strings.Join(v.Pages, ",")
	if !strings.Contains(joined, "SKILL.md") || !strings.Contains(joined, "references/areas/internal-order.md") {
		t.Fatalf("pages = %v", v.Pages)
	}
	page, err := f.hb.ReadPage(f.ctx, f.repoID, "references/index.md")
	if err != nil || !strings.Contains(page, "Package order stores orders.") {
		t.Fatalf("page = %q err=%v", page, err)
	}
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "../manifest.json"); !errors.Is(err, biz.ErrInvalidRepo) {
		t.Fatalf("traversal err = %v", err)
	}
	if _, err := f.hb.ReadPage(f.ctx, f.repoID, "nope.md"); !errors.Is(err, biz.ErrHandbookNotFound) {
		t.Fatalf("missing page err = %v", err)
	}
}

func TestHandbookUsecase_CodeMapWithoutHandbook(t *testing.T) {
	f := newHandbookFixture(t)
	if _, err := f.reg.ReplaceBindings(f.ctx, "other", []*biz.AgentRepoBinding{{TargetKind: biz.RepoTargetRepo, TargetID: f.repoID}}, "alice"); err != nil {
		t.Fatal(err)
	}
	dirs, err := f.hb.SkillDirsForAgent(f.ctx, "other")
	if err != nil || len(dirs) != 1 {
		t.Fatalf("dirs = %v err=%v", dirs, err)
	}
	b, err := os.ReadFile(filepath.Join(dirs[0], "SKILL.md"))
	if err != nil || !strings.Contains(string(b), "暂无 handbook") {
		t.Fatalf("code-map = %s err=%v", b, err)
	}
}
```

> `mkRepoDir` 写的是 `ref: refs/heads/main` 而没有 ref 文件，`ReadGitInfo` 对未出生分支返回空 commit，所以 svc-b 的 `HeadCommit` 为空，`RebuildStale` 必须跳过它。若 `ReplaceBindings` 的签名与上面不同，按 `repo_registry_usecase.go` 中的实际签名调整。

- [ ] **Step 2: 运行确认失败**

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"; cd portal; go test -p 1 ./internal/data/ -run HandbookUsecase
```

Expected: 编译失败（`NewHandbookUsecase` 未定义）。

- [ ] **Step 3: 实现**

`portal/internal/biz/repo_registry_usecase.go`，`RCARootsForAgent` 之后：

```go
// EffectiveRepository is one effective repo of an agent with its optional sub-path filter.
type EffectiveRepository struct {
	Repo     *Repository
	SubPaths []string
}

// EffectiveRepositories returns the agent's effective repos sorted by rel_path; bound is
// false when the agent has no repo bindings.
func (uc *RepoRegistryUsecase) EffectiveRepositories(ctx context.Context, agentID string) (bool, []EffectiveRepository, error) {
	bound, eff, err := uc.readAgentEffective(ctx, agentID)
	if err != nil || !bound {
		return false, nil, err
	}
	ids := make([]string, 0, len(eff))
	for _, e := range eff {
		ids = append(ids, e.RepoID)
	}
	repos, err := uc.repo.GetRepositoriesByIDs(ctx, ids)
	if err != nil {
		return true, nil, err
	}
	out := make([]EffectiveRepository, 0, len(eff))
	for _, e := range eff {
		if r := repos[e.RepoID]; r != nil {
			out = append(out, EffectiveRepository{Repo: r, SubPaths: e.SubPaths})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo.RelPath != out[j].Repo.RelPath {
			return out[i].Repo.RelPath < out[j].Repo.RelPath
		}
		return out[i].Repo.CodeRoot < out[j].Repo.CodeRoot
	})
	return true, out, nil
}

// ResolveRepoPath returns the repo root with symlinks resolved. Repos under code roots that
// are no longer configured, missing paths and paths escaping their code root are rejected.
func (uc *RepoRegistryUsecase) ResolveRepoPath(r *Repository) (string, error) {
	cr, ok := uc.configuredRoot(r.CodeRoot)
	if !ok {
		return "", fmt.Errorf("%w: code root %s is not configured", ErrInvalidRepo, r.CodeRoot)
	}
	p := r.AbsPath()
	if err := checkRootPath(cr, p); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}
```

`portal/internal/biz/handbook.go`：

```go
package biz

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"backend/internal/handbook"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	handbookLease        = 30 * time.Minute
	handbookKeepVersions = 2
	maxHandbookErrorLen  = 500
)

var (
	ErrHandbookBuilding = errors.New("handbook: build already running")
	ErrHandbookNotFound = errors.New("handbook: not found")
)

// HandbookBuilder builds the handbook files of one checkout.
type HandbookBuilder func(ctx context.Context, in handbook.BuildInput) (*handbook.Output, error)

// HandbookView is the API view of a repository handbook.
type HandbookView struct {
	RepoID     string         `json:"repo_id"`
	Status     string         `json:"status"`
	Commit     string         `json:"commit"`
	HeadCommit string         `json:"head_commit"`
	Version    int            `json:"version"`
	Stats      map[string]any `json:"stats,omitempty"`
	Pages      []string       `json:"pages"`
}

// HandbookUsecase builds repository handbooks and assembles per-agent handbook skill dirs.
type HandbookUsecase struct {
	repo      RepoRegistryRepo
	registry  *RepoRegistryUsecase
	store     handbook.Store
	build     HandbookBuilder
	staleMu   sync.Mutex
	codeMapMu sync.Mutex
	wg        sync.WaitGroup
	now       func() time.Time
	log       *log.Helper
}

func NewHandbookUsecase(repo RepoRegistryRepo, registry *RepoRegistryUsecase, dataRoot string, logger log.Logger) *HandbookUsecase {
	return &HandbookUsecase{
		repo: repo, registry: registry, store: handbook.Store{Root: filepath.Join(dataRoot, "handbooks")},
		build: handbook.Build, now: time.Now, log: log.NewHelper(logger),
	}
}

// SetBuilder replaces the handbook generator.
func (uc *HandbookUsecase) SetBuilder(b HandbookBuilder) { uc.build = b }

// Wait blocks until rebuilds started by RequestRebuild have finished.
func (uc *HandbookUsecase) Wait() { uc.wg.Wait() }

func handbookNeedsRebuild(r *Repository) bool {
	if r.Status != RepoStatusActive || r.HeadCommit == "" {
		return false
	}
	if r.HandbookStatus == HandbookStatusFailed {
		failed, _ := r.HandbookStats["failed_commit"].(string)
		return failed != r.HeadCommit
	}
	gen, _ := r.HandbookStats["generator_version"].(string)
	return r.HandbookCommit != r.HeadCommit || gen != handbook.GeneratorVersion
}

// RebuildStale rebuilds, one at a time, every active repo whose handbook lags its HEAD or
// the generator version. A commit that failed is not retried until HEAD moves. Concurrent
// calls return immediately. It returns the number of builds attempted.
func (uc *HandbookUsecase) RebuildStale(ctx context.Context) (int, error) {
	if !uc.staleMu.TryLock() {
		return 0, nil
	}
	defer uc.staleMu.Unlock()
	repos, err := uc.repo.ListRepositories(ctx, RepoFilter{Status: RepoStatusActive})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range repos {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if !handbookNeedsRebuild(r) {
			continue
		}
		ok, err := uc.claim(ctx, r.ID)
		if err != nil {
			return n, err
		}
		if !ok {
			continue
		}
		n++
		if err := uc.runClaimed(ctx, r.ID); err != nil {
			uc.log.Warnf("handbook rebuild %s: %v", r.RelPath, err)
		}
	}
	return n, nil
}

// RequestRebuild starts an asynchronous rebuild of one active repo regardless of staleness.
func (uc *HandbookUsecase) RequestRebuild(ctx context.Context, id string) error {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != RepoStatusActive {
		return fmt.Errorf("%w: repository is %s", ErrInvalidRepo, r.Status)
	}
	ok, err := uc.claim(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrHandbookBuilding
	}
	uc.wg.Add(1)
	go func() {
		defer uc.wg.Done()
		bctx, cancel := context.WithTimeout(context.Background(), handbookLease)
		defer cancel()
		if err := uc.runClaimed(bctx, id); err != nil {
			uc.log.Warnf("handbook rebuild %s: %v", id, err)
		}
	}()
	return nil
}

func (uc *HandbookUsecase) claim(ctx context.Context, id string) (bool, error) {
	now := uc.now()
	return uc.repo.ClaimHandbookBuild(ctx, id, now, now.Add(handbookLease))
}

// runClaimed builds and publishes under a held lease. The row is re-read after the claim so
// the next version number cannot collide with a build that finished in between.
func (uc *HandbookUsecase) runClaimed(ctx context.Context, id string) error {
	start := uc.now()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return uc.finishFailed(ctx, &Repository{ID: id}, err)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	out, err := uc.build(ctx, handbook.BuildInput{RepoID: r.ID, RelPath: r.RelPath, Root: root, Commit: r.HeadCommit, Now: start})
	if err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	version := r.HandbookVersion + 1
	if err := uc.store.Publish(r.ID, version, out.Files, handbookKeepVersions); err != nil {
		return uc.finishFailed(ctx, r, err)
	}
	stats := out.Stats.Map()
	stats["duration_ms"] = uc.now().Sub(start).Milliseconds()
	return uc.repo.FinishHandbookBuild(context.WithoutCancel(ctx), r.ID, HandbookBuildResult{
		Status: HandbookStatusReady, Commit: r.HeadCommit, Version: version, Stats: stats,
	})
}

func (uc *HandbookUsecase) finishFailed(ctx context.Context, r *Repository, cause error) error {
	stats := make(map[string]any, len(r.HandbookStats)+2)
	for k, v := range r.HandbookStats {
		stats[k] = v
	}
	msg := cause.Error()
	if len(msg) > maxHandbookErrorLen {
		msg = msg[:maxHandbookErrorLen]
	}
	stats["last_error"] = msg
	stats["failed_commit"] = r.HeadCommit
	if err := uc.repo.FinishHandbookBuild(context.WithoutCancel(ctx), r.ID, HandbookBuildResult{Status: HandbookStatusFailed, Stats: stats}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (uc *HandbookUsecase) getRepo(ctx context.Context, id string) (*Repository, error) {
	m, err := uc.repo.GetRepositoriesByIDs(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	r := m[id]
	if r == nil {
		return nil, ErrRepoNotFound
	}
	return r, nil
}

// GetHandbook returns the handbook state and page list of the published version.
func (uc *HandbookUsecase) GetHandbook(ctx context.Context, id string) (*HandbookView, error) {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	v := &HandbookView{
		RepoID: r.ID, Status: r.HandbookStatus, Commit: r.HandbookCommit, HeadCommit: r.HeadCommit,
		Version: r.HandbookVersion, Stats: r.HandbookStats, Pages: []string{},
	}
	if r.HandbookVersion > 0 {
		pages, err := uc.store.ListSkillFiles(r.ID, r.HandbookVersion)
		if err != nil {
			uc.log.Warnf("list handbook pages %s v%d: %v", r.ID, r.HandbookVersion, err)
		} else {
			v.Pages = pages
		}
	}
	return v, nil
}

// ReadPage returns one page (path relative to the skill dir) of the published version.
func (uc *HandbookUsecase) ReadPage(ctx context.Context, id, rel string) (string, error) {
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return "", err
	}
	if r.HandbookVersion == 0 {
		return "", ErrHandbookNotFound
	}
	b, err := uc.store.ReadSkillFile(r.ID, r.HandbookVersion, rel)
	switch {
	case errors.Is(err, handbook.ErrBadPath):
		return "", fmt.Errorf("%w: %v", ErrInvalidRepo, err)
	case errors.Is(err, fs.ErrNotExist):
		return "", ErrHandbookNotFound
	case err != nil:
		return "", err
	}
	return string(b), nil
}

// SkillDirsForAgent returns the agent's code-map skill dir followed by the handbook skill
// dirs of its effective repos. nil means the agent has no repo bindings.
func (uc *HandbookUsecase) SkillDirsForAgent(ctx context.Context, agentID string) ([]string, error) {
	bound, repos, err := uc.registry.EffectiveRepositories(ctx, agentID)
	if err != nil || !bound {
		return nil, err
	}
	var dirs []string
	entries := make([]handbook.CodeMapEntry, 0, len(repos))
	for _, e := range repos {
		entry := handbook.CodeMapEntry{RelPath: e.Repo.RelPath, Description: e.Repo.Description, SubPaths: e.SubPaths}
		if e.Repo.HandbookVersion > 0 {
			d := uc.store.SkillDir(e.Repo.ID, e.Repo.HandbookVersion)
			if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil {
				dirs = append(dirs, d)
				entry.SkillName = handbook.SkillName(e.Repo.RelPath)
			}
		}
		entries = append(entries, entry)
	}
	cm, err := uc.writeCodeMap(agentID, handbook.RenderCodeMap(entries))
	if err != nil {
		return dirs, err
	}
	return append([]string{cm}, dirs...), nil
}

func (uc *HandbookUsecase) writeCodeMap(agentID, content string) (string, error) {
	if agentID == "" || filepath.Base(agentID) != agentID || agentID == "." || agentID == ".." {
		return "", fmt.Errorf("handbook: invalid agent id %q", agentID)
	}
	dir := filepath.Join(uc.store.Root, "agents", agentID, handbook.CodeMapSkillName)
	p := filepath.Join(dir, "SKILL.md")
	uc.codeMapMu.Lock()
	defer uc.codeMapMu.Unlock()
	if old, err := os.ReadFile(p); err == nil && string(old) == content {
		return dir, nil
	}
	if err := handbook.WriteFileAtomic(p, []byte(content)); err != nil {
		return "", err
	}
	return dir, nil
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"; cd portal; go test -p 1 ./internal/data/ ./internal/biz/ ./internal/handbook/
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/biz/repo_registry_usecase.go portal/internal/biz/handbook.go portal/internal/data/repo_handbook_test.go
git commit -m "feat(handbook): HandbookUsecase with lease, versions and per-agent code-map"
```

---

### Task 10: 接线：DI、调度、Skill 目录注入、HTTP 接口

**Files:**
- Modify: `portal/internal/biz/biz.go`
- Modify: `portal/cmd/backend/wire_gen.go`
- Modify: `portal/internal/cron/scheduler.go`
- Create: `portal/internal/service/handbook_dirs.go`、`portal/internal/service/handbook_dirs_test.go`
- Modify: `portal/internal/service/agent.go`、`portal/internal/service/chat.go`
- Modify: `portal/internal/server/repo_registry.go`、`portal/internal/server/http.go`、`portal/internal/server/repo_registry_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/service/handbook_dirs_test.go`：

```go
package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
)

type fakeHandbookDirs struct {
	dirs []string
	err  error
}

func (f fakeHandbookDirs) SkillDirsForAgent(context.Context, string) ([]string, error) {
	return f.dirs, f.err
}

func TestAppendHandbookDirs(t *testing.T) {
	logger := log.NewHelper(log.DefaultLogger)
	base := []string{"/data/skills/s1"}
	if got := appendHandbookDirs(context.Background(), nil, "ag", base, logger); !reflect.DeepEqual(got, base) {
		t.Fatalf("nil resolver: %v", got)
	}
	got := appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/hb/code-map", "/hb/r1"}}, "ag", base, logger)
	if !reflect.DeepEqual(got, []string{"/data/skills/s1", "/hb/code-map", "/hb/r1"}) {
		t.Fatalf("append: %v", got)
	}
	got = appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/hb/r1"}, err: errors.New("code-map write failed")}, "ag", base, logger)
	if !reflect.DeepEqual(got, []string{"/data/skills/s1", "/hb/r1"}) {
		t.Fatalf("partial result must still be used: %v", got)
	}
	if got := appendHandbookDirs(context.Background(), fakeHandbookDirs{dirs: []string{"/x"}}, "", base, logger); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty agent id: %v", got)
	}
}
```

追加到 `portal/internal/server/repo_registry_test.go`：`TestRepoRegistryErr` 的 cases 中加入

```go
		{"handbook building", biz.ErrHandbookBuilding, 409},
		{"handbook not found", biz.ErrHandbookNotFound, 404},
```

并新增：

```go
func TestHandbookRoutes_DisabledWithoutUsecase(t *testing.T) {
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	h := NewRepoRegistryHandlers(nil, nil)
	srv.Route("/").GET("/api/v1/repos/{id}/handbook", h.GetHandbook())
	srv.Route("/").POST("/api/v1/repos/{id}/handbook/rebuild", h.RebuildHandbook())
	for _, tc := range []struct{ method, url string }{
		{http.MethodGet, "/api/v1/repos/r1/handbook"},
		{http.MethodPost, "/api/v1/repos/r1/handbook/rebuild"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: status = %d body = %s", tc.method, tc.url, rec.Code, rec.Body.String())
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd portal; go test -p 1 ./internal/service/ -run AppendHandbookDirs; go test -p 1 ./internal/server/ -run "RepoRegistryErr|HandbookRoutes"`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`portal/internal/service/handbook_dirs.go`：

```go
package service

import (
	"context"

	"github.com/go-kratos/kratos/v2/log"
)

// HandbookSkillDirResolver returns the code-map and handbook skill dirs of an agent;
// nil when the agent has no repo bindings.
type HandbookSkillDirResolver interface {
	SkillDirsForAgent(ctx context.Context, agentID string) ([]string, error)
}

// appendHandbookDirs fails open: on error it still appends whatever dirs were resolved.
func appendHandbookDirs(ctx context.Context, r HandbookSkillDirResolver, agentID string, dirs []string, logger *log.Helper) []string {
	if r == nil || agentID == "" {
		return dirs
	}
	extra, err := r.SkillDirsForAgent(ctx, agentID)
	if err != nil {
		logger.Warnf("resolve handbook skill dirs for agent %s: %v", agentID, err)
	}
	return append(dirs, extra...)
}
```

`portal/internal/service/agent.go`：

1. `AgentService` 结构体加字段 `handbookDirs HandbookSkillDirResolver`，`SetRCARootResolver` 之后加：

```go
// SetHandbookSkillDirs wires repository handbook skills into the agent's skill dirs.
func (s *AgentService) SetHandbookSkillDirs(r HandbookSkillDirResolver) {
	if s == nil {
		return
	}
	s.handbookDirs = r
}
```

2. `sharedSkillDirs` 改为：

```go
func (s *AgentService) sharedSkillDirs(ctx context.Context, agentID string) ([]string, error) {
	var dirs []string
	if s.skillUC != nil {
		d, err := s.skillUC.SharedSkillDirs(ctx, agentID)
		if err != nil {
			return nil, err
		}
		dirs = d
	}
	return appendHandbookDirs(ctx, s.handbookDirs, agentID, dirs, s.log), nil
}
```

3. `ListSkills` 中 `all := skillsIdx.All()` 改为 `all := skills.VisibleSkills(skillsIdx.All())`，import 加 `"github.com/sixath/framework/skills"`（若与本地变量名冲突，用别名 `fwskills`）。

`portal/internal/service/chat.go`：`ChatService` 同样加字段 `handbookDirs HandbookSkillDirResolver`、`SetHandbookSkillDirs` 方法（与 `SetRCARootResolver` 放一起），`sharedSkillDirs` 同上改写。

`portal/internal/biz/biz.go`：`ProviderSet` 末尾加 `NewHandbookUsecase`。

`portal/internal/cron/scheduler.go`：

```go
	handbookUC *biz.HandbookUsecase
```

```go
// SetHandbook rebuilds stale repository handbooks after every successful repo scan.
func (s *Scheduler) SetHandbook(uc *biz.HandbookUsecase) {
	s.handbookUC = uc
}
```

`runRepoScan` 末尾（`Infof` 之后）：

```go
	if s.handbookUC != nil {
		go s.runHandbookRebuild(ctx)
	}
```

```go
func (s *Scheduler) runHandbookRebuild(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Errorf("handbook rebuild panic: %v", r)
		}
	}()
	n, err := s.handbookUC.RebuildStale(ctx)
	if err != nil && ctx.Err() == nil {
		s.log.Warnf("handbook rebuild: %v", err)
	}
	if n > 0 {
		s.log.Infof("handbook rebuild: %d repos", n)
	}
}
```

`portal/internal/server/repo_registry.go`：

1. `RepoRegistryHandlers` 加字段 `handbook *biz.HandbookUsecase` 与：

```go
// WithHandbook enables the /api/v1/repos/{id}/handbook endpoints.
func (h *RepoRegistryHandlers) WithHandbook(uc *biz.HandbookUsecase) *RepoRegistryHandlers {
	h.handbook = uc
	return h
}

var errHandbookDisabled = kratosErrors.ServiceUnavailable("HANDBOOK_DISABLED", "repository handbooks are not configured")
```

2. `repoRegistryErr` 的 switch 在 `default` 前加：

```go
	case errors.Is(err, biz.ErrHandbookBuilding):
		return kratosErrors.Conflict("HANDBOOK_BUILDING", err.Error())
	case errors.Is(err, biz.ErrHandbookNotFound):
		return kratosErrors.NotFound("HANDBOOK_NOT_FOUND", err.Error())
```

3. 新增三个 handler：

```go
// GET /api/v1/repos/{id}/handbook
func (h *RepoRegistryHandlers) GetHandbook() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		if h.handbook == nil {
			return errHandbookDisabled
		}
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) { return h.handbook.GetHandbook(c, id) })
	}
}

// GET /api/v1/repos/{id}/handbook/page?path=references/index.md
func (h *RepoRegistryHandlers) HandbookPage() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		if h.handbook == nil {
			return errHandbookDisabled
		}
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		p := ctx.Query().Get("path")
		return h.serve(ctx, func(c context.Context) (any, error) {
			content, err := h.handbook.ReadPage(c, id, p)
			if err != nil {
				return nil, err
			}
			return map[string]any{"path": p, "content": content}, nil
		})
	}
}

// POST /api/v1/repos/{id}/handbook/rebuild
func (h *RepoRegistryHandlers) RebuildHandbook() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		if h.handbook == nil {
			return errHandbookDisabled
		}
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			if err := h.handbook.RequestRebuild(c, id); err != nil {
				return nil, err
			}
			return map[string]any{"accepted": true}, nil
		})
	}
}
```

`portal/internal/server/http.go`：`NewHTTPServer` 参数末尾加 `handbookUC *biz.HandbookUsecase`；`repoH := NewRepoRegistryHandlers(repoUC, agentUC).WithHandbook(handbookUC)`；在 `PATCH /api/v1/repos/{id}` 之后注册：

```go
	r.GET("/api/v1/repos/{id}/handbook", repoH.GetHandbook())
	r.GET("/api/v1/repos/{id}/handbook/page", repoH.HandbookPage())
	r.POST("/api/v1/repos/{id}/handbook/rebuild", repoH.RebuildHandbook())
```

> 用 `rg "NewHTTPServer\(" portal` 找出所有调用点（含测试），一并补上新参数（测试传 `nil`）。

`portal/cmd/backend/wire_gen.go`（手工维护，与现有 setter 风格一致）：

```go
	repoRegistryUsecase := biz.NewRepoRegistryUsecase(repoRegistryRepo, agentRepo, v, logger)
	handbookUsecase := biz.NewHandbookUsecase(repoRegistryRepo, repoRegistryUsecase, string2, logger)
	agentService.SetRCARootResolver(repoRegistryUsecase)
	agentService.SetHandbookSkillDirs(handbookUsecase)
	...
	chatService.SetRCARootResolver(repoRegistryUsecase)
	chatService.SetHandbookSkillDirs(handbookUsecase)
	...
	httpServer := server.NewHTTPServer(..., evolutionUsecase, repoRegistryUsecase, handbookUsecase)
	...
	scheduler.SetRepoRegistry(repoRegistryUsecase, cron.DefaultRepoScanInterval)
	scheduler.SetHandbook(handbookUsecase)
```

- [ ] **Step 4: 运行确认通过**

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"; cd portal
go build -p 1 ./cmd/backend/
go vet -p 1 ./internal/handbook/ ./internal/biz/ ./internal/service/ ./internal/server/ ./internal/cron/
go test -p 1 ./internal/handbook/ ./internal/biz/ ./internal/data/ ./internal/service/ ./internal/server/ ./internal/cron/
```

Expected: 构建成功，测试 PASS。

- [ ] **Step 5: 提交**

```bash
git add portal/internal/biz/biz.go portal/cmd/backend/wire_gen.go portal/internal/cron/scheduler.go portal/internal/service/handbook_dirs.go portal/internal/service/handbook_dirs_test.go portal/internal/service/agent.go portal/internal/service/chat.go portal/internal/server/repo_registry.go portal/internal/server/http.go portal/internal/server/repo_registry_test.go
git commit -m "feat(handbook): wire rebuild loop, skill dir injection and HTTP endpoints"
```

（若有其它 `NewHTTPServer` 调用点被修改，一并 `git add`。）

---

### Task 11: Web：Handbook 列、浏览弹窗、重建

**Files:**
- Modify: `web/src/api/repoRegistryTypes.ts`
- Modify: `web/src/api/repoRegistry.ts`
- Modify: `web/src/utils/repoRegistry.ts`
- Create: `web/src/components/HandbookDialog.tsx`
- Modify: `web/src/pages/RepoListPage.tsx`、`web/src/pages/RepoRegistry.css`
- Test: `web/tests/repoRegistry.test.ts`、`web/e2e/repo-registry.spec.ts`

- [ ] **Step 1: 写失败的单元测试**

`web/tests/repoRegistry.test.ts`：import 列表加入 `handbookState`、`sortHandbookPages`，文件末尾追加：

```ts
describe('handbookState', () => {
  const base = { head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa' }
  it('maps backend status and commit drift', () => {
    assert.equal(handbookState(base), 'ready')
    assert.equal(handbookState({ ...base, handbook_commit: 'old' }), 'outdated')
    assert.equal(handbookState({ ...base, handbook_status: 'building' }), 'building')
    assert.equal(handbookState({ ...base, handbook_status: 'failed' }), 'failed')
    assert.equal(handbookState({ head_commit: 'aaa', handbook_status: 'none' }), 'none')
  })
})

describe('sortHandbookPages', () => {
  it('puts SKILL.md first and area pages last', () => {
    assert.deepEqual(
      sortHandbookPages([
        'references/areas/b.md',
        'references/registers.md',
        'SKILL.md',
        'references/areas/a.md',
        'references/index.md',
      ]),
      ['SKILL.md', 'references/index.md', 'references/registers.md', 'references/areas/a.md', 'references/areas/b.md'],
    )
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npm --prefix web test`
Expected: FAIL（导出不存在）。

- [ ] **Step 3: 实现类型、API 与纯函数**

`web/src/api/repoRegistryTypes.ts`：`Repository` 中 `handbook_status` 之后加：

```ts
  handbook_commit?: string
  handbook_version?: number
  handbook_stats?: HandbookStats | null
```

文件末尾加：

```ts
export interface HandbookStats {
  generator_version?: string
  built_at?: string
  duration_ms?: number
  files?: number
  go_files?: number
  packages?: number
  areas?: number
  symbols?: number
  registers?: number
  truncated?: boolean
  last_error?: string
  failed_commit?: string
}

export interface HandbookView {
  repo_id: string
  status: string
  commit: string
  head_commit: string
  version: number
  stats?: HandbookStats | null
  pages: string[]
}
```

`web/src/api/repoRegistry.ts`：import 加 `HandbookView`，`repoApi` 加：

```ts
  handbook: (id: string) => request<HandbookView>(`/repos/${enc(id)}/handbook`),
  handbookPage: (id: string, path: string) =>
    request<{ path: string; content: string }>(`/repos/${enc(id)}/handbook/page?path=${enc(path)}`),
  rebuildHandbook: (id: string) =>
    request<{ accepted: boolean }>(`/repos/${enc(id)}/handbook/rebuild`, send('POST')),
```

`web/src/utils/repoRegistry.ts` 末尾：

```ts
export type HandbookState = 'none' | 'building' | 'ready' | 'outdated' | 'failed'

export const HANDBOOK_STATE_LABELS: Record<HandbookState, string> = {
  none: '未生成',
  building: '生成中',
  ready: '最新',
  outdated: '待更新',
  failed: '失败',
}

/** Derives the display state; a ready handbook built from an older commit is outdated. */
export function handbookState(
  r: Pick<Repository, 'handbook_status' | 'head_commit'> & { handbook_commit?: string },
): HandbookState {
  if (r.handbook_status === 'building') return 'building'
  if (r.handbook_status === 'failed') return 'failed'
  if (!r.handbook_commit) return 'none'
  return r.handbook_commit === r.head_commit ? 'ready' : 'outdated'
}

/** Orders pages as SKILL.md, then references/*.md, then area pages. */
export function sortHandbookPages(pages: string[]): string[] {
  const rank = (p: string) => (p === 'SKILL.md' ? 0 : p.startsWith('references/areas/') ? 2 : 1)
  return [...pages].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
```

- [ ] **Step 4: 运行单元测试确认通过**

Run: `npm --prefix web test`
Expected: PASS（数量 = 之前的 109 + 新增）。

- [ ] **Step 5: 写失败的 e2e**

`web/e2e/repo-registry.spec.ts`：

1. `mockRepoRegistry` 的 opts 类型加 `repos?: unknown[]`，列表分支改为 `json: { items: opts.repos ?? [repoA, repoB], total: (opts.repos ?? [repoA, repoB]).length }`。
2. `test.describe` 内追加：

```ts
  test('仓库页展示 handbook 状态，可浏览与重建', async ({ page }) => {
    const rebuilt: string[] = []
    const withHandbook = {
      ...repoA,
      handbook_status: 'ready',
      handbook_commit: repoA.head_commit,
      handbook_version: 2,
      handbook_stats: { files: 12 },
    }
    await mockRepoRegistry(page, { repos: [withHandbook, repoB] })
    await page.route(/\/api\/v1\/repos\/[^/]+\/handbook(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
      const url = new URL(route.request().url())
      const id = url.pathname.split('/')[4]
      if (url.pathname.endsWith('/handbook/rebuild')) {
        rebuilt.push(id)
        await route.fulfill({ json: { accepted: true } })
        return
      }
      if (url.pathname.endsWith('/handbook/page')) {
        const p = url.searchParams.get('path')
        await route.fulfill({ json: { path: p, content: p === 'SKILL.md' ? 'Handbook body' : `page ${p}` } })
        return
      }
      await route.fulfill({
        json: {
          repo_id: id,
          status: 'ready',
          commit: repoA.head_commit,
          head_commit: repoA.head_commit,
          version: 2,
          pages: ['references/index.md', 'SKILL.md'],
        },
      })
    })

    await page.goto('/repos')
    await expect(page.getByTestId('handbook-state-r-a')).toHaveText('最新')
    await expect(page.getByTestId('handbook-state-r-b')).toHaveText('未生成')

    await page.getByRole('button', { name: '查看 cloudgame/svc-a 的 handbook' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByTestId('handbook-content')).toHaveText('Handbook body')
    await dialog.getByRole('button', { name: 'references/index.md' }).click()
    await expect(dialog.getByTestId('handbook-content')).toHaveText('page references/index.md')
    await dialog.getByRole('button', { name: '关闭' }).click()
    await expect(dialog).toHaveCount(0)

    await expect(page.getByRole('button', { name: '查看 cloudgame/svc-b 的 handbook' })).toBeDisabled()
    await page.getByRole('button', { name: '重建 cloudgame/svc-b 的 handbook' }).click()
    await expect(page.getByTestId('handbook-state-r-b')).toHaveText('生成中')
    expect(rebuilt).toEqual(['r-b'])
  })
```

Run: `cd web; npx playwright test e2e/repo-registry.spec.ts`
Expected: 新用例 FAIL（页面还没有 Handbook 列）。

- [ ] **Step 6: 实现弹窗与列**

`web/src/components/HandbookDialog.tsx`：

```tsx
import { useEffect, useState } from 'react'
import { repoApi } from '../api/repoRegistry'
import type { HandbookView, Repository } from '../api/repoRegistryTypes'
import { HANDBOOK_STATE_LABELS, handbookState, shortCommit, sortHandbookPages } from '../utils/repoRegistry'
import './ConfirmDialog.css'

interface HandbookDialogProps {
  repo: Repository | null
  onClose: () => void
}

export function HandbookDialog({ repo, onClose }: HandbookDialogProps) {
  const [view, setView] = useState<HandbookView | null>(null)
  const [page, setPage] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState('')
  const repoId = repo?.id ?? ''

  useEffect(() => {
    if (!repoId) return
    let cancelled = false
    setView(null)
    setPage('')
    setContent('')
    setError('')
    repoApi
      .handbook(repoId)
      .then((v) => {
        if (cancelled) return
        setView(v)
        setPage(sortHandbookPages(v.pages)[0] ?? '')
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [repoId])

  useEffect(() => {
    if (!repoId || !page) return
    let cancelled = false
    setContent('')
    repoApi
      .handbookPage(repoId, page)
      .then((p) => {
        if (!cancelled) setContent(p.content)
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [repoId, page])

  if (!repo) return null
  const pages = view ? sortHandbookPages(view.pages) : []
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div
        className="confirm-dialog form-dialog handbook-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="handbook-dialog-title"
      >
        <h2 id="handbook-dialog-title">Handbook · {repo.rel_path}</h2>
        {view ? (
          <p className="muted">
            {HANDBOOK_STATE_LABELS[handbookState(repo)]} · v{view.version} · commit{' '}
            <code>{shortCommit(view.commit)}</code>
          </p>
        ) : null}
        {error ? <div className="error">{error}</div> : null}
        <div className="handbook-dialog__body">
          <ul className="handbook-dialog__pages" aria-label="handbook 页面">
            {pages.map((p) => (
              <li key={p}>
                <button
                  type="button"
                  className={`btn btn-ghost btn-sm ${p === page ? 'active' : ''}`}
                  aria-pressed={p === page}
                  onClick={() => setPage(p)}
                >
                  {p}
                </button>
              </li>
            ))}
          </ul>
          <pre className="handbook-dialog__content" data-testid="handbook-content">
            {content}
          </pre>
        </div>
        <div className="confirm-dialog-actions">
          <button type="button" className="btn btn-secondary" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  )
}
```

`web/src/pages/RepoListPage.tsx`：

1. import 加 `HandbookDialog`（`'../components/HandbookDialog'`）、`HANDBOOK_STATE_LABELS`、`handbookState`。
2. state：

```tsx
  const [viewing, setViewing] = useState<Repository | null>(null)
  const [rebuildingId, setRebuildingId] = useState<string | null>(null)
```

3. 方法（放在 `confirmStatus` 之后）：

```tsx
  const requestRebuild = async (repo: Repository) => {
    setRebuildingId(repo.id)
    try {
      await repoApi.rebuildHandbook(repo.id)
      setRepos((prev) => prev.map((r) => (r.id === repo.id ? { ...r, handbook_status: 'building' } : r)))
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setRebuildingId(null)
    }
  }
```

4. 表头 `<th>状态</th>` 之后加 `<th>Handbook</th>`；对应单元格（状态单元格之后）：

```tsx
                  <td>
                    <span
                      className={`badge badge-handbook-${handbookState(r)}`}
                      title={r.handbook_stats?.last_error ?? ''}
                      data-testid={`handbook-state-${r.id}`}
                    >
                      {HANDBOOK_STATE_LABELS[handbookState(r)]}
                    </span>
                  </td>
```

5. `row-actions` 中「编辑」按钮之前加：

```tsx
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        aria-label={`查看 ${r.rel_path} 的 handbook`}
                        disabled={!r.handbook_version}
                        onClick={() => setViewing(r)}
                      >
                        Handbook
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        aria-label={`重建 ${r.rel_path} 的 handbook`}
                        disabled={r.status !== 'active' || rebuildingId === r.id || handbookState(r) === 'building'}
                        onClick={() => void requestRebuild(r)}
                      >
                        重建
                      </button>
```

6. 组件末尾 `ConfirmDialog` 之后：

```tsx
      <HandbookDialog repo={viewing} onClose={() => setViewing(null)} />
```

7. 页头说明 `page-sub` 改为：`code root 下自动发现的 git 仓库。启动时和每 10 分钟扫描一次，HEAD 变化后自动重建 handbook。`

`web/src/pages/RepoRegistry.css` 末尾：

```css
.badge-handbook-ready {
  background: color-mix(in srgb, #16a34a 15%, transparent);
  color: #15803d;
}

.badge-handbook-outdated,
.badge-handbook-building {
  background: color-mix(in srgb, var(--warning) 18%, transparent);
  color: #b45309;
}

.badge-handbook-failed {
  background: color-mix(in srgb, #dc2626 15%, transparent);
  color: #b91c1c;
}

.badge-handbook-none {
  background: color-mix(in srgb, #6b7280 15%, transparent);
  color: #4b5563;
}

.handbook-dialog {
  width: min(1100px, 94vw);
  max-width: none;
}

.handbook-dialog__body {
  display: grid;
  grid-template-columns: 260px 1fr;
  gap: 1rem;
  height: 60vh;
}

.handbook-dialog__pages {
  list-style: none;
  margin: 0;
  padding: 0;
  overflow: auto;
}

.handbook-dialog__pages .btn {
  width: 100%;
  justify-content: flex-start;
  text-align: left;
  font-family: var(--mono);
  font-size: 12px;
}

.handbook-dialog__pages .btn.active {
  background: var(--accent-subtle);
}

.handbook-dialog__content {
  margin: 0;
  overflow: auto;
  white-space: pre-wrap;
  font-family: var(--mono);
  font-size: 12px;
  padding: 0.75rem;
  border: 1px solid var(--border);
  border-radius: 6px;
}
```

- [ ] **Step 7: 运行全部前端验证**

```powershell
npm --prefix web test
npm --prefix web run build
cd web; npx playwright test e2e/repo-registry.spec.ts
```

Expected: 单元测试全 PASS；构建成功；e2e 6/6 通过。

- [ ] **Step 8: 提交**

```bash
git add web/src/api/repoRegistryTypes.ts web/src/api/repoRegistry.ts web/src/utils/repoRegistry.ts web/src/components/HandbookDialog.tsx web/src/pages/RepoListPage.tsx web/src/pages/RepoRegistry.css web/tests/repoRegistry.test.ts web/e2e/repo-registry.spec.ts
git commit -m "feat(web): repo handbook status, viewer and rebuild"
```

---

### Task 12: 文档同步与全量验证

**Files:**
- Modify: `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md`

- [ ] **Step 1: 更新设计文档**

1. §7.1 存储布局：`v<N>/` 下的 `generated/` 标注"P2b"；`skill/references/` 增加 `areas/<id>.md`（P2a 目录分区），`stages/<id>.md` 标注"P2b"；`facts/` 列出 P2a 实际文件（`files.json`、`symbols.json`、`registers.json`、`packages.json`），`graph.json` 标注"P2b"。
2. §7.2 Phase I 末尾加一段"P2a 实现"：不解析 `.gitignore`，固定跳过的目录与文件类型；Go 用 `go/parser` 产出符号与函数 body 指纹，不做调用图；寄存器候选的四类正则及"源码只匹配大写 SQL 关键字"的约束；`TableName()` 字面量记为表引用。
3. §8.2 并发：写明租约列 `handbook_lease_until`（migration `020_repo_handbook.sql`）、租约 30 分钟、失败的 commit 不自动重试（HEAD 变化或手动重建才重试）、`generator_version` 变化触发全部重建。
4. §11 API：把 `GET /api/v1/repos/{id}/handbook`、`GET /api/v1/repos/{id}/handbook/page?path=`、`POST /api/v1/repos/{id}/handbook/rebuild` 从"后续阶段"移到已实现表，错误码补 `HANDBOOK_BUILDING`（409）、`HANDBOOK_NOT_FOUND`（404）、`HANDBOOK_DISABLED`（503）。
5. §12 前端：加"P2a 已实现：仓库表 Handbook 列（未生成/生成中/最新/待更新/失败）、查看弹窗、重建按钮"。
6. §15 分期：P2 拆为 P2a（本计划，验收：任选 1 个业务仓库生成 handbook，agent 绑定后 `code-map` 出现在摘要首位、handbook 不进摘要；HEAD 变化后下一次扫描内重建）与 P2b（LLM 文件卡片、行为阶段、总览、增量刷新、骨架重建、冻结）。
7. §16 文件清单：`020_rca_feedback.sql` 改为 `021_rca_feedback.sql`；新增 `portal/migrations/020_repo_handbook.sql`、`portal/internal/handbook/` 实际文件、`service/handbook_dirs.go`、`web/src/components/HandbookDialog.tsx`。
8. §18 待决问题 1：改为已决——"handbook 生成模型（P2b）从模型目录按 `provider/model` 选择（同 critic 模型解析方式）；预算在 P2b 计划中定"。

- [ ] **Step 2: 全量验证**

```powershell
cd framework; go test -p 1 ./skills/... ./tool/skillops/...; cd ..
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"
cd portal
go build -p 1 ./cmd/backend/
go test -p 1 ./internal/handbook/ ./internal/biz/ ./internal/data/ ./internal/service/ ./internal/server/ ./internal/cron/ ./internal/chat/
cd ..
npm --prefix web test
npm --prefix web run build
cd web; npx playwright test e2e/repo-registry.spec.ts; cd ..
git status --short
```

Expected: 全部通过（`internal/chat` 仅允许已知无关失败 `TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook`）；`git status` 只剩 `evals/` 下的未跟踪文件。

- [ ] **Step 3: 提交**

```bash
git add docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md
git commit -m "docs: sync repo handbook design with P2a implementation"
```
