# MemoryStore P2-E1 可插拔向量 Sidecar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 引入可插拔 `UnitVectorIndex`（首个 provider 为 SQLite），把 P2-D2 的 peer 发现从 `LIKE` 子串升级为向量近邻，Embed 不可用时熔断回退 LIKE。

**Architecture:** framework 新增稳定接口 `UnitVectorIndex` + `UnitEmbedder`；Facade 仅在 D2 语义门对本次调用启用时做 Embed/Upsert，Delete 只受 `UnitVectors != nil` 门控；hydrate 阶段回主表过滤 active，保证 sidecar 漏删不影响正确性。Portal 按调用动态解析 Embed 模型（aux → Agent chat model），与 `dynamicSemanticConflictResolver` 同构。

**Tech Stack:** Go 1.26；`modernc.org/sqlite`（framework 已有依赖）；无新 MySQL 迁移。

**Spec:** `docs/superpowers/specs/2026-07-27-memory-store-vector-sidecar-design.md`

**Repos 说明:** `framework/`、`portal/` 为嵌套 git，分别 commit；本计划与规格在 monorepo 根。Windows PowerShell：`git commit -m "..."`。Go 测试**分包跑**避免 OOM：`$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off`。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/unit_vector.go` | `UnitVectorIndex`/`UnitVectorRecord`/`UnitVectorQuery`/`UnitVectorHit`/`UnitEmbedder`；`InMemoryUnitVectorIndex` |
| `framework/memory/unit_vector_test.go` | 内存实现：CRUD、scope 隔离、并发 |
| `framework/memory/sqlite_unit_vector.go` | `SQLiteUnitVectorIndex`（schema、BLOB 编解码、dims 基准、余弦 Search） |
| `framework/memory/sqlite_unit_vector_test.go` | 持久化、重启 dims 基准、维度冲突、Close |
| `framework/memory/facade.go` | `UnitVectors`/`UnitEmbedder` 字段；peer 发现向量化；熔断；写同步 |
| `framework/memory/facade_vector_test.go` | 向量 peer、熔断、D2 关闭零副作用、写同步 |
| `framework/config/tool_guardrails.go` | `MemoryVector` 结构 + 挂到 `PortalAgentExtra` |
| `framework/config/config_test.go` | `memory_vector` YAML 解析 |
| `portal/internal/chat/memory_vector.go`（新建） | `dynamicUnitEmbedder`、sqlite 单例、开关解析 |
| `portal/internal/chat/memory_vector_test.go` | provider=none、路径、单例缓存、embedder 可用性 |
| `portal/internal/chat/memory_vector_testmain_test.go`（新建） | 测试二进制默认 `provider=none`，避免污染既有测试 |
| `portal/internal/chat/memory_store.go` | `MemoryStoreOptions` 增加向量字段并透传 Facade |
| `portal/internal/chat/memory_conflict.go` | `DefaultMemoryStoreOptions` 注入向量 |
| `portal/internal/chat/portal_agent_extra.go` | 装载 `memory_vector` |
| `portal/cmd/backend/main.go` | 启动时把 `data_root` 交给 chat 包 |
| `portal/configs/agent_extra.yaml` | 注释示例 |
| `portal/docs/memory-integration.md` | P2-E1 小节 |
| monorepo specs | 规格状态 → 实现中；门面 §8.3 / D2 回写 |

**禁止本迭代:** 改通用 `Recall(source=units)` 排序、Prefetch 混合检索、Qdrant provider、`memory_units` 新列/迁移、rebuild/backfill 命令、级联链 id 回传。

---

### Task 1: UnitVectorIndex 接口 + 内存实现

**Files:**
- Create: `framework/memory/unit_vector.go`
- Create: `framework/memory/unit_vector_test.go`

- [ ] **Step 1: 写失败测试**

```go
package memory

import (
	"context"
	"sync"
	"testing"
)

func TestInMemoryUnitVectorIndex_UpsertSearchDelete(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	must(idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}}))
	must(idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "b", Vector: []float32{0, 1}}))

	hits, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 2})
	must(err)
	if len(hits) != 2 || hits[0].UnitID != "a" {
		t.Fatalf("want a first, got %+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("scores must be descending: %+v", hits)
	}

	// Upsert 覆盖同键，不新增行
	must(idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{0, 1}}))
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	must(err)
	if len(hits) != 2 {
		t.Fatalf("upsert must overwrite, got %d", len(hits))
	}

	must(idx.Delete(ctx, ScopeSession, "s1", "a"))
	hits, err = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	must(err)
	if len(hits) != 1 || hits[0].UnitID != "b" {
		t.Fatalf("delete failed: %+v", hits)
	}
	must(idx.Delete(ctx, ScopeSession, "s1")) // 空 id 列表 no-op
}

func TestInMemoryUnitVectorIndex_ScopeIsolation(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s2", UnitID: "b", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeUser, ScopeID: "s1", UnitID: "c", Vector: []float32{1, 0}})

	hits, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("scope leak: %+v", hits)
	}
}

func TestInMemoryUnitVectorIndex_MinScoreAndLimit(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{0, 1}})

	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 1})
	if len(hits) != 1 {
		t.Fatalf("limit ignored: %+v", hits)
	}
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10, MinScore: 0.5})
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("min score ignored: %+v", hits)
	}
}

func TestInMemoryUnitVectorIndex_Concurrent(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func(n int) { defer wg.Done(); _ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: string(rune('a' + n%26)), Vector: []float32{1, 0}}) }(i)
		go func() { defer wg.Done(); _, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5}) }()
		go func(n int) { defer wg.Done(); _ = idx.Delete(ctx, ScopeSession, "s", string(rune('a'+n%26))) }(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory -run TestInMemoryUnitVectorIndex -count=1
```

Expected: FAIL（`NewInMemoryUnitVectorIndex` / 类型未定义）

- [ ] **Step 3: 实现接口 + 内存 provider**

`framework/memory/unit_vector.go`：

```go
package memory

import (
	"context"
	"sort"
	"sync"
)

// UnitVectorIndex is a pluggable vector sidecar for memory_units (session/user scopes).
// Implementations must isolate results by (Scope, ScopeID) and treat
// (Scope, ScopeID, UnitID) as the upsert primary key.
type UnitVectorIndex interface {
	Upsert(ctx context.Context, rec UnitVectorRecord) error
	Delete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) error
	Search(ctx context.Context, q UnitVectorQuery) ([]UnitVectorHit, error)
	Close() error
}

type UnitVectorRecord struct {
	Scope   Scope
	ScopeID string
	UnitID  string
	Vector  []float32
}

