# MemoryStore P2-E2 Hybrid Recall Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `Facade.Recall(source=units)` 实现 LIKE∪向量的 RRF hybrid 召回；向量写入与 D2 解耦；Agent 级 `hybrid_recall` 读开关。

**Architecture:** framework 新增纯函数 RRF 融合 + 读路径 Embed helper（800ms 超时、超时不 trip）+ 进程内查询向量 LRU；`Facade.Recall` 串行编排；`syncUpsert` 去掉 `semanticEnabled` 门控并补 `rememberAdd` 早退路径。Portal 用 proto3 `optional bool hybrid_recall` 贯穿 biz/model，经 `FacadeConfig.HybridRecall` 回调注入。

**Tech Stack:** Go；既有 `UnitVectorIndex`/`UnitEmbedder`（E1）；protobuf optional；无新 MySQL migration。

**Spec:** `docs/superpowers/specs/2026-07-26-memory-store-hybrid-recall-design.md`

**Repos 说明:** `framework/`、`portal/` 为嵌套 git，分别 commit；本计划与规格在 monorepo 根 worktree `feat/p2e-vector-sidecar`。Windows PowerShell：`git commit -m "..."`。Go 测试分包跑避免 OOM：

```powershell
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off
```

Portal：

```powershell
$env:GOMAXPROCS='1'; go test ./internal/biz ./internal/chat ./internal/data -count=1 -p 1 -vet=off
```

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/rrf.go` | RRF 融合纯函数（k=60、去重、同分次序） |
| `framework/memory/rrf_test.go` | RRF 单测 |
| `framework/memory/query_embed_cache.go` | 进程内并发安全 LRU（容量 64） |
| `framework/memory/query_embed_cache_test.go` | 缓存命中 + `-race` |
| `framework/memory/facade.go` | 写解耦；`HybridRecall` 回调；`Recall` hybrid；读路径 `embedQuery` |
| `framework/memory/facade_hybrid_test.go` | spec §4 Framework #1–13 |
| `framework/memory/store_prefetch_backend.go` | user/session Recall 补传 `AgentID` |
| `framework/memory/store_prefetch_backend_test.go` | 断言 AgentID 透传 |
| `portal/api/agent/v1/agent.proto` | `optional bool hybrid_recall = 9` |
| `portal/internal/biz/runtime_tools.go` | `HybridRecall *bool` + From/ToProto presence |
| `portal/internal/biz/runtime_tools_test.go` | unset/true/false round-trip |
| `portal/internal/data/model/runtime_tools.go` | JSON `*bool` omitempty |
| `portal/internal/data/agent_mysql.go` | biz↔model 映射带 `HybridRecall` |
| `portal/internal/chat/memory_store.go` | `HybridRecall` 透传 Facade |
| `portal/internal/chat/memory_conflict.go` 或新建 `memory_hybrid.go` | 注入 gate 回调 |
| `portal/internal/chat/memory_hybrid_test.go` | gate：nil/false/查不到 → true/false/true |
| `portal/docs/memory-integration.md` | E2 说明 |
| monorepo specs | E2 状态；门面 §8.3；E1 §7 回写 |

**禁止本迭代:** backfill、Qdrant、前端面板、可配置 RRF-k/超时、`agent_extra` 全局 `hybrid_recall`、改 D2 裁决。

---

### Task 1: 写路径解耦（syncUpsert 去 D2 门控 + rememberAdd 早退）

**Files:**
- Modify: `framework/memory/facade.go`（`syncUpsert`、`rememberAdd`）
- Modify: `framework/memory/facade_vector_test.go`（或新建断言）
- Test: `framework/memory/facade_hybrid_test.go`（本 task 先写 #8）

- [ ] **Step 1: 写失败测试（D2 关仍 Upsert）**

在 `framework/memory/facade_hybrid_test.go`：

```go
package memory

import (
	"context"
	"testing"
)

