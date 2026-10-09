# 仓库 Handbook P2b（LLM 增强层）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 P2a 确定性 handbook 之上加一层 LLM 增强：为非测试源码文件生成文件卡片，推断执行阶段（stage）骨架，写阶段说明、仓库总览和寄存器用途；按预算分轮后台运行、按内容哈希缓存、HEAD 变化后增量刷新、满足阈值时重建骨架；模型从模型目录按 `provider/model` 选择（全局配置 + 仓库级覆盖）；仓库页展示 LLM 进度。

**Architecture:** 两层。确定性层（P2a 的 `Build`）不变地负责每次 HEAD 变化后的快速发布，只是渲染时额外读取 LLM 缓存（卡片按文件内容 sha256 命中，内容变了的文件显示旧卡片并标"已过期"）。LLM 层是独立的后台任务 `EnrichPending`：持有独立的 LLM 租约，每轮在时间与文件数预算内生成缺失卡片，卡片全部尝试过后做骨架/阶段/总览/寄存器合成，结果只写缓存目录 `handbooks/repos/<id>/llm/`（不在版本目录里），完成后更新 `handbook_llm.rev` 并触发一次确定性重建把新内容渲染发布。纯逻辑都在 `portal/internal/handbook`（模型通过 `framework/model.Model` 接口注入，测试用假模型），biz 负责租约、预算、状态；service 层把模型目录解析成 `model.Model`。

**Tech Stack:** Go 1.26、`github.com/sixath/framework/model`、GORM（MySQL / SQLite 测试）、kratos HTTP、React 19 + TypeScript、node:test、Playwright。

**设计文档:** `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md` §7.2 Phase II/III、§8.1、§8.3、§15 P2b。P2a 计划：`docs/superpowers/plans/2026-10-09-repo-handbook-p2a.md`。

---

## 约定（每个任务都适用）

- 工作分支：`main`，每个任务一个提交。**只 `git add` 本任务列出的文件，绝不暂存 `evals/`。不要 push。**
- Portal 构建与测试必须 `-p 1`，**禁止** `go build ./...`（会 OOM）。涉及 SQLite 的测试（`portal/internal/data`）需要 CGO：

```powershell
$env:CGO_ENABLED="1"; $env:PATH="D:\tool\mingw64\bin;$env:PATH"
```

- 检查 backend 能编译：`cd portal; go build -p 1 -o $env:TEMP\backend-check.exe ./cmd/backend/`，之后删除产物。
- Web：`npm --prefix web test`（node:test，被测模块只能 `import type`）、`npm --prefix web run build`、e2e `cd web; npx playwright test e2e/repo-registry.spec.ts`。
- 新文件用 gofmt 且 LF 换行（Write 工具可能写出 CRLF，提交前 `gofmt -w`）。
- 已知无关失败：`portal/internal/chat` 的 `TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook`、`TestNotifySessionMessageIndexed_WithDetachedCaller`（SQLite 锁，偶发）；e2e 的 `session-sidebar.spec.ts`、`compact-boundary.spec.ts`（选择器过时）。
- `framework/model` 关键 API（已核实）：`type Model interface { Chat(ctx, []Message, ...Option) (*Generation, error); Generate(...); Embed(...) }`；`Message{Role string; Content string; ...}`（role 用 `"system"`/`"user"`）；`Generation{Text string; TokenUsage *TokenUsage{InputTokens, OutputTokens int}; FinishReason string}`；`model.WithMaxTokens(n)`、`model.WithTemperature(t float32)`。**OpenAI 兼容实现里 temperature 为 0 时会被替换为默认 0.7，所以一律用 0.2。**

## 关键决策

| 项 | 决定 |
|---|---|
| 分层 | 确定性层每次 HEAD 变化立即发布；LLM 层独立租约后台补齐，完成后触发确定性重建渲染 |
| 模型 | 全局 `handbook.model`（portal 配置文件），仓库级 `handbook_model` 覆盖：空 = 继承全局，`off` = 该仓库禁用；最终名字为空则 LLM 层禁用。解析方式与 critic 相同（精确模型名，或 `<provider 名称或 ID>/<模型名>`） |
| 卡片范围 | `CardEligible`：非测试、语言属于源码类（go、proto、sql、python、java、js/ts、shell、lua、c/cpp、rust、php、ruby、kotlin、csharp、scala、vue）；测试、文档、配置只在清单里 |
| 卡片缓存键 | 文件内容 sha256 + `LLMPromptVersion`；换模型不作废卡片 |
| 预算 | 每轮最多 `max_cards_per_run`（默认 600）个新卡片、`max_run_minutes`（默认 15）分钟、并发 `concurrency`（默认 4）；跑不完下轮继续（状态 `partial`） |
| 合成触发 | 本轮所有缺卡片文件都已尝试过（没有被预算或时间截断）才合成；个别文件卡片失败不阻塞合成（计入 `card_errors`，不重试到 HEAD 变化） |
| 骨架 | LLM 按**目录**分配阶段（每个目录归一个阶段，文件继承），3–15 个阶段；提示词超过 120KB 或回复不可用时退化为按目录分区作阶段（`fallback`） |
| 骨架重建阈值 | 无骨架 / 提示词版本变化 / 距上次重建 > `skeleton_rebuild_days`（默认 30）/ 自上次重建改动文件 > 20% / 出现新顶层目录 / 未归类 > 10% / 手动"重新生成"；否则增量：新文件按同目录或上级目录多数阶段归类，改动文件保留阶段，受影响阶段重写说明 |
| 冻结 | 渲染时内容哈希没有对应卡片、但骨架里有旧卡片的文件标"已过期，以源码为准"；设计文档"冻结比例 > 15% 触发重建"不做（每轮增量就会补上新卡片） |
| 失败 | 模型连续 5 次调用出错（非回复格式问题）→ 本轮失败，`handbook_llm.state=failed`、`failed_commit`=当前 commit，HEAD 变化或手动"重新生成"前不重试；父 context 取消 → 释放租约不记失败 |
| 状态存储 | 新列 `handbook_model`、`handbook_llm`（JSON）、`handbook_llm_lease_until`、`handbook_llm_lease_token`（migration `021_repo_handbook_llm.sql`；设计文档里的 `rca_feedback` 顺延为 022） |
| 版本 | `GeneratorVersion` → `p2b-1`（全部仓库确定性重建一次，很快） |

## `handbook_llm` JSON 字段

| 字段 | 含义 |
|---|---|
| `state` | `partial` / `complete` / `failed` |
| `model` | 本轮使用的模型名（配置值） |
| `commit` | 本轮基于的 handbook commit |
| `prompt_version` | `handbook.LLMPromptVersion` |
| `cards_total` / `cards_done` / `cards_new` / `card_errors` | 需要卡片的文件数 / 已有卡片数 / 本轮新生成 / 本轮回复不可用 |
| `stages` / `fallback` / `skeleton_rebuilt` / `rebuild_reason` / `skeleton_built_at` | 骨架信息 |
| `tokens_in` / `tokens_out` | 本轮 token |
| `run_at` / `duration_ms` | 本轮开始时间与耗时 |
| `rev` | 内容修订号；变化即触发确定性重建（确定性构建把它写进 `handbook_stats.llm_rev`） |
| `last_error` / `failed_commit` | 失败信息 |

## 文件结构

| 操作 | 文件 | 职责 |
|---|---|---|
| CREATE | `portal/internal/handbook/llmcache.go` | `CardEligible`、`Card`、`Skeleton`、`LLMCache`（卡片/骨架读写与清理）、`LLMLayer`、`LoadLLMLayer` |
| CREATE | `portal/internal/handbook/llmcall.go` | `llmJSON`、`usage`、`extractJSONObject`、`errBadReply`、`clipRunes` |
| CREATE | `portal/internal/handbook/cards.go` | 卡片提示词、`generateCard`、`sanitizeCard`、`clipContent` |
| CREATE | `portal/internal/handbook/organize.go` | 骨架推断（LLM + 退化）、增量更新、重建判定 |
| CREATE | `portal/internal/handbook/synthesize.go` | 阶段说明、总览、寄存器用途 |
| CREATE | `portal/internal/handbook/enrich.go` | `Enrich` 编排：预算、并发、token、合成、清理 |
| MODIFY | `portal/internal/handbook/store.go` | `LLMCache(id)`、`ReadFacts(id, v)` |
| MODIFY | `portal/internal/handbook/render.go`、`builder.go` | 渲染卡片/阶段/总览/寄存器用途；`BuildInput.LLMDir`/`LLMRev`；`Stats` 新字段；`GeneratorVersion = "p2b-1"` |
| CREATE | `portal/internal/handbook/*_test.go`（`llmcache_test.go`、`cards_test.go`、`organize_test.go`、`synthesize_test.go`、`enrich_test.go`、`fakemodel_test.go`、`render_llm_test.go`） | 单元测试 |
| CREATE | `portal/migrations/021_repo_handbook_llm.sql` | 四个新列 |
| MODIFY | `portal/internal/data/model/repo_registry.go`、`portal/internal/data/repo_registry.go` | 列映射、LLM 租约三个方法、`handbook_model` 更新 |
| MODIFY | `portal/internal/biz/repo_registry.go`、`repo_registry_usecase.go` | `Repository` 新字段、`RepoMetaPatch.HandbookModel`、仓储接口、校验 |
| CREATE | `portal/internal/biz/handbook_llm.go` | `HandbookLLMConfig`、`SetLLM`、`modelFor`、`EnrichPending`、`RequestEnrich`、`runEnrichClaimed` |
| MODIFY | `portal/internal/biz/handbook.go` | 重建判定加 `llm_rev`；构建时传 `LLMDir`/`LLMRev`；`HandbookView` 加 LLM 字段 |
| CREATE | `portal/internal/data/repo_handbook_llm_test.go` | 仓储与 usecase 集成测试（SQLite + 假模型） |
| CREATE | `portal/internal/conf/handbook_config.go`（+ test） | `handbook:` 配置段 |
| MODIFY | `portal/internal/service/critic_model.go` → 抽出 `catalogModelResolver`；CREATE `portal/internal/service/handbook_llm.go` | 模型目录解析、`ConfigureHandbookLLM` |
| MODIFY | `portal/cmd/backend/main.go`、`wire.go`、`wire_gen.go` | 加载配置并接线 |
| MODIFY | `portal/internal/cron/scheduler.go` | 重建后跑 `EnrichPending` |
| MODIFY | `portal/internal/server/repo_registry.go`、`http.go`（+ test） | `POST /repos/{id}/handbook/enrich`、`GET /handbook/config`、PATCH 接受 `handbook_model` |
| MODIFY | `web/src/api/repoRegistryTypes.ts`、`api/repoRegistry.ts`、`utils/repoRegistry.ts`、`components/HandbookDialog.tsx`、`pages/RepoListPage.tsx`、`pages/RepoRegistry.css` | 类型、API、LLM 状态、编辑模型、弹窗 LLM 区块 |
| MODIFY | `web/tests/repoRegistry.test.ts`、`web/e2e/repo-registry.spec.ts` | 测试 |
| MODIFY | 设计文档、`portal/configs/config.yaml` | 同步与配置示例 |

---

### Task 1: LLM 缓存、卡片与骨架类型、加载 LLM 层

**Files:**
- Create: `portal/internal/handbook/llmcache.go`
- Modify: `portal/internal/handbook/store.go`
- Test: `portal/internal/handbook/llmcache_test.go`

- [ ] **Step 1: 写失败测试**

```go
package handbook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hashOf(s string) string { return strings.Repeat(s, 64)[:64] }

func TestCardEligible(t *testing.T) {
	cases := []struct {
		f    File
		want bool
	}{
		{File{Path: "a.go", Lang: "go"}, true},
		{File{Path: "a_test.go", Lang: "go", Test: true}, false},
		{File{Path: "api.proto", Lang: "proto"}, true},
		{File{Path: "schema.sql", Lang: "sql"}, true},
		{File{Path: "README.md", Lang: "markdown"}, false},
		{File{Path: "config.yaml", Lang: "yaml"}, false},
		{File{Path: "Makefile", Lang: ""}, false},
	}
	for _, c := range cases {
		if got := CardEligible(c.f); got != c.want {
			t.Errorf("%s: got %v", c.f.Path, got)
		}
	}
}

func TestLLMCache_CardsAndSkeleton(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	h := hashOf("a")
	if got, err := c.Card(h); err != nil || got != nil {
		t.Fatalf("missing card: %v %v", got, err)
	}
	card := &Card{Purpose: "处理订单", Role: "service", Hash: h, PromptVersion: LLMPromptVersion}
	if err := c.PutCard(h, card); err != nil {
		t.Fatal(err)
	}
	got, err := c.Card(h)
	if err != nil || got == nil || got.Purpose != "处理订单" {
		t.Fatalf("card: %#v %v", got, err)
	}
	if _, err := c.Card("../x"); err == nil {
		t.Fatal("bad hash must be rejected")
	}
	if sk, err := c.Skeleton(); err != nil || sk != nil {
		t.Fatalf("missing skeleton: %v %v", sk, err)
	}
	sk := &Skeleton{PromptVersion: LLMPromptVersion, BuiltAt: time.Unix(100, 0).UTC(), Stages: []Stage{{ID: "s1", Title: "下单"}},
		Files: map[string]FileAssign{"a.go": {Stage: "s1", CardHash: h}}}
	if err := c.PutSkeleton(sk); err != nil {
		t.Fatal(err)
	}
	back, err := c.Skeleton()
	if err != nil || back == nil || back.Files["a.go"].Stage != "s1" {
		t.Fatalf("skeleton: %#v %v", back, err)
	}
}

func TestLLMCache_PruneCards(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	keep, drop := hashOf("b"), hashOf("c")
	for _, h := range []string{keep, drop} {
		if err := c.PutCard(h, &Card{Purpose: "x", Hash: h}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := c.PruneCards(map[string]bool{keep: true})
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if got, _ := c.Card(keep); got == nil {
		t.Fatal("kept card removed")
	}
	if got, _ := c.Card(drop); got != nil {
		t.Fatal("dropped card still present")
	}
}

func TestLoadLLMLayer_CurrentAndStaleCards(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	oldH, newH, keptH := hashOf("d"), hashOf("e"), hashOf("f")
	_ = c.PutCard(oldH, &Card{Purpose: "旧", Hash: oldH})
	_ = c.PutCard(keptH, &Card{Purpose: "现", Hash: keptH})
	_ = c.PutSkeleton(&Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "s", Title: "S"}},
		Files: map[string]FileAssign{"changed.go": {Stage: "s", CardHash: oldH}, "kept.go": {Stage: "s", CardHash: keptH}}})
	f := &Facts{Files: []File{
		{Path: "changed.go", Lang: "go", Hash: newH},
		{Path: "kept.go", Lang: "go", Hash: keptH},
		{Path: "kept_test.go", Lang: "go", Hash: keptH, Test: true},
	}}
	l, err := LoadLLMLayer(c, f)
	if err != nil {
		t.Fatal(err)
	}
	if l.Skeleton == nil || l.Cards["kept.go"] == nil || l.Cards["kept_test.go"] != nil {
		t.Fatalf("layer: %#v", l)
	}
	if l.Stale["changed.go"] == nil || l.Stale["changed.go"].Purpose != "旧" || l.Cards["changed.go"] != nil {
		t.Fatalf("stale: %#v", l.Stale)
	}
}

func TestLoadLLMLayer_IgnoresOldPromptSkeleton(t *testing.T) {
	c := LLMCache{Dir: t.TempDir()}
	_ = c.PutSkeleton(&Skeleton{PromptVersion: "old", Stages: []Stage{{ID: "s"}}})
	l, err := LoadLLMLayer(c, &Facts{})
	if err != nil || l.Skeleton != nil || !l.Empty() {
		t.Fatalf("layer %#v %v", l, err)
	}
}

func TestStore_LLMCacheAndReadFacts(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if _, err := s.LLMCache("../x"); err == nil {
		t.Fatal("bad id must be rejected")
	}
	c, err := s.LLMCache("r1")
	if err != nil || c.Dir != filepath.Join(s.Root, "repos", "r1", "llm") {
		t.Fatalf("cache %#v %v", c, err)
	}
	files := map[string][]byte{
		"facts/files.json":     []byte(`[{"path":"a.go","lang":"go","size":3,"lines":1,"hash":"` + hashOf("a") + `"}]`),
		"facts/symbols.json":   []byte(`{"a.go":[{"kind":"func","name":"A","line":1,"end_line":1}]}`),
		"facts/registers.json": []byte(`[]`),
		"facts/packages.json":  []byte(`[{"dir":".","name":"a","files":1}]`),
		"skill/SKILL.md":       []byte("x"),
	}
	if err := s.Publish("r1", 1, files, 2); err != nil {
		t.Fatal(err)
	}
	f, err := s.ReadFacts("r1", 1)
	if err != nil || len(f.Files) != 1 || len(f.Symbols["a.go"]) != 1 || len(f.Packages) != 1 {
		t.Fatalf("facts %#v %v", f, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "repos", "r1", "llm")); !os.IsNotExist(err) {
		t.Fatal("LLMCache must not create directories")
	}
}
```

> `Symbol` 的 JSON 字段名以 `gosyms.go` 为准（若不是 `end_line`，按实际修改测试里的 JSON）。

- [ ] **Step 2: 运行，确认编译失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "CardEligible|LLMCache|LoadLLMLayer|Store_LLMCache"`
Expected: FAIL（`undefined: CardEligible` 等）

- [ ] **Step 3: 实现 `llmcache.go`**

```go
package handbook

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LLMPromptVersion changes whenever card or skeleton prompts change; cached output of an older
// version is ignored and regenerated.
const LLMPromptVersion = "p2b-1"