type UnitVectorQuery struct {
	Scope    Scope
	ScopeID  string
	Vector   []float32
	Limit    int
	MinScore float64
}

// UnitVectorHit carries cosine similarity in [-1, 1]; providers must not use other scales.
type UnitVectorHit struct {
	UnitID string
	Score  float64
}

// UnitEmbedder embeds unit text. agentID lets Portal resolve the model per call
// (memory_extraction.auxiliary, else the agent chat model).
type UnitEmbedder interface {
	Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error)
}

type unitVectorKey struct {
	scope   Scope
	scopeID string
	unitID  string
}

// InMemoryUnitVectorIndex is the reference provider used by tests.
type InMemoryUnitVectorIndex struct {
	mu      sync.RWMutex
	vectors map[unitVectorKey][]float32
}

func NewInMemoryUnitVectorIndex() *InMemoryUnitVectorIndex {
	return &InMemoryUnitVectorIndex{vectors: make(map[unitVectorKey][]float32)}
}

var _ UnitVectorIndex = (*InMemoryUnitVectorIndex)(nil)

func (s *InMemoryUnitVectorIndex) Upsert(_ context.Context, rec UnitVectorRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	vec := make([]float32, len(rec.Vector))
	copy(vec, rec.Vector)
	s.vectors[unitVectorKey{rec.Scope, rec.ScopeID, rec.UnitID}] = vec
	return nil
}

func (s *InMemoryUnitVectorIndex) Delete(_ context.Context, scope Scope, scopeID string, unitIDs ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range unitIDs {
		delete(s.vectors, unitVectorKey{scope, scopeID, id})
	}
	return nil
}

func (s *InMemoryUnitVectorIndex) Search(_ context.Context, q UnitVectorQuery) ([]UnitVectorHit, error) {
	if q.Limit <= 0 || len(q.Vector) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	hits := make([]UnitVectorHit, 0, len(s.vectors))
	for k, v := range s.vectors {
		if k.scope != q.Scope || k.scopeID != q.ScopeID {
			continue
		}
		score := float64(cosineSimilarity(v, q.Vector))
		if q.MinScore != 0 && score < q.MinScore {
			continue
		}
		hits = append(hits, UnitVectorHit{UnitID: k.unitID, Score: score})
	}
	s.mu.RUnlock()

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].UnitID < hits[j].UnitID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits, nil
}

func (s *InMemoryUnitVectorIndex) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vectors = make(map[unitVectorKey][]float32)
	return nil
}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./memory -run TestInMemoryUnitVectorIndex -count=1
go test ./memory -run TestInMemoryUnitVectorIndex_Concurrent -race -count=1
```

Expected: PASS（含 `-race`）

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/unit_vector.go memory/unit_vector_test.go
git commit -m "feat(memory): add pluggable UnitVectorIndex with in-memory provider"
```

---

### Task 2: SQLiteUnitVectorIndex

**Files:**
- Create: `framework/memory/sqlite_unit_vector.go`
- Create: `framework/memory/sqlite_unit_vector_test.go`

- [ ] **Step 1: 写失败测试**

```go
package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteUnitVectorIndex_PersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")

	idx, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil { // 幂等
		t.Fatalf("Close must be idempotent: %v", err)
	}

	reopened, err := NewSQLiteUnitVectorIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	hits, err := reopened.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0, 0}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].UnitID != "a" {
		t.Fatalf("not persisted: %+v", hits)
	}
	if hits[0].Score < 0.99 {
		t.Fatalf("cosine decode broken: %+v", hits)
	}
}

// 重启后维度基准必须从库中已有行恢复，而不是被下一次 Upsert 重置。
func TestSQLiteUnitVectorIndex_DimsBaselineAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uv.db")

	idx, _ := NewSQLiteUnitVectorIndex(path)
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}})
	_ = idx.Close()

	reopened, _ := NewSQLiteUnitVectorIndex(path)
	defer reopened.Close()

	if err := reopened.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{1, 0}}); err == nil {
		t.Fatal("want dimension mismatch error after reopen")
	}
}

func TestSQLiteUnitVectorIndex_DimensionMismatch(t *testing.T) {
	ctx := context.Background()
	idx, _ := NewSQLiteUnitVectorIndex(filepath.Join(t.TempDir(), "uv.db"))
	defer idx.Close()

	if err := idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "a", Vector: []float32{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s", UnitID: "b", Vector: []float32{1, 0}}); err == nil {
		t.Fatal("want upsert dimension error")
	}
	if _, err := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 3}); err == nil {
		t.Fatal("want search dimension error")
	}
}

func TestSQLiteUnitVectorIndex_ScopeIsolationAndDelete(t *testing.T) {
	ctx := context.Background()
	idx, _ := NewSQLiteUnitVectorIndex(filepath.Join(t.TempDir(), "uv.db"))
	defer idx.Close()

	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeSession, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})
	_ = idx.Upsert(ctx, UnitVectorRecord{Scope: ScopeUser, ScopeID: "s1", UnitID: "a", Vector: []float32{1, 0}})

	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 {
		t.Fatalf("scope leak: %+v", hits)
	}
	_ = idx.Delete(ctx, ScopeSession, "s1", "a")
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("delete failed: %+v", hits)
	}
	hits, _ = idx.Search(ctx, UnitVectorQuery{Scope: ScopeUser, ScopeID: "s1", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 {
		t.Fatalf("user scope must survive: %+v", hits)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory -run TestSQLiteUnitVectorIndex -count=1
```

Expected: FAIL（`NewSQLiteUnitVectorIndex` 未定义）

- [ ] **Step 3: 实现 SQLite provider**

`framework/memory/sqlite_unit_vector.go`。要点：`float32` 小端 BLOB；`dims` 基准进程内缓存，构造时从任意已有行读取；`Search` 同 scope 扫描 + `cosineSimilarity`（复用 `vector.go`）。