func TestFacade_Add_D2Off_StillUpsertsVector(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	emb := &countingEmbedder{vec: []float32{1, 0}}
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session:              sess,
		UnitVectors:          idx,
		UnitEmbedder:         emb,
		ToolSemanticConflict: false, // D2 off
	})
	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "prefers dark mode",
	})
	if err != nil || hit.ID == "" {
		t.Fatalf("Remember = %+v err=%v", hit, err)
	}
	if emb.n != 1 {
		t.Fatalf("embed calls = %d, want 1 (write decoupled)", emb.n)
	}
	hits, err := idx.Search(ctx, UnitVectorQuery{
		Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5,
	})
	if err != nil || len(hits) != 1 || hits[0].UnitID != hit.ID {
		t.Fatalf("Search = %+v err=%v", hits, err)
	}
}

// countingEmbedder / reuse stubs from facade_vector_test.go if already exported;
// otherwise duplicate a minimal local stub in this file.
```

若 `countingEmbedder` 已在 `facade_vector_test.go` 且同 package，直接复用。

- [ ] **Step 2: 跑测试确认失败**

```powershell
cd framework/.worktrees/p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off -run TestFacade_Add_D2Off_StillUpsertsVector
```

Expected: FAIL（当前 `syncUpsert` 因 `!semanticEnabled` 直接 return，且 `rememberAdd` 早退不调用 sync）

- [ ] **Step 3: 最小实现**

1. `syncUpsert`：把门控改为 `unitID == "" || !f.vectorReady()`（去掉 `semanticEnabled`）；更新注释，删除「d2 == false → 禁止 Embed」措辞，注明被 E2 取代。  
2. `rememberAdd` 早退分支改为：

```go
if !f.semanticEnabled(in) || f.semanticConflicts == nil {
	hit, err := f.session.Remember(ctx, in)
	if err != nil {
		return MemoryHit{}, err
	}
	f.syncUpsert(ctx, in, hit.ID)
	return hit, nil
}
```

勿改 D2 peer / `syncUpsertVec` / Delete 门控。

- [ ] **Step 4: 跑测试确认通过 + 回归 E1 向量测**

```powershell
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off -run "TestFacade_Add_D2Off_StillUpsertsVector|TestFacade_"
```

Expected: PASS（注意：E1 里「D2 关零 Upsert」类测试若存在，须按 E2 改写断言为「仍 Upsert」——搜 `D2Off` / `SkipsUpsert` / `zero` 并更新）

- [ ] **Step 5: Commit（framework）**

```powershell
cd framework/.worktrees/p2e-vector-sidecar
git add memory/facade.go memory/facade_hybrid_test.go memory/facade_vector_test.go
git commit -m "feat(memory): decouple unit vector upsert from D2 gate"
```

---

### Task 2: RRF 纯函数

**Files:**
- Create: `framework/memory/rrf.go`
- Create: `framework/memory/rrf_test.go`

- [ ] **Step 1: 写失败测试**

```go
package memory

import "testing"

func TestRRFMerge_DedupSumsRanks(t *testing.T) {
	like := []MemoryHit{
		{ID: "a", Content: "a", Score: 0},
		{ID: "b", Content: "b", Score: 0},
	}
	vec := []MemoryHit{
		{ID: "b", Content: "b", Score: 0.9},
		{ID: "c", Content: "c", Score: 0.8},
	}
	out := rrfMerge(like, vec, 3)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	// b gets 1/(60+2)+1/(60+1) > a gets 1/(60+1) > c gets 1/(60+2)
	if out[0].ID != "b" {
		t.Fatalf("want b first, got %+v", out)
	}
	wantB := 1.0/62 + 1.0/61
	if out[0].Score < wantB-1e-9 || out[0].Score > wantB+1e-9 {
		t.Fatalf("b score=%v want %v", out[0].Score, wantB)
	}
}