var cardLangs = map[string]bool{
	"go": true, "proto": true, "sql": true, "python": true, "java": true, "javascript": true,
	"typescript": true, "shell": true, "lua": true, "c": true, "cpp": true, "rust": true,
	"php": true, "ruby": true, "kotlin": true, "csharp": true, "scala": true, "vue": true,
}

// CardEligible reports whether a file gets an LLM card: non-test source code.
func CardEligible(f File) bool { return !f.Test && cardLangs[f.Lang] }

// CardFunc is one key function of a card; Name is always one of the file's symbols.
type CardFunc struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Card is the LLM description of one file content (keyed by its sha256).
type Card struct {
	Purpose       string     `json:"purpose"`
	Description   string     `json:"description,omitempty"`
	Role          string     `json:"role,omitempty"`
	Lifecycle     string     `json:"lifecycle,omitempty"`
	Functions     []CardFunc `json:"functions,omitempty"`
	Hash          string     `json:"hash"`
	PromptVersion string     `json:"prompt_version"`
	Model         string     `json:"model,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Stage is one behavioral stage of a repository.
type Stage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// FileAssign records a card-eligible file's stage ("" = unassigned) and the content hash of
// the card it was organized with.
type FileAssign struct {
	Stage    string `json:"stage,omitempty"`
	CardHash string `json:"card_hash"`
}

// Skeleton is the LLM organization of a repository: stages, file assignment, overview and
// register notes. It lives outside versioned dirs and is updated incrementally.
type Skeleton struct {
	PromptVersion       string                `json:"prompt_version"`
	Commit              string                `json:"commit"`
	BuiltAt             time.Time             `json:"built_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	BaseFiles           int                   `json:"base_files"`
	ChangedSinceRebuild int                   `json:"changed_since_rebuild"`
	TopDirs             []string              `json:"top_dirs"`
	FallbackAreas       bool                  `json:"fallback_areas,omitempty"`
	Stages              []Stage               `json:"stages"`
	Files               map[string]FileAssign `json:"files"`
	Overview            string                `json:"overview,omitempty"`
	RegisterNotes       map[string]string     `json:"register_notes,omitempty"`
}

// LLMCache stores LLM output of one repository outside its versioned dirs.
type LLMCache struct{ Dir string }

var errBadHash = errors.New("handbook: invalid content hash")

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

func (c LLMCache) cardPath(hash string) string {
	return filepath.Join(c.Dir, "cards", hash[:2], hash+"-"+LLMPromptVersion+".json")
}

func (c LLMCache) skeletonPath() string { return filepath.Join(c.Dir, "skeleton.json") }

// Card returns the cached card of a content hash, or nil when there is none.
func (c LLMCache) Card(hash string) (*Card, error) {
	if !validHash(hash) {
		return nil, errBadHash
	}
	var card Card
	if ok, err := readJSON(c.cardPath(hash), &card); !ok || err != nil {
		return nil, err
	}
	return &card, nil
}

// PutCard stores the card of a content hash.
func (c LLMCache) PutCard(hash string, card *Card) error {
	if !validHash(hash) {
		return errBadHash
	}
	return writeJSON(c.cardPath(hash), card)
}

// Skeleton returns the cached skeleton, or nil when there is none.
func (c LLMCache) Skeleton() (*Skeleton, error) {
	var sk Skeleton
	if ok, err := readJSON(c.skeletonPath(), &sk); !ok || err != nil {
		return nil, err
	}
	if sk.Files == nil {
		sk.Files = map[string]FileAssign{}
	}
	return &sk, nil
}

// PutSkeleton stores the skeleton.
func (c LLMCache) PutSkeleton(sk *Skeleton) error { return writeJSON(c.skeletonPath(), sk) }

// PruneCards removes cached cards whose content hash is not in keep and returns how many
// were removed.
func (c LLMCache) PruneCards(keep map[string]bool) (int, error) {
	n := 0
	err := filepath.WalkDir(filepath.Join(c.Dir, "cards"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		hash, _, _ := strings.Cut(d.Name(), "-")
		if keep[hash] && strings.HasSuffix(d.Name(), "-"+LLMPromptVersion+".json") {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

func readJSON(p string, v any) (bool, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("handbook: decode %s: %w", filepath.Base(p), err)
	}
	return true, nil
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(p, b)
}

// LLMLayer is the LLM content available to one render.
type LLMLayer struct {
	Cards    map[string]*Card // path -> card of the file's current content
	Stale    map[string]*Card // path -> last card of a file whose content changed since
	Skeleton *Skeleton        // nil until the first synthesis
}

// Empty reports whether the layer adds nothing to a render.
func (l *LLMLayer) Empty() bool {
	return l == nil || (len(l.Cards) == 0 && len(l.Stale) == 0 && l.Skeleton == nil)
}

// LoadLLMLayer reads the cached cards of f's card-eligible files and the skeleton.
func LoadLLMLayer(c LLMCache, f *Facts) (*LLMLayer, error) {
	l := &LLMLayer{Cards: map[string]*Card{}, Stale: map[string]*Card{}}
	sk, err := c.Skeleton()
	if err != nil {
		return nil, err
	}
	if sk != nil && sk.PromptVersion == LLMPromptVersion {
		l.Skeleton = sk
	}
	for _, file := range f.Files {
		if !CardEligible(file) || !validHash(file.Hash) {
			continue
		}
		card, err := c.Card(file.Hash)
		if err != nil {
			return nil, err
		}
		if card != nil {
			l.Cards[file.Path] = card
			continue
		}
		if l.Skeleton == nil {
			continue
		}
		if a, ok := l.Skeleton.Files[file.Path]; ok && a.CardHash != "" && a.CardHash != file.Hash && validHash(a.CardHash) {
			if old, err := c.Card(a.CardHash); err == nil && old != nil {
				l.Stale[file.Path] = old
			}
		}
	}
	return l, nil
}
```

- [ ] **Step 4: `store.go` 加两个方法**

在 `ListSkillFiles` 之前加入（`checkID` 是现有的 id 校验函数）：

```go
// LLMCache returns the LLM cache of a repository; the directory is created on first write.
func (s Store) LLMCache(id string) (LLMCache, error) {
	if err := checkID(id); err != nil {
		return LLMCache{}, err
	}
	return LLMCache{Dir: filepath.Join(s.repoDir(id), "llm")}, nil
}

// ReadFacts loads the facts of a published version.
func (s Store) ReadFacts(id string, v int) (*Facts, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.VersionDir(id, v), "facts")
	f := &Facts{Symbols: map[string][]Symbol{}}
	for name, dst := range map[string]any{
		"files.json": &f.Files, "symbols.json": &f.Symbols, "registers.json": &f.Registers, "packages.json": &f.Packages,
	} {
		ok, err := readJSON(filepath.Join(dir, name), dst)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("handbook: %s v%d: missing facts/%s: %w", id, v, name, fs.ErrNotExist)
		}
	}
	if f.Symbols == nil {
		f.Symbols = map[string][]Symbol{}
	}
	return f, nil
}
```

（按需补 `fmt`、`io/fs` import。`prune` 只处理 `v` 开头的目录，`llm/` 不会被清理，无需改动。）

- [ ] **Step 5: 运行测试**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`
Expected: PASS

- [ ] **Step 6: 提交**

```powershell
git add portal/internal/handbook/llmcache.go portal/internal/handbook/llmcache_test.go portal/internal/handbook/store.go
git commit -m "feat(handbook): LLM cache for file cards and stage skeleton"
```

---

### Task 2: 模型调用助手与文件卡片

**Files:**
- Create: `portal/internal/handbook/llmcall.go`、`portal/internal/handbook/cards.go`
- Test: `portal/internal/handbook/fakemodel_test.go`、`portal/internal/handbook/cards_test.go`

- [ ] **Step 1: 写测试用假模型 `fakemodel_test.go`**

```go
package handbook

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/sixath/framework/model"
)

// fakeModel answers by the first rule whose key occurs in the system or user prompt.
type fakeModel struct {
	mu      sync.Mutex
	rules   []fakeRule
	calls   []string // user prompts in call order
	failErr error    // returned by every call when set
}

type fakeRule struct {
	key   string
	reply func(user string) string
}

func (m *fakeModel) on(key string, reply func(user string) string) *fakeModel {
	m.rules = append(m.rules, fakeRule{key, reply})
	return m
}

func (m *fakeModel) Chat(ctx context.Context, msgs []model.Message, _ ...model.Option) (*model.Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var sys, user string
	for _, msg := range msgs {
		if msg.Role == "system" {
			sys = msg.Content
		} else {
			user = msg.Content
		}
	}
	m.mu.Lock()
	m.calls = append(m.calls, user)
	fail := m.failErr
	m.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	for _, r := range m.rules {
		if strings.Contains(sys, r.key) || strings.Contains(user, r.key) {
			return &model.Generation{Text: r.reply(user), TokenUsage: &model.TokenUsage{InputTokens: 10, OutputTokens: 5}, FinishReason: "stop"}, nil
		}
	}
	return nil, errors.New("fake model: no rule")
}

func (m *fakeModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return m.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}

func (m *fakeModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, errors.New("fake model: no embeddings")
}

func (m *fakeModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}
```

> 如果 `model.Model` 接口还有别的方法、或 `Embed` 的签名不同，按 `framework/model/model.go` 补齐。

- [ ] **Step 2: 写失败测试 `cards_test.go`**

```go
package handbook

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExtractJSONObject(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"好的：{\"a\":{\"b\":2}} 以上": `{"a":{"b":2}}`,
		"没有": "",
	}
	for in, want := range cases {
		if got := extractJSONObject(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

func TestClipContent(t *testing.T) {
	src := strings.Repeat("一行中文内容\n", 100)
	got := clipContent([]byte(src), 50)
	if len(got) > 50+len(clippedMark) || !strings.HasSuffix(got, clippedMark) || !strings.HasSuffix(strings.TrimSuffix(got, clippedMark), "\n") {
		t.Fatalf("clip: %q", got)
	}
	if clipContent([]byte("short"), 50) != "short" {
		t.Fatal("short content must be kept")
	}
}

func TestGenerateCard_ValidatesAgainstSymbols(t *testing.T) {
	m := (&fakeModel{}).on("文件卡片", func(string) string {
		return "```json\n" + `{"purpose":"订单存储","description":"读写 orders 表。","role":"Repository","lifecycle":"请求时",` +
			`"functions":[{"name":"(*Store).Get","summary":"按 id 查询"},{"name":"Invented","summary":"不存在"},{"name":"(*Store).Get","summary":"重复"}]}` + "\n```"
	})
	f := File{Path: "internal/order/store.go", Lang: "go", Lines: 20, Hash: hashOf("a")}
	syms := []Symbol{{Kind: "method", Name: "(*Store).Get", Line: 12, EndLine: 15}, {Kind: "method", Name: "(*Store).MarkPaid", Line: 17, EndLine: 20}}
	var u usage
	card, err := generateCard(context.Background(), m, "p/m", "svc-a", f, syms, []byte("package order"), 1024, &u)
	if err != nil {
		t.Fatal(err)
	}
	if card.Purpose != "订单存储" || card.Role != "repository" || card.Hash != f.Hash || card.PromptVersion != LLMPromptVersion || card.Model != "p/m" {
		t.Fatalf("card %#v", card)
	}
	if len(card.Functions) != 1 || card.Functions[0].Name != "(*Store).Get" {
		t.Fatalf("functions must be limited to known symbols without duplicates: %#v", card.Functions)
	}
	if u.in.Load() != 10 || u.out.Load() != 5 {
		t.Fatalf("usage %d/%d", u.in.Load(), u.out.Load())
	}
	if !strings.Contains(m.calls[0], "(*Store).MarkPaid") || !strings.Contains(m.calls[0], "internal/order/store.go") {
		t.Fatalf("prompt must list the file path and symbols: %s", m.calls[0])
	}
}

func TestGenerateCard_NonGoDropsFunctionsAndUnknownRole(t *testing.T) {
	m := (&fakeModel{}).on("文件卡片", func(string) string {
		return `{"purpose":"建表","role":"weird","functions":[{"name":"x","summary":"y"}]}`
	})
	var u usage
	card, err := generateCard(context.Background(), m, "m", "r", File{Path: "s.sql", Lang: "sql", Hash: hashOf("b")}, nil, []byte("CREATE TABLE t"), 1024, &u)
	if err != nil || card.Role != "other" || len(card.Functions) != 0 {
		t.Fatalf("card %#v %v", card, err)
	}
}

func TestGenerateCard_BadReplies(t *testing.T) {
	for name, reply := range map[string]string{"no json": "抱歉", "no purpose": `{"purpose":"  "}`, "broken": `{"purpose": }`} {
		m := (&fakeModel{}).on("文件卡片", func(string) string { return reply })
		var u usage
		_, err := generateCard(context.Background(), m, "m", "r", File{Path: "a.go", Lang: "go", Hash: hashOf("c")}, nil, []byte("x"), 1024, &u)
		if !errors.Is(err, errBadReply) {
			t.Errorf("%s: want errBadReply, got %v", name, err)
		}
	}
}
```

- [ ] **Step 3: 运行，确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "ExtractJSON|ClipContent|GenerateCard"`
Expected: FAIL（未定义）

- [ ] **Step 4: 实现 `llmcall.go`**

```go
package handbook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

// errBadReply marks a model reply that could not be used; the call itself succeeded.
var errBadReply = errors.New("handbook: unusable model reply")

// llmTemperature stays above 0: the OpenAI-compatible client replaces 0 with its default.
const llmTemperature = 0.2

// usage accumulates token counts across concurrent calls.
type usage struct{ in, out atomic.Int64 }

func (u *usage) add(g *model.Generation) {
	if g != nil && g.TokenUsage != nil {
		u.in.Add(int64(g.TokenUsage.InputTokens))
		u.out.Add(int64(g.TokenUsage.OutputTokens))
	}
}

// llmJSON sends one system + user exchange and decodes the JSON object in the reply into out.
func llmJSON(ctx context.Context, m model.Model, system, user string, maxTokens int, out any, u *usage) error {
	g, err := m.Chat(ctx, []model.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		model.WithMaxTokens(maxTokens), model.WithTemperature(llmTemperature))
	if err != nil {
		return err
	}
	u.add(g)
	if g.FinishReason == "length" {
		return fmt.Errorf("%w: reply truncated at %d tokens", errBadReply, maxTokens)
	}
	raw := extractJSONObject(g.Text)
	if raw == "" {
		return fmt.Errorf("%w: no JSON object", errBadReply)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("%w: %v", errBadReply, err)
	}
	return nil
}

// extractJSONObject returns the text from the first '{' to the last '}'.
func extractJSONObject(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return ""
	}
	return s[i : j+1]
}

// clipRunes trims s and keeps at most n runes.
func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}
```

- [ ] **Step 5: 实现 `cards.go`**

```go
package handbook

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/model"
)

const (
	maxCardFuncs   = 8
	cardMaxTokens  = 900
	maxCardSymbols = 80
	clippedMark    = "…（后续内容已截断）\n"
)

var cardRoles = map[string]bool{
	"entry": true, "handler": true, "service": true, "repository": true, "model": true,
	"config": true, "util": true, "client": true, "job": true, "other": true,
}

const cardSystemPrompt = "你是资深后端工程师，为故障排查（RCA）生成代码文件卡片。只输出一个 JSON 对象，不要输出其他文字。"

func cardPrompt(relPath string, f File, syms []Symbol, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "为下面的文件生成文件卡片。\n\n仓库：%s\n文件：%s（%s，%d 行）\n", relPath, f.Path, f.Lang, f.Lines)
	if len(syms) > 0 {
		b.WriteString("\n符号清单（functions.name 只能从这里原样选取）：\n")
		for i, s := range syms {
			if i == maxCardSymbols {
				fmt.Fprintf(&b, "- …另有 %d 个\n", len(syms)-i)
				break
			}
			fmt.Fprintf(&b, "- %s `%s` L%d-%d\n", s.Kind, s.Name, s.Line, s.EndLine)
		}
	}
	fmt.Fprintf(&b, "\n文件内容：\n```%s\n%s\n```\n\n", f.Lang, content)
	b.WriteString(`输出 JSON：{"purpose":"一句话说明文件职责（不超过 60 字）",` +
		`"description":"2-4 句：做什么、被谁调用、读写哪些表/缓存/topic/外部接口",` +
		`"role":"entry|handler|service|repository|model|config|util|client|job|other 之一",` +
		`"lifecycle":"何时执行：启动/请求/定时任务/消息消费等，不确定留空",` +
		`"functions":[{"name":"符号清单中的名字","summary":"一句话"}]}` + "\n")
	fmt.Fprintf(&b, "functions 最多 %d 个，选与业务流程和状态读写关系最大的；没有符号清单时输出空数组。只依据文件内容，不要猜测。\n", maxCardFuncs)
	return b.String()
}

// clipContent keeps at most maxBytes of content, cut at a line boundary.
func clipContent(content []byte, maxBytes int) string {
	if len(content) <= maxBytes {
		return string(content)
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	if i := strings.LastIndexByte(string(content[:cut]), '\n'); i > 0 {
		cut = i + 1
	}
	return string(content[:cut]) + clippedMark
}

type cardReply struct {
	Purpose     string     `json:"purpose"`
	Description string     `json:"description"`
	Role        string     `json:"role"`
	Lifecycle   string     `json:"lifecycle"`
	Functions   []CardFunc `json:"functions"`
}

// generateCard asks the model for the card of one file whose content hashes to f.Hash.
func generateCard(ctx context.Context, m model.Model, modelName, relPath string, f File, syms []Symbol, content []byte, maxBytes int, u *usage) (*Card, error) {
	var r cardReply
	if err := llmJSON(ctx, m, cardSystemPrompt, cardPrompt(relPath, f, syms, clipContent(content, maxBytes)), cardMaxTokens, &r, u); err != nil {
		return nil, err
	}
	card := sanitizeCard(r, syms)
	if card.Purpose == "" {
		return nil, fmt.Errorf("%w: empty purpose", errBadReply)
	}
	card.Hash, card.PromptVersion, card.Model, card.CreatedAt = f.Hash, LLMPromptVersion, modelName, time.Now().UTC()
	return card, nil
}

// sanitizeCard clips fields and keeps only functions that name one of the file's symbols.
func sanitizeCard(r cardReply, syms []Symbol) *Card {
	c := &Card{
		Purpose:     clipRunes(r.Purpose, 120),
		Description: clipRunes(r.Description, 600),
		Lifecycle:   clipRunes(r.Lifecycle, 120),
		Role:        strings.ToLower(strings.TrimSpace(r.Role)),
	}
	if !cardRoles[c.Role] {
		c.Role = "other"
	}
	known := make(map[string]bool, len(syms))
	for _, s := range syms {
		known[s.Name] = true
	}
	seen := map[string]bool{}
	for _, fn := range r.Functions {
		name := strings.TrimSpace(fn.Name)
		if !known[name] || seen[name] || len(c.Functions) == maxCardFuncs {
			continue
		}
		seen[name] = true
		c.Functions = append(c.Functions, CardFunc{Name: name, Summary: clipRunes(fn.Summary, 160)})
	}
	return c
}
```