```go
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteUnitVectorIndex stores unit vectors in a standalone SQLite file.
type SQLiteUnitVectorIndex struct {
	mu     sync.RWMutex
	db     *sql.DB
	dims   int
	closed bool
}

var _ UnitVectorIndex = (*SQLiteUnitVectorIndex)(nil)

func NewSQLiteUnitVectorIndex(path string) (*SQLiteUnitVectorIndex, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("memory: unit vector dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("memory: open unit vector db: %w", err)
	}
	if _, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE IF NOT EXISTS unit_vectors (
			scope_type TEXT NOT NULL,
			scope_id   TEXT NOT NULL,
			unit_id    TEXT NOT NULL,
			dims       INTEGER NOT NULL,
			embedding  BLOB NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (scope_type, scope_id, unit_id)
		);
		CREATE INDEX IF NOT EXISTS idx_uv_scope ON unit_vectors(scope_type, scope_id);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory: init unit vector schema: %w", err)
	}

	idx := &SQLiteUnitVectorIndex{db: db}
	// Restore the dimension baseline so a reopened index keeps rejecting mismatches.
	var dims int
	if err := db.QueryRow(`SELECT dims FROM unit_vectors LIMIT 1`).Scan(&dims); err == nil {
		idx.dims = dims
	} else if err != sql.ErrNoRows {
		db.Close()
		return nil, fmt.Errorf("memory: read unit vector dims: %w", err)
	}
	return idx, nil
}

// checkDims validates against the baseline, adopting it when the index is empty.
func (s *SQLiteUnitVectorIndex) checkDims(n int, adopt bool) error {
	if n == 0 {
		return fmt.Errorf("memory: empty vector")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dims == 0 {
		if !adopt {
			return nil
		}
		s.dims = n
		return nil
	}
	if s.dims != n {
		return fmt.Errorf("memory: vector dim %d != index dim %d", n, s.dims)
	}
	return nil
}

func (s *SQLiteUnitVectorIndex) Upsert(ctx context.Context, rec UnitVectorRecord) error {
	if err := s.errIfClosed(); err != nil {
		return err
	}
	if err := s.checkDims(len(rec.Vector), true); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO unit_vectors (scope_type, scope_id, unit_id, dims, embedding, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope_type, scope_id, unit_id)
		DO UPDATE SET dims=excluded.dims, embedding=excluded.embedding, updated_at=excluded.updated_at
	`, string(rec.Scope), rec.ScopeID, rec.UnitID, len(rec.Vector), encodeVector(rec.Vector), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("memory: upsert unit vector: %w", err)
	}
	return nil
}

func (s *SQLiteUnitVectorIndex) Delete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) error {
	if len(unitIDs) == 0 {
		return nil
	}
	if err := s.errIfClosed(); err != nil {
		return err
	}
	for _, id := range unitIDs {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM unit_vectors WHERE scope_type=? AND scope_id=? AND unit_id=?`,
			string(scope), scopeID, id); err != nil {
			return fmt.Errorf("memory: delete unit vector: %w", err)
		}
	}
	return nil
}

func (s *SQLiteUnitVectorIndex) Search(ctx context.Context, q UnitVectorQuery) ([]UnitVectorHit, error) {
	if err := s.errIfClosed(); err != nil {
		return nil, err
	}
	if q.Limit <= 0 || len(q.Vector) == 0 {
		return nil, nil
	}
	if err := s.checkDims(len(q.Vector), false); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT unit_id, embedding FROM unit_vectors WHERE scope_type=? AND scope_id=?`,
		string(q.Scope), q.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("memory: search unit vectors: %w", err)
	}
	defer rows.Close()

	var hits []UnitVectorHit
	for rows.Next() {
		var unitID string
		var blob []byte
		if err := rows.Scan(&unitID, &blob); err != nil {
			continue
		}
		score := float64(cosineSimilarity(decodeVector(blob), q.Vector))
		if q.MinScore != 0 && score < q.MinScore {
			continue
		}
		hits = append(hits, UnitVectorHit{UnitID: unitID, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].UnitID < hits[j].UnitID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits, nil
}

func (s *SQLiteUnitVectorIndex) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func (s *SQLiteUnitVectorIndex) errIfClosed() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("memory: unit vector index closed")
	}
	return nil
}

func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./memory -run TestSQLiteUnitVectorIndex -count=1
```

Expected: PASS

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/sqlite_unit_vector.go memory/sqlite_unit_vector_test.go
git commit -m "feat(memory): add SQLite UnitVectorIndex provider"
```

---

### Task 3: Facade 向量 peer 发现 + 熔断

**Files:**
- Modify: `framework/memory/facade.go`
- Create: `framework/memory/facade_vector_test.go`

- [ ] **Step 1: 写失败测试**

用 `NewSessionMemory()` + `StubSemanticConflictResolver` + `InMemoryUnitVectorIndex` + 假 Embedder。

```go
package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeEmbedder maps keywords to fixed vectors so "no shared substring" pairs can still be near.
type fakeEmbedder struct {
	mu    sync.Mutex
	calls int
	err   error
	byKey map[string][]float32
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		vec := []float32{0, 1}
		for k, v := range f.byKey {
			if strings.Contains(t, k) {
				vec = v
				break
			}
		}
		out = append(out, vec)
	}
	return out, nil
}

func (f *fakeEmbedder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newVectorFacade(t *testing.T, emb UnitEmbedder, idx UnitVectorIndex, stub *StubSemanticConflictResolver, toolGate bool) *Facade {
	t.Helper()
	return NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		SemanticConflicts:    stub,
		ToolSemanticConflict: toolGate,
		UnitVectors:          idx,
		UnitEmbedder:         emb,
	})
}

// 语义近但无共享子串：LIKE 召不回，向量能召回。
func TestFacade_VectorPeerDiscovery_FindsNonOverlappingPeer(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := newVectorFacade(t, emb, idx, stub, true)

	first, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if err != nil || first.ID == "" {
		t.Fatalf("seed failed: %+v %v", first, err)
	}
	if stub.Calls != 0 {
		t.Fatalf("first add has no peers, resolver must not run")
	}

	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "界面用亮色模式"}); err != nil {
		t.Fatal(err)
	}
	if stub.Calls != 1 {
		t.Fatalf("resolver should see vector peer, calls=%d", stub.Calls)
	}
	if len(stub.LastPeers) != 1 || stub.LastPeers[0].ID != first.ID {
		t.Fatalf("wrong peers: %+v", stub.LastPeers)
	}
}

// Embed 失败 → 熔断 → 回退 LIKE，且不再重复 Embed。
func TestFacade_EmbedFailure_TripsBreakerAndFallsBackToLike(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{err: errors.New("no /embeddings")}
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := newVectorFacade(t, emb, NewInMemoryUnitVectorIndex(), stub, true)

	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color blue"}); err != nil {
		t.Fatal(err)
	}
	afterFirst := emb.count()
	if afterFirst == 0 {
		t.Fatal("first call must attempt embed")
	}
	// LIKE 可互命中，确认走了回退路径
	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color red"}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != afterFirst {
		t.Fatalf("breaker must stop further embeds: %d -> %d", afterFirst, emb.count())
	}
	if stub.Calls == 0 {
		t.Fatal("LIKE fallback should still find peers and run resolver")
	}
}

// D2 关闭：禁止任何 Embed / Upsert。
func TestFacade_D2Disabled_NoEmbedNoUpsert(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, false)

	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != 0 {
		t.Fatalf("D2 off must not embed, got %d", emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 0 {
		t.Fatalf("D2 off must not upsert: %+v", hits)
	}
}

// 索引未装配时行为与 P2-D2 现网一致（LIKE）。
func TestFacade_NoVectorIndex_UsesLike(t *testing.T) {
	ctx := context.Background()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{Session: NewSessionMemory(), SemanticConflicts: stub, ToolSemanticConflict: true})

	_, _ = f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color blue"})
	_, _ = f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color red"})
	if stub.Calls == 0 {
		t.Fatal("LIKE path must still work")
	}
}
```