func TestRRFMerge_TieBreakLikeThenVectorThenID(t *testing.T) {
	// two singles with same single-list rank-1 score
	like := []MemoryHit{{ID: "z"}, {ID: "a"}}
	vec := []MemoryHit{{ID: "y"}}
	out := rrfMerge(like, vec, 3)
	// all score 1/61; order: like order for z,a then y
	if out[0].ID != "z" || out[1].ID != "a" || out[2].ID != "y" {
		t.Fatalf("tie order = %+v", out)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run TestRRFMerge
```

Expected: FAIL（undefined `rrfMerge`）

- [ ] **Step 3: 实现**

`framework/memory/rrf.go`：

```go
package memory

import "sort"

const rrfK = 60

// rrfMerge fuses two ranked lists. Rank is 1-based index in each input slice
// (caller must pass post-hydrate vector hits). Ties: like order → vector order → unit_id.
func rrfMerge(like, vector []MemoryHit, limit int) []MemoryHit {
	if limit <= 0 {
		return nil
	}
	type acc struct {
		hit       MemoryHit
		score     float64
		likeRank  int // 0 = absent
		vecRank   int
	}
	m := map[string]*acc{}
	order := make([]string, 0)
	add := func(list []MemoryHit, isLike bool) {
		for i, h := range list {
			if h.ID == "" {
				continue
			}
			a, ok := m[h.ID]
			if !ok {
				cp := h
				a = &acc{hit: cp}
				m[h.ID] = a
				order = append(order, h.ID)
			}
			rank := i + 1
			a.score += 1.0 / float64(rrfK+rank)
			if isLike {
				if a.likeRank == 0 {
					a.likeRank = rank
					a.hit.Content = h.Content
					a.hit.Metadata = h.Metadata
					a.hit.Scope = h.Scope
					a.hit.Source = h.Source
				}
			} else if a.vecRank == 0 {
				a.vecRank = rank
				if a.likeRank == 0 {
					a.hit = h
				}
			}
		}
	}
	add(like, true)
	add(vector, false)
	ids := append([]string{}, order...)
	sort.SliceStable(ids, func(i, j int) bool {
		ai, aj := m[ids[i]], m[ids[j]]
		if ai.score != aj.score {
			return ai.score > aj.score
		}
		// like present first (lower likeRank wins; 0 = absent → after)
		li, lj := ai.likeRank, aj.likeRank
		if (li == 0) != (lj == 0) {
			return li != 0
		}
		if li != 0 && li != lj {
			return li < lj
		}
		vi, vj := ai.vecRank, aj.vecRank
		if (vi == 0) != (vj == 0) {
			return vi != 0
		}
		if vi != 0 && vi != vj {
			return vi < vj
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]MemoryHit, 0, len(ids))
	for _, id := range ids {
		h := m[id].hit
		h.Score = m[id].score
		out = append(out, h)
	}
	return out
}
```

- [ ] **Step 4: 跑测试确认通过**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run TestRRFMerge
```

- [ ] **Step 5: Commit**

```powershell
git add memory/rrf.go memory/rrf_test.go
git commit -m "feat(memory): add RRF merge helper for hybrid recall"
```

---

### Task 3: 查询向量 LRU 缓存

**Files:**
- Create: `framework/memory/query_embed_cache.go`
- Create: `framework/memory/query_embed_cache_test.go`

- [ ] **Step 1: 写失败测试**

```go
package memory

import (
	"sync"
	"testing"
)

func TestQueryEmbedCache_HitAndEvict(t *testing.T) {
	c := newQueryEmbedCache(2)
	c.put("a\x00q", []float32{1})
	c.put("b\x00q", []float32{2})
	if got := c.get("a\x00q"); got == nil {
		t.Fatal("expected hit for a")
	}
	c.put("c\x00q", []float32{3}) // evict LRU (b if a was touched)
	if c.get("b\x00q") != nil {
		t.Fatal("expected b evicted")
	}
}

func TestQueryEmbedCache_Concurrent(t *testing.T) {
	c := newQueryEmbedCache(64)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "a\x00q"
			c.put(key, []float32{float32(i)})
			_ = c.get(key)
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: 跑确认失败** → Expected: undefined

- [ ] **Step 3: 实现**（`sync.Mutex` + 简单 map+list 或 slice LRU；容量默认 64；不引第三方）

- [ ] **Step 4: 跑测试（含 race）**

```powershell
go test ./memory -count=1 -p 1 -vet=off -race -run TestQueryEmbedCache
```

- [ ] **Step 5: Commit**

```powershell
git add memory/query_embed_cache.go memory/query_embed_cache_test.go
git commit -m "feat(memory): add concurrent query embed LRU cache"
```

---

### Task 4: Facade.Recall hybrid 编排

**Files:**
- Modify: `framework/memory/facade.go`（`FacadeConfig`/`Facade`/`NewFacade`/`Recall`；新增 `embedQuery`/`hybridReadable`/`hybridUnitsRecall`）
- Create/Modify: `framework/memory/facade_hybrid_test.go`（spec #1–7, #9, #11–13）

- [ ] **Step 1: 写失败测试（核心路径）**

```go
func TestFacade_HybridRecall_FindsSemanticNeighbor(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	emb := &fixedEmbedder{byText: map[string][]float32{
		"dark theme preference": {1, 0},
		"UI uses dark mode":     {0.99, 0.01},
		"unrelated meeting":     {0, 1},
	}}
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session: sess, UnitVectors: idx, UnitEmbedder: emb,
		ToolSemanticConflict: false,
	})
	// seed via Remember so write-path upserts
	h1, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "UI uses dark mode", AgentID: "a1"})
	_, _ = f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "unrelated meeting", AgentID: "a1"})
	if h1.ID == "" {
		t.Fatal("seed failed")
	}
	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "dark theme preference", Limit: 5, AgentID: "a1",
		MinScore: 0.9, // MUST be ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.ID == h1.ID {
			found = true
			if h.Score <= 0 {
				t.Fatalf("want RRF score > 0, got %v", h.Score)
			}
		}
	}
	if !found {
		t.Fatalf("semantic neighbor missing: %+v", hits)
	}
}

