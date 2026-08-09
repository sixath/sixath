package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countingKeyEmbedder struct {
	mu    sync.Mutex
	n     int
	byKey map[string][]float32
	err   error
	empty bool // return empty vector slice on success
}

func (e *countingKeyEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.n++
	if e.err != nil {
		return nil, e.err
	}
	if e.empty {
		return [][]float32{{}}, nil
	}
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec := []float32{0, 1}
		for k, v := range e.byKey {
			if text == k || strings.Contains(text, k) {
				vec = v
				break
			}
		}
		out = append(out, vec)
	}
	return out, nil
}

func (e *countingKeyEmbedder) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.n
}

type failUpsertIndex struct {
	UnitVectorIndex
	failIDs map[string]error
}

func (f *failUpsertIndex) Upsert(ctx context.Context, rec UnitVectorEntry) error {
	if err, ok := f.failIDs[rec.UnitID]; ok {
		return err
	}
	return f.UnitVectorIndex.Upsert(ctx, rec)
}

func seedSessionUnits(t *testing.T, sess *SessionMemory, scope Scope, scopeID, agentID string, contents ...string) []MemoryHit {
	t.Helper()
	hits := make([]MemoryHit, 0, len(contents))
	for _, c := range contents {
		meta := map[string]any{}
		if scope == ScopeUser {
			meta["user_id"] = scopeID
		}
		h, err := sess.Remember(context.Background(), RememberInput{
			Scope: scope, ScopeID: scopeID, AgentID: agentID,
			Action: ActionAdd, Content: c, Metadata: meta,
		})
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, h)
	}
	return hits
}

func TestUnitBackfiller_FillMissing(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	hits := seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a", "unit-b", "unit-c")
	_ = idx.Upsert(ctx, UnitVectorEntry{
		Scope: ScopeSession, ScopeID: "s1", UnitID: hits[0].ID, Vector: []float32{1, 0},
	})
	emb := &countingKeyEmbedder{byKey: map[string][]float32{
		"unit-a": {1, 0}, "unit-b": {0, 1}, "unit-c": {1, 1},
	}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSize: 50, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upserted != 2 || emb.calls() != 2 {
		t.Fatalf("stats=%+v emb=%d", st, emb.calls())
	}
	if st.Scanned != 3 || st.Missing != 2 {
		t.Fatalf("scanned/missing: %+v", st)
	}
}

func TestUnitBackfiller_ForceRebuild(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	hits := seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a", "unit-b")
	for _, h := range hits {
		_ = idx.Upsert(ctx, UnitVectorEntry{
			Scope: ScopeSession, ScopeID: "s1", UnitID: h.ID, Vector: []float32{9, 9},
		})
	}
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"unit-a": {1, 0}, "unit-b": {0, 1}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, Force: true, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Upserted != 2 || emb.calls() != 2 {
		t.Fatalf("stats=%+v emb=%d err=%v", st, emb.calls(), err)
	}
}

func TestUnitBackfiller_DryRun(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a", "unit-b")
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"unit-a": {1, 0}, "unit-b": {0, 1}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, DryRun: true, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Missing != 2 || st.Upserted != 0 || emb.calls() != 0 {
		t.Fatalf("stats=%+v emb=%d", st, emb.calls())
	}
	has, _ := idx.Has(ctx, ScopeSession, "s1", []string{"x"})
	_ = has
	// index still empty for real unit ids
	page, _ := sess.List(ctx, ListFilter{Scope: ScopeSession, Status: "active", Limit: 10})
	ids := make([]string, len(page))
	for i, h := range page {
		ids[i] = h.ID
	}
	present, err := idx.Has(ctx, ScopeSession, "s1", ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if present[id] {
			t.Fatalf("dry-run wrote vector for %s", id)
		}
	}
}

func TestUnitBackfiller_TripSharedBreaker(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a", "unit-b")
	tripped := &atomic.Bool{}
	emb := &countingKeyEmbedder{err: errors.New("upstream down")}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession}, EmbedTripped: tripped,
	})
	st, err := bf.Run(ctx)
	if err != nil {
		t.Fatalf("expected (stats,nil), got err=%v", err)
	}
	if !st.Tripped || !tripped.Load() {
		t.Fatalf("stats=%+v tripped=%v", st, tripped.Load())
	}
	if emb.calls() != 1 {
		t.Fatalf("should stop after first trip, emb=%d", emb.calls())
	}
}