- [ ] **Step 6: 运行测试**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`
Expected: PASS

- [ ] **Step 7: 提交**

```powershell
git add portal/internal/handbook/llmcall.go portal/internal/handbook/cards.go portal/internal/handbook/fakemodel_test.go portal/internal/handbook/cards_test.go
git commit -m "feat(handbook): LLM file cards validated against symbol facts"
```

---

### Task 3: 骨架推断、增量更新与重建判定

**Files:**
- Create: `portal/internal/handbook/organize.go`
- Test: `portal/internal/handbook/organize_test.go`

- [ ] **Step 1: 写失败测试**

```go
package handbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func orgFacts() (*Facts, map[string]*Card) {
	files := []File{
		{Path: "cmd/server/main.go", Lang: "go", Hash: hashOf("1")},
		{Path: "internal/order/store.go", Lang: "go", Hash: hashOf("2")},
		{Path: "internal/order/service.go", Lang: "go", Hash: hashOf("3")},
		{Path: "internal/pay/client.go", Lang: "go", Hash: hashOf("4")},
		{Path: "internal/pay/client_test.go", Lang: "go", Hash: hashOf("5"), Test: true},
		{Path: "README.md", Lang: "markdown", Hash: hashOf("6")},
	}
	cards := map[string]*Card{}
	for _, f := range files {
		if CardEligible(f) {
			cards[f.Path] = &Card{Purpose: "职责 " + f.Path, Hash: f.Hash}
		}
	}
	return &Facts{Files: files}, cards
}

func skeletonReply(assign map[string]string) func(string) string {
	return func(string) string {
		b, _ := json.Marshal(map[string]any{
			"stages": []map[string]string{
				{"id": "boot", "title": "启动", "summary": "进程启动"},
				{"id": "order", "title": "下单", "summary": "订单处理"},
				{"id": "pay", "title": "支付", "summary": "调用支付"},
				{"id": "Bad ID", "title": "非法"},
			},
			"assign": assign,
		})
		return string(b)
	}
}

