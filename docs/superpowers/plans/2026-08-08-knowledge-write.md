# Knowledge Write (draft → approve) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Local Knowledge 增加 `knowledge_write` / `knowledge_approve`（wiki 主路径 + units 可选），draft 不进默认召回，工具与 Portal UI 共用审批服务层。

**Architecture:** `DirWiki` 用 `*.draft.md` 落盘 draft；`LocalKnowledge` 注入可选 `UnitWriter` 后声明 `Capabilities.Write` 并注册写工具；Portal 实现 `UnitWriter` + HTTP `ListDrafts`/`Approve`，Web Agent Detail 列出 draft 并 Approve。权威规格：[`2026-08-08-knowledge-write-design.md`](../specs/2026-08-08-knowledge-write-design.md)。

**Tech Stack:** Go（framework `memory/hub/local` + portal Kratos HTTP）、React（`web/src` Agent Detail）、既有 `metadata.hub_status` / `ApplyHubStatusMeta`。

**Repos:** nested `framework/`、`portal/`、`web/`。**Do not commit unless asked.**

---

## File map

| Path | Responsibility |
|------|----------------|
| `framework/memory/hub/local/wiki_draft.go` | draft 路径映射、`HasSuffix(".draft.md")`、正式 id 规范化 |
| `framework/memory/hub/local/wiki_dir.go` | `WriteDraft` / `ApproveDraft` / `ListDrafts`；Search 跳过 draft；Read+includeDraft |
| `framework/memory/hub/local/wiki_draft_test.go` | wiki draft round-trip / escape / search skip |
| `framework/memory/hub/local/unit_writer.go` | `UnitWriter` 接口 + `UnitDraftMeta` |
| `framework/memory/hub/local/knowledge.go` | `Write` 能力、写工具 Describe/Call、read `include_draft` |
| `framework/memory/hub/local/knowledge_write_test.go` | 工具层单测（fake UnitWriter） |
| `portal/internal/chat/hub_unit_writer.go` | MemoryStore 适配 `UnitWriter` |
| `portal/internal/chat/hub_bootstrap.go` | 注入 UnitWriter；必要时重建 Knowledge |
| `portal/internal/chat/hub_knowledge_service.go` | `HubKnowledgeApprove` / `ListDrafts`（工具与 HTTP 共用） |
| `portal/internal/service/hub_knowledge.go` | ChatService 薄封装 |
| `portal/internal/server/memory_hub_knowledge.go` | HTTP handlers |
| `portal/internal/server/http.go` | 挂路由 |
| `web/src/api/client.ts` | `listKnowledgeDrafts` / `approveKnowledgeDraft` |
| `web/src/pages/AgentDetail.tsx` | draft 列表 + Approve（可勾 overwrite） |
| `portal/docs/memory-integration.md` | 文档小节 |

---

### Task 1: Wiki draft path helpers

**Files:**
- Create: `framework/memory/hub/local/wiki_draft.go`
- Create: `framework/memory/hub/local/wiki_draft_test.go`

- [ ] **Step 1: Write failing tests**

```go
package local_test

func TestWikiDraftPaths(t *testing.T) {
	got, err := local.CanonicalWikiID("docs/foo.draft.md")
	if err != nil || got != "docs/foo.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = local.CanonicalWikiID("docs/foo")
	if err != nil || got != "docs/foo.md" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := local.CanonicalWikiID("docs/foo.txt"); err == nil {
		t.Fatal("expected error for non-.md")
	}
	if got := local.DraftPathForWikiID("docs/foo.md"); got != "docs/foo.draft.md" {
		t.Fatalf("got %q", got)
	}
	if !local.IsWikiDraftFile("foo.draft.md") {
		t.Fatal("expected draft")
	}
	if local.IsWikiDraftFile("foo.md") {
		t.Fatal("not draft")
	}
}
```

- [ ] **Step 2: Run test — expect fail**

```bash
cd framework && go test ./memory/hub/local/ -run TestWikiDraftPaths -count=1
```

