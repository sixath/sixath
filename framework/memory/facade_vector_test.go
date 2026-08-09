package memory

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubEmbed maps text containing "alpha" / "beta" / "gamma" to orthogonal-ish vectors.
// Used by the Qdrant-style VectorIndex (EmbedFunc) tests below.
func stubEmbed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		switch {
		case strings.Contains(strings.ToLower(text), "alpha"):
			out[i] = []float32{1, 0, 0}
		case strings.Contains(strings.ToLower(text), "beta"):
			out[i] = []float32{0.95, 0.05, 0}
		case strings.Contains(strings.ToLower(text), "gamma"):
			out[i] = []float32{0, 1, 0}
		default:
			out[i] = []float32{0, 0, 1}
		}
	}
	return out, nil
}

func TestFacade_VectorHybridRecall(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewSQLiteVectorIndex(filepath.Join(dir, "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	async := false
	f := NewFacade(FacadeConfig{
		Session:     NewSessionMemory(),
		Vectors:     idx,
		Embed:       stubEmbed,
		VectorAsync: &async,
	})
	ctx := context.Background()

	_, err = f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd,
		Content: "user prefers alpha theme permanently",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd,
		Content: "system uses gamma scheduler only",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Query has no shared substring with first fact, but same "alpha" embedding.
	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "please recall alpha preference", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected vector recall hits")
	}
	found := false
	for _, h := range hits {
		if strings.Contains(h.Content, "alpha theme") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alpha fact via vector, got %#v", hits)
	}
}

func TestFacade_VectorPeerSearchForSemanticAdd(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewSQLiteVectorIndex(filepath.Join(dir, "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	async := false
	stub := &StubSemanticConflictResolver{Decision: ConflictSupersede}
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		Vectors:              idx,
		Embed:                stubEmbed,
		VectorAsync:          &async,
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
		SemanticConflictK:    5,
	})
	ctx := context.Background()

	old, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd,
		Content: "favorite color is alpha blue",
	})
	if err != nil || old.ID == "" {
		t.Fatalf("old=%+v err=%v", old, err)
	}
	stub.TargetUnitID = old.ID

	// No shared LIKE substring with "alpha blue", but beta≈alpha embedding.
	neu, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd,
		Content: "favorite color is beta azure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.Calls == 0 {
		t.Fatal("expected semantic resolver called via vector peers")
	}
	if neu.ID == "" {
		t.Fatal("expected supersede write")
	}
}

func TestFacade_VectorDeleteOnRemove(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewSQLiteVectorIndex(filepath.Join(dir, "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	async := false
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), Vectors: idx, Embed: stubEmbed, VectorAsync: &async,
	})
	ctx := context.Background()
	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "alpha note",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionRemove, UnitID: hit.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	scored, err := idx.Search(ctx, VectorSearchQuery{
		Scope: ScopeSession, ScopeID: "s1", Embedding: []float32{1, 0, 0}, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range scored {
		if s.UnitID == hit.ID {
			t.Fatalf("vector still present after remove: %#v", scored)
		}
	}
}

func TestFacade_NoVector_Unchanged(t *testing.T) {
	f := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()
	_, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "plain fact",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// recordingIndex counts Upserts for async smoke.
type recordingIndex struct {
	inner VectorIndex
	mu    sync.Mutex
	n     int
}

func (r *recordingIndex) Upsert(ctx context.Context, rec UnitVectorRecord) error {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return r.inner.Upsert(ctx, rec)
}
func (r *recordingIndex) Delete(ctx context.Context, id string) error {
	return r.inner.Delete(ctx, id)
}
func (r *recordingIndex) Search(ctx context.Context, q VectorSearchQuery) ([]ScoredUnitID, error) {
	return r.inner.Search(ctx, q)
}
func (r *recordingIndex) Close() error { return r.inner.Close() }

func TestFacade_VectorAsyncEventuallyIndexes(t *testing.T) {
	dir := t.TempDir()
	inner, err := NewSQLiteVectorIndex(filepath.Join(dir, "v.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	rec := &recordingIndex{inner: inner}
	async := true
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), Vectors: rec, Embed: stubEmbed, VectorAsync: &async,
	})
	ctx := context.Background()
	_, err = f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "alpha async",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := rec.n
		rec.mu.Unlock()
		if n >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("async upsert never ran")
}

// fakeEmbedder maps keywords to fixed vectors so "no shared substring" pairs can still be near.
// Used by the SQLite/in-memory UnitVectorIndex hybrid tests below.
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
	for _, text := range texts {
		vec := []float32{0, 1}
		for key, value := range f.byKey {
			if strings.Contains(text, key) {
				vec = value
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
	if _, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color"}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != afterFirst {
		t.Fatalf("breaker must stop further embeds: %d -> %d", afterFirst, emb.count())
	}
	if stub.Calls == 0 {
		t.Fatal("LIKE fallback should still find peers and run resolver")
	}
}

// E2: D2 关闭仍 Embed + Upsert（与语义 peer 门控解耦）。
func TestFacade_D2Disabled_StillEmbedsAndUpserts(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"hello": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, false)

	hit, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "hello"})
	if err != nil || hit.ID == "" {
		t.Fatalf("add failed: %+v %v", hit, err)
	}
	if emb.count() != 1 {
		t.Fatalf("D2 off must still embed for upsert, got %d", emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 || hits[0].UnitID != hit.ID {
		t.Fatalf("D2 off must still upsert: %+v want %s", hits, hit.ID)
	}
}

// 索引未装配时行为与 P2-D2 现网一致（LIKE）。
func TestFacade_NoVectorIndex_UsesLike(t *testing.T) {
	ctx := context.Background()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{Session: NewSessionMemory(), SemanticConflicts: stub, ToolSemanticConflict: true})

	_, _ = f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color blue"})
	_, _ = f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "favorite color"})
	if stub.Calls == 0 {
		t.Fatal("LIKE path must still work")
	}
}