func TestFacade_HybridRecall_GateFalse_SkipsEmbed(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1, 0}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(),
		UnitEmbedder: emb,
		HybridRecall: func(context.Context, string) bool { return false },
	})
	_, _ = f.Remember(context.Background(), RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "x", AgentID: "a1",
	})
	emb.n = 0
	_, err := f.Recall(context.Background(), RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits, Query: "x", AgentID: "a1",
	})
	if err != nil || emb.n != 0 {
		t.Fatalf("embed calls=%d err=%v, want 0", emb.n, err)
	}
}
```

再补：Embed error → trip；超时 mock → 不 trip；空 query 不 Embed；fail-open 截断 `len<=limit`；LIKE-only 无向量 unit 仍返回；缓存二次 Recall Embed=1；RRF 去重。

超时 stub 示例：`Embed` 里 `<-ctx.Done(); return nil, ctx.Err()`，且 Facade 必须用 800ms 子 context。

- [ ] **Step 2: 跑确认失败**

- [ ] **Step 3: 实现**

`FacadeConfig` / `Facade` 增加：

```go
HybridRecall func(ctx context.Context, agentID string) bool
```

`NewFacade` 保存字段；Facade 持有 `queryCache *queryEmbedCache`（`newQueryEmbedCache(64)`）。

`Recall` 的 `SourceUnits` 分支：

```go
case SourceUnits:
	if f.session == nil {
		return []MemoryHit{}, nil
	}
	if !f.hybridReadable(ctx, q) {
		return f.session.Recall(ctx, q) // 原样 Limit
	}
	return f.hybridUnitsRecall(ctx, q)