Expected: undefined symbols.

- [ ] **Step 3: Implement helpers**

```go
package local

import "strings"

func IsWikiDraftFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".draft.md")
}

// CanonicalWikiID maps *.draft.md → *.md; ensures .md for bare names.
// Returns error for empty, path escape (..), or non-.md formal extensions.
func CanonicalWikiID(id string) (string, error) { /* ... */ }

func DraftPathForWikiID(canonical string) string {
	// docs/foo.md → docs/foo.draft.md (caller passes already-canonical id)
}
```

规则锁死（规格 §3.1）：一期只允许正式扩展名 `.md`；非 `.md` 返回 error。

- [ ] **Step 4: Run tests — pass**

```bash
cd framework && go test ./memory/hub/local/ -run TestWikiDraftPaths -count=1
```

- [ ] **Step 5: Commit only if user asks**

---

### Task 2: DirWiki write / approve / list + search skip

**Files:**
- Modify: `framework/memory/hub/local/wiki_dir.go`
- Modify: `framework/memory/hub/local/wiki_codegraph_test.go`（或新 `wiki_draft_test.go` 扩写）
- Test: `framework/memory/hub/local/wiki_draft_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestDirWiki_WriteApprove_SearchSkipsDraft(t *testing.T) {
	dir := t.TempDir()
	w, _ := local.NewDirWiki(dir)
	_, err := w.WriteDraft(ctx, "note.md", "# secret-token-xyz\n")
	// Search("secret-token-xyz") → empty
	// ApproveDraft(..., overwrite=false) → note.md exists, draft gone
	// Search → 1 hit
}

func TestDirWiki_ApproveRequiresOverwrite(t *testing.T) {
	// write formal note.md, write draft, Approve without overwrite → error
}

func TestDirWiki_WriteRejectsEscape(t *testing.T) {
	_, err := w.WriteDraft(ctx, "../x.md", "no")
	// expect error
}
```

- [ ] **Step 2: Run — fail**

```bash
cd framework && go test ./memory/hub/local/ -run 'TestDirWiki_(Write|Approve)' -count=1
```

- [ ] **Step 3: Implement on `DirWiki`**

- `Search`: 在 `isWikiFile` 之后若 `IsWikiDraftFile(d.Name())` → skip
- `WriteDraft(ctx, id, content)`: canonicalize → draft path → `MkdirAll` + write；cap `wikiMaxFileBytes`；return canonical id
- `ApproveDraft(ctx, id, overwrite bool)`: read draft → if formal exists && !overwrite → error → write formal → `Remove` draft（**best-effort**：正式页已写成功则仍返回 `nil`/`active`；删 draft 失败只 `log`，与规格 §3.2 一致，勿因此 fail 整个 approve）
- `ListDrafts(ctx, limit)`: Walk `*.draft.md`，返回 `[]WikiDraftMeta`（见下）
- `ReadPreferDraft(ctx, id)`：**本 Task 实现**——若对应 `*.draft.md` 存在则读 draft，否则读正式页；Hit.ID 始终为 canonical formal id
- 既有 `Read`：读正式页（显式 `*.draft.md` id 的处理在 Task 3 `knowledge_read`）

```go
type WikiDraftMeta struct {
	ID        string // canonical formal id (*.md)
	Preview   string
	UpdatedAt string // RFC3339 or empty
}
```

Approve 顺序：先写正式文件；成功后再删 draft（删失败不回滚正式页）。

- [ ] **Step 4: Tests pass**

```bash
cd framework && go test ./memory/hub/local/ -count=1
```

---

### Task 3: UnitWriter interface + LocalKnowledge write tools

**Files:**
- Create: `framework/memory/hub/local/unit_writer.go`
- Modify: `framework/memory/hub/local/knowledge.go`
- Create: `framework/memory/hub/local/knowledge_write_test.go`

- [ ] **Step 1: Define interfaces（含 agent 作用域）**

