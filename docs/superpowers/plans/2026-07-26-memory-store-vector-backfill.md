# MemoryStore P2-E2.1 Vector Backfill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 共享 `UnitBackfiller` + `UnitVectorIndex.Has`，支持启动增量补缺与 CLI `--force`/`--dry-run`，并把 Facade 熔断改为可注入共享指针。

**Architecture:** framework 扩展 `Has`、实现 `UnitBackfiller`（经 `SessionUnitsBackend.List`，禁止 Facade）、`FacadeConfig.EmbedTripped *atomic.Bool`。Portal 启动单例 job + `cmd/backfill-vectors` CLI，注入同一 Index/Embedder/breaker。

**Tech Stack:** Go；既有 SQLite sidecar / MySQL units；无新 migration。

**Spec:** `docs/superpowers/specs/2026-07-26-memory-store-vector-backfill-design.md`

**Repos 说明:** `framework/`、`portal/` 嵌套 git 分别 commit；本计划在 monorepo 根 worktree `feat/p2e-vector-sidecar`。Windows：

```powershell
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off
```

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/unit_vector.go` | `Has` 加入接口；`ErrVectorDimMismatch`；可选 `ErrEmbedModelUnavailable` |
| `framework/memory/unit_vector_test.go` | InMemory `Has` |
| `framework/memory/sqlite_unit_vector.go` | SQLite `Has`；dims 错误 wrap sentinel |
| `framework/memory/sqlite_unit_vector_test.go` | SQLite `Has` + dims |
| `framework/memory/unit_backfill.go` | `UnitBackfiller` / Stats / Run |
| `framework/memory/unit_backfill_test.go` | spec §4 #3–14 |
| `framework/memory/facade.go` | `EmbedTripped *atomic.Bool` 注入 |
| `framework/memory/facade_*_test.go` | 适配 pointer breaker；共享 trip 测 |
| `portal/internal/chat/memory_backfill.go` | 启动 job 单例 + 装配 Backfiller |
| `portal/internal/chat/memory_backfill_test.go` | 单例 / nil Index no-op |
| `portal/internal/chat/memory_backfill_cli.go` | CLI flag→config 纯函数 |
| `portal/internal/chat/memory_vector.go` | `dynamicUnitEmbedder`：模型不可用时 wrap `ErrEmbedModelUnavailable` |
| `portal/internal/chat/memory_store.go` / `memory_conflict.go` | 共享 breaker 注入 Facade |
| `portal/internal/service/chat.go`（`newChatService`） | `BuildMemoryStore` 后调用 `StartUnitVectorBackfill(sessionUnits)` |
| `portal/cmd/backfill-vectors/main.go` | CLI |
| `portal/docs/memory-integration.md` | 文档 |
| monorepo specs | E2.1 状态 / 交叉链接 |

**禁止:** Qdrant、分布式锁、前端 UI、keyset 分页、yaml 暴露限速。

---

### Task 1: UnitVectorIndex.Has + dims sentinel

**Files:**
- Modify: `framework/memory/unit_vector.go`
- Modify: `framework/memory/unit_vector_test.go`
- Modify: `framework/memory/sqlite_unit_vector.go`
- Modify: `framework/memory/sqlite_unit_vector_test.go`

- [ ] **Step 1: 写失败测试（InMemory Has）**

```go
func TestInMemoryUnitVectorIndex_Has(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1}})
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeUser, ScopeID: "u1", UnitID: "a", Vector: []float32{1}})

	got, err := idx.Has(ctx, ScopeSession, "s1", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !got["a"] || got["b"] {
		t.Fatalf("got %+v", got)
	}
	empty, err := idx.Has(ctx, ScopeSession, "s1", nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty ids: %+v err=%v", empty, err)
	}
}
```

- [ ] **Step 2: RED**

```powershell
cd framework/.worktrees/p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off -run TestInMemoryUnitVectorIndex_Has
```

Expected: compile fail（无 `Has`）

- [ ] **Step 3: 接口 + InMemory 实现**

`unit_vector.go` 接口加：

```go
Has(ctx context.Context, scope Scope, scopeID string, unitIDs []string) (map[string]bool, error)
```

并加：

```go
var ErrVectorDimMismatch = errors.New("memory: vector dimension mismatch")
```

InMemory：在持锁 map 上查 key；空 unitIDs → 空 map。

- [ ] **Step 4: SQLite Has + wrap dims**

`Has`：`WHERE scope_type=? AND scope_id=? AND unit_id IN (...)`；空 ids 早退。  
`validateDims` 失败时：`fmt.Errorf("%w: vector dim %d != index dim %d", ErrVectorDimMismatch, n, s.dims)`。

测试：Upsert 后 Has；跨 scope 隔离；重启后 Has；错误 dims Upsert → `errors.Is(err, ErrVectorDimMismatch)`。

- [ ] **Step 5: GREEN + commit**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run "Has|Dim"
git add memory/unit_vector.go memory/unit_vector_test.go memory/sqlite_unit_vector.go memory/sqlite_unit_vector_test.go
git commit -m "feat(memory): UnitVectorIndex.Has and dim mismatch sentinel"
```