func TestUnitBackfiller_EmptyVectorTrips(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a")
	emb := &countingKeyEmbedder{empty: true}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || !st.Tripped {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
}

func TestUnitBackfiller_EmbedModelUnavailableSkipped(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "unit-a", "unit-b")
	emb := &countingKeyEmbedder{err: fmt.Errorf("wrap: %w", ErrEmbedModelUnavailable)}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Tripped || st.Skipped != 2 || st.Upserted != 0 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
	if emb.calls() != 2 {
		t.Fatalf("emb=%d", emb.calls())
	}
}

func TestUnitBackfiller_MissingScopeIDSkipped(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	// user without Metadata["user_id"] — SessionMemory does not auto-fill
	_, err := sess.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", AgentID: "ag1",
		Action: ActionAdd, Content: "orphan",
	})
	if err != nil {
		t.Fatal(err)
	}
	emb := &countingKeyEmbedder{}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeUser},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Skipped != 1 || emb.calls() != 0 {
		t.Fatalf("stats=%+v emb=%d err=%v", st, emb.calls(), err)
	}
}

func TestUnitBackfiller_UserScopeFill(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeUser, "u1", "ag1", "pref-a")
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"pref-a": {1, 0}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeUser},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Upserted != 1 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
}

func TestUnitBackfiller_Pagination(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	// Distinct updatedAt so SessionMemory offset pages stay stable across List calls.
	for _, c := range []string{"a", "b", "c", "d", "e"} {
		_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", c)
		time.Sleep(2 * time.Millisecond)
	}
	emb := &countingKeyEmbedder{byKey: map[string][]float32{
		"a": {1}, "b": {1}, "c": {1}, "d": {1}, "e": {1},
	}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSize: 2, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Scanned != 5 || st.Upserted != 5 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
}

func TestUnitBackfiller_MultiScopeIDGroups(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_ = seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "s1-a")
	_ = seedSessionUnits(t, sess, ScopeSession, "s2", "ag1", "s2-a")
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"s1-a": {1, 0}, "s2-a": {0, 1}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Upserted != 2 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
	page, _ := sess.List(ctx, ListFilter{Scope: ScopeSession, Status: "active", Limit: 10})
	for _, h := range page {
		sid := metaString(h.Metadata, "source_session_id")
		present, err := idx.Has(ctx, ScopeSession, sid, []string{h.ID})
		if err != nil || !present[h.ID] {
			t.Fatalf("missing vector for %s scope=%s present=%v err=%v", h.ID, sid, present, err)
		}
	}
}

func TestUnitBackfiller_UpsertFailed(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	base := NewInMemoryUnitVectorIndex()
	defer base.Close()
	hits := seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "ok", "bad")
	idx := &failUpsertIndex{
		UnitVectorIndex: base,
		failIDs:         map[string]error{hits[1].ID: errors.New("disk full")},
	}
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"ok": {1}, "bad": {1}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Failed != 1 || st.Upserted != 1 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
}

func TestUnitBackfiller_DimMismatch(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	base := NewInMemoryUnitVectorIndex()
	defer base.Close()
	hits := seedSessionUnits(t, sess, ScopeSession, "s1", "ag1", "a")
	idx := &failUpsertIndex{
		UnitVectorIndex: base,
		failIDs: map[string]error{
			hits[0].ID: fmt.Errorf("%w: vector dim 2 != index dim 3", ErrVectorDimMismatch),
		},
	}
	emb := &countingKeyEmbedder{byKey: map[string][]float32{"a": {1, 0}}}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if !errors.Is(err, ErrVectorDimMismatch) {
		t.Fatalf("want dim mismatch, stats=%+v err=%v", st, err)
	}
}

func TestUnitBackfiller_EmptyContentSkipped(t *testing.T) {
	ctx := context.Background()
	sess := NewSessionMemory()
	idx := NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_, err := sess.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "ag1",
		Action: ActionAdd, Content: "   ",
	})
	if err != nil {
		t.Fatal(err)
	}
	emb := &countingKeyEmbedder{}
	bf := NewUnitBackfiller(BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []Scope{ScopeSession},
	})
	st, err := bf.Run(ctx)
	if err != nil || st.Skipped != 1 || emb.calls() != 0 {
		t.Fatalf("stats=%+v emb=%d err=%v", st, emb.calls(), err)
	}
}

func TestUnitBackfiller_Defaults(t *testing.T) {
	bf := NewUnitBackfiller(BackfillConfig{
		Units: NewSessionMemory(), Index: NewInMemoryUnitVectorIndex(), Embedder: &countingKeyEmbedder{},
	})
	if bf.cfg.BatchSize != defaultBackfillBatchSize {
		t.Fatalf("BatchSize=%d", bf.cfg.BatchSize)
	}
	if len(bf.cfg.Scopes) != 2 {
		t.Fatalf("Scopes=%v", bf.cfg.Scopes)
	}
}