```

`hybridReadable`：`vectorReady() && TrimSpace(q.Query)!="" && hybridAllowed(ctx,q.AgentID)`。

`hybridAllowed`：`HybridRecall==nil` → true；否则调回调。

`hybridUnitsRecall` 伪代码：

```text
effLimit = q.Limit; if <=0 { effLimit = 5 }
N = 2 * effLimit
likeQ = q; likeQ.Limit = N
likeHits, err := session.Recall(ctx, likeQ); if err != nil { return nil, err }
vecHits := tryVectorBranch(ctx, q, N) // embedQuery + Search + hydrate; on any fail return nil slice
if vecHits == nil {
  return truncate(likeHits, effLimit), nil
}
return rrfMerge(likeHits, vecHits, effLimit), nil
```

`embedQuery`（读路径专用，**不**改写路径 `embedOne`）：

```text
key = agentID + "\x00" + query
if cached := f.queryCache.get(key); cached != nil { return cached, nil }
ctx2, cancel := context.WithTimeout(ctx, 800*time.Millisecond); defer cancel()
vecs, err := f.unitEmbedder.Embed(ctx2, agentID, []string{query})
if err != nil {
  if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
    return nil, err // caller fail-open; DO NOT trip
  }
  f.embedTripped.Store(true)
  return nil, err
}
// validate non-empty; cache put; return
```

hydrate 复用 `vectorPeers` 内循环逻辑（可抽 `hydrateActive(scope, scopeID, hits []UnitVectorHit) []MemoryHit`）。

**忽略 `q.MinScore`**——不要用它过滤 RRF 结果。

- [ ] **Step 4: 全量 hybrid + 向量回归**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run "TestFacade_Hybrid|TestFacade_Vector|TestRRF|TestQueryEmbed"
go test ./memory -count=1 -p 1 -vet=off -race -run "TestFacade_HybridRecall_Cache|TestQueryEmbedCache_Concurrent"
```

- [ ] **Step 5: Commit**

```powershell
git add memory/facade.go memory/facade_hybrid_test.go
git commit -m "feat(memory): hybrid RRF recall for source=units"
```

---

### Task 5: Prefetch 补传 AgentID

**Files:**
- Modify: `framework/memory/store_prefetch_backend.go`
- Modify: `framework/memory/store_prefetch_backend_test.go`

- [ ] **Step 1: 扩展既有测试断言**

在 Prefetch 测试里（已有 `store.calls` 记录），对 user/session 两路断言：

```go
if store.calls[0].AgentID != "agent-1" { // 使用 PrefetchQuery 里设的 AgentID
	t.Fatalf("user Recall AgentID = %q", store.calls[0].AgentID)
}
```

session 路同理。

- [ ] **Step 2: 跑确认失败**

- [ ] **Step 3: 实现**

user / session `RecallQuery` 增加：

```go
AgentID: strings.TrimSpace(q.AgentID),
```

- [ ] **Step 4: 跑通过**

```powershell
go test ./memory -count=1 -p 1 -vet=off -run Prefetch
```

- [ ] **Step 5: Commit**

```powershell
git add memory/store_prefetch_backend.go memory/store_prefetch_backend_test.go
git commit -m "fix(memory): pass AgentID on prefetch units recall"
```

---

### Task 6: Agent `hybrid_recall` 三态（proto → biz → model）

**Files:**
- Modify: `portal/api/agent/v1/agent.proto`
- Regenerate: `make api` 或项目既有 proto 生成命令（查 `portal/Makefile` / README；Windows 用同等命令）
- Modify: `portal/internal/biz/runtime_tools.go`
- Modify: `portal/internal/biz/runtime_tools_test.go`
- Modify: `portal/internal/data/model/runtime_tools.go`
- Modify: `portal/internal/data/agent_mysql.go`（`bizRuntimeToolsToModel` / `modelRuntimeToolsToBiz`）

- [ ] **Step 1: proto 加字段**