> `StubSemanticConflictResolver` 需新增 `LastPeers []MemoryHit` 字段（在 `semantic_conflict.go` 的 `ResolveAdd` 里记录）。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory -run 'TestFacade_VectorPeer|TestFacade_EmbedFailure|TestFacade_D2Disabled|TestFacade_NoVectorIndex' -count=1
```

Expected: FAIL（`UnitVectors` 字段不存在 / `LastPeers` 未定义）

- [ ] **Step 3: 实现**

`semantic_conflict.go`：给 stub 加 `LastPeers`，在 `ResolveAdd` 中 `s.LastPeers = peers`。

`facade.go`：**import 块需新增 `"sync/atomic"`**（当前只有 `context/errors/fmt/strings`）。

```go
// FacadeConfig 增加
UnitVectors  UnitVectorIndex
UnitEmbedder UnitEmbedder

// Facade 增加
unitVectors  UnitVectorIndex
unitEmbedder UnitEmbedder
embedTripped atomic.Bool
```

> 熔断作用域：`embedTripped` 挂在 `*Facade` 实例上。Portal 现网每个 `BuildMemoryStore` 造一个 Facade，等价于「进程级」只要 Facade 不被重建。已知例外：`RebuildPrefetchMemoryOrchestrator` 会重建 prefetch 侧 store，熔断状态随之重置——E1 接受该行为，在注释中写明，不引入包级全局状态。

`NewFacade` 透传两字段（**不**给默认实现，nil 保持 nil）。

新增方法：

```go
// vectorReady reports whether the vector peer path may run for this process.
func (f *Facade) vectorReady() bool {
	return f.unitVectors != nil && f.unitEmbedder != nil && !f.embedTripped.Load()
}

// embedOne returns the candidate vector, tripping the process-wide breaker on failure
// so a gateway without /embeddings is only probed once (spec §2.2).
func (f *Facade) embedOne(ctx context.Context, agentID, text string) []float32 {
	vecs, err := f.unitEmbedder.Embed(ctx, agentID, []string{text})
	if err != nil || len(vecs) == 0 || len(vecs[0]) == 0 {
		f.embedTripped.Store(true)
		return nil
	}
	return vecs[0]
}

// vectorPeers returns hydrated active peers, or ok=false to fall back to LIKE.
func (f *Facade) vectorPeers(ctx context.Context, in RememberInput) ([]MemoryHit, bool) {
	vec := f.embedOne(ctx, in.AgentID, in.Content)
	if vec == nil {
		return nil, false
	}
	hits, err := f.unitVectors.Search(ctx, UnitVectorQuery{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		Vector:  vec,
		Limit:   f.semanticConflictK,
	})
	if err != nil {
		return nil, false
	}
	peers := make([]MemoryHit, 0, len(hits))
	for _, h := range hits {
		got, err := f.session.Get(ctx, GetRef{Scope: in.Scope, ID: h.UnitID, ScopeID: in.ScopeID})
		if err != nil || got.ID == "" {
			continue // stale sidecar entry; hydrate keeps correctness
		}
		if st, _ := got.Metadata["status"].(string); st != "" && st != "active" {
			continue
		}
		got.Score = h.Score
		peers = append(peers, got)
	}
	return peers, true
}
```

`rememberAdd` 的 peer 获取改为：

```go
	var peers []MemoryHit
	if f.vectorReady() {
		if p, ok := f.vectorPeers(ctx, in); ok {
			peers = p
		} else {
			p2, err := f.session.Recall(ctx, f.peerRecallQuery(in))
			if err != nil {
				return MemoryHit{}, nil
			}
			peers = p2
		}
	} else {
		p2, err := f.session.Recall(ctx, f.peerRecallQuery(in))
		if err != nil {
			return MemoryHit{}, nil
		}
		peers = p2
	}
```

抽出：

```go
func (f *Facade) peerRecallQuery(in RememberInput) RecallQuery {
	return RecallQuery{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		Source:  SourceUnits,
		Query:   in.Content,
		Limit:   f.semanticConflictK,
	}
}
```

其余分支（`len(peers)==0` 直 add、`ResolveAdd`、Ignore/KeepBoth/Supersede）保持 P2-D2 语义不变。

- [ ] **Step 4: 测试通过**

```bash
go test ./memory -run 'TestFacade_' -count=1
```

Expected: PASS（含既有 `facade_semantic_test.go` 全部用例）

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/facade.go memory/facade_vector_test.go memory/semantic_conflict.go
git commit -m "feat(memory): vector-backed D2 peer discovery with embed breaker"
```

---

### Task 4: Facade 写路径向量同步