// Sidecar 指向主表不存在的 ID：hydrate 丢弃，写路径仍成功且不调 resolver。
func TestFacade_VectorHydrate_DropsMissingUnit(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"亮色": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := newVectorFacade(t, emb, idx, stub, true)

	if err := idx.Upsert(ctx, UnitVectorEntry{
		Scope: ScopeSession, ScopeID: "s", UnitID: "ghost-unit-id", Vector: []float32{1, 0},
	}); err != nil {
		t.Fatal(err)
	}

	hit, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "界面用亮色模式"})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID == "" {
		t.Fatal("write must still succeed after filtering stale sidecar hit")
	}
	if stub.Calls != 0 {
		t.Fatalf("stale missing unit must not become a peer, calls=%d peers=%+v", stub.Calls, stub.LastPeers)
	}
}

// Sidecar 指向已 superseded 的 unit：hydrate 按 status 过滤，不调 resolver。
func TestFacade_VectorHydrate_DropsNonActiveUnit(t *testing.T) {
	ctx := context.Background()
	sm := NewSessionMemory()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()
	stub := &StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	f := NewFacade(FacadeConfig{
		Session:              sm,
		SemanticConflicts:    stub,
		ToolSemanticConflict: true,
		UnitVectors:          idx,
		UnitEmbedder:         emb,
	})

	old, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if err != nil || old.ID == "" {
		t.Fatalf("seed failed: %+v %v", old, err)
	}
	if _, err := sm.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s", Action: ActionReplace, UnitID: old.ID, Content: "用户偏好已更新",
	}); err != nil {
		t.Fatalf("supersede via backend: %v", err)
	}
	gotOld, err := sm.Get(ctx, GetRef{Scope: ScopeSession, ScopeID: "s", ID: old.ID})
	if err != nil {
		t.Fatalf("Get(old) = %v", err)
	}
	if st, _ := gotOld.Metadata["status"].(string); st != "superseded" {
		t.Fatalf("old status = %q, want superseded (backend reality)", st)
	}
	// First Remember already indexed old.ID; backend-bypass supersede leaves the
	// sidecar stale — that is the hydrate-filter condition under test.

	hit, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "界面用亮色模式"})
	if err != nil {
		t.Fatal(err)
	}
	if hit.ID == "" {
		t.Fatal("write must still succeed after filtering non-active peer")
	}
	if stub.Calls != 0 {
		t.Fatalf("superseded unit must not become a peer, calls=%d peers=%+v", stub.Calls, stub.LastPeers)
	}
}

func TestFacade_AddUpsertsVector(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	hit, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if err != nil || hit.ID == "" {
		t.Fatalf("add failed: %v", err)
	}
	if emb.count() != 1 {
		t.Fatalf("vector-path add must embed once (reuse for upsert), got %d", emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5})
	if len(hits) != 1 || hits[0].UnitID != hit.ID {
		t.Fatalf("add must upsert vector: %+v", hits)
	}
}

// D1 显式 replace + D2 开启但已熔断：删旧 id，跳过 Upsert（规格 §2.3 D1 表第 4 行）。
func TestFacade_StructuralReplace_D2On_Tripped_DeletesOldSkipsUpsert(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()
	f := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)

	old, _ := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	f.embedTripped.Store(true)
	before := emb.count()
	neu, err := f.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionReplace, UnitID: old.ID, Content: "用户偏好亮色主题"})
	if err != nil || neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("replace failed: %+v %v", neu, err)
	}
	if emb.count() != before {
		t.Fatalf("tripped replace must not embed: %d -> %d", before, emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 0 {
		t.Fatalf("want old deleted and no new vector, got %+v", hits)
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

// E2: D1 显式 replace + D2 关闭：删旧 id，并为新 id Upsert（与 D2 解耦）。
func TestFacade_StructuralReplace_D2Off_DeletesOldAndUpsertsNew(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"深色": {1, 0}, "亮色": {0.99, 0.01}}}
	idx := NewInMemoryUnitVectorIndex()

	// 先用 D2 开着的 facade 播种，保证旧 id 在 sidecar 里确实有向量。
	seed := newVectorFacade(t, emb, idx, &StubSemanticConflictResolver{Decision: ConflictKeepBoth}, true)
	old, _ := seed.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "用户偏好深色主题"})
	if n, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10}); len(n) != 1 {
		t.Fatalf("seed vector missing: %+v", n)
	}

	// 关掉 D2 门（同包可访问私有字段）；此后 replace 仍 Delete 旧 + Upsert 新。
	seed.toolSemanticConflict = false
	before := emb.count()
	neu, err := seed.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s", Action: ActionReplace, UnitID: old.ID, Content: "用户偏好亮色主题"})
	if err != nil || neu.ID == "" || neu.ID == old.ID {
		t.Fatalf("replace failed: %+v %v", neu, err)
	}
	if emb.count() != before+1 {
		t.Fatalf("D2 off replace must embed once for new upsert: %d -> %d", before, emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 10})
	if len(hits) != 1 || hits[0].UnitID != neu.ID {
		t.Fatalf("want old deleted and new upserted, got %+v (old=%s new=%s)", hits, old.ID, neu.ID)
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