```protobuf
message RuntimeToolsConfig {
  // ... fields 1–8 unchanged ...
  optional bool hybrid_recall = 9; // unset = on for memory hybrid recall
}
```

- [ ] **Step 2: 生成代码**

按 portal 仓库惯例生成（常见：`make api` 或 `buf generate`）。确认生成物含 `HasHybridRecall()` / `GetHybridRecall()`。

- [ ] **Step 3: 写失败测试**

```go
func TestRuntimeTools_HybridRecallTriState(t *testing.T) {
	// unset
	bizCfg := RuntimeToolsFromProto(&agentv1.RuntimeToolsConfig{})
	if bizCfg.HybridRecall != nil {
		t.Fatalf("unset want nil, got %v", *bizCfg.HybridRecall)
	}
	out := RuntimeToolsToProto(bizCfg)
	if out.HybridRecall != nil { // or !out.HasHybridRecall()
		t.Fatal("ToProto must keep unset")
	}
	// explicit false
	f := false
	in := &agentv1.RuntimeToolsConfig{}
	in.HybridRecall = &f // or proto setter
	bizCfg = RuntimeToolsFromProto(in)
	if bizCfg.HybridRecall == nil || *bizCfg.HybridRecall {
		t.Fatal("want false")
	}
	// explicit true
	tr := true
	in.HybridRecall = &tr
	bizCfg = RuntimeToolsFromProto(in)
	if bizCfg.HybridRecall == nil || !*bizCfg.HybridRecall {
		t.Fatal("want true")
	}
}
```

按实际生成 API 调整赋值方式（`proto` 可能用指针字段）。

- [ ] **Step 4: 实现 biz/model/mapping**

```go
// biz
HybridRecall *bool `json:"hybrid_recall,omitempty"`

// FromProto
if p.HasHybridRecall() {
	v := p.GetHybridRecall()
	cfg.HybridRecall = &v
}
// ToProto
if c.HybridRecall != nil {
	out.HybridRecall = c.HybridRecall // keep pointer / SetHybridRecall
}
```

model 同构 `*bool`；`bizRuntimeToolsToModel` / `modelRuntimeToolsToBiz` 拷贝指针（注意勿共享可变状态时可拷贝值再取址）。

**Update 语义：** 若 service 层整包替换 `runtime_tools`，保持现状即可（整包 FromProto 已带 presence）。若存在字段级 merge，须「未携带则保留」——检查 `agent.go` Update；本切片若整包替换则在测试注明。

- [ ] **Step 5: 跑测试**

```powershell
cd portal/.worktrees/p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./internal/biz -count=1 -p 1 -vet=off -run HybridRecall
```

- [ ] **Step 6: Commit（portal）**

```powershell
git add api/agent/v1 internal/biz/runtime_tools.go internal/biz/runtime_tools_test.go internal/data/model/runtime_tools.go internal/data/agent_mysql.go
git commit -m "feat(agent): optional hybrid_recall on runtime_tools"
```

---

### Task 7: Portal 注入 HybridRecall 回调

**Files:**
- Modify: `portal/internal/chat/memory_store.go`
- Create: `portal/internal/chat/memory_hybrid.go`
- Create: `portal/internal/chat/memory_hybrid_test.go`
- Modify: `portal/internal/chat/memory_conflict.go`（`DefaultMemoryStoreOptions` 注入 gate）

- [ ] **Step 1: 写失败测试**

```go
func TestHybridRecallGate(t *testing.T) {
	falseVal := false
	agents := &fakeAgentGetter{byID: map[string]*biz.AgentMeta{
		"off": {ID: "off", RuntimeTools: biz.RuntimeToolsConfig{HybridRecall: &falseVal}},
		"on":  {ID: "on", RuntimeTools: biz.RuntimeToolsConfig{}}, // nil → on
	}}
	gate := hybridRecallGate(agents)
	if !gate(context.Background(), "") { t.Fatal("empty agentID → true") }
	if !gate(context.Background(), "missing") { t.Fatal("missing → true") }
	if !gate(context.Background(), "on") { t.Fatal("nil field → true") }
	if gate(context.Background(), "off") { t.Fatal("false → false") }
}
```