**Files:**
- Modify: `framework/memory/facade.go`
- Modify: `framework/memory/facade_vector_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestFacade_AddUpsertsVector(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	hit, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if err != nil || hit.ID == "" {
		t.Fatalf("add failed: %v", err)
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 || hits[0].UnitID != hit.ID {
		t.Fatalf("add must upsert vector: %+v", hits)
	}
}

// 语义 Supersede：新 id upsert，旧 id 从 sidecar 删除。
func TestFacade_SemanticSupersede_SyncsVectors(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := newVectorFacade(t, emb, idx, stub, true)

	old, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})

	stub.Decision = ConflictSupersede
	stub.TargetUnitID = old.ID
	neu, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "界面用亮色模式"})
	if err != nil || neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("supersede failed: %+v %v", neu, err)
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 1 || hits[0].UnitID != neu.ID {
		t.Fatalf("old vector must be deleted, new upserted: %+v", hits)
	}
}

// D1 显式 replace + D2 关闭：删旧 id，且【不得】为新 id 写向量（规格 §2.3 D1 表第 2 行）。
func TestFacade_StructuralReplace_D2Off_DeletesOldAndSkipsUpsert(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()

	// 先用 D2 开着的 facade 播种，保证旧 id 在 sidecar 里确实有向量。
	seed := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)
	old, _ := seed.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if n, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10}); len(n) != 1 {
		t.Fatalf("seed vector missing: %+v", n)
	}

	// 关掉 D2 门（同包可访问私有字段）；此后 replace 必须完全无 Upsert。
	seed.toolSemanticConflict = false
	before := emb.count()
	if _, err := seed.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionReplace, UnitID: old.ID, Content: "用户偏好亮色主题"}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != before {
		t.Fatalf("D2 off replace must not embed: %d -> %d", before, emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 0 {
		t.Fatalf("want old deleted and no new vector, got %+v", hits)
	}
}

// D1 显式 replace + D2 开启且未熔断：删旧 id + 写新 id（规格 §2.3 D1 表第 3 行）。
func TestFacade_StructuralReplace_D2On_DeletesOldAndUpsertsNew(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	old, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	neu, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionReplace, UnitID: old.ID, Content: "用户偏好亮色主题"})
	if err != nil || neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("replace failed: %+v %v", neu, err)
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 1 || hits[0].UnitID != neu.ID {
		t.Fatalf("want only new id indexed, got %+v (old=%s new=%s)", hits, old.ID, neu.ID)
	}
}

func TestFacade_RemoveDeletesVector(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	hit, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionRemove, UnitID: hit.ID}); err != nil {
		t.Fatal(err)
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 0 {
		t.Fatalf("remove must delete vector: %+v", hits)
	}
}

func TestFacade_DeleteRefDeletesVector(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	hit, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if err := f.Delete(ctx, GetRef{Scope: ScopeSession, ScopeID: "s", ID: hit.ID}); err != nil {
		t.Fatal(err)
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 0 {
		t.Fatalf("Delete must drop vector: %+v", hits)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory -run 'TestFacade_AddUpserts|TestFacade_SemanticSupersede_Sync|TestFacade_StructuralReplace_D2|TestFacade_RemoveDeletes|TestFacade_DeleteRefDeletes' -count=1
```

Expected: FAIL

- [ ] **Step 3: 实现同步钩子**

```go
// syncUpsert embeds and indexes a freshly written unit. Gated on the same D2 switch
// as peer discovery so a D2-disabled write never triggers an embedding call
// (spec §2.3: "d2 == false → 禁止 Embed 与 Upsert", including the D1 replace path).
// Best-effort: index drift is tolerated because hydrate re-checks active status.
func (f *Facade) syncUpsert(ctx context.Context, in RememberInput, unitID string) {
	if unitID == "" || !f.semanticEnabled(in) || !f.vectorReady() {
		return
	}
	vec := f.embedOne(ctx, in.AgentID, in.Content)
	if vec == nil {
		return
	}
	_ = f.unitVectors.Upsert(ctx, UnitVectorRecord{
		Scope:   in.Scope,
		ScopeID: in.ScopeID,
		UnitID:  unitID,
		Vector:  vec,
	})
}

// syncDelete drops vectors for ids that are no longer active. Independent of the
// D2 gate and the embed breaker (no embedding required).
func (f *Facade) syncDelete(ctx context.Context, scope Scope, scopeID string, unitIDs ...string) {
	if f.unitVectors == nil || len(unitIDs) == 0 {
		return
	}
	_ = f.unitVectors.Delete(ctx, scope, scopeID, unitIDs...)
}
```

挂载点（严格对齐规格 §2.3 表）。关键改写示意：

```go
// rememberAdd — 每个成功写分支都捕获 hit 再 sync：
hit, err := f.session.Remember(ctx, in) // 或 KeepBoth / Supersede 的 Remember
if err != nil {
	return MemoryHit{}, err
}
if verdict.Decision == ConflictSupersede { // only on supersede branch
	f.syncDelete(ctx, in.Scope, in.ScopeID, verdict.TargetUnitID)
}
f.syncUpsert(ctx, in, hit.ID)
return hit, nil

// rememberReplace — supersede 成功后：
hit, err := f.session.Remember(ctx, in)
if err != nil {
	return MemoryHit{}, err
}
f.syncDelete(ctx, in.Scope, in.ScopeID, in.UnitID)
f.syncUpsert(ctx, in, hit.ID) // D2 关 / 熔断时内部跳过
return hit, nil

// ActionRemove / Facade.Delete — 成功后：
f.syncDelete(ctx, scope, scopeID, unitID)
```

| 位置 | 调用 |
|------|------|
| `rememberAdd` 无 peers 直写成功后 | `syncUpsert(ctx, in, hit.ID)` |
| `rememberAdd` KeepBoth 成功后 | `syncUpsert(ctx, in, hit.ID)` |
| `rememberAdd` Supersede 成功后 | 先 `syncDelete(..., verdict.TargetUnitID)`，再 `syncUpsert(ctx, in, hit.ID)` |
| `rememberReplace` supersede 成功后 | `syncDelete(..., in.UnitID)`；再 `syncUpsert(ctx, in, hit.ID)`（D2 关或熔断时内部自动跳过） |
| `rememberUnits` 的 `ActionRemove` 成功后 | `syncDelete(..., in.UnitID)` |
| `Facade.Delete`（user/session）成功后 | `syncDelete(..., ref.ID)` |

两条门控不变量，实现时不要绕过：

- `syncUpsert` 内部同时检查 `semanticEnabled(in)` 与 `vectorReady()` —— 这是唯一的 Upsert 入口，因此 D1 replace 在 D2 关闭时天然不会 Embed。
- `syncDelete` 只检查 `unitVectors != nil` —— 与 D2 开关、熔断状态均无关。

`d2==false` 时 `rememberAdd` 走 `f.session.Remember` 早返回分支，也不调用 `syncUpsert`。

- [ ] **Step 4: 测试通过**

```bash
go test ./memory -count=1
```

Expected: PASS（全包，含 D1/D2 既有回归）

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/facade.go memory/facade_vector_test.go
git commit -m "feat(memory): sync unit vectors on add/supersede/remove"
```

---

### Task 5: MemoryVector 配置

**Files:**
- Modify: `framework/config/tool_guardrails.go`
- Modify: `framework/config/config_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestPortalAgentExtra_MemoryVector(t *testing.T) {
	var extra PortalAgentExtra
	y := []byte("memory_vector:\n  provider: none\n  path: custom.db\n")
	if err := yaml.Unmarshal(y, &extra); err != nil {
		t.Fatal(err)
	}
	if extra.MemoryVector == nil || extra.MemoryVector.Provider != "none" || extra.MemoryVector.Path != "custom.db" {
		t.Fatalf("got %+v", extra.MemoryVector)
	}
}