---

### Task 2: Facade EmbedTripped 可注入

**Files:**
- Modify: `framework/memory/facade.go`
- Modify: 所有直接写 `f.embedTripped` 的测试（`facade_hybrid_test.go` / `facade_vector_test.go`）

- [ ] **Step 1: 写失败测试**

```go
func TestFacade_SharedEmbedTripped(t *testing.T) {
	tripped := &atomic.Bool{}
	emb := &fakeEmbedder{err: errors.New("no embed")}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(),
		UnitEmbedder: emb, EmbedTripped: tripped,
	})
	// seed one unit so Search path is meaningful; hybrid still embeds query
	_, _ = f.Remember(context.Background(), RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "x", AgentID: "a1",
	})
	// Remember may have already tripped via write-path embedOne — reset for read path proof:
	tripped.Store(false)
	emb.err = errors.New("no embed") // ensure next embed fails
	_, _ = f.Recall(context.Background(), RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits, Query: "q", AgentID: "a1",
	})
	if !tripped.Load() {
		t.Fatal("shared breaker not tripped by Facade")
	}
}
```

更稳妥：跳过 Remember，直接构造 Facade，手动 `idx.Upsert` 一条，再 Recall 触发 `embedQuery` trip。

- [ ] **Step 2: RED**（无 EmbedTripped 字段）

- [ ] **Step 3: 实现**

```go
type FacadeConfig struct {
	// ...
	EmbedTripped *atomic.Bool // nil → NewFacade 自建
}

type Facade struct {
	// ...
	embedTripped *atomic.Bool
}

func NewFacade(cfg FacadeConfig) *Facade {
	tripped := cfg.EmbedTripped
	if tripped == nil {
		tripped = &atomic.Bool{}
	}
	return &Facade{ ..., embedTripped: tripped }
}
```

所有 `f.embedTripped.Load/Store` 改为指针方法。既有测试 `f.embedTripped.Store(true)` 仍可用。  
顺手更新 `Facade` 上「Rebuild 会复位 breaker」类注释：改为「若注入共享指针则跨 Facade 实例保持；仅自建指针时随实例丢弃」。

测试文件需 `import "sync/atomic"`（若尚未导入）。

- [ ] **Step 4: 全量 memory 回归**

```powershell
go test ./memory -count=1 -p 1 -vet=off
```

- [ ] **Step 5: Commit**

```powershell
git add memory/facade.go memory/facade_*.go
git commit -m "feat(memory): injectable shared EmbedTripped breaker"
```

---

### Task 3: UnitBackfiller 核心

**Files:**
- Create: `framework/memory/unit_backfill.go`
- Create: `framework/memory/unit_backfill_test.go`
- Modify: `framework/memory/unit_vector.go`（`ErrEmbedModelUnavailable`）

- [ ] **Step 1: 写失败测试（补缺 + DryRun + Force）**

用 `SessionMemory` 作 Units。确认 `SessionMemory.Remember` 对 Metadata 的保留方式；测试写入：