复用既有 `AgentGetter` fake（参考 `memory_conflict_test.go`）。

- [ ] **Step 2: 实现**

```go
func hybridRecallGate(agents AgentGetter) func(context.Context, string) bool {
	return func(ctx context.Context, agentID string) bool {
		if agents == nil || strings.TrimSpace(agentID) == "" {
			return true
		}
		meta, err := agents.Get(ctx, agentID)
		if err != nil || meta == nil {
			return true
		}
		if meta.RuntimeTools.HybridRecall == nil {
			return true
		}
		return *meta.RuntimeTools.HybridRecall
	}
}
```

`MemoryStoreOptions` 增加 `HybridRecall func(context.Context, string) bool`；`BuildMemoryStore` 传入 `FacadeConfig`；`DefaultMemoryStoreOptions` 在 `AgentGetter` 可用时注入 `hybridRecallGate(globalMemoryAgentGetter)`。

- [ ] **Step 3: 跑测试**

```powershell
go test ./internal/chat -count=1 -p 1 -vet=off -run HybridRecall
```

- [ ] **Step 4: Commit**

```powershell
git add internal/chat/memory_store.go internal/chat/memory_hybrid.go internal/chat/memory_hybrid_test.go internal/chat/memory_conflict.go
git commit -m "feat(memory): wire agent hybrid_recall gate into Facade"
```

---

### Task 8: 文档回写

**Files:**
- Modify: `portal/docs/memory-integration.md`
- Modify: monorepo `.worktrees/p2e-vector-sidecar/docs/superpowers/specs/2026-07-26-memory-store-hybrid-recall-design.md`（状态 → 已交付 / 实现中）
- Modify: `docs/superpowers/specs/2026-07-27-memory-store-vector-sidecar-design.md` §7
- Modify: `docs/superpowers/specs/2026-07-25-memory-store-facade-design.md` §8.3

- [ ] **Step 1: 更新 memory-integration.md**

新增 E2 小节：hybrid RRF、写解耦、`runtime_tools.hybrid_recall`（unset=开）、MinScore 对 units 无效、Prefetch 带 AgentID。Backlog 去掉 E2，保留 E3/backfill/前端。

- [ ] **Step 2: 回写规格状态**

E2 规格状态改为「实现中」或实现完成后「已交付」；E1 §7 指向本规格并注明「D2 关零 Embed」已被 E2 取代；门面 §8.3 更新 E2 状态。

- [ ] **Step 3: Commit**

```powershell
# portal docs
cd portal/.worktrees/p2e-vector-sidecar
git add docs/memory-integration.md
git commit -m "docs(memory): document P2-E2 hybrid recall"

# monorepo specs
cd ../../../.worktrees/p2e-vector-sidecar   # adjust to root worktree
git add docs/superpowers/specs
git commit -m "docs(memory): P2-E2 status and cross-links"
```

---

## 验收清单（手工）

1. Agent 默认：措辞不同的相关事实可被 `memory_recall` / Prefetch 召回。  
2. Agent `hybrid_recall=false`：同数据仅 LIKE。  
3. D2 关、hybrid 开：新写入有向量且 Recall 可命中。  
4. Embed 不可用：纯 LIKE，无风暴。  
5. 无新 MySQL migration。

---

## 执行顺序依赖

```text
Task 1 (写解耦) ─┐
Task 2 (RRF)     ─┼─→ Task 4 (Facade.Recall hybrid) → Task 5 (Prefetch AgentID)
Task 3 (LRU)     ─┘
Task 6 (proto 三态) → Task 7 (Portal gate) → Task 8 (docs)
```

Task 1–3 可并行；Task 6 与 Task 1–5 可并行（跨 repo）。