```go
package local

type UnitDraftMeta struct {
	ID        string
	Title     string
	UpdatedAt string // RFC3339 or empty
	Preview   string
}

// UnitWriter is agent-scoped: every method takes agentID (from hub.Identity.AgentID / HTTP path).
type UnitWriter interface {
	WriteDraft(ctx context.Context, agentID, id, title, content string) (unitID string, err error)
	ApproveDraft(ctx context.Context, agentID, id string) error
	ListDrafts(ctx context.Context, agentID string, limit int) ([]UnitDraftMeta, error)
}
```

`LocalKnowledge.Call` 写 units 时传入 `id.AgentID`（空则错误）。HTTP 服务层传 path 的 `agent_id`。进程单例 `SetHubUnitWriter` 只注入**实现**，不绑定某个 agent。

扩展 `KnowledgeBackends`:

```go
UnitsWrite UnitWriter // optional
```

`WikiWriter`（推荐）：

```go
type WikiWriter interface {
	WikiSearcher
	WriteDraft(ctx context.Context, id, content string) (canonicalID string, err error)
	ApproveDraft(ctx context.Context, id string, overwrite bool) error
	ListDrafts(ctx context.Context, limit int) ([]WikiDraftMeta, error)
	Read(ctx context.Context, id string) (*KnowledgeHit, error)
	ReadPreferDraft(ctx context.Context, id string) (*KnowledgeHit, error)
}
```

`*DirWiki` 实现之。

- [ ] **Step 2: Failing tool tests** with fake UnitWriter + temp DirWiki

覆盖：
1. `Capabilities().Write == true` when Wiki or UnitsWrite non-nil
2. `DescribeTools` 含 `knowledge_write` / `knowledge_approve` 当 Write
3. `Call knowledge_write source=wiki` → draft on disk；search wiki 无命中；approve → 命中
4. `source=units` 无 UnitsWrite → 明确错误；有 fake 时 WriteDraft 收到正确 agentID
5. `knowledge_read`：`include_draft=true` 优先 draft；**显式 id=`foo.draft.md` 可读且 Hit.ID 为正式 `foo.md`**（规格 §3.3 / Q3）

- [ ] **Step 3: Implement Call branches**

`Capabilities`:

```go
write := k.backends.Wiki != nil || k.backends.UnitsWrite != nil
flags["knowledge_write"] = write
return hub.Capabilities{Write: write, Flags: flags}
```

`DescribeTools`: if Write，append write/approve schemas（字段对齐规格）。同时给既有 `knowledge_read` schema 增加可选 `include_draft`（boolean）。

`Call`:
- `knowledge_write`: switch source；wiki 调 `WikiWriter.WriteDraft`；units 调 `UnitsWrite.WriteDraft(ctx, id.AgentID, ...)`
- `knowledge_approve`: wiki `ApproveDraft`；units `ApproveDraft(ctx, id.AgentID, ...)`
- `knowledge_read`（wiki）：
  1. 若 `id` 以 `.draft.md` 结尾 → 读该 draft 文件，返回 Hit.ID=`CanonicalWikiID(id)`
  2. 否则若 `include_draft==true` → `ReadPreferDraft`
  3. 否则 → 正式 `Read`

实现 HTTP 时从 Catalog Resolve 得到 `*local.LocalKnowledge` 后，经新增导出方法取后端，例如：

```go
func (k *LocalKnowledge) WikiWriter() WikiWriter { /* type-assert backends.Wiki */ }
func (k *LocalKnowledge) UnitWriter() UnitWriter { return k.backends.UnitsWrite }
```

HTTP **不要**另开旁路磁盘 API。`source` 查询参数：空 = **wiki + units**（规格 §5.2）；`wiki` / `units` = 单源。

- [ ] **Step 4: Tests pass**

```bash
cd framework && go test ./memory/hub/local/ -run KnowledgeWrite -count=1
```

---

### Task 4: Portal UnitWriter + bootstrap wiring