```go
meta := map[string]any{
	"source_session_id": "s1", // session 路径：Remember 会用 ScopeID 覆盖写出；断言以 hit.Metadata 为准
	"agent_id":          "ag1",
}
// user scope 测试 MUST 显式设 Metadata["user_id"]——SessionMemory 不会自动补（MySQL 后端才会）
```

最小用例：

```go
func TestUnitBackfiller_FillMissing(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	emb := &countingKeyEmbedder{/* content → vec */}
	// seed 3 active units; pre-upsert only unit1 into idx
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSize: 50, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Upserted != 2 || emb.n != 2 {
		t.Fatalf("stats=%+v emb=%d err=%v", st, emb.n, err)
	}
}
```

再写：Force、DryRun（emb.n==0）、trip（`(stats,nil)` + Tripped + shared breaker）、`ErrEmbedModelUnavailable` → Skipped、缺 scopeID → Skipped、分页 BatchSize=2、多 scopeID 分组、Upsert Failed++、dims mismatch → `errors.Is(err, ErrVectorDimMismatch)`。

- [ ] **Step 2: RED**

- [ ] **Step 3: 实现 `unit_backfill.go`**

```text
defaults: BatchSize=50, BatchSleep=200ms, Scopes=[session,user]
tripped := cfg.EmbedTripped or new
for each scope:
  offset=0
  loop:
    page = Units.List({Scope, Status:active, Limit:BatchSize, Offset})
    if err → return stats, err
    if len==0 → break
    Scanned += len
    group hits by derived scopeID (session→source_session_id, user→user_id);
      missing → Skipped++
    for each group:
      present = Index.Has(...)
      if err → return stats, err
      for hit:
        if !Force && present[id] → continue
        Missing++
        if DryRun → continue
        if Content empty → Skipped++; continue
        if tripped.Load() → Tripped=true; return stats, nil
        vecs, err = Embedder.Embed(ctx, agentID, []content)
        if err != nil:
          if errors.Is(err, ErrEmbedModelUnavailable) → Skipped++; continue
          tripped.Store(true); Tripped=true; return stats, nil
        if empty vector → same as capability trip
        Upsert...
        if errors.Is(err, ErrVectorDimMismatch) → return stats, err
        if err != nil → Failed++; continue
        Upserted++
    offset += len(page)
    if len < BatchSize → break
    sleep BatchSleep (honor ctx.Done)
return stats, nil
```

**Embed 错误约定（代码注释 MUST）：**

| err | 行为 |
|-----|------|
| `nil` + 空向量 | trip |
| `errors.Is(ErrEmbedModelUnavailable)` | Skipped |
| 其他 | trip |

声明 `var ErrEmbedModelUnavailable = errors.New("memory: embed model unavailable")`。