// 只写 memory_vector 时不得被 LoadPortalAgentExtra 的空值折叠判断吞掉。
func TestLoadPortalAgentExtra_MemoryVectorOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_extra.yaml")
	if err := os.WriteFile(path, []byte("memory_vector:\n  provider: none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra, err := LoadPortalAgentExtra(path)
	if err != nil {
		t.Fatal(err)
	}
	if extra == nil || extra.MemoryVector == nil {
		t.Fatalf("memory_vector-only config must not collapse to nil, got %+v", extra)
	}
}
```

> 与 `config_test.go` 现有 `MemoryConflict` 用例同风格；沿用该文件已引入的 yaml 包别名，并按需补 `os`/`path/filepath`。

- [ ] **Step 2: 运行确认失败**

```bash
go test ./config -run TestPortalAgentExtra_MemoryVector -count=1
```

- [ ] **Step 3: 实现**

`tool_guardrails.go`（紧邻 `MemoryConflict`）：

```go
// MemoryVector configures the pluggable unit vector sidecar (P2-E1).
// Provider "" defaults to sqlite when an embedder is available; "none" disables it.
type MemoryVector struct {
	Provider string `yaml:"provider" json:"provider"`
	Path     string `yaml:"path" json:"path"`
}
```

`PortalAgentExtra` 增加字段：

```go
// MemoryVector 配置 units 向量 sidecar（P2-E1）；省略 = sqlite 默认。
MemoryVector *MemoryVector `json:"memory_vector" yaml:"memory_vector"`
```

**必须**同步 `LoadPortalAgentExtra`（`tool_guardrails.go:102-105`）的空值折叠条件，否则只配 `memory_vector` 时整份配置会被静默丢成 nil：

```go
	if extra.ToolGuardrails == nil && extra.Portal == nil && extra.MemoryOrchestratorPrefetch == nil &&
		extra.MemoryExtraction == nil && extra.MemoryConflict == nil && extra.MemoryVector == nil && extra.Web == nil {
		return nil, nil
	}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./config -count=1
```

- [ ] **Step 5: Commit（framework）**

```bash
git add config/tool_guardrails.go config/config_test.go
git commit -m "feat(config): MemoryVector on PortalAgentExtra"
```

---

### Task 6: Portal 接线

**Files:**
- Create: `portal/internal/chat/memory_vector.go`
- Create: `portal/internal/chat/memory_vector_test.go`
- Create: `portal/internal/chat/memory_vector_testmain_test.go`（仅测试二进制；默认禁用 sidecar）
- Modify: `portal/internal/chat/memory_store.go`
- Modify: `portal/internal/chat/memory_conflict.go`
- Modify: `portal/internal/chat/portal_agent_extra.go`
- Modify: `portal/cmd/backend/main.go`
- Modify: `portal/configs/agent_extra.yaml`

> 先在 `portal/go.mod` 用 `replace github.com/sixath/framework => ../framework` 联调；Task 7 收尾时恢复。

- [ ] **Step 1: 写失败测试**

```go
package chat

import (
	"path/filepath"
	"testing"

	"github.com/sixath/framework/config"
)

// restoreTestVectorDefaults resets the package-level sidecar knobs to the
// TestMain baseline (provider=none, data root ./data). Prefer this over
// SetMemoryVectorConfig(nil), which would re-enable the production sqlite default
// and let later tests open a real DB under the package cwd.
func restoreTestVectorDefaults(t *testing.T) {
	t.Helper()
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	SetMemoryVectorDataRoot("./data")
}

func TestMemoryVectorProviderNone_DisablesIndex(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	if memoryVectorEnabled() {
		t.Fatal("provider=none must disable the sidecar")
	}
	if idx := sharedUnitVectorIndex(); idx != nil {
		t.Fatalf("provider=none must not open an index, got %T", idx)
	}
}

// Production default (nil config) means sqlite. TestMain pins provider=none for the
// package, so this case explicitly clears to nil and restores afterwards.
func TestMemoryVectorPath(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })

	SetMemoryVectorConfig(nil)
	if !memoryVectorEnabled() {
		t.Fatal("omitted config should default to sqlite")
	}
	if got := memoryVectorPath("data"); got != filepath.Join("data", "memory_unit_vectors.db") {
		t.Fatalf("default path wrong: %s", got)
	}

	SetMemoryVectorConfig(&config.MemoryVector{Path: "custom.db"})
	if got := memoryVectorPath("data"); got != filepath.Join("data", "custom.db") {
		t.Fatalf("relative path must join data root: %s", got)
	}
}

// SetMemoryVectorConfig / SetMemoryVectorDataRoot must force the next
// sharedUnitVectorIndex() call to re-resolve (hot reload + test isolation).
func TestSharedUnitVectorIndex_CachesAndResets(t *testing.T) {
	t.Cleanup(func() { restoreTestVectorDefaults(t) })
	SetMemoryVectorDataRoot(t.TempDir())
	SetMemoryVectorConfig(nil) // enable production-default sqlite under TempDir

	first := sharedUnitVectorIndex()
	if first == nil {
		t.Fatal("default provider should open an index")
	}
	if second := sharedUnitVectorIndex(); second != first {
		t.Fatal("index must be a cached singleton across calls")
	}
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	if sharedUnitVectorIndex() != nil {
		t.Fatal("reconfigure must drop the cached index")
	}
}

// 无 aux 且无 AgentGetter → 不注入 embedder（与 semantic resolver 判定一致）。
func TestDefaultMemoryStoreOptions_NoModelFactory_NoEmbedder(t *testing.T) {
	t.Cleanup(func() {
		restoreTestVectorDefaults(t)
		SetMemoryAgentGetter(nil)
	})
	SetMemoryAgentGetter(nil)
	SetMemoryConflictConfig(nil)

	opts := DefaultMemoryStoreOptions()
	if opts.UnitEmbedder != nil {
		t.Fatal("embedder must be nil without a model factory")
	}
	if opts.UnitVectors != nil {
		t.Fatal("TestMain baseline (provider=none) must not inject an index")
	}
}
```

> `sharedUnitVectorIndex()` 返回 `memory.UnitVectorIndex` 接口，`nil` 判定需返回真正的 `nil` 接口值（见 Step 3 的实现，不要返回带 nil 指针的接口）。

另建 `memory_vector_testmain_test.go`（只在测试二进制编译）：

```go
package chat

import (
	"os"
	"testing"

	"github.com/sixath/framework/config"
)