func TestInferSkeleton_AssignsDirectories(t *testing.T) {
	f, cards := orgFacts()
	m := (&fakeModel{}).on("执行阶段", skeletonReply(map[string]string{
		"cmd/server": "boot", "internal/order": "order", "internal/pay": "nope",
	}))
	now := time.Unix(1000, 0).UTC()
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", now, &u)
	if err != nil {
		t.Fatal(err)
	}
	if sk.FallbackAreas || sk.Commit != "c1" || !sk.BuiltAt.Equal(now) || sk.BaseFiles != 4 || sk.PromptVersion != LLMPromptVersion {
		t.Fatalf("skeleton meta %#v", sk)
	}
	ids := []string{}
	for _, s := range sk.Stages {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "boot,order" {
		t.Fatalf("stages without files or with invalid ids must be dropped: %v", ids)
	}
	if sk.Files["internal/order/store.go"].Stage != "order" || sk.Files["internal/pay/client.go"].Stage != "" ||
		sk.Files["internal/order/store.go"].CardHash != hashOf("2") {
		t.Fatalf("files %#v", sk.Files)
	}
	if _, ok := sk.Files["internal/pay/client_test.go"]; ok {
		t.Fatal("tests are not organized")
	}
	if strings.Join(sk.TopDirs, ",") != "cmd,internal" {
		t.Fatalf("top dirs %v", sk.TopDirs)
	}
	if !strings.Contains(m.calls[0], "internal/order（2 个文件）") || !strings.Contains(m.calls[0], "职责 internal/order/store.go") {
		t.Fatalf("prompt must list dirs with purposes: %s", m.calls[0])
	}
}

func TestInferSkeleton_FallsBackToAreas(t *testing.T) {
	f, cards := orgFacts()
	m := (&fakeModel{}).on("执行阶段", func(string) string { return `{"stages":[{"id":"only","title":"一个"}],"assign":{}}` })
	var u usage
	sk, err := inferSkeleton(context.Background(), m, "svc", f, cards, "c1", time.Now(), &u)
	if err != nil {
		t.Fatal(err)
	}
	if !sk.FallbackAreas || len(sk.Stages) != 3 || sk.Files["internal/pay/client.go"].Stage != "internal-pay" {
		t.Fatalf("fallback skeleton %#v", sk)
	}
}

func TestUpdateSkeleton_Incremental(t *testing.T) {
	f, _ := orgFacts()
	sk := &Skeleton{PromptVersion: LLMPromptVersion, Stages: []Stage{{ID: "order"}, {ID: "pay"}}, BaseFiles: 4, TopDirs: []string{"cmd", "internal"},
		Files: map[string]FileAssign{
			"internal/order/store.go":   {Stage: "order", CardHash: hashOf("2")},
			"internal/order/service.go": {Stage: "order", CardHash: hashOf("0")},
			"internal/pay/client.go":    {Stage: "pay", CardHash: hashOf("4")},
			"internal/gone/x.go":        {Stage: "pay", CardHash: hashOf("9")},
		}}
	f.Files = append(f.Files, File{Path: "internal/order/refund/refund.go", Lang: "go", Hash: hashOf("7")})
	now := time.Unix(2000, 0).UTC()
	affected, changed := updateSkeleton(sk, f, "c2", now)
	if _, ok := sk.Files["internal/gone/x.go"]; ok {
		t.Fatal("removed file must be dropped")
	}
	if sk.Files["internal/order/service.go"].CardHash != hashOf("3") {
		t.Fatal("changed file must point at its new hash")
	}
	if sk.Files["internal/order/refund/refund.go"].Stage != "order" {
		t.Fatalf("new file must inherit the majority stage of its nearest directory: %#v", sk.Files["internal/order/refund/refund.go"])
	}
	if sk.Files["cmd/server/main.go"].Stage != "" {
		t.Fatal("a file with no organized neighbour stays unassigned")
	}
	if !affected["order"] || !affected["pay"] || len(changed) != 4 {
		t.Fatalf("affected %v changed %v", affected, changed)
	}
	if sk.ChangedSinceRebuild != 4 || sk.Commit != "c2" || !sk.UpdatedAt.Equal(now) {
		t.Fatalf("meta %#v", sk)
	}
}

func TestRebuildReason(t *testing.T) {
	f, _ := orgFacts()
	now := time.Unix(100*86400, 0).UTC()
	base := func() *Skeleton {
		return &Skeleton{PromptVersion: LLMPromptVersion, BuiltAt: now.Add(-time.Hour), BaseFiles: 10, TopDirs: []string{"cmd", "internal"},
			Files: map[string]FileAssign{"cmd/server/main.go": {Stage: "s"}, "internal/order/store.go": {Stage: "s"}, "internal/order/service.go": {Stage: "s"}, "internal/pay/client.go": {Stage: "s"}}}
	}
	if r := rebuildReason(nil, f, now, 30); r != "none" {
		t.Fatalf("nil: %q", r)
	}
	sk := base()
	if r := rebuildReason(sk, f, now, 30); r != "" {
		t.Fatalf("fresh: %q", r)
	}
	sk.PromptVersion = "old"
	if r := rebuildReason(sk, f, now, 30); r != "prompt" {
		t.Fatalf("prompt: %q", r)
	}
	sk = base()
	sk.BuiltAt = now.Add(-31 * 24 * time.Hour)
	if r := rebuildReason(sk, f, now, 30); r != "age" {
		t.Fatalf("age: %q", r)
	}
	sk = base()
	sk.ChangedSinceRebuild = 3
	if r := rebuildReason(sk, f, now, 30); r != "changes" {
		t.Fatalf("changes: %q", r)
	}
	sk = base()
	sk.TopDirs = []string{"internal"}
	if r := rebuildReason(sk, f, now, 30); r != "topdir" {
		t.Fatalf("topdir: %q", r)
	}
	sk = base()
	sk.Files["internal/pay/client.go"] = FileAssign{}
	if r := rebuildReason(sk, f, now, 30); r != "unassigned" {
		t.Fatalf("unassigned: %q", r)
	}
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "InferSkeleton|UpdateSkeleton|RebuildReason"`
Expected: FAIL

- [ ] **Step 3: 实现 `organize.go`**

```go
package handbook

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

const (
	minStages            = 2
	maxStages            = 15
	skeletonInputBudget  = 120 << 10
	skeletonMaxTokens    = 6000
	maxDirPurposes       = 3
	rebuildChangedRatio  = 5  // full rebuild when changed files exceed 1/5 (20%) of the base
	rebuildUnassignedPct = 10 // full rebuild when more than 10% of files are unassigned
)

var stageIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type dirSummary struct {
	Dir      string
	Files    []File
	Purposes []string
}

func eligibleFiles(f *Facts) []File {
	var out []File
	for _, file := range f.Files {
		if CardEligible(file) {
			out = append(out, file)
		}
	}
	return out
}

func eligibleDirs(f *Facts, cards map[string]*Card) []dirSummary {
	byDir := map[string]*dirSummary{}
	var dirs []string
	for _, file := range eligibleFiles(f) {
		d := path.Dir(file.Path)
		s := byDir[d]
		if s == nil {
			s = &dirSummary{Dir: d}
			byDir[d] = s
			dirs = append(dirs, d)
		}
		s.Files = append(s.Files, file)
		if c := cards[file.Path]; c != nil && len(s.Purposes) < maxDirPurposes {
			s.Purposes = append(s.Purposes, path.Base(file.Path)+"："+clipRunes(c.Purpose, 60))
		}
	}
	sort.Strings(dirs)
	out := make([]dirSummary, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, *byDir[d])
	}
	return out
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

func topDirsOf(files []File) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if t := topDir(f.Path); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

const skeletonSystemPrompt = "你是资深架构师，把代码仓库按运行时行为划分为执行阶段（stage），供故障排查使用。只输出一个 JSON 对象，不要输出其他文字。"

func skeletonPrompt(relPath string, f *Facts, dirs []dirSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n", relPath)
	var mains []string
	for _, p := range f.Packages {
		if p.Main {
			mains = append(mains, p.Dir)
		}
	}
	if len(mains) > 0 {
		fmt.Fprintf(&b, "入口（package main）：%s\n", strings.Join(mains, "、"))
	}
	b.WriteString("\n目录（括号内为源码文件数，后面是部分文件的职责）：\n")
	for _, d := range dirs {
		fmt.Fprintf(&b, "- %s（%d 个文件）", d.Dir, len(d.Files))
		if len(d.Purposes) > 0 {
			b.WriteString("：" + strings.Join(d.Purposes, "；"))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n把仓库划分为 %d-%d 个执行阶段（按请求/任务的处理流程，而不是按技术分层），每个目录归入一个阶段。\n", minStages+1, maxStages)
	b.WriteString(`输出 JSON：{"stages":[{"id":"小写字母数字和连字符","title":"中文短标题","summary":"一句话"}],"assign":{"目录":"stage id"}}` + "\n")
	b.WriteString("assign 必须覆盖上面列出的每个目录，目录名原样复制。\n")
	return b.String()
}

type skeletonReplyJSON struct {
	Stages []Stage            `json:"stages"`
	Assign map[string]string `json:"assign"`
}

func newSkeleton(files []File, commit string, now time.Time) *Skeleton {
	return &Skeleton{
		PromptVersion: LLMPromptVersion, Commit: commit, BuiltAt: now, UpdatedAt: now,
		BaseFiles: len(files), TopDirs: topDirsOf(files), Files: map[string]FileAssign{},
	}
}

// inferSkeleton builds a fresh skeleton by asking the model to assign directories to stages.
// An over-budget prompt or unusable reply falls back to directory areas as stages.
func inferSkeleton(ctx context.Context, m model.Model, relPath string, f *Facts, cards map[string]*Card, commit string, now time.Time, u *usage) (*Skeleton, error) {
	dirs := eligibleDirs(f, cards)
	prompt := skeletonPrompt(relPath, f, dirs)
	if len(prompt) > skeletonInputBudget {
		return fallbackSkeleton(f, commit, now), nil
	}
	var r skeletonReplyJSON
	if err := llmJSON(ctx, m, skeletonSystemPrompt, prompt, skeletonMaxTokens, &r, u); err != nil {
		if errors.Is(err, errBadReply) {
			return fallbackSkeleton(f, commit, now), nil
		}
		return nil, err
	}
	sk := newSkeleton(eligibleFiles(f), commit, now)
	valid := map[string]bool{}
	var stages []Stage
	for _, s := range r.Stages {
		id := strings.TrimSpace(s.ID)
		if !stageIDRe.MatchString(id) || valid[id] || len(stages) == maxStages {
			continue
		}
		valid[id] = true
		stages = append(stages, Stage{ID: id, Title: clipRunes(firstNonEmpty(s.Title, id), 40), Summary: clipRunes(s.Summary, 200)})
	}
	used := map[string]bool{}
	for _, d := range dirs {
		stage := strings.TrimSpace(r.Assign[d.Dir])
		if !valid[stage] {
			stage = ""
		}
		for _, file := range d.Files {
			sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash}
		}
		if stage != "" {
			used[stage] = true
		}
	}
	for _, s := range stages {
		if used[s.ID] {
			sk.Stages = append(sk.Stages, s)
		}
	}
	if len(sk.Stages) < minStages {
		return fallbackSkeleton(f, commit, now), nil
	}
	return sk, nil
}

// fallbackSkeleton uses the directory areas of card-eligible files as stages.
func fallbackSkeleton(f *Facts, commit string, now time.Time) *Skeleton {
	files := eligibleFiles(f)
	sk := newSkeleton(files, commit, now)
	sk.FallbackAreas = true
	for _, a := range buildAreas(&Facts{Files: files}) {
		sk.Stages = append(sk.Stages, Stage{ID: a.ID, Title: a.Name})
		for _, file := range a.Files {
			sk.Files[file.Path] = FileAssign{Stage: a.ID, CardHash: file.Hash}
		}
	}
	return sk
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// updateSkeleton applies file changes since the skeleton was last updated. It returns the
// stages whose file set or file contents changed and the changed paths.
func updateSkeleton(sk *Skeleton, f *Facts, commit string, now time.Time) (map[string]bool, map[string]bool) {
	affected, changed := map[string]bool{}, map[string]bool{}
	current := map[string]File{}
	for _, file := range eligibleFiles(f) {
		current[file.Path] = file
	}
	for p, a := range sk.Files {
		if _, ok := current[p]; !ok {
			delete(sk.Files, p)
			changed[p] = true
			if a.Stage != "" {
				affected[a.Stage] = true
			}
		}
	}
	var added []File
	for p, file := range current {
		a, ok := sk.Files[p]
		if !ok {
			added = append(added, file)
			continue
		}
		if a.CardHash != file.Hash {
			a.CardHash = file.Hash
			sk.Files[p] = a
			changed[p] = true
			if a.Stage != "" {
				affected[a.Stage] = true
			}
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	for _, file := range added {
		stage := guessStage(sk, path.Dir(file.Path))
		sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash}
		changed[file.Path] = true
		if stage != "" {
			affected[stage] = true
		}
	}
	sk.ChangedSinceRebuild += len(changed)
	sk.Commit, sk.UpdatedAt = commit, now
	return affected, changed
}

// guessStage returns the majority stage of organized files under dir, walking up to the
// top-level directory; ties go to the smallest stage id.
func guessStage(sk *Skeleton, dir string) string {
	for d := dir; d != "." && d != ""; d = path.Dir(d) {
		counts := map[string]int{}
		for p, a := range sk.Files {
			if a.Stage != "" && strings.HasPrefix(p, d+"/") {
				counts[a.Stage]++
			}
		}
		best, n := "", 0
		for s, c := range counts {
			if c > n || (c == n && s < best) {
				best, n = s, c
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// rebuildReason reports why the skeleton needs a full rebuild, or "" when an incremental
// update suffices. Call it after updateSkeleton so new files are already organized.
func rebuildReason(sk *Skeleton, f *Facts, now time.Time, maxAgeDays int) string {
	switch {
	case sk == nil:
		return "none"
	case sk.PromptVersion != LLMPromptVersion:
		return "prompt"
	case maxAgeDays > 0 && now.Sub(sk.BuiltAt) > time.Duration(maxAgeDays)*24*time.Hour:
		return "age"
	case sk.BaseFiles > 0 && sk.ChangedSinceRebuild*rebuildChangedRatio > sk.BaseFiles:
		return "changes"
	}
	known := map[string]bool{}
	for _, d := range sk.TopDirs {
		known[d] = true
	}
	files := eligibleFiles(f)
	for _, d := range topDirsOf(files) {
		if !known[d] {
			return "topdir"
		}
	}
	unassigned := 0
	for _, file := range files {
		if sk.Files[file.Path].Stage == "" {
			unassigned++
		}
	}
	if len(files) > 0 && unassigned*100 > len(files)*rebuildUnassignedPct {
		return "unassigned"
	}
	return ""
}
```

> `TestRebuildReason` 的 `base()` 中 4 个文件全部已归类、`TopDirs` 覆盖 `cmd`/`internal`，因此"fresh"返回空；`fallbackSkeleton` 的分区 id 来自 P2a 的 `buildAreas`（`internal/pay` → `internal-pay`）。

- [ ] **Step 4: 运行测试**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`
Expected: PASS

- [ ] **Step 5: 提交**

```powershell
git add portal/internal/handbook/organize.go portal/internal/handbook/organize_test.go
git commit -m "feat(handbook): infer and incrementally update stage skeleton"
```

---

### Task 4: 合成（阶段说明、总览、寄存器用途）

**Files:**
- Create: `portal/internal/handbook/synthesize.go`
- Test: `portal/internal/handbook/synthesize_test.go`

- [ ] **Step 1: 写失败测试**

```go
package handbook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeStage(t *testing.T) {
	m := (&fakeModel{}).on("阶段说明", func(string) string { return `{"summary":"处理下单：校验后写 orders 表。"}` })
	cards := map[string]*Card{"internal/order/store.go": {Purpose: "订单存储", Functions: []CardFunc{{Name: "(*Store).Get"}}}}
	var u usage
	got, err := summarizeStage(context.Background(), m, "svc", Stage{ID: "order", Title: "下单"}, []string{"internal/order/store.go", "internal/order/x.go"}, cards, &u)
	if err != nil || got != "处理下单：校验后写 orders 表。" {
		t.Fatalf("%q %v", got, err)
	}
	if !strings.Contains(m.calls[0], "internal/order/store.go：订单存储；关键函数：(*Store).Get") || !strings.Contains(m.calls[0], "internal/order/x.go：（无卡片）") {
		t.Fatalf("prompt %s", m.calls[0])
	}
}

func TestWriteOverview(t *testing.T) {
	m := (&fakeModel{}).on("仓库总览", func(string) string { return `{"overview":"## 主流程\n下单 → 支付"}` })
	f := &Facts{Module: &GoModule{Path: "example.com/svc"}, Packages: []GoPackage{{Dir: "cmd/server", Main: true}},
		Registers: []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "a.go", Line: 1}}}
	sk := &Skeleton{Stages: []Stage{{ID: "order", Title: "下单", Summary: "写订单"}}}
	var u usage
	got, err := writeOverview(context.Background(), m, "svc", f, sk, &u)
	if err != nil || !strings.Contains(got, "下单 → 支付") {
		t.Fatalf("%q %v", got, err)
	}
	for _, want := range []string{"example.com/svc", "cmd/server", "下单：写订单", "orders"} {
		if !strings.Contains(m.calls[0], want) {
			t.Fatalf("prompt missing %q: %s", want, m.calls[0])
		}
	}
}

func TestRegisterNotes_SelectsAndValidates(t *testing.T) {
	var hits []RegisterHit
	for i := 0; i < 3; i++ {
		hits = append(hits, RegisterHit{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 10 + i})
	}
	hits = append(hits, RegisterHit{Kind: RegTopic, Name: "order-events", Access: AccessRef, Path: "internal/mq/topics.go", Line: 3})
	m := (&fakeModel{}).on("用途", func(user string) string {
		b, _ := json.Marshal(map[string]any{"notes": map[string]string{"table:orders": "订单主表", "table:unknown": "x"}})
		return string(b)
	})
	cards := map[string]*Card{"internal/order/store.go": {Purpose: "订单存储"}}
	var u usage
	notes, err := registerNotes(context.Background(), m, "svc", hits, cards, nil, map[string]string{"topic:order-events": "旧说明"}, &u)
	if err != nil {
		t.Fatal(err)
	}
	if notes["table:orders"] != "订单主表" || notes["topic:order-events"] != "旧说明" || len(notes) != 2 {
		t.Fatalf("notes %#v", notes)
	}
	if !strings.Contains(m.calls[0], "table:orders") || strings.Contains(m.calls[0], "topic:order-events") {
		t.Fatalf("only registers without notes are asked when nothing changed: %s", m.calls[0])
	}
	if !strings.Contains(m.calls[0], "写 internal/order/store.go:10") || !strings.Contains(m.calls[0], "订单存储") {
		t.Fatalf("prompt must carry locations and file purposes: %s", m.calls[0])
	}
}

func TestRegisterNotes_RefreshesRegistersInChangedFiles(t *testing.T) {
	hits := []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "a.go", Line: 1}}
	m := (&fakeModel{}).on("用途", func(string) string { return `{"notes":{"table:orders":"新说明"}}` })
	var u usage
	notes, err := registerNotes(context.Background(), m, "svc", hits, nil, map[string]bool{"a.go": true}, map[string]string{"table:orders": "旧"}, &u)
	if err != nil || notes["table:orders"] != "新说明" {
		t.Fatalf("%#v %v", notes, err)
	}
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "SummarizeStage|WriteOverview|RegisterNotes"`
Expected: FAIL

- [ ] **Step 3: 实现 `synthesize.go`**

```go
package handbook

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sixath/framework/model"
)

const (
	stageInputBudget    = 40 << 10
	stageMaxTokens      = 700
	overviewMaxTokens   = 1500
	maxNotedRegisters   = 60
	registerBatch       = 20
	registerMaxTokens   = 1500
	maxLocationsInNote  = 6
)

func regKey(kind, name string) string { return kind + ":" + name }

// stageSystemPrompt must not contain "执行阶段" (the skeleton prompt's marker in tests).
const stageSystemPrompt = "你是资深后端工程师，为故障排查撰写代码阶段说明。只输出一个 JSON 对象，不要输出其他文字。"

func stagePrompt(relPath string, st Stage, files []string, cards map[string]*Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n阶段：%s（%s）", relPath, st.Title, st.ID)
	if st.Summary != "" {
		b.WriteString("，提示：" + st.Summary)
	}
	b.WriteString("\n\n阶段内文件：\n")
	for _, p := range files {
		line := "- " + p + "："
		if c := cards[p]; c != nil {
			line += c.Purpose
			if len(c.Functions) > 0 {
				names := make([]string, 0, len(c.Functions))
				for _, fn := range c.Functions {
					names = append(names, fn.Name)
				}
				line += "；关键函数：" + strings.Join(names, "、")
			}
		} else {
			line += "（无卡片）"
		}
		if b.Len()+len(line) > stageInputBudget {
			b.WriteString("- …其余文件省略\n")
			break
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n写阶段说明，输出 JSON：" + `{"summary":"3-6 句：该阶段在运行时做什么、主流程顺序、关键文件和函数、读写哪些共享状态（表/缓存/topic/外部接口）、常见故障点"}` + "\n只依据上面的信息，不要编造文件或函数。\n")
	return b.String()
}

// summarizeStage writes the L2 description of one stage.
func summarizeStage(ctx context.Context, m model.Model, relPath string, st Stage, files []string, cards map[string]*Card, u *usage) (string, error) {
	var r struct {
		Summary string `json:"summary"`
	}
	if err := llmJSON(ctx, m, stageSystemPrompt, stagePrompt(relPath, st, files, cards), stageMaxTokens, &r, u); err != nil {
		return "", err
	}
	s := clipRunes(r.Summary, 1500)
	if s == "" {
		return "", fmt.Errorf("%w: empty stage summary", errBadReply)
	}
	return s, nil
}

const overviewSystemPrompt = "你是资深架构师，为故障排查撰写仓库总览。只输出一个 JSON 对象，不要输出其他文字。"

func overviewPrompt(relPath string, f *Facts, sk *Skeleton) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n", relPath)
	if f.Module != nil && f.Module.Path != "" {
		fmt.Fprintf(&b, "Go module：%s\n", f.Module.Path)
	}
	for _, p := range f.Packages {
		if p.Main {
			fmt.Fprintf(&b, "入口：%s\n", p.Dir)
		}
	}
	b.WriteString("\n各阶段概要：\n")
	for _, s := range sk.Stages {
		fmt.Fprintf(&b, "- %s：%s\n", s.Title, clipRunes(s.Summary, 300))
	}
	byKind := map[string]map[string]int{}
	for _, h := range f.Registers {
		if byKind[h.Kind] == nil {
			byKind[h.Kind] = map[string]int{}
		}
		byKind[h.Kind][h.Name]++
	}
	b.WriteString("\n共享状态（按出现次数，最多 10 个）：\n")
	for _, k := range registerKinds {
		names := keysByCount(byKind[k.kind])
		if len(names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s：%s\n", k.title, strings.Join(names[:min(10, len(names))], "、"))
	}
	b.WriteString("\n输出 JSON：" + `{"overview":"Markdown，3-5 段：系统做什么；主流程（按阶段顺序串起来）；外部依赖与共享状态；排查时的切入建议"}` + "\n只依据上面的信息。\n")
	return b.String()
}

// writeOverview writes the L1 overview of the repository.
func writeOverview(ctx context.Context, m model.Model, relPath string, f *Facts, sk *Skeleton, u *usage) (string, error) {
	var r struct {
		Overview string `json:"overview"`
	}
	if err := llmJSON(ctx, m, overviewSystemPrompt, overviewPrompt(relPath, f, sk), overviewMaxTokens, &r, u); err != nil {
		return "", err
	}
	s := clipRunes(r.Overview, 4000)
	if s == "" {
		return "", fmt.Errorf("%w: empty overview", errBadReply)
	}
	return s, nil
}

const registerSystemPrompt = "你是资深后端工程师，为共享状态（数据表、HTTP 路由、MQ topic、缓存键）写一句话用途，供故障排查使用。只输出一个 JSON 对象。"

type regGroup struct {
	Key  string
	Hits []RegisterHit
}

// registerNotes returns notes for the most referenced registers. Registers with an existing
// note keep it unless one of their locations is in changed; unusable replies keep old notes.
func registerNotes(ctx context.Context, m model.Model, relPath string, hits []RegisterHit, cards map[string]*Card, changed map[string]bool, old map[string]string, u *usage) (map[string]string, error) {
	byKey := map[string]*regGroup{}
	var groups []*regGroup
	for _, h := range hits {
		k := regKey(h.Kind, h.Name)
		g := byKey[k]
		if g == nil {
			g = &regGroup{Key: k}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.Hits = append(g.Hits, h)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Hits) != len(groups[j].Hits) {
			return len(groups[i].Hits) > len(groups[j].Hits)
		}
		return groups[i].Key < groups[j].Key
	})
	if len(groups) > maxNotedRegisters {
		groups = groups[:maxNotedRegisters]
	}
	notes := map[string]string{}
	var todo []*regGroup
	for _, g := range groups {
		n, ok := old[g.Key]
		if ok && !touches(g.Hits, changed) {
			notes[g.Key] = n
			continue
		}
		if ok {
			notes[g.Key] = n
		}
		todo = append(todo, g)
	}
	for i := 0; i < len(todo); i += registerBatch {
		batch := todo[i:min(i+registerBatch, len(todo))]
		var r struct {
			Notes map[string]string `json:"notes"`
		}
		err := llmJSON(ctx, m, registerSystemPrompt, registerPrompt(relPath, batch, cards), registerMaxTokens, &r, u)
		if err != nil {
			if errors.Is(err, errBadReply) {
				continue
			}
			return nil, err
		}
		for _, g := range batch {
			if n := clipRunes(r.Notes[g.Key], 160); n != "" {
				notes[g.Key] = n
			}
		}
	}
	return notes, nil
}

func touches(hits []RegisterHit, changed map[string]bool) bool {
	for _, h := range hits {
		if changed[h.Path] {
			return true
		}
	}
	return false
}

func registerPrompt(relPath string, batch []*regGroup, cards map[string]*Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n\n", relPath)
	for _, g := range batch {
		fmt.Fprintf(&b, "%s\n", g.Key)
		for i, h := range g.Hits {
			if i == maxLocationsInNote {
				fmt.Fprintf(&b, "  - …另有 %d 处\n", len(g.Hits)-i)
				break
			}
			fmt.Fprintf(&b, "  - %s %s:%d", accessLabels[h.Access], h.Path, h.Line)
			if c := cards[h.Path]; c != nil {
				b.WriteString("（" + clipRunes(c.Purpose, 60) + "）")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n为每个条目写一句话用途，输出 JSON：" + `{"notes":{"上面的条目原样作为 key":"一句话用途"}}` + "\n")
	return b.String()
}
```

> 注：`registerNotes` 测试里 `table:unknown` 不在本批 key 中，被忽略；`topic:order-events` 已有旧说明且未改动，不再提问。测试里的 key 字面量（`table:`、`topic:`）假定 `RegTable == "table"`、`RegTopic == "topic"`，若 `registers.go` 中常量值不同，按实际值改测试字面量。
>
> 测试用假模型按关键字匹配 system/user 提示词，所以各提示词的标记必须互不包含：卡片 `文件卡片`、骨架 `执行阶段`、阶段说明 `阶段说明`（且不得出现"执行阶段"）、总览 `仓库总览`（user 提示里阶段列表标题用"各阶段概要"）、寄存器 `用途`。

- [ ] **Step 4: 运行测试**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`
Expected: PASS

- [ ] **Step 5: 提交**

```powershell
git add portal/internal/handbook/synthesize.go portal/internal/handbook/synthesize_test.go
git commit -m "feat(handbook): stage summaries, overview and register notes"
```

---

### Task 5: `Enrich` 编排

**Files:**
- Create: `portal/internal/handbook/enrich.go`
- Test: `portal/internal/handbook/enrich_test.go`

- [ ] **Step 1: 写失败测试**

```go
package handbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type enrichRepo struct {
	root  string
	facts *Facts
}

func writeRepoFile(t *testing.T, root, rel, content string) File {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	lang := "go"
	if strings.HasSuffix(rel, ".md") {
		lang = "markdown"
	}
	return File{Path: rel, Lang: lang, Lines: strings.Count(content, "\n"), Hash: hex.EncodeToString(sum[:]), Test: strings.HasSuffix(rel, "_test.go")}
}

func newEnrichRepo(t *testing.T) *enrichRepo {
	root := t.TempDir()
	f := &Facts{Symbols: map[string][]Symbol{}}
	for rel, content := range map[string]string{
		"cmd/server/main.go":        "package main\nfunc main() {}\n",
		"internal/order/store.go":   "package order\nfunc Get() {}\n",
		"internal/order/service.go": "package order\nfunc Place() {}\n",
		"internal/pay/client.go":    "package pay\nfunc Charge() {}\n",
		"internal/pay/client_test.go": "package pay\n",
		"README.md":                 "# svc\n",
	} {
		f.Files = append(f.Files, writeRepoFile(t, root, rel, content))
	}
	f.Symbols["internal/order/store.go"] = []Symbol{{Kind: "func", Name: "Get", Line: 2, EndLine: 2}}
	f.Registers = []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 2}}
	f.Packages = []GoPackage{{Dir: "cmd/server", Name: "main", Main: true}}
	return &enrichRepo{root: root, facts: f}
}

func happyModel() *fakeModel {
	return (&fakeModel{}).
		on("文件卡片", func(user string) string { return `{"purpose":"职责","role":"service","functions":[{"name":"Get","summary":"查询"}]}` }).
		on("执行阶段", skeletonReply(map[string]string{"cmd/server": "boot", "internal/order": "order", "internal/pay": "pay"})).
		on("阶段说明", func(string) string { return `{"summary":"阶段说明文本"}` }).
		on("仓库总览", func(string) string { return `{"overview":"总览文本"}` }).
		on("用途", func(string) string { return `{"notes":{"table:orders":"订单主表"}}` })
}

func (r *enrichRepo) input(m *fakeModel, cache LLMCache, opts EnrichOptions) EnrichInput {
	return EnrichInput{RelPath: "svc", Root: r.root, Commit: "c1", ModelName: "p/m", Facts: r.facts, Cache: cache, Model: m, Opts: opts, Now: time.Unix(5000, 0).UTC()}
}

func TestEnrich_FullRunCompletes(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	m := happyModel()
	res, err := Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != LLMStateComplete || res.CardsTotal != 4 || res.CardsDone != 4 || res.CardsNew != 4 || !res.SkeletonRebuilt ||
		res.RebuildReason != "none" || res.Stages != 3 || !res.Changed || res.TokensIn == 0 {
		t.Fatalf("result %#v", res)
	}
	sk, _ := cache.Skeleton()
	if sk == nil || sk.Overview != "总览文本" || sk.RegisterNotes["table:orders"] != "订单主表" || sk.Stages[0].Summary != "阶段说明文本" {
		t.Fatalf("skeleton %#v", sk)
	}
	if card, _ := cache.Card(r.facts.Files[0].Hash); card == nil && CardEligible(r.facts.Files[0]) {
		t.Fatal("card not cached")
	}

	calls := m.callCount()
	res, err = Enrich(context.Background(), r.input(m, cache, EnrichOptions{}))
	if err != nil || res.CardsNew != 0 || res.SkeletonRebuilt || res.Changed {
		t.Fatalf("a rerun with nothing changed must be a no-op: %#v %v", res, err)
	}
	if m.callCount() != calls {
		t.Fatalf("rerun made %d model calls", m.callCount()-calls)
	}
}

func TestEnrich_BudgetLeavesPartial(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	res, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{MaxCardsPerRun: 2}))
	if err != nil || res.State != LLMStatePartial || res.CardsDone != 2 || res.CardsNew != 2 || !res.Changed {
		t.Fatalf("%#v %v", res, err)
	}
	if sk, _ := cache.Skeleton(); sk != nil {
		t.Fatal("no synthesis before all cards were attempted")
	}
	res, err = Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{MaxCardsPerRun: 2}))
	if err != nil || res.State != LLMStateComplete || res.CardsDone != 4 {
		t.Fatalf("second run must finish: %#v %v", res, err)
	}
}

func TestEnrich_IncrementalAfterChange(t *testing.T) {
	r := newEnrichRepo(t)
	cache := LLMCache{Dir: t.TempDir()}
	if _, err := Enrich(context.Background(), r.input(happyModel(), cache, EnrichOptions{})); err != nil {
		t.Fatal(err)
	}
	sk0, _ := cache.Skeleton()
	sk0.BaseFiles = 100 // one changed file of four would otherwise exceed the 20% rebuild threshold
	if err := cache.PutSkeleton(sk0); err != nil {
		t.Fatal(err)
	}
	for i, f := range r.facts.Files {
		if f.Path == "internal/order/service.go" {
			r.facts.Files[i] = writeRepoFile(t, r.root, f.Path, "package order\nfunc Place() { /* v2 */ }\n")
		}
	}
	m := happyModel()
	in := r.input(m, cache, EnrichOptions{})
	in.Commit = "c2"
	res, err := Enrich(context.Background(), in)
	if err != nil || res.CardsNew != 1 || res.SkeletonRebuilt || !res.Changed || res.State != LLMStateComplete {
		t.Fatalf("%#v %v", res, err)
	}
	stageCalls := 0
	for _, c := range m.calls {
		if strings.Contains(c, "阶段：") {
			stageCalls++
		}
	}
	if stageCalls != 1 {
		t.Fatalf("only the affected stage is rewritten, got %d stage calls", stageCalls)
	}
	sk, _ := cache.Skeleton()
	if sk.Commit != "c2" || sk.ChangedSinceRebuild != 1 {
		t.Fatalf("skeleton %#v", sk)
	}
}

func TestEnrich_SkipsFilesChangedOnDisk(t *testing.T) {
	r := newEnrichRepo(t)
	if err := os.WriteFile(filepath.Join(r.root, "internal", "pay", "client.go"), []byte("package pay // moved on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Enrich(context.Background(), r.input(happyModel(), LLMCache{Dir: t.TempDir()}, EnrichOptions{}))
	if err != nil || res.CardsDone != 3 || res.State != LLMStateComplete {
		t.Fatalf("a file whose content no longer matches the facts is skipped: %#v %v", res, err)
	}
}

func TestEnrich_ModelFailuresAbort(t *testing.T) {
	r := newEnrichRepo(t)
	m := &fakeModel{failErr: errors.New("503 upstream")}
	_, err := Enrich(context.Background(), r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{Concurrency: 1}))
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("want ErrModelUnavailable, got %v", err)
	}
}

func TestEnrich_BadRepliesDoNotBlockSynthesis(t *testing.T) {
	r := newEnrichRepo(t)
	m := happyModel()
	m.rules[0] = fakeRule{"文件卡片", func(user string) string {
		if strings.Contains(user, "internal/pay/client.go") {
			return "无法回答"
		}
		return `{"purpose":"职责"}`
	}}
	res, err := Enrich(context.Background(), r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{}))
	if err != nil || res.State != LLMStateComplete || res.CardErrors != 1 || res.CardsDone != 3 {
		t.Fatalf("%#v %v", res, err)
	}
}

func TestEnrich_DeadlineLeavesPartial(t *testing.T) {
	r := newEnrichRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	m := happyModel()
	m.rules[0] = fakeRule{"文件卡片", func(string) string { cancel(); return `{"purpose":"职责"}` }}
	res, err := Enrich(ctx, r.input(m, LLMCache{Dir: t.TempDir()}, EnrichOptions{Concurrency: 1}))
	if err != nil || res.State != LLMStatePartial {
		t.Fatalf("a cancelled run reports partial progress without error: %#v %v", res, err)
	}
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run Enrich`
Expected: FAIL

- [ ] **Step 3: 实现 `enrich.go`**

```go
package handbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sixath/framework/model"
)

const (
	LLMStatePartial  = "partial"
	LLMStateComplete = "complete"
	LLMStateFailed   = "failed"

	maxConsecutiveCallErrors = 5
)

// ErrModelUnavailable aborts a run whose model calls keep failing.
var ErrModelUnavailable = errors.New("handbook: model calls keep failing")

// EnrichOptions bounds one LLM run; zero values take defaults.
type EnrichOptions struct {
	Concurrency         int
	MaxCardsPerRun      int
	MaxFileBytes        int
	SkeletonRebuildDays int
	Full                bool // rebuild the skeleton regardless of thresholds
}

func (o EnrichOptions) withDefaults() EnrichOptions {
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.MaxCardsPerRun <= 0 {
		o.MaxCardsPerRun = 600
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = 24 << 10
	}
	if o.SkeletonRebuildDays <= 0 {
		o.SkeletonRebuildDays = 30
	}
	return o
}

// EnrichInput is one LLM run over the facts of a published version.
type EnrichInput struct {
	RelPath   string
	Root      string // repository checkout; files whose content no longer matches Facts are skipped
	Commit    string
	ModelName string
	Facts     *Facts
	Cache     LLMCache
	Model     model.Model
	Opts      EnrichOptions
	Now       time.Time
}

// EnrichResult summarizes one run.
type EnrichResult struct {
	State           string
	CardsTotal      int
	CardsDone       int
	CardsNew        int
	CardErrors      int
	Stages          int
	Fallback        bool
	SkeletonRebuilt bool
	RebuildReason   string
	SkeletonBuiltAt time.Time
	TokensIn        int64
	TokensOut       int64
	Changed         bool // cache content changed; the handbook needs a re-render
}

// Enrich generates missing file cards within budget and, once every card-eligible file has
// been attempted, synthesizes the skeleton. Cancelling ctx stops the run and reports partial
// progress without error; everything already generated stays cached.
func Enrich(ctx context.Context, in EnrichInput) (*EnrichResult, error) {
	o := in.Opts.withDefaults()
	var u usage
	res := &EnrichResult{}
	defer func() { res.TokensIn, res.TokensOut = u.in.Load(), u.out.Load() }()

	files := eligibleFiles(in.Facts)
	res.CardsTotal = len(files)
	cards := map[string]*Card{}
	var todo []File
	for _, f := range files {
		c, err := in.Cache.Card(f.Hash)
		if err != nil {
			return res, err
		}
		if c != nil {
			cards[f.Path] = c
		} else {
			todo = append(todo, f)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool {
		if (todo[i].Lang == "go") != (todo[j].Lang == "go") {
			return todo[i].Lang == "go"
		}
		return todo[i].Path < todo[j].Path
	})
	cut := len(todo) > o.MaxCardsPerRun
	if cut {
		todo = todo[:o.MaxCardsPerRun]
	}
	if err := generateCards(ctx, in, o, todo, cards, res, &u); err != nil {
		res.CardsDone = len(cards)
		return res, err
	}
	res.CardsDone = len(cards)
	res.Changed = res.CardsNew > 0
	if cut || ctx.Err() != nil {
		res.State = LLMStatePartial
		return res, nil
	}
	if err := synthesizeAll(ctx, in, o, cards, res, &u); err != nil {
		if ctx.Err() != nil {
			res.State = LLMStatePartial
			return res, nil
		}
		return res, err
	}
	res.State = LLMStateComplete
	return res, nil
}

func generateCards(ctx context.Context, in EnrichInput, o EnrichOptions, todo []File, cards map[string]*Card, res *EnrichResult, u *usage) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu          sync.Mutex
		consecutive int
		fatal       error
		wg          sync.WaitGroup
	)
	limit := min(maxConsecutiveCallErrors, len(todo))
	jobs := make(chan File)
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				content, ok := readMatching(in.Root, f)
				if !ok {
					continue
				}
				card, err := generateCard(ctx, in.Model, in.ModelName, in.RelPath, f, in.Facts.Symbols[f.Path], content, o.MaxFileBytes, u)
				if err == nil {
					err = in.Cache.PutCard(f.Hash, card)
				}
				mu.Lock()
				switch {
				case err == nil:
					cards[f.Path] = card
					res.CardsNew++
					consecutive = 0
				case ctx.Err() != nil:
				case errors.Is(err, errBadReply):
					res.CardErrors++
					consecutive = 0
				default:
					consecutive++
					if consecutive >= limit && fatal == nil {
						fatal = fmt.Errorf("%w: %v", ErrModelUnavailable, err)
						cancel()
					}
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, f := range todo {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return fatal
}

// readMatching reads a file of the checkout and reports whether it still matches the facts.
func readMatching(root string, f File) ([]byte, bool) {
	fh, err := os.OpenInRoot(root, filepath.FromSlash(f.Path))
	if err != nil {
		return nil, false
	}
	defer fh.Close()
	b, err := io.ReadAll(io.LimitReader(fh, MaxFileBytes+1))
	if err != nil || len(b) > MaxFileBytes {
		return nil, false
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]) == f.Hash
}

func synthesizeAll(ctx context.Context, in EnrichInput, o EnrichOptions, cards map[string]*Card, res *EnrichResult, u *usage) error {
	sk, err := in.Cache.Skeleton()
	if err != nil {
		return err
	}
	if sk != nil && sk.PromptVersion != LLMPromptVersion {
		sk = nil
	}
	var affected, changed map[string]bool
	reason := ""
	if o.Full {
		reason = "manual"
	} else if sk != nil {
		affected, changed = updateSkeleton(sk, in.Facts, in.Commit, in.Now)
	}
	if reason == "" {
		reason = rebuildReason(sk, in.Facts, in.Now, o.SkeletonRebuildDays)
	}
	if reason != "" {
		var old *Skeleton = sk
		sk, err = inferSkeleton(ctx, in.Model, in.RelPath, in.Facts, cards, in.Commit, in.Now, u)
		if err != nil {
			return err
		}
		if old != nil {
			sk.RegisterNotes = old.RegisterNotes
		}
		affected, changed = map[string]bool{}, nil
		for _, s := range sk.Stages {
			affected[s.ID] = true
		}
		res.SkeletonRebuilt, res.RebuildReason = true, reason
	}
	filesOf := map[string][]string{}
	for p, a := range sk.Files {
		filesOf[a.Stage] = append(filesOf[a.Stage], p)
	}
	rewritten := 0
	for i, st := range sk.Stages {
		if !affected[st.ID] && st.Summary != "" && !res.SkeletonRebuilt {
			continue
		}
		files := filesOf[st.ID]
		sort.Strings(files)
		s, err := summarizeStage(ctx, in.Model, in.RelPath, st, files, cards, u)
		if err != nil {
			if errors.Is(err, errBadReply) {
				continue
			}
			return err
		}
		sk.Stages[i].Summary = s
		rewritten++
	}
	if rewritten > 0 || sk.Overview == "" {
		ov, err := writeOverview(ctx, in.Model, in.RelPath, in.Facts, sk, u)
		switch {
		case err == nil:
			sk.Overview = ov
		case !errors.Is(err, errBadReply):
			return err
		}
	}
	notesChanged := changed
	if res.SkeletonRebuilt {
		notesChanged = map[string]bool{}
		for p := range sk.Files {
			notesChanged[p] = true
		}
	}
	notes, err := registerNotes(ctx, in.Model, in.RelPath, in.Facts.Registers, cards, notesChanged, sk.RegisterNotes, u)
	if err != nil {
		return err
	}
	sk.RegisterNotes = notes
	sk.Commit, sk.UpdatedAt = in.Commit, in.Now
	if err := in.Cache.PutSkeleton(sk); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, f := range in.Facts.Files {
		keep[f.Hash] = true
	}
	for _, a := range sk.Files {
		keep[a.CardHash] = true
	}
	if _, err := in.Cache.PruneCards(keep); err != nil {
		return err
	}
	res.Stages, res.Fallback, res.SkeletonBuiltAt = len(sk.Stages), sk.FallbackAreas, sk.BuiltAt
	res.Changed = res.Changed || res.SkeletonRebuilt || rewritten > 0 || len(changed) > 0
	return nil
}
```

> 说明：
> - 重建骨架后 `registerNotes` 收到全部文件为 changed，所有前 60 个寄存器都会重新写用途；增量时只问位置落在改动文件里的寄存器与还没有用途的寄存器。
> - "rerun 无变化"测试：第二次运行没有新卡片，`updateSkeleton` 无改动，`rebuildReason` 为空，阶段都有说明、总览非空，`registerNotes` 不发请求（已有用途且无改动）——因此零模型调用、`Changed=false`。如果实现中仍有调用，检查 `registerNotes` 对 `old` 的处理。
> - `PruneCards` 保留当前所有文件的哈希（包括没有卡片资格的文件，无害）与骨架里的旧卡片哈希。

- [ ] **Step 4: 运行测试（含 race）**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`，然后（CGO 环境）`go test -p 1 -race -count=1 ./internal/handbook/ -run Enrich`
Expected: PASS

- [ ] **Step 5: 提交**

```powershell
git add portal/internal/handbook/enrich.go portal/internal/handbook/enrich_test.go
git commit -m "feat(handbook): budgeted LLM enrichment run"
```

---

### Task 6: 渲染 LLM 内容

**Files:**
- Modify: `portal/internal/handbook/render.go`、`portal/internal/handbook/builder.go`
- Modify（调用点）：现有调用 `Render(meta, f)` 的测试
- Test: `portal/internal/handbook/render_llm_test.go`

**渲染规则：**

1. `Render(meta RenderMeta, f *Facts, l *LLMLayer) map[string]string`；`l == nil` 或 `l.Empty()` 时输出与 P2a 完全相同（除 `GeneratorVersion` 外，P2a 的 render 测试应原样通过，只需在调用处补 `nil`）。
2. **分区页**（总是生成）：每个有卡片的文件在文件行下面加 `  - 职责：<purpose>（<role>）`；只有旧卡片时加 `  - 职责（已过期，以源码为准）：<purpose>`；符号行若该符号在卡片 `functions` 中，行尾追加 ` —— <summary>`。
3. **阶段页**（`l.Skeleton` 有阶段时）：`references/stages/<id>.md`，用 `paginate("references/stages/"+id, "# 阶段 "+title, sections)`。第一节为阶段说明（`Summary`，无则"（暂无说明）"）；之后每个文件一节：

```
### `internal/order/store.go`（go，20 行）

职责：订单存储（repository）
<description>
执行时机：<lifecycle>
关键函数：
- `(*Store).Get` L12-15 —— 按 id 查询
```

   已过期文件在标题后加 `（已过期，以源码为准，改用 rca_grep）` 并用旧卡片内容；没有卡片的文件只写标题行和"（暂无卡片）"。文件按路径排序。
4. **index**：有阶段时在分区表之前加三节：
   - `## 执行阶段` 表：`| 阶段 | 文件数 | 说明 | 页面 |`，说明取 `Summary` 第一句（在第一个 `。`、`. ` 或换行处截断，**保留句末的 `。`**，最多 80 字），页面 `` `references/stages/<id>.md` ``；
   - `## 未归类文件`（骨架里 stage 为空、或不在骨架里的卡片资格文件；超过 100 个只列前 100 并注明总数），无则省略；
   - `## 已过期文件`（`l.Stale` 的路径），无则省略。
   标题改为 `# <rel> 索引`。
5. **registers**：`### \`name\`` 下若 `Skeleton.RegisterNotes[kind:name]` 存在，插入 `用途：<note>\n\n`。
6. **overview**：`Skeleton.Overview` 非空时，在头部列表之后插入 `## 系统总览（LLM 生成）\n\n<overview>\n`，再插入 `## 执行阶段` 列表（`- <title>（\`references/stages/<id>.md\`）：<说明第一句>`）；头部"（静态分析，无 LLM）"改为"（静态分析 + LLM，文件卡片 N 个）"；覆盖说明末尾加 `- LLM 内容（卡片、阶段、总览）可能落后于代码，以源码为准`。
7. **SKILL.md**：有阶段时用设计文档 §7.2 的 RCA 模板：description 为 `<rel> 的行为地图（commit X）：按执行阶段组织文件卡片，并列出每个共享状态（表/路由/topic/缓存键）的全部读写位置。RCA 定位时按需下钻，结论以 rca_read 读到的源码为准。`；"文件"列 overview、index、registers、`references/stages/<id>.md`、`references/areas/<分区>.md`（完整文件与符号清单，含测试）；"用法"按设计文档 5 步，第 4 步原文为"4. 对每个候选位置用 `rca_read`（repo=`<rel>`）读真实源码确认；已过期条目以源码为准，改用 `rca_grep`。"（其余：1 读 overview 和 index，确定相关阶段与寄存器，不要过早收窄；2 涉及共享状态时读 registers.md 记下所有写入点；3 打开相关 stages 页；5 结论只能基于读到的源码）。无阶段时保持 P2a 文案。
8. **Stats**：新增 `Cards int json:"cards"`、`StaleCards int json:"stale_cards"`、`Stages int json:"stages"`、`LLMRev string json:"llm_rev,omitempty"`。
9. **BuildInput**：新增 `LLMDir string`（空 = 不读 LLM 缓存）、`LLMRev string`。`Build` 在 `CollectFacts` 之后：`LLMDir` 非空时 `LoadLLMLayer(LLMCache{Dir: in.LLMDir}, facts)`，出错返回错误；Stats 填 `Cards=len(l.Cards)`、`StaleCards=len(l.Stale)`、`Stages`（骨架阶段数）、`LLMRev=in.LLMRev`。
10. `GeneratorVersion = "p2b-1"`。

- [ ] **Step 1: 写失败测试 `render_llm_test.go`**

```go
package handbook

import (
	"strings"
	"testing"
	"time"
)

func llmRenderFixture() (RenderMeta, *Facts, *LLMLayer) {
	meta := RenderMeta{RelPath: "svc-a", Commit: "abcdef1234567890", GeneratedAt: time.Unix(0, 0)}
	f := &Facts{
		Files: []File{
			{Path: "internal/order/store.go", Lang: "go", Lines: 20, Hash: hashOf("1")},
			{Path: "internal/order/service.go", Lang: "go", Lines: 30, Hash: hashOf("2")},
			{Path: "internal/pay/client.go", Lang: "go", Lines: 10, Hash: hashOf("3")},
			{Path: "internal/order/store_test.go", Lang: "go", Lines: 5, Hash: hashOf("4"), Test: true},
		},
		Symbols: map[string][]Symbol{"internal/order/store.go": {{Kind: "method", Name: "(*Store).Get", Line: 12, EndLine: 15}}},
		Registers: []RegisterHit{{Kind: RegTable, Name: "orders", Access: AccessWrite, Path: "internal/order/store.go", Line: 13}},
		Coverage:  Coverage{Graph: "none"},
	}
	l := &LLMLayer{
		Cards: map[string]*Card{
			"internal/order/store.go": {Purpose: "订单存储", Role: "repository", Description: "读写 orders 表。", Lifecycle: "请求时",
				Functions: []CardFunc{{Name: "(*Store).Get", Summary: "按 id 查询"}}},
		},
		Stale: map[string]*Card{"internal/order/service.go": {Purpose: "旧的下单逻辑"}},
		Skeleton: &Skeleton{PromptVersion: LLMPromptVersion, Overview: "下单后调用支付。",
			Stages: []Stage{{ID: "order", Title: "下单", Summary: "处理下单。随后写库。"}},
			Files: map[string]FileAssign{
				"internal/order/store.go":   {Stage: "order", CardHash: hashOf("1")},
				"internal/order/service.go": {Stage: "order", CardHash: hashOf("9")},
			},
			RegisterNotes: map[string]string{"table:orders": "订单主表"}},
	}
	return meta, f, l
}

func TestRender_WithLLMLayer(t *testing.T) {
	meta, f, l := llmRenderFixture()
	pages := Render(meta, f, l)
	checks := map[string][]string{
		"SKILL.md":                        {"行为地图", "references/stages/<id>.md", "已过期条目"},
		"references/stages/order.md":      {"# 阶段 下单", "处理下单。随后写库。", "### `internal/order/store.go`（go，20 行）", "职责：订单存储（repository）", "执行时机：请求时", "- `(*Store).Get` L12-15 —— 按 id 查询", "`internal/order/service.go`（go，30 行）（已过期，以源码为准，改用 rca_grep）", "旧的下单逻辑"},
		"references/index.md":             {"# svc-a 索引", "## 执行阶段", "| 下单 | 2 | 处理下单。 | `references/stages/order.md` |", "## 未归类文件", "`internal/pay/client.go`", "## 已过期文件", "`internal/order/service.go`"},
		"references/registers.md":         {"### `orders`\n\n用途：订单主表"},
		"references/overview.md":          {"## 系统总览（LLM 生成）", "下单后调用支付。", "文件卡片 1 个", "以源码为准"},
		"references/areas/internal-order.md": {"  - 职责：订单存储（repository）", "L12-15 —— 按 id 查询", "  - 职责（已过期，以源码为准）：旧的下单逻辑"},
	}
	for page, wants := range checks {
		got, ok := pages[page]
		if !ok {
			t.Fatalf("missing page %s; have %v", page, keysOf(pages))
		}
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s missing %q:\n%s", page, w, got)
			}
		}
	}
	if strings.Contains(pages["references/index.md"], "store_test.go") {
		t.Error("test files are never listed as unassigned")
	}
}

func TestRender_EmptyLayerMatchesStatic(t *testing.T) {
	meta, f, _ := llmRenderFixture()
	a, b := Render(meta, f, nil), Render(meta, f, &LLMLayer{})
	if len(a) != len(b) {
		t.Fatalf("pages differ: %v vs %v", keysOf(a), keysOf(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("page %s differs", k)
		}
	}
	if _, ok := a["references/stages/order.md"]; ok {
		t.Fatal("no stage pages without a skeleton")
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

再在 `builder_test.go` 增加：

```go
func TestBuild_ReadsLLMCache(t *testing.T) {
	root := writeFixtureRepo(t) // 现有 builder 测试使用的仓库夹具函数，名字以 builder_test.go 为准
	facts, err := CollectFacts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	cache := LLMCache{Dir: t.TempDir()}
	var target File
	for _, f := range facts.Files {
		if CardEligible(f) {
			target = f
			break
		}
	}
	if err := cache.PutCard(target.Hash, &Card{Purpose: "缓存里的职责", Hash: target.Hash, PromptVersion: LLMPromptVersion}); err != nil {
		t.Fatal(err)
	}
	out, err := Build(context.Background(), BuildInput{RepoID: "r", RelPath: "svc", Root: root, Commit: "c", LLMDir: cache.Dir, LLMRev: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Cards != 1 || out.Stats.LLMRev != "7" || out.Stats.GeneratorVersion != "p2b-1" {
		t.Fatalf("stats %#v", out.Stats)
	}
	found := false
	for p, b := range out.Files {
		if strings.HasPrefix(p, "skill/references/areas/") && strings.Contains(string(b), "缓存里的职责") {
			found = true
		}
	}
	if !found {
		t.Fatal("card not rendered")
	}
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `cd portal; go test -p 1 ./internal/handbook/ -run "Render_With|Render_Empty|Build_ReadsLLM"`
Expected: FAIL（`Render` 参数个数不符等）

- [ ] **Step 3: 实现**

按"渲染规则"修改 `render.go` 与 `builder.go`：

- `Render` 增加 `l *LLMLayer` 参数；内部用小助手 `cardOf(l, path) (card *Card, stale bool)`。
- `areaSections(a, f, l)`、`registerSections(hits, notes map[string]string)`、`indexSections(areas, f, l)`、`renderOverview(meta, f, areas, l)`、`renderSkill(meta, hasStages bool)` 增加参数；新增 `stageSections(st Stage, files []string, f *Facts, l *LLMLayer) []string` 与 `firstSentence(s string, n int) string`（按 `。`、`. `、换行截断第一句，再 `clipRunes(…, n)`）。
- 阶段页、index 中的阶段表按 `Skeleton.Stages` 顺序；阶段文件 = `Skeleton.Files` 中 stage 等于该 id、且仍在 `f.Files` 里的卡片资格文件。
- 未归类 = `f.Files` 中 `CardEligible` 且（不在 `Skeleton.Files` 或 stage 为空）的文件。
- 修改现有测试中 `Render(meta, f)` 调用为 `Render(meta, f, nil)`；`builder_test.go` 中的 `GeneratorVersion` 断言若写死 `"p2a-2"` 改为常量。

- [ ] **Step 4: 运行测试**

Run: `cd portal; go test -p 1 -count=1 ./internal/handbook/...`、`go vet -p 1 ./internal/handbook/`
Expected: PASS；同时跑 `go test -p 1 -count=1 ./internal/biz/...`（CGO 环境下再跑 `./internal/data/...`），确认 `GeneratorVersion` 变化未破坏既有断言（若有测试写死 `p2a-2`，改用 `handbook.GeneratorVersion`）。

- [ ] **Step 5: 提交**

```powershell
git add portal/internal/handbook/render.go portal/internal/handbook/builder.go portal/internal/handbook/render_llm_test.go portal/internal/handbook/render_test.go portal/internal/handbook/builder_test.go
git commit -m "feat(handbook): render file cards, stages, overview and register notes"
```

（如有其他测试文件因 `Render` 签名改动而修改，一并 `git add` 并在报告中列出。）

---

### Task 7: 仓储层：模型覆盖、LLM 状态与 LLM 租约

**Files:**
- Create: `portal/migrations/021_repo_handbook_llm.sql`
- Modify: `portal/internal/data/model/repo_registry.go`、`portal/internal/data/repo_registry.go`、`portal/internal/biz/repo_registry.go`、`portal/internal/biz/repo_registry_usecase.go`
- Test: `portal/internal/data/repo_handbook_llm_test.go`

- [ ] **Step 1: 写迁移**

```sql
-- 021: handbook LLM layer (P2b): per-repo model override, LLM state and the LLM run lease.
ALTER TABLE repositories
  ADD COLUMN handbook_model VARCHAR(255) NOT NULL DEFAULT '' AFTER handbook_lease_token,
  ADD COLUMN handbook_llm JSON NULL AFTER handbook_model,
  ADD COLUMN handbook_llm_lease_until DATETIME(3) NULL AFTER handbook_llm,
  ADD COLUMN handbook_llm_lease_token VARCHAR(36) NULL AFTER handbook_llm_lease_until;
```

- [ ] **Step 2: 模型与 biz 类型**

`model.Repository` 在 `HandbookLeaseToken` 之后加：

```go
	HandbookModel         string     `gorm:"column:handbook_model;size:255;not null;default:''"`
	HandbookLLM           JSONObject `gorm:"column:handbook_llm;type:json"`
	HandbookLLMLeaseUntil *time.Time `gorm:"column:handbook_llm_lease_until"`
	HandbookLLMLeaseToken *string    `gorm:"column:handbook_llm_lease_token;size:36"`
```

`biz.Repository` 在 `HandbookLeaseUntil` 之后加：

```go
	// HandbookModel overrides the global handbook model: "" inherits it, "off" disables the LLM layer.
	HandbookModel string `json:"handbook_model"`
	// HandbookLLM is the state of the LLM layer (see the P2b plan for fields).
	HandbookLLM map[string]any `json:"handbook_llm,omitempty"`
	// HandbookLLMLeaseUntil is set while an LLM run holds its lease.
	HandbookLLMLeaseUntil *time.Time `json:"handbook_llm_lease_until,omitempty"`
```

常量：`HandbookModelOff = "off"`；`maxHandbookModelLen = 255`。

`RepoMetaPatch` 加 `HandbookModel *string \`json:"handbook_model,omitempty"\``。`PatchRepo` 校验：`TrimSpace` 后长度 ≤ 255，且不含空白或控制字符（`strings.IndexFunc(s, unicode.IsSpace) < 0` 且无 `unicode.IsControl`），否则 `ErrInvalidRepo`；校验后写回修剪过的值。

`RepoRegistryRepo` 接口加：

```go
	// ClaimHandbookEnrich takes the LLM run lease when it is free or expired and returns its token.
	ClaimHandbookEnrich(ctx context.Context, id string, now, leaseUntil time.Time) (string, bool, error)
	// FinishHandbookEnrich stores the LLM state and clears the lease; ErrHandbookLeaseLost when the token no longer matches.
	FinishHandbookEnrich(ctx context.Context, id, token string, llm map[string]any) error
	// ReleaseHandbookEnrich clears the lease without touching the LLM state.
	ReleaseHandbookEnrich(ctx context.Context, id, token string) error
```

- [ ] **Step 3: 写失败测试 `repo_handbook_llm_test.go`**

参照 `repo_handbook_test.go` 里现有的建库与扫描夹具（同包 `data`），覆盖：

```go
func TestHandbookEnrichLease(t *testing.T) {
	// 1. 新仓库：Claim 成功（ok=true，token 非空），repositoryToBiz 后 HandbookLLMLeaseUntil 非 nil，
	//    且 handbook_status 不变（LLM 租约与构建租约互不影响：此时 ClaimHandbookBuild 仍能成功）。
	// 2. 租约未过期时再次 Claim → ok=false。
	// 3. now 超过 leaseUntil 后 Claim → ok=true，得到新 token；用旧 token Finish → ErrHandbookLeaseLost。
	// 4. 新 token Finish(map{"state":"complete","rev":"1"}) → 读回 HandbookLLM["state"]=="complete"、租约清空。
	// 5. Release 用错误 token → ErrHandbookLeaseLost；正确 token → 租约清空、HandbookLLM 不变。
}

func TestPatchRepo_HandbookModel(t *testing.T) {
	// PatchRepo(id, RepoMetaPatch{HandbookModel: ptr("  qwen/qwen-max ")}) → 读回 "qwen/qwen-max"；
	// "off" 合法；"a b" 与 256 个字符 → ErrInvalidRepo；Ptr("") 清空覆盖。
}
```

按注释写成完整测试（断言用 `t.Fatalf`），并使用 UTC 时间，与 `ClaimHandbookBuild` 测试一致。

- [ ] **Step 4: 运行，确认失败**

Run（CGO 环境）: `cd portal; go test -p 1 -count=1 ./internal/data/ -run "HandbookEnrichLease|PatchRepo_HandbookModel"`
Expected: FAIL（编译失败）

- [ ] **Step 5: 实现 data 层**

- `repositoryToBiz` 映射三个新字段（`HandbookLLM: map[string]any(m.HandbookLLM)`）。
- `UpdateRepositoryMeta` 处理 `p.HandbookModel`。
- 三个方法完全仿照 `ClaimHandbookBuild` / `FinishHandbookBuild` / `ReleaseHandbookBuild` 的写法（token 用 `uuid.NewString()`，时间 `.UTC()`，`RowsAffected == 0` → `biz.ErrHandbookLeaseLost`），只操作 `handbook_llm_lease_until`、`handbook_llm_lease_token`、`handbook_llm` 三列，**不改 `handbook_status`**。Claim 条件：`id = ? AND (handbook_llm_lease_until IS NULL OR handbook_llm_lease_until < ?)`。

- [ ] **Step 6: 运行测试**

Run（CGO 环境）: `cd portal; go test -p 1 -count=1 ./internal/data/... ./internal/biz/...`、`go vet -p 1 ./internal/...`
Expected: PASS

- [ ] **Step 7: 提交**

```powershell
git add portal/migrations/021_repo_handbook_llm.sql portal/internal/data/model/repo_registry.go portal/internal/data/repo_registry.go portal/internal/biz/repo_registry.go portal/internal/biz/repo_registry_usecase.go portal/internal/data/repo_handbook_llm_test.go
git commit -m "feat(repo-registry): handbook model override, LLM state and LLM lease"
```

---

### Task 8: `HandbookUsecase` 的 LLM 层

**Files:**
- Create: `portal/internal/biz/handbook_llm.go`
- Modify: `portal/internal/biz/handbook.go`
- Test: 追加到 `portal/internal/data/repo_handbook_llm_test.go`

- [ ] **Step 1: 设计（实现照此）**

`handbook_llm.go`：

```go
package biz

// HandbookLLMConfig is the handbook: section of the portal config.
type HandbookLLMConfig struct {
	Model               string // "" disables the LLM layer unless a repo overrides it
	Concurrency         int    // model calls in flight per run, default 4, max 16
	MaxCardsPerRun      int    // default 600
	MaxFileKB           int    // file content sent per card, default 24, max 256
	MaxRunMinutes       int    // default 15, max 20 (lease = run + 10 min)
	SkeletonRebuildDays int    // default 30
}

// HandbookModelResolver turns a model name ("model" or "provider/model") into a model client.
type HandbookModelResolver func(ctx context.Context, name string) (model.Model, error)

var (
	ErrHandbookLLMDisabled = errors.New("handbook: LLM layer is disabled for this repository")
	ErrHandbookNotReady    = errors.New("handbook: deterministic handbook is not current")
)
```

`HandbookUsecase` 新增字段：`llmCfg HandbookLLMConfig`（已套默认值）、`resolve HandbookModelResolver`、`enrichMu sync.Mutex`（EnrichPending 的 TryLock）、`enrichSlots chan struct{}`（容量 1，`EnrichPending` 与 `RequestEnrich` 共用）。`NewHandbookUsecase` 中初始化 `enrichSlots`。

方法：

- `SetLLM(cfg HandbookLLMConfig, resolve HandbookModelResolver)`：套默认值和上限后保存。
- `LLMConfig() HandbookLLMConfig`：给 HTTP `GET /handbook/config` 用。
- `modelFor(r *Repository) string`：`r.HandbookModel == "off"` → `""`；非空 → 它；否则 `uc.llmCfg.Model`；`uc.resolve == nil` 时一律 `""`。
- `expectedLLMRev(r) string`：`modelFor(r) == ""` → `""`；否则 `r.HandbookLLM["rev"]` 字符串。
- `needsEnrich(r, now) bool`：

```go
	if uc.modelFor(r) == "" || r.Status != RepoStatusActive || r.HandbookVersion == 0 ||
		r.HandbookCommit == "" || r.HandbookCommit != r.HeadCommit || r.HandbookStatus == HandbookStatusBuilding {
		return false
	}
	if r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(now) {
		return false
	}
	state, _ := r.HandbookLLM["state"].(string)
	if state == handbook.LLMStateFailed {
		failed, _ := r.HandbookLLM["failed_commit"].(string)
		return failed != r.HandbookCommit
	}
	commit, _ := r.HandbookLLM["commit"].(string)
	pv, _ := r.HandbookLLM["prompt_version"].(string)
	if state != handbook.LLMStateComplete || commit != r.HandbookCommit || pv != handbook.LLMPromptVersion {
		return true
	}
	if ts, _ := r.HandbookLLM["skeleton_built_at"].(string); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil && now.Sub(t) > time.Duration(uc.llmCfg.SkeletonRebuildDays)*24*time.Hour {
			return true
		}
	}
	return false
```

- `EnrichPending(ctx) (int, error)`：`enrichMu.TryLock` 失败返回 0；列出 active 仓库；对 `needsEnrich` 的仓库依次：阻塞获取 `enrichSlots`（尊重 ctx）、`ClaimHandbookEnrich`（租约 `MaxRunMinutes + 10` 分钟）、同步执行 `runEnrichClaimed(ctx, id, token, false)`、释放槽位。返回执行次数。
- `RequestEnrich(ctx, id string, full bool) error`：仓库不存在 → `ErrRepoNotFound`；非 active → `ErrInvalidRepo`；`modelFor == ""` → `ErrHandbookLLMDisabled`；`HandbookVersion == 0 || HandbookCommit != HeadCommit` → `ErrHandbookNotReady`；`enrichSlots` 非阻塞获取失败或 Claim 失败 → `ErrHandbookBuilding`（Claim 失败时立即归还槽位）；成功后 `wg.Add(1)` 起 goroutine 跑 `runEnrichClaimed(context.WithoutCancel(ctx), id, token, full)`，goroutine 结束时归还槽位。Claim 的租约时长为 `MaxRunMinutes + 10` 分钟，时间用 UTC。
- `runEnrichClaimed(ctx, id, token string, full bool) error`：

```go
	start := uc.now()
	r, err := uc.getRepo(ctx, id)
	if err != nil {
		return errors.Join(err, uc.releaseEnrich(ctx, id, token))
	}
	name := uc.modelFor(r)
	if name == "" || r.Status != RepoStatusActive || r.HandbookVersion == 0 || r.HandbookCommit != r.HeadCommit {
		return uc.releaseEnrich(ctx, id, token)
	}
	fail := func(cause error) error { /* 复制 r.HandbookLLM，写 state=failed、last_error（truncateRunes 500）、failed_commit=r.HandbookCommit、model=name、run_at；finishEnrich；返回 cause */ }
	m, err := uc.resolve(ctx, name)
	if err != nil {
		return fail(fmt.Errorf("解析模型 %s: %w", name, err))
	}
	facts, err := uc.store.ReadFacts(r.ID, r.HandbookVersion)
	if err != nil {
		return fail(err)
	}
	root, err := uc.registry.ResolveRepoPath(r)
	if err != nil {
		return fail(err)
	}
	cache, err := uc.store.LLMCache(r.ID)
	if err != nil {
		return fail(err)
	}
	rctx, cancel := context.WithTimeout(ctx, time.Duration(uc.llmCfg.MaxRunMinutes)*time.Minute)
	defer cancel()
	res, err := handbook.Enrich(rctx, handbook.EnrichInput{
		RelPath: r.RelPath, Root: root, Commit: r.HandbookCommit, ModelName: name, Facts: facts, Cache: cache, Model: m, Now: start.UTC(),
		Opts: handbook.EnrichOptions{Concurrency: uc.llmCfg.Concurrency, MaxCardsPerRun: uc.llmCfg.MaxCardsPerRun,
			MaxFileBytes: uc.llmCfg.MaxFileKB << 10, SkeletonRebuildDays: uc.llmCfg.SkeletonRebuildDays, Full: full},
	})
	if ctx.Err() != nil {
		return errors.Join(err, uc.releaseEnrich(ctx, id, token))
	}
	if err != nil {
		return fail(err)
	}
	llm := map[string]any{
		"state": res.State, "model": name, "commit": r.HandbookCommit, "prompt_version": handbook.LLMPromptVersion,
		"cards_total": res.CardsTotal, "cards_done": res.CardsDone, "cards_new": res.CardsNew, "card_errors": res.CardErrors,
		"stages": res.Stages, "fallback": res.Fallback, "skeleton_rebuilt": res.SkeletonRebuilt, "rebuild_reason": res.RebuildReason,
		"tokens_in": res.TokensIn, "tokens_out": res.TokensOut,
		"run_at": start.UTC().Format(time.RFC3339), "duration_ms": uc.now().Sub(start).Milliseconds(),
	}
	if !res.SkeletonBuiltAt.IsZero() {
		llm["skeleton_built_at"] = res.SkeletonBuiltAt.UTC().Format(time.RFC3339)
	} else if v, ok := r.HandbookLLM["skeleton_built_at"]; ok {
		llm["skeleton_built_at"] = v
	}
	rev, _ := r.HandbookLLM["rev"].(string)
	if res.Changed || rev == "" {
		rev = strconv.FormatInt(uc.now().UnixNano(), 36)
	}
	llm["rev"] = rev
	if err := uc.finishEnrich(ctx, id, token, llm); err != nil {
		return err
	}
	if res.Changed {
		if err := uc.RequestRebuild(context.WithoutCancel(ctx), id); err != nil && !errors.Is(err, ErrHandbookBuilding) {
			uc.log.Warnf("handbook re-render %s: %v", id, err)
		}
	}
	return nil
```

  `finishEnrich` / `releaseEnrich` 仿照 `finish` / `release`（`ErrHandbookLeaseLost` 记 warn 后返回 nil，写库用 `context.WithoutCancel`）。

`handbook.go` 修改：

- `handbookNeedsRebuild` 改为方法 `uc.needsRebuild(r, now)`，在原逻辑最后一行改为：

```go
	gen, _ := r.HandbookStats["generator_version"].(string)
	rev, _ := r.HandbookStats["llm_rev"].(string)
	return r.HandbookCommit != r.HeadCommit || gen != handbook.GeneratorVersion || rev != uc.expectedLLMRev(r)
```

- `runClaimed` 构建时传：

```go
	in := handbook.BuildInput{RepoID: r.ID, RelPath: r.RelPath, Root: root, Commit: r.HeadCommit, Now: start, LLMRev: uc.expectedLLMRev(r)}
	if uc.modelFor(r) != "" {
		if c, err := uc.store.LLMCache(r.ID); err == nil {
			in.LLMDir = c.Dir
		}
	}
```

- `HandbookView` 加 `LLM map[string]any \`json:"llm,omitempty"\``、`LLMModel string \`json:"llm_model"\``、`LLMRunning bool \`json:"llm_running"\``；`GetHandbook` 填 `r.HandbookLLM`、`uc.modelFor(r)`、`r.HandbookLLMLeaseUntil != nil && r.HandbookLLMLeaseUntil.After(uc.now().UTC())`。
- `Wait()` 也覆盖 `RequestEnrich` 的 goroutine（共用 `wg`）。

- [ ] **Step 2: 写失败测试（追加到 `repo_handbook_llm_test.go`）**

在 data 包测试里实现一个最小假模型（`framework/model.Model`，按 system prompt 关键字返回固定 JSON：`文件卡片` → `{"purpose":"职责"}`；`执行阶段` → 让所有目录归同一阶段的骨架需至少 2 个阶段——用两个阶段并按目录名奇偶分配，或直接返回无效 JSON 让其退化为分区，二选一并在断言中对应；`阶段说明` → `{"summary":"说明"}`；`仓库总览` → `{"overview":"总览"}`；`用途` → `{"notes":{}}`）。复用 P2a 的 `newHandbookFixture`（或同等夹具：建仓库目录、写几个 `.go` 文件、扫描、RebuildStale）。覆盖：

1. `TestHandbookLLM_DisabledWithoutModel`：未 `SetLLM` 时 `EnrichPending` 返回 0、`RequestEnrich` → `ErrHandbookLLMDisabled`，`GetHandbook().LLMModel == ""`。
2. `TestHandbookLLM_EnrichThenRerender`：`SetLLM({Model:"fake"}, resolver 返回假模型)`；`RebuildStale` 后 `EnrichPending` 返回 1；`Wait()`；仓库 `HandbookLLM["state"]=="complete"`、`cards_done == cards_total > 0`、`rev` 非空；`HandbookStats["llm_rev"] == HandbookLLM["rev"]`、`HandbookVersion` 增加了 1（重渲染发布）；`ReadPage(..., "references/index.md")` 含 `## 执行阶段` 或（退化时）分区阶段表；再次 `EnrichPending` 返回 0。
3. `TestHandbookLLM_RepoOverrideOff`：`PatchRepo` 设 `handbook_model="off"` 后 `needsRebuild` 触发一次确定性重建（`llm_rev` 变为空），`EnrichPending` 返回 0，页面不再含 `## 执行阶段`。
4. `TestHandbookLLM_ModelFailureRecorded`：resolver 返回错误 → `state=failed`、`failed_commit == HandbookCommit`、`last_error` 含模型名；再次 `EnrichPending` 返回 0；`RequestEnrich(full=false)` 仍可手动触发（返回 nil）。
5. `TestHandbookLLM_ParentCancelReleases`：resolver 返回的假模型在第一次调用时取消父 ctx（`EnrichPending(ctx)` 的 ctx）→ 租约释放（`HandbookLLMLeaseUntil == nil`），`HandbookLLM` 不变（仍为空），不记失败。
6. `TestHandbookLLM_RequestEnrichErrors`：仓库无 handbook（未构建）→ `ErrHandbookNotReady`；持有 LLM 租约时 → `ErrHandbookBuilding`。

- [ ] **Step 3: 运行，确认失败；实现；再运行**

Run（CGO 环境）: `cd portal; go test -p 1 -count=1 ./internal/data/ -run HandbookLLM -v`，然后 `go test -p 1 -count=1 ./internal/data/... ./internal/biz/... ./internal/handbook/...`、`go test -p 1 -race -count=1 ./internal/data/ -run Handbook`、`go vet -p 1 ./internal/...`
Expected: PASS

- [ ] **Step 4: 提交**

```powershell
git add portal/internal/biz/handbook_llm.go portal/internal/biz/handbook.go portal/internal/data/repo_handbook_llm_test.go
git commit -m "feat(handbook): LLM enrichment usecase with its own lease and budget"
```

---

### Task 9: 配置、模型解析、接线、调度与 HTTP

**Files:**
- Create: `portal/internal/conf/handbook_config.go`、`portal/internal/conf/handbook_config_test.go`
- Modify: `portal/internal/service/critic_model.go`；Create: `portal/internal/service/handbook_llm.go`（+ `handbook_llm_test.go`）
- Modify: `portal/cmd/backend/main.go`、`portal/cmd/backend/wire.go`、`portal/cmd/backend/wire_gen.go`
- Modify: `portal/internal/cron/scheduler.go`
- Modify: `portal/internal/server/repo_registry.go`、`portal/internal/server/http.go`、`portal/internal/server/repo_registry_test.go`
- Modify: `portal/configs/config.yaml`

- [ ] **Step 1: 配置段（照 `chat_config.go` 的模式）**

```go
package conf

// HandbookConfig is the handbook: section of config.yaml (LLM layer of repository handbooks).
type HandbookConfig struct {
	Model               string `yaml:"model"`
	Concurrency         int    `yaml:"concurrency"`
	MaxCardsPerRun      int    `yaml:"max_cards_per_run"`
	MaxFileKB           int    `yaml:"max_file_kb"`
	MaxRunMinutes       int    `yaml:"max_run_minutes"`
	SkeletonRebuildDays int    `yaml:"skeleton_rebuild_days"`
}

// LoadHandbookFromConfigPath reads handbook.* from the -conf file/dir; env SATH_HANDBOOK_MODEL overrides model.
func LoadHandbookFromConfigPath(confPath string) (*HandbookConfig, error)
```

测试：临时目录写 `config.yaml`（含 `handbook: {model: "qwen/qwen-max", concurrency: 2}`），读回；设置 `SATH_HANDBOOK_MODEL=x/y`（`t.Setenv`）覆盖；文件不存在时返回零值不报错。

`config.yaml` 加（注释说明，默认不启用）：

```yaml
# 仓库 handbook 的 LLM 增强层（P2b）。model 为空则只生成静态 handbook；
# 名字按模型目录解析：模型名，或 "<provider 名称或 ID>/<模型名>"。仓库页可单独覆盖。
handbook:
  model: ""
  concurrency: 4
  max_cards_per_run: 600
  max_file_kb: 24
  max_run_minutes: 15
  skeleton_rebuild_days: 30
```

- [ ] **Step 2: 模型解析**

把 `criticModelResolver` 的核心抽成：

```go
// catalogModelResolver resolves "model" or "<provider name or ID>/<model>" among usable catalog models.
func catalogModelResolver(cat criticModelCatalog, build func(provider, modelName, apiKey, baseURL string) (model.Model, error)) func(ctx context.Context, name string) (model.Model, error)
```

`criticModelResolver` 改为包一层 5 秒超时后调用它（行为与错误信息保持不变，现有 critic 测试必须通过）。新文件 `handbook_llm.go`：

```go
// ConfigureHandbookLLM enables the handbook LLM layer with models from the catalog.
func ConfigureHandbookLLM(uc *biz.HandbookUsecase, cfg *conf.HandbookConfig, cat criticModelCatalog) {
	if uc == nil || cfg == nil {
		return
	}
	resolve := catalogModelResolver(cat, chat.BuildModel)
	uc.SetLLM(biz.HandbookLLMConfig{
		Model: strings.TrimSpace(cfg.Model), Concurrency: cfg.Concurrency, MaxCardsPerRun: cfg.MaxCardsPerRun,
		MaxFileKB: cfg.MaxFileKB, MaxRunMinutes: cfg.MaxRunMinutes, SkeletonRebuildDays: cfg.SkeletonRebuildDays,
	}, func(ctx context.Context, name string) (model.Model, error) {
		ctx, cancel := context.WithTimeout(ctx, criticModelLookupTimeout)
		defer cancel()
		return resolve(ctx, name)
	})
}
```

注意：即使 `cfg.Model` 为空也要 `SetLLM`（仓库级覆盖仍可启用）。测试 `handbook_llm_test.go`：用假 catalog（实现 `ListUsable` / `GetProviderSecret`）验证 `catalogModelResolver` 的 `provider/model` 匹配与 provider 禁用报错。

- [ ] **Step 3: 接线**

- `main.go`：在加载 chat 配置的旁边 `handbookCfg, err := conf.LoadHandbookFromConfigPath(flagconf)`（出错则 `panic`/返回，与 chat 一致），作为新参数传给 `wireApp`。
- `wire.go`：`wireApp` 签名加 `*conf.HandbookConfig`（保持 wire 注入声明一致，不运行 wire）。
- `wire_gen.go`：`wireApp` 加参数 `handbookConfig *conf.HandbookConfig`；在创建 `handbookUsecase` 之后：`service.ConfigureHandbookLLM(handbookUsecase, handbookConfig, data.NewModelCatalogStore(dataData.DB()))`（变量名以文件实际为准）。
- `cron/scheduler.go` 的 `runHandbookRebuild`：`RebuildStale` 之后调用 `EnrichPending`（同一个 recover/日志模式；`EnrichPending` 出错只记日志）。因为 `EnrichPending` 内部会对有变化的仓库调用 `RequestRebuild`，不需要再跑一次 `RebuildStale`。

- [ ] **Step 4: HTTP**

- `POST /api/v1/repos/{id}/handbook/enrich`（`?full=1` 表示重建骨架）→ `RequestEnrich`，成功 202 `{"ok":true}`（与 rebuild 一致的响应风格）。错误映射：`ErrHandbookLLMDisabled` → 400 `HANDBOOK_LLM_DISABLED`；`ErrHandbookNotReady` → 409 `HANDBOOK_NOT_READY`；`ErrHandbookBuilding` → 409 `HANDBOOK_BUILDING`；其余沿用现有 `repoRegistryErr`。handbook usecase 为 nil → 503 `HANDBOOK_DISABLED`（与现有三个接口一致）。
- `GET /api/v1/handbook/config` → `{"model": "<全局模型>", "enabled": <全局模型非空>}`（usecase 为 nil 时 `{"model":"","enabled":false}`）。
- `PATCH /api/v1/repos/{id}` 透传 `handbook_model`（`RepoMetaPatch` 已有字段，确认 handler 解码的是该结构体；若是手写字段映射则补上）。
- `repo_registry_test.go`：错误映射表加三项；`TestHandbookRoutes_DisabledWithoutUsecase` 覆盖新接口返回 503。

- [ ] **Step 5: 验证**

Run（CGO 环境）:
- `cd portal; go test -p 1 -count=1 ./internal/conf/... ./internal/service/... ./internal/server/... ./internal/cron/... ./internal/biz/... ./internal/data/... ./internal/handbook/...`
- `go vet -p 1 ./internal/... ./cmd/...`
- `go build -p 1 -o $env:TEMP\backend-check.exe ./cmd/backend/`，之后删除产物
Expected: 全部通过（只允许约定里列出的已知无关失败）

- [ ] **Step 6: 提交**

```powershell
git add portal/internal/conf/handbook_config.go portal/internal/conf/handbook_config_test.go portal/internal/service/critic_model.go portal/internal/service/handbook_llm.go portal/internal/service/handbook_llm_test.go portal/cmd/backend/main.go portal/cmd/backend/wire.go portal/cmd/backend/wire_gen.go portal/internal/cron/scheduler.go portal/internal/server/repo_registry.go portal/internal/server/http.go portal/internal/server/repo_registry_test.go portal/configs/config.yaml
git commit -m "feat(handbook): wire LLM layer config, catalog models, scheduling and API"
```

---

### Task 10: Web：LLM 状态、模型覆盖、弹窗 LLM 区块

**Files:**
- Modify: `web/src/api/repoRegistryTypes.ts`、`web/src/api/repoRegistry.ts`、`web/src/utils/repoRegistry.ts`、`web/src/components/HandbookDialog.tsx`、`web/src/pages/RepoListPage.tsx`、`web/src/pages/RepoRegistry.css`
- Test: `web/tests/repoRegistry.test.ts`、`web/e2e/repo-registry.spec.ts`

- [ ] **Step 1: 类型与 API**

```ts
export interface HandbookLLMStats {
  state?: 'partial' | 'complete' | 'failed'
  model?: string
  commit?: string
  cards_total?: number
  cards_done?: number
  cards_new?: number
  card_errors?: number
  stages?: number
  fallback?: boolean
  skeleton_rebuilt?: boolean
  rebuild_reason?: string
  skeleton_built_at?: string
  tokens_in?: number
  tokens_out?: number
  run_at?: string
  duration_ms?: number
  rev?: string
  last_error?: string
  failed_commit?: string
}

export interface HandbookConfigView {
  model: string
  enabled: boolean
}
```

`Repository` 加 `handbook_model?: string`、`handbook_llm?: HandbookLLMStats | null`、`handbook_llm_lease_until?: string`；`RepoMetaPatch` 加 `handbook_model?: string`；`HandbookStats` 加 `cards?`、`stale_cards?`、`stages?`、`llm_rev?`；`HandbookView` 加 `llm?: HandbookLLMStats | null`、`llm_model: string`、`llm_running: boolean`。

`repoApi` 加 `enrichHandbook: (id: string, full: boolean) => request<{ ok: boolean }>(\`/repos/${enc(id)}/handbook/enrich${full ? '?full=1' : ''}\`, send('POST', {}))`；新增 `handbookConfigApi.get = () => request<HandbookConfigView>('/handbook/config')`（放在 `repoRegistry.ts`，风格与现有一致）。

- [ ] **Step 2: 纯函数（先写测试）**

`utils/repoRegistry.ts`：

```ts
export type LLMState = 'off' | 'running' | 'pending' | 'partial' | 'complete' | 'failed'

export const LLM_STATE_LABELS: Record<LLMState, string> = {
  off: '未启用',
  running: '增强中',
  pending: '待增强',
  partial: '部分完成',
  complete: '已完成',
  failed: '失败',
}

/** Model used for a repo's LLM layer: the repo override, else the global model; '' when disabled. */
export function effectiveHandbookModel(repo: Pick<Repository, 'handbook_model'>, globalModel: string): string {
  const own = (repo.handbook_model ?? '').trim()
  if (own === 'off') return ''
  return own || globalModel.trim()
}

export function llmState(repo: Repository, globalModel: string, now: number = Date.now()): LLMState {
  if (!effectiveHandbookModel(repo, globalModel)) return 'off'
  if (repo.handbook_llm_lease_until && Date.parse(repo.handbook_llm_lease_until) > now) return 'running'
  const llm = repo.handbook_llm
  if (llm?.state === 'failed') return 'failed'
  if (!llm?.state || llm.commit !== repo.handbook_commit) return 'pending'
  return llm.state === 'complete' ? 'complete' : 'partial'
}

export function llmProgress(repo: Repository): string {
  const llm = repo.handbook_llm
  if (!llm || llm.cards_total === undefined) return ''
  return `${llm.cards_done ?? 0}/${llm.cards_total}`
}
```

`anyHandbookBuildActive` 扩展：任一仓库 LLM 租约未过期也返回 true（轮询期间刷新进度）。

测试（`web/tests/repoRegistry.test.ts`）覆盖：`effectiveHandbookModel` 的继承 / 覆盖 / `off`；`llmState` 的 off、running（租约未来时间）、过期租约不算 running、failed、commit 不一致 → pending、partial、complete；`llmProgress`；`anyHandbookBuildActive` 对 LLM 租约的判断。

- [ ] **Step 3: 页面**

- `RepoListPage`：加载时 `handbookConfigApi.get()`（失败按 `{model:'', enabled:false}`）。Handbook 列在现有状态徽章下面加一行 LLM 徽章：`<span className={\`badge badge-llm-${state}\`} data-testid={\`llm-state-${r.id}\`} title={llm.last_error ?? ''}>LLM {LLM_STATE_LABELS[state]}{progress ? \` ${progress}\` : ''}</span>`；`off` 时显示灰色"LLM 未启用"。
- 编辑弹窗加"Handbook 模型"字段：`<input list="handbook-model-options" placeholder={globalModel ? \`继承全局（${globalModel}）\` : '未配置全局模型'} />`，下方提示"留空继承全局，填 off 禁用该仓库的 LLM 增强"。`datalist` 选项：`off` + 打开弹窗时异步加载的 `modelCatalogApi.listProviders()` 与 `listCatalog()`，生成 `${provider.name}/${entry.model}`（只取 `enabled` 的 provider 与 `!hidden` 的条目，加载失败则只有 `off`）。保存时把修剪后的值放进 PATCH 的 `handbook_model`（与原值相同时也一并发送，后端幂等）。
- `HandbookDialog`：在头部下面加"LLM 增强"区块（`data-testid="handbook-llm"`）：状态（用 `view.llm_running` / `view.llm` / `view.llm_model` 推导，规则与 `llmState` 一致——可把 view 映射成 Repository 形状复用 `llmState`）、模型、卡片 `cards_done/cards_total`（有 `card_errors` 时注明"N 个文件生成失败"）、阶段数（`fallback` 时注明"按目录分区"）、最近一轮 token（输入/输出）与时间、`last_error`。按钮"重新生成 LLM 内容"（`llm_model` 为空时禁用并提示"未启用"），点击调用 `enrichHandbook(id, true)`，成功提示"已开始，完成后自动刷新"并重新拉取 view；409/400 把服务端消息显示在区块内。
- CSS：`badge-llm-*` 六种颜色沿用现有变量（running 用 `--accent-subtle`，complete 用 `--ok`，failed 用 `--destructive`，partial/pending 用 `--warning`，off 用 `--muted`）。

- [ ] **Step 4: e2e（`page.route` mock）**

在 `repo-registry.spec.ts` 增加：

1. 列表：mock `/api/v1/handbook/config` 返回 `{model:'qwen/qwen-max',enabled:true}`，仓库 `handbook_llm: {state:'partial', commit:<同 handbook_commit>, cards_done:120, cards_total:600}` → `llm-state-<id>` 文本含 `部分完成 120/600`；另一个仓库 `handbook_model:'off'` → `未启用`。
2. 编辑：打开编辑弹窗，填 Handbook 模型 `off`，保存，断言 PATCH 请求体 `handbook_model === 'off'`。
3. 弹窗：`/handbook` view 带 `llm` 与 `llm_model`，点击"重新生成 LLM 内容"，断言 POST 到 `/handbook/enrich?full=1`，并出现"已开始"。

- [ ] **Step 5: 验证**

Run: `npm --prefix web test`、`npm --prefix web run build`、`cd web; npx playwright test e2e/repo-registry.spec.ts`
Expected: 全部通过

- [ ] **Step 6: 提交**

```powershell
git add web/src/api/repoRegistryTypes.ts web/src/api/repoRegistry.ts web/src/utils/repoRegistry.ts web/src/components/HandbookDialog.tsx web/src/pages/RepoListPage.tsx web/src/pages/RepoRegistry.css web/tests/repoRegistry.test.ts web/e2e/repo-registry.spec.ts
git commit -m "feat(web): handbook LLM state, model override and enrichment controls"
```

---

### Task 11: 文档同步、全量验证与真实模型冒烟

**Files:**
- Modify: `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md`、本计划（文末"实施后说明"）

- [ ] **Step 1: 设计文档**

- §4.1：补 `handbook_model`、`handbook_llm`（字段表）、`handbook_llm_lease_until/token`（migration 021）。
- §7.1：`generated/` 改为实际的 `llm/`（`cards/<hash 前 2 位>/<hash>-<prompt 版本>.json`、`skeleton.json`），说明它不随版本切换、由确定性构建读取渲染；`references/stages/<id>.md` 标为已实现。
- §7.2 Phase II/III：增加"P2b 实现"小节，写明卡片资格、提示词要点、按目录分配阶段与退化、合成内容、`Changed` → `rev` → 重渲染。
- §8.1：三层更新改为实际做法（确定性层每次 HEAD 变化发布；LLM 层每轮预算、增量与重建阈值；冻结 = 渲染时的"已过期"标注）。
- §8.2：补 LLM 租约（独立于构建租约，时长 = `max_run_minutes + 10`）、`enrichSlots` 容量 1、失败与取消语义。
- §11 API、§12 前端：新增接口与 UI。
- §15：P2b 验收保留；§16：`rca_feedback` 迁移改为 `022`，加入 P2b 新文件；§13.1 指标：`handbook_llm` 中的 token 与卡片计数即构建 token 指标来源。

- [ ] **Step 2: 全量验证**

- `cd framework; go test -p 1 -count=1 ./skills/... ./tool/skillops/...`
- （CGO 环境）`cd portal; go vet -p 1 ./internal/... ./cmd/...`；`go test -p 1 -count=1 ./internal/...`；`go test -p 1 -race -count=1 ./internal/handbook/... ./internal/data/ -run "Handbook|Enrich"`；`go build -p 1 -o $env:TEMP\backend-check.exe ./cmd/backend/` 后删除
- `npm --prefix web test`、`npm --prefix web run build`、`cd web; npx playwright test`

记录通过/失败数；失败只允许约定里列出的已知无关项，其余需分析。

- [ ] **Step 3: 真实模型冒烟（需要人工配合，不阻塞提交）**

在计划末尾"实施后说明"记录以下步骤供人工执行：

1. 在模型目录确认一个可用模型（建议 openai_compat 类型；DashScope 实现的结果解析未验证，若卡片全部"回复不可用"，先换 openai_compat 模型）。
2. `config.yaml` 设 `handbook.model: "<provider 名>/<模型名>"`，或在仓库页给单个仓库填覆盖。
3. 重启 portal，在仓库页对一个业务仓库点"重建"，观察 LLM 徽章从"待增强"→"增强中 x/y"→"已完成"；打开 Handbook 查看 `references/index.md` 的执行阶段与某个阶段页。
4. 修改该仓库一个文件并提交，下一次扫描后确认只有该文件重新生成卡片（`cards_new = 1`），阶段页中的对应条目更新。

- [ ] **Step 4: 提交**

```powershell
git add docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md docs/superpowers/plans/2026-10-09-repo-handbook-p2b.md
git commit -m "docs: sync P2b handbook LLM layer design"
```

---

## 实施后说明

实现提交 `62e7f47..0acf6df`（`git log --oneline 8ae99c8..HEAD`）。以代码为准，设计文档 §4.1、§7.1、§7.2"P2b 实现"、§8.1–§8.3、§11、§12、§13.1、§15、§16 已同步。

### 实施偏差

评审后与上文计划正文不一致的主要地方：

- **卡片按内容去重**：内容相同的文件共用一张卡片、一次模型调用；`max_cards_per_run` 计的是**模型调用数**，不是文件数。`cards_*` 计数仍按文件。
- **卡片失败分两类**：回复不可用 → 内容哈希写入失败集 `llm/failed-<prompt 版本>.json`（计划为"本轮计数、HEAD 变化前不重试"）；传输错误 → 计入新字段 `card_transport_errors`，状态 `partial`，下轮重试，同一哈希 3 轮传输错误后才转入失败集。有待重试的传输错误时不合成。`full` 运行清空失败集。
- **过期卡片**：`FileAssign` 增加 `Hash`（上次归类时的内容），`CardHash` 只在当前内容有卡片时更新，否则保留旧卡片哈希用于"已过期"渲染；清理时保留骨架引用的旧卡片。
- **骨架提示词降级**：计划是超过 120KB 直接退化；实际先逐级减少职责行（3→1→0），再把目录合并到前 4→1 级，同时要求目录数 ≤ 359。回复 `max_tokens = min(1000 + 20 × 目录数, 8192)`（计划固定 6000）。阶段下限为 2（提示词要求 3–15），超过 15 个保留文件最多的 15 个。
- **退化原因**：新增 `fallback_reason`（`too_large` / `bad_reply` / `few_stages` / `unassigned` / `no_model`），超过 15 个分区时合并出"其他"；`bad_reply` 退化 24 小时后重试（重建原因 `fallback_retry`，`needsEnrich` 同样检查）。骨架推断的传输错误不退化，按失败处理。
- **增量与重建**：变得没有文件的阶段被删除；`ChangedPaths` 记录去重后的路径（同一文件多次改动只算一次）；`unassigned` 在增量更新后判断，其余条件先判断。
- **合成**：传输错误重试 2 次（线性退避）；0 个阶段不写总览；寄存器用途只重写读写位置有改动的条目，重建时沿用同提示词版本的旧用途后再重写；所有步骤成功才写骨架，内容和 commit 都没变时不写。
- **渲染**：LLM 文本在渲染时再次截断并净化（单行折叠、表格转义 `|`、行首转义、多行 Markdown 标题降级到 `####`、补闭合代码围栏、非法阶段 id 跳过）；读取 `llm/` 出错时按无 LLM 渲染且 `llm_rev` 留空，下轮重试；版本目录新增 `facts/module.json` 供 `ReadFacts` 使用。
- **biz 运行**：`EnrichPending` 按 `run_at` 升序（从未运行的最先），本次调用已用时超过运行超时后不再启动新运行；`RequestEnrich` 在其他仓库占用槽位时返回新的 `ErrHandbookLLMBusy`（409 `HANDBOOK_LLM_BUSY`），该仓库租约被占才是 `HANDBOOK_BUILDING`；`handbookCurrent` 额外要求 `generator_version` 为当前版本。`failed` 在 HEAD **或模型**变化后自动重试。读取 facts、解析仓库路径、打开缓存等基础设施错误不记失败，只写 `last_error`，下轮重试；失败轮次若已改变缓存仍换新 rev。未合成的轮次沿用上轮的 `stages` / `fallback` / `fallback_reason` / `skeleton_built_at`；`last_error` 也记录非失败轮次的最后一次传输错误。
- **配置**：`concurrency` 上限 16、`max_file_kb` 上限 256、`max_run_minutes` 上限 20；有模型目录就安装解析器（即使全局模型为空，仓库覆盖也能运行）；解析带超时。`handbook_model` 校验：≤ 255、无空白/控制字符，`off` 不区分大小写。
- **HTTP**：enrich 成功返回 200 `{"accepted":true}`（计划为 202 `{"ok":true}`）；`full` 用 `strconv.ParseBool` 解析，非法 400；`GET /handbook/config` 返回完整配置及 `available`（已安装解析器）与 `enabled`（`available` 且全局模型非空）。
- **Web**：`request()` 抛出带 `status` / `reason` 的 `ApiError`，enrich 错误按 reason 映射中文提示；新增 `off` / `running` / `pending` 状态，`failed` 只在 `failed_commit` 与模型都未变时显示；列表在 LLM 运行中也轮询；弹窗运行中每 3 秒刷新并在结束后重新加载页面内容；模型候选来自模型目录（仅启用且有 key 的 provider，名称不可用时用 ID）。

### 真实模型冒烟（人工执行，需要模型凭据）

1. 在"模型目录"确认一个可用模型：provider 已启用且有 API key，模型条目未隐藏。建议 openai_compat 类型；DashScope 实现的结果解析未验证，若卡片全部"生成失败"（`card_errors` ≈ `cards_total`），先换 openai_compat 模型。
2. 配置模型（三选一）：`portal/configs/config.yaml` 的 `handbook.model: "<provider 名称或 ID>/<模型名>"`（或精确模型名）；或环境变量 `SATH_HANDBOOK_MODEL`；或在仓库页"编辑"弹窗的"Handbook 模型"里给单个仓库填覆盖（立即生效，无需重启）。前两种需要重启 portal（配置只在启动时读取）。重启后 `GET /api/v1/handbook/config` 应返回 `available: true`，设置了全局模型时 `enabled: true`。
3. 让 LLM 层跑起来：确认该仓库 Handbook 列为"最新"（生成器版本升到 `p2b-1` 后首次扫描会全部重建，也可点"重建"）。之后等下一次扫描（默认 10 分钟，或 `POST /api/v1/repos/scan`）——扫描成功后 cron 先 `RebuildStale` 再 `EnrichPending`；或打开 Handbook 弹窗点"重新生成 LLM 内容"（`POST /api/v1/repos/{id}/handbook/enrich?full=1`）。观察 LLM 徽章"待增强"→"增强中"→"部分完成 x/y"（超出单轮预算时，下一次扫描继续）→"已完成 x/y"。完成后的重渲染结束时，打开 Handbook：`references/overview.md` 有"系统总览（LLM 生成）"，`references/index.md` 有"执行阶段"表，任一 `references/stages/<id>.md` 有阶段说明和文件卡片。通过 `GET /api/v1/repos/{id}/handbook` 检查 `llm`：`fallback` 应为 `false`（若为 `true` 看 `fallback_reason`，`bad_reply` 说明模型输出不可用）、`tokens_in/out` 非零、`stats.llm_rev == llm.rev`。
4. 修改该仓库一个卡片资格文件（非测试源码）并提交。下一次扫描后：确定性重建先发布（该文件在阶段页标"已过期"），随后 LLM 运行只为该文件生成卡片——`cards_new = 1`（若有其他文件与新内容完全相同则为相同内容的文件数）、`skeleton_rebuilt = false`（未触发重建阈值时）；重渲染后对应阶段页条目更新、"已过期"标注消失。