- [ ] **Step 4: GREEN**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run UnitBackfill
go test ./memory -count=1 -p 1 -vet=off
```

- [ ] **Step 5: Commit**

```powershell
git add memory/unit_backfill.go memory/unit_backfill_test.go memory/unit_vector.go
git commit -m "feat(memory): UnitBackfiller for vector sidecar fill/rebuild"
```

---

### Task 4: Portal 共享 breaker + 启动 job

**Files:**
- Modify: `portal/internal/chat/memory_store.go`、`memory_conflict.go`（或 `memory_vector.go`）
- Modify: `portal/internal/chat/memory_vector.go`（`dynamicUnitEmbedder` 返回 sentinel）
- Create: `portal/internal/chat/memory_backfill.go`
- Create: `portal/internal/chat/memory_backfill_test.go`
- Modify: `portal/internal/service/chat.go` 的 `newChatService`（**不是** `cmd/backend/main.go`——`sessionUnits` 只在此作用域可得）：在 `BuildMemoryStore` + `SetMemoryAgentGetter` 之后调用 `StartUnitVectorBackfill(sessionUnits)`

- [ ] **Step 1: 共享 breaker + Embedder sentinel**

```go
var memoryEmbedTripped = &atomic.Bool{}
```

`BuildMemoryStore` → `FacadeConfig.EmbedTripped: memoryEmbedTripped`。  
更新 `Facade` 旁注释：共享指针后「Rebuild Prefetch Facade 不再复位 breaker」（符合 E2.1 §2.5）。

**MUST（对齐 spec §0.5）**：`dynamicUnitEmbedder.Embed` 在「解析不到可用模型」时返回：

```go
fmt.Errorf("%w: unit embed model unavailable", memory.ErrEmbedModelUnavailable)
```

至少覆盖：`resolveMemoryAuxModel` 失败、`m == nil`。这样 Backfiller `errors.Is(..., ErrEmbedModelUnavailable)` → Skipped 不 trip；在线 `embedOne` 仍对任意 err `Store(true)`（E1 行为不变）。

单测：stub/集成断言「模型不可用 → Backfill Skipped、shared breaker 仍 false」。

- [ ] **Step 2: 启动 job**

```go
func StartUnitVectorBackfill(units memory.SessionUnitsBackend) {
  // sync.Once; if index or embedder nil → return
  // go NewUnitBackfiller(... Force:false, EmbedTripped:memoryEmbedTripped, Units:units ...).Run
}
```

`Units` 必须是 MySQL `sessionUnitsBackend`（与 Facade.Session 同源），**禁止**传 Facade。注入点：`newChatService`。

单测：并发 Start 两次 → 内部 Run 启动计数 1；Index nil → no-op。

- [ ] **Step 3: 测试 + commit**

```powershell
cd portal/.worktrees/p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./internal/chat -count=1 -p 1 -vet=off -run "Backfill|EmbedModelUnavailable"
git add internal/chat internal/service/chat.go
git commit -m "feat(memory): start unit vector backfill job with shared breaker"
```

---

### Task 5: CLI `backfill-vectors`

**Files:**
- Create: `portal/cmd/backfill-vectors/main.go`
- Create: `portal/internal/chat/memory_backfill_cli.go`（flag→config 纯函数，可测）

- [ ] **Step 1: flag 映射测试**

```go
func TestParseBackfillFlags(t *testing.T) {
	cfg, err := parseBackfillArgs([]string{"--force", "--dry-run", "--scope", "session", "--batch", "10", "--sleep", "0s"})
	// Force, DryRun, Scopes==[session], BatchSize==10, BatchSleep==0
	cfgAll, err := parseBackfillArgs([]string{"--scope", "all"})
	// Scopes == [session, user]（或空切片表示默认，与 NewUnitBackfiller 默认一致——钉死一种）
}
```

- [ ] **Step 2: main 装配**

加载 conf（仿 backend 最小集：data_root、agent_extra、DB → units backend、vector index、embedder）。  
`Run`；打印 Stats JSON 或文本；`err != nil` → `os.Exit(1)`；`Tripped` → stderr warning + exit 0。

- [ ] **Step 3: commit**

```powershell
git add cmd/backfill-vectors internal/chat/memory_backfill_cli.go internal/chat/memory_backfill_cli_test.go
git commit -m "feat(memory): backfill-vectors CLI"
```

---

### Task 6: 文档回写

**Files:**
- `portal/docs/memory-integration.md`
- monorepo：`2026-07-26-memory-store-vector-backfill-design.md` 状态→已交付；E2 §7 / 门面 §8.3 / E1 §7 交叉链接

- [ ] **Step 1:** CLI 示例、启动 job、多副本勿 Force、换模型删 DB、Backlog 去掉存量 backfill  
- [ ] **Step 2:** 两仓分别 commit docs

---

## 依赖顺序

```text
Task 1 (Has) ─┐
Task 2 (breaker) ─┴→ Task 3 (Backfiller) → Task 4 (Portal job) → Task 5 (CLI) → Task 6 (docs)
Task 1 ∥ Task 2 可并行
```

## 验收清单

1. CLI 对老数据补缺后 hybrid 可语义召回  
2. 二次跑 Upserted=0  
3. `--force` 重算  
4. Embed 不可用 Tripped 退 0  
5. 启动不阻塞就绪  