// TestMain pins the vector sidecar off for the whole package binary. Production
// DefaultMemoryStoreOptions opens sqlite when config is omitted; without this
// pin, every existing test that calls BuildMemoryStore / DefaultMemoryStoreOptions
// (memory_conflict_test, hermes_*, browser_wiring_test, memory_extract_pipeline_test,
// …) would create portal/internal/chat/data/memory_unit_vectors.db under the
// package cwd — a path NOT covered by portal/.gitignore's repo-root /data/ rule.
func TestMain(m *testing.M) {
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})
	os.Exit(m.Run())
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd portal
go test ./internal/chat -run 'TestMemoryVector|TestSharedUnitVectorIndex|TestDefaultMemoryStoreOptions_NoModelFactory' -count=1
```

- [ ] **Step 3: 实现**

`memory_vector.go`：

```go
package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

var (
	vectorMu         sync.Mutex
	storedVectorYAML *config.MemoryVector
	vectorDataRoot   = "./data"
	vectorIndex      memory.UnitVectorIndex
	vectorIndexBuilt bool
)

// SetMemoryVectorConfig stores agent_extra memory_vector settings and drops any
// index opened under the previous configuration.
func SetMemoryVectorConfig(cfg *config.MemoryVector) {
	vectorMu.Lock()
	defer vectorMu.Unlock()
	if cfg == nil {
		storedVectorYAML = nil
	} else {
		cp := *cfg
		storedVectorYAML = &cp
	}
	closeVectorIndexLocked()
}

// SetMemoryVectorDataRoot supplies Portal's data.data_root; call once at startup
// before any BuildMemoryStore. Defaults to ./data.
func SetMemoryVectorDataRoot(root string) {
	vectorMu.Lock()
	defer vectorMu.Unlock()
	if r := strings.TrimSpace(root); r != "" {
		vectorDataRoot = r
	}
	closeVectorIndexLocked()
}

func closeVectorIndexLocked() {
	if vectorIndex != nil {
		_ = vectorIndex.Close()
	}
	vectorIndex = nil
	vectorIndexBuilt = false
}

func memoryVectorEnabled() bool {
	// Callers must hold vectorMu, or be single-goroutine test code.
	// Go's Mutex is not reentrant — do not lock again here.
	if storedVectorYAML == nil {
		return true // default sqlite; still requires an embedder to take effect
	}
	return !strings.EqualFold(strings.TrimSpace(storedVectorYAML.Provider), "none")
}

func memoryVectorPath(dataRoot string) string {
	// Callers must hold vectorMu, or be single-goroutine test code.
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "."
	}
	name := "memory_unit_vectors.db"
	if storedVectorYAML != nil && strings.TrimSpace(storedVectorYAML.Path) != "" {
		name = strings.TrimSpace(storedVectorYAML.Path)
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dataRoot, name)
}

// dynamicUnitEmbedder resolves the embedding model per call, mirroring
// dynamicSemanticConflictResolver (auxiliary → agent chat model).
type dynamicUnitEmbedder struct {
	agents AgentGetter
}

var _ memory.UnitEmbedder = (*dynamicUnitEmbedder)(nil)

func (e *dynamicUnitEmbedder) Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error) {
	var meta *biz.AgentMeta
	getter := globalMemoryAgentGetter
	if e != nil && e.agents != nil {
		getter = e.agents
	}
	if getter != nil && strings.TrimSpace(agentID) != "" {
		got, err := getter.Get(ctx, agentID)
		if err != nil {
			return nil, err
		}
		meta = got
	}
	m, err := resolveMemoryAuxModel(meta)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("memory: unit embed model unavailable")
	}
	embs, err := m.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, 0, len(embs))
	for _, e := range embs {
		out = append(out, e.Vector)
	}
	return out, nil
}

// sharedUnitVectorIndex lazily opens one sqlite sidecar per process. BuildMemoryStore
// runs at several call sites (chat service, prefetch bootstrap, runtime tools); each
// must reuse the same handle rather than opening the db again.
// Returns a nil interface when disabled or unopenable (fail-open: D2 keeps using LIKE).
func sharedUnitVectorIndex() memory.UnitVectorIndex {
	vectorMu.Lock()
	defer vectorMu.Unlock()
	if vectorIndexBuilt {
		return vectorIndex
	}
	vectorIndexBuilt = true
	if !memoryVectorEnabled() {
		return nil
	}
	idx, err := memory.NewSQLiteUnitVectorIndex(memoryVectorPath(vectorDataRoot))
	if err != nil {
		return nil
	}
	vectorIndex = idx // assign only on success so the interface stays truly nil otherwise
	return vectorIndex
}
```

> import：`context`、`fmt`、`path/filepath`、`strings`、`sync`、`backend/internal/biz`、framework 的 `config`/`memory`。

`memory_conflict.go` 的 `DefaultMemoryStoreOptions` 增补：

```go
	var embedder memory.UnitEmbedder
	if memorySemanticModelFactoryAvailable() {
		embedder = &dynamicUnitEmbedder{agents: globalMemoryAgentGetter}
	}
	return MemoryStoreOptions{
		SemanticConflicts:    resolver,
		ToolSemanticConflict: memoryConflictEnabled(),
		SemanticConflictK:    memoryConflictK(),
		UnitEmbedder:         embedder,
		UnitVectors:          sharedUnitVectorIndex(),
	}
```

`memory_store.go`：`MemoryStoreOptions` 增加 `UnitVectors memory.UnitVectorIndex` / `UnitEmbedder memory.UnitEmbedder`，并在 `NewFacade` 调用中透传。

`portal_agent_extra.go`：在 `SetPortalAgentExtra` 内、`SetMemoryConflictConfig` 分支之后，按同样的 if/else 形状补：

```go
	if extra.MemoryVector != nil {
		SetMemoryVectorConfig(extra.MemoryVector)
	} else {
		SetMemoryVectorConfig(nil)
	}
```

`cmd/backend/main.go`：在 `if portalExtra != nil { chat.SetPortalAgentExtra(...) }`（约 218 行）**之前**注入 data root，确保后续开库落在正确目录：

```go
	if bc.Data != nil {
		chat.SetMemoryVectorDataRoot(bc.Data.GetDataRoot())
	}