**Files:**
- Create: `portal/internal/chat/hub_unit_writer.go`
- Create: `portal/internal/chat/hub_unit_writer_test.go`（可用 fake MemoryStore 若现成；否则 table-driven 对接口 mock）
- Modify: `portal/internal/chat/hub_bootstrap.go`
- Modify: `portal/internal/chat/hub_knowledge_tools.go`（通常无需改——已从 DescribeTools 注册；确认即可）

- [ ] **Step 1: Implement `memoryUnitWriter`**

依赖：进程内可拿到的 `memory.Store` / units backend（看 `chat` 包现有 `SetMemory*` / session store 接线）。

行为：
- `WriteDraft(ctx, agentID, ...)`: 空 `agentID` → error；`Remember` with `Metadata: ApplyHubStatusMeta(..., AssetDraft)` 且 scope 绑定该 agent；若 `id` 非空则按规格只允许更新 draft
- `ApproveDraft(ctx, agentID, id)`: load unit（校验归属 agent）→ require draft → `ApplyHubStatusMeta(..., AssetActive)`（删键）并 persist
- `ListDrafts(ctx, agentID, limit)`: 仅该 agent 的 `hub_status=draft`（若 store 无 metadata 过滤，一期可扫再过滤；测试注明局限）

若当前 Prefetch **尚未**过滤 `hub_status=draft`，本 Task **追加最小过滤**（规格验收 #4）：在 units Prefetch 路径用 `MapUnitToAssetStatus` + `LoadoutEligible`（或等价：跳过 `hub_status=draft`）。优先查 `portal/internal/data` / `framework/memory` 的 Prefetch 组装处；缺则补一处。

- [ ] **Step 2: Wire into `buildKnowledgeBackendsLocked`**

```go
if uw := hubUnitWriter; uw != nil {
    b.UnitsWrite = uw
}
```

提供 `SetHubUnitWriter(uw local.UnitWriter)` + reset in `ResetLocalMemoryHubForTest`（与 BindingStore 相同：设 writer → `hubReady=false`）。**单例只持有实现；agent 作用域每次调用传入。**

生产接线：`WireMemoryHubFromData` 或 `NewChatService` 在 MemoryStore 就绪后 `SetHubUnitWriter`。

**注意:** `memory_write_enabled==false` 时 UnitWriter 方法应返回清晰错误；wiki 仍可写（与规格「按 source 校验」一致）。实现若需读 agent 配置，可用 `agentID` 查 RuntimeTools，或在 Call 外层由 Portal 包装一层先检查再调。

**服务层边界（规格 §5.1）：** 一期 **不做** HTTP `HubKnowledgeWrite`。工具走 `LocalKnowledge.Call`；HTTP 只做 `ListDrafts` + `Approve`，与工具共用同一 `WikiWriter`/`UnitWriter` 后端即可。

- [ ] **Step 3: Unit / integration test**

```bash
cd portal && go test ./internal/chat/ -run 'UnitWriter|KnowledgeWrite|Wiki' -count=1
```

- [ ] **Step 4: Confirm tools register when wiki root set**

临时：`SATH_HUB_WIKI_ROOT` + `InitLocalMemoryHub` → resolved Knowledge `DescribeTools` 含 write。

---

### Task 5: Shared service + HTTP API

**Files:**
- Create: `portal/internal/chat/hub_knowledge_service.go`
- Create: `portal/internal/service/hub_knowledge.go`
- Create: `portal/internal/server/memory_hub_knowledge.go`
- Modify: `portal/internal/server/http.go`
- Test: `portal/internal/chat/hub_knowledge_service_test.go`

- [ ] **Step 1: Service API**