```

`agent_extra.yaml`：追加注释示例

```yaml
# 可选：units 向量 sidecar（P2-E1）。仅在语义冲突会跑且 Embed 可用时生效。
# memory_vector:
#   provider: sqlite   # sqlite | none
#   path: ""           # 相对 data_root；空则 memory_unit_vectors.db
```

- [ ] **Step 4: 测试通过**

```bash
go test ./internal/chat -count=1
```

- [ ] **Step 5: Commit（portal）**

```bash
git add internal/chat/memory_vector.go internal/chat/memory_vector_test.go internal/chat/memory_vector_testmain_test.go internal/chat/memory_store.go internal/chat/memory_conflict.go internal/chat/portal_agent_extra.go cmd/backend/main.go configs/agent_extra.yaml
git commit -m "feat(memory): wire sqlite unit vector sidecar and dynamic embedder"
```

---

### Task 7: 文档 + 全量回归

**Files:**
- Modify: `portal/docs/memory-integration.md`
- Modify: `docs/superpowers/specs/2026-07-27-memory-store-vector-sidecar-design.md`（状态 → 实现中/已交付）
- Modify: `docs/superpowers/specs/2026-07-25-memory-store-facade-design.md`（§8 第 4 条）
- Modify: `docs/superpowers/specs/2026-07-26-memory-store-llm-conflict-design.md`（peer 来源回写）

- [ ] **Step 1: 更新 Portal 文档**

在「语义冲突消解（P2-D2）」后新增：

```markdown
## 向量 Sidecar（P2-E1）

D2 的 peer 发现可由可插拔 `UnitVectorIndex` 提供语义近邻，替代 `LIKE` 子串：

- 配置：`agent_extra.yaml` 的 `memory_vector.provider`（`sqlite` 默认 / `none` 关闭）、`path`
- 存储：独立 SQLite 文件（默认 `data_root/memory_unit_vectors.db`），**不改 `memory_units` 表**
- Embed 模型：复用 `memory_extraction.auxiliary`，否则当前 Agent chat model（按调用解析）
- 仅当本次写入会走 D2 语义门时才 Embed/Upsert；D2 关闭时无向量副作用
- Embed 失败（如网关无 `/embeddings`）→ **进程级熔断**，回退 LIKE，需重启恢复
- 命中后回主表校验 `active`，故 sidecar 陈旧条目不会污染裁决
```

Backlog 补一行 E2（hybrid recall / Qdrant）。

- [ ] **Step 2: 回写规格状态**

精确落点（行号按当前版本，若已漂移用引号内文字定位）：

| 文件 | 位置 | 改动 |
|------|------|------|
| `2026-07-27-...-vector-sidecar-design.md` | 第 3 行 `> 状态：已确认（待实现计划）` | 改为 `> 状态：已交付（E1）` |
| `2026-07-25-...-facade-design.md` | §8.3 二期清单第 4 条（向量） | 标注 E1 已交付＝接口 + SQLite provider + D2 peer；E2 hybrid recall / E3 其他 provider 待开 |
| `2026-07-26-...-llm-conflict-design.md` | 第 142 行「**Peer 发现依赖现有关键词 LIKE 子串匹配**（非向量）」 | 改为「peer 发现默认 LIKE 子串匹配；装配 `UnitVectorIndex` + `UnitEmbedder` 且未熔断时改走向量近邻（见 P2-E1 规格），熔断后回退 LIKE」 |
| 同上 | 第 50 行 mermaid `R[Recall units top-K LIKE]` | 文案改为 `Recall units top-K（向量或 LIKE）` |

- [ ] **Step 3: 恢复 go.mod 并跑全量回归**

```bash
cd portal
git checkout go.mod go.sum   # 若 Task 6 改过 replace
cd ../framework
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off
$env:GOMAXPROCS='1'; go test ./config -count=1 -p 1 -vet=off
$env:GOMAXPROCS='1'; go test ./tool/memory -count=1 -p 1 -vet=off
cd ../portal
$env:GOMAXPROCS='1'; go test ./internal/chat -count=1 -p 1 -vet=off
$env:GOMAXPROCS='1'; go test ./internal/data -count=1 -p 1 -vet=off
```

Expected: 全部 ok

跑完后确认无测试污染：

```bash
cd ../portal
git status --porcelain
```

Expected: 无 `internal/chat/data/` 或 `*.db` / `*-wal` / `*-shm` 未跟踪文件。若有，说明 TestMain / restoreTestVectorDefaults 隔离失败，先修再交。

- [ ] **Step 4: 构建校验**

```bash
cd portal
go build ./...
```

- [ ] **Step 5: Commit（三处）**

```bash
# portal
git add docs/memory-integration.md
git commit -m "docs(memory): document P2-E1 vector sidecar"

# monorepo 根
cd ..
git add docs/superpowers/specs docs/superpowers/plans
git commit -m "docs(memory): P2-E1 spec status and plan"
```

---

## 验收对照（规格 §4）

| 规格验收 | 覆盖 |
|----------|------|
| 内存 provider CRUD / scope 隔离 / 并发 | Task 1 |
| SQLite 持久化 / 重启 dims 基准 / 维度冲突 / Close | Task 2 |
| 向量 peer 让 LIKE 召不回的矛盾对进入 resolver | Task 3 |
| Embed 失败回退 LIKE 且熔断不重复 Embed | Task 3 |
| D2 关闭时零 Embed / 零 Upsert | Task 3 |
| add 后 Upsert；supersede/remove/Delete 后向量消失 | Task 4 |
| D1 replace：d2 关只删旧、d2 开删旧+写新 | Task 4（两个 `TestFacade_StructuralReplace_D2*`） |
| 无新 MySQL migration | 全程（未触碰 `portal/migrations`） |
| aux 未配 + AgentGetter 可用时 embedder 动态可用 | Task 6 |
| 只配 `memory_vector` 时配置不被折叠丢弃 | Task 5（`TestLoadPortalAgentExtra_MemoryVectorOnly`） |
| 多个 `BuildMemoryStore` 调用点复用同一 sqlite 句柄 | Task 6（`TestSharedUnitVectorIndex_CachesAndResets`） |
| 既有 chat 测试不落盘 sqlite | Task 6（`TestMain` 默认 `provider=none` + Task 7 `git status`） |

## 手工验收（可选，需真实 `/embeddings`）

1. `agent_extra.yaml` 配 `memory_extraction.auxiliary` 指向支持 embeddings 的网关；`SATH_MEMORY_CONFLICT_ENABLED=true` 重启 Portal。
2. 依次 `memory_remember(add)`：「用户偏好深色主题」→「界面用亮色模式」。
3. 期望：第二条触发 D2，返回带 `supersedes_id` 的新单元（或 KeepBoth，取决于 LLM 裁决），且 `LIKE` 无共享子串。
4. 网关不支持 embeddings 时：行为与 P2-D2 现状一致，日志仅一次 embed 失败。