```go
type KnowledgeDraftItem struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	Preview   string `json:"preview,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func ListKnowledgeDrafts(ctx, rt, agentID, source string, limit int) ([]KnowledgeDraftItem, error)
func ApproveKnowledgeDraft(ctx, rt, agentID, source, id string, overwrite bool) error
```

实现：Resolve Knowledge backends → 直接调 `WikiWriter` / `UnitWriter`（**传入 agentID**）。工具路径走 `LocalKnowledge.Call`（内部同样调这些后端）；HTTP **禁止**旁路直写磁盘。

一期服务层只强制共用 **Approve + List**；Write 仅工具（无 HTTP write）。

鉴权：HTTP handler 经 `ChatService` → `agentUC.Get` / `GetForEdit`（与 `SetAgentHubAssetStatus` 相同）。

- [ ] **Step 2: Routes**（规格 §5.2）

```go
r.GET("/api/v1/agents/{agent_id}/hub/knowledge/drafts", AgentHubKnowledgeDraftsHandler(chat))
r.POST("/api/v1/agents/{agent_id}/hub/knowledge/approve", AgentHubKnowledgeApproveHandler(chat))
```

Body approve: `{ "source":"wiki"|"units", "id":"...", "overwrite": false }`

- [ ] **Step 3: Handler tests**（可选 httptest）或 service 单测：temp wiki root → WriteDraft via DirWiki → List → Approve → formal exists。

```bash
cd portal && go test ./internal/chat/ ./internal/server/ -run Knowledge -count=1
```

---

### Task 6: Web UI — draft list + Approve

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/AgentDetail.tsx`

- [ ] **Step 1: API client**

```ts
listKnowledgeDrafts: (agentId: string, source?: string) =>
  request<{ drafts: KnowledgeDraftItem[] }>(
    `/agents/${agentId}/hub/knowledge/drafts${source ? `?source=${source}` : ''}`
  ),
approveKnowledgeDraft: (agentId: string, body: { source: string; id: string; overwrite?: boolean }) =>
  request<{ ok: boolean }>(`/agents/${agentId}/hub/knowledge/approve`, {
    method: 'POST',
    body: JSON.stringify(body),
  }),
```

- [ ] **Step 2: Agent Detail 区块**

在现有 Hub /「确认激活」附近增加「Knowledge drafts」：
- load 时并行拉 `listKnowledgeDrafts`
- 每行：`source` + `id` + Approve 按钮
- wiki 且可能覆盖：checkbox `overwrite`（默认 false）
- 成功后刷新列表 + 短 message

保持现有 skill draft「确认激活」不变（那是 Governance `assets/status`）。

- [ ] **Step 3: 手动点验**（实现后）

浏览器打开 Agent Detail，造一条 `*.draft.md`，应出现在列表；Approve 后变正式页。

---

### Task 7: Docs + acceptance checklist

**Files:**
- Modify: `portal/docs/memory-integration.md`
- Optional: 链到本计划 / 规格

- [x] **Step 1: 文档小节**

说明：
- 工具 `knowledge_write` / `knowledge_approve`
- `*.draft.md` 约定与 search 跳过
- env `SATH_HUB_WIKI_ROOT`；units 需 `memory_write_enabled`
- HTTP drafts / approve
- 自批语义（draft 不进召回 ≠ 双人审）

- [x] **Step 2: 验收对照规格 §8**

| # | Check | How |
|---|-------|-----|
| 1 | write wiki → `*.draft.md`；search 默认无 | `go test` Task2 |
| 2 | approve → formal；search 命中 | 同上 |
| 3 | overwrite 门控 | Task2 |
| 4 | units draft 过滤 | Task4 + test |
| 5 | UI 与工具同一结果 | Task5/6 手工或 API |
| 6 | `../` 拒绝 | Task2 |

- [x] **Step 3: 全量相关测试**

```bash
cd framework && go test ./memory/hub/... -count=1
cd portal && go test ./internal/chat/ -count=1
```

---

## Out of scope（本计划不做）

- Confluence / 飞书 / 外部 Hub
- CodeGraph 写入
- UI Wiki 编辑器 / write HTTP
- 强制 Governance Asset 登记
- 禁止 Agent 自批

## Scope note

Wiki 与 units 可分 PR，但同一计划顺序执行：Task1–3（framework）可先合；Task4–6（portal/web）依赖 framework `replace`。若只要最小可用：完成 Task1–3 + Task5 的 wiki-only Approve/List（UnitWriter=nil）即可演示主路径。
