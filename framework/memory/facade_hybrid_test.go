package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// E2: D2 (ToolSemanticConflict) off and no SemanticConflicts still Upserts on successful Add
// when vectorReady — decoupled from the E1 D2 gate.
func TestFacade_Add_D2Off_StillUpsertsVector(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"hello": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	f := NewFacade(FacadeConfig{
		Session:              NewSessionMemory(),
		ToolSemanticConflict: false,
		SemanticConflicts:    nil,
		UnitVectors:          idx,
		UnitEmbedder:         emb,
	})

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s", Action: ActionAdd, Content: "hello world",
	})
	if err != nil || hit.ID == "" {
		t.Fatalf("add failed: %+v %v", hit, err)
	}
	if emb.count() != 1 {
		t.Fatalf("D2 off add must still embed once for upsert, got %d", emb.count())
	}
	hits, _ := idx.Search(ctx, UnitVectorQuery{
		Scope: ScopeSession, ScopeID: "s", Vector: []float32{1, 0}, Limit: 5,
	})
	if len(hits) != 1 || hits[0].UnitID != hit.ID {
		t.Fatalf("D2 off add must upsert vector: %+v want unit %s", hits, hit.ID)
	}
}

func TestFacade_HybridRecall_FindsSemanticNeighbor(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	emb := &fakeEmbedder{byKey: map[string][]float32{
		"dark mode":             {1, 0},
		"dark theme preference": {0.99, 0.01},
		"meeting":               {0, 1},
	}}
	f := NewFacade(FacadeConfig{
		Session:      NewSessionMemory(),
		UnitVectors:  idx,
		UnitEmbedder: emb,
	})

	target, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "UI uses dark mode",
	})
	if err != nil || target.ID == "" {
		t.Fatalf("seed target: %+v %v", target, err)
	}
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "unrelated meeting notes",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "dark theme preference", Limit: 5, AgentID: "a1",
		MinScore: 0.9, // must be ignored on units hybrid path
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.ID == target.ID {
			found = true
			if h.Score <= 0 {
				t.Fatalf("want RRF score > 0, got %v", h.Score)
			}
		}
	}
	if !found {
		t.Fatalf("semantic neighbor missing (MinScore must be ignored): %+v", hits)
	}
}

func TestFacade_HybridRecall_LikeOnlyHitPreserved(t *testing.T) {
	ctx := context.Background()
	idx := NewInMemoryUnitVectorIndex()
	emb := &fakeEmbedder{byKey: map[string][]float32{"alpha keyword": {1, 0}}}
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session: sess, UnitVectors: idx, UnitEmbedder: emb,
	})

	hit, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "alpha keyword present",
	})
	if err != nil || hit.ID == "" {
		t.Fatalf("seed: %+v %v", hit, err)
	}
	if err := idx.Delete(ctx, ScopeSession, "s1", hit.ID); err != nil {
		t.Fatal(err)
	}

	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "alpha keyword", Limit: 5, AgentID: "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.ID == hit.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("LIKE-only unit missing after vector delete: %+v", hits)
	}
}

func TestFacade_HybridRecall_GateFalse_SkipsEmbed(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"shared": {1, 0}}}
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session: sess, UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
		HybridRecall: func(context.Context, string) bool { return false },
	})
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "shared token value",
	}); err != nil {
		t.Fatal(err)
	}
	before := emb.count()
	q := RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "shared", Limit: 5, AgentID: "a1",
	}
	got, err := f.Recall(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if emb.count() != before {
		t.Fatalf("gate false must not embed on recall: before=%d after=%d", before, emb.count())
	}
	want, err := sess.Recall(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("gate false results len=%d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].ID != want[i].ID {
			t.Fatalf("gate false mismatch at %d: got=%s want=%s", i, got[i].ID, want[i].ID)
		}
	}
}

func TestFacade_HybridRecall_GateNilEmptyAgentID_RunsHybrid(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{
		"dark mode":             {1, 0},
		"dark theme preference": {0.99, 0.01},
	}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
		// HybridRecall nil → always allow
	})
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1",
		Action: ActionAdd, Content: "UI uses dark mode",
	}); err != nil {
		t.Fatal(err)
	}
	before := emb.count()
	_, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "dark theme preference", Limit: 5, // AgentID empty
	})
	if err != nil {
		t.Fatal(err)
	}
	if emb.count() <= before {
		t.Fatalf("nil gate + empty AgentID must run hybrid embed: before=%d after=%d", before, emb.count())
	}
}

func TestFacade_HybridRecall_GateFalse_BlankAgentID_StillRunsHybrid(t *testing.T) {
	ctx := context.Background()
	for _, agentID := range []string{"", " "} {
		t.Run("agentID="+agentID, func(t *testing.T) {
			emb := &fakeEmbedder{byKey: map[string][]float32{
				"dark mode":             {1, 0},
				"dark theme preference": {0.99, 0.01},
			}}
			gateCalls := 0
			f := NewFacade(FacadeConfig{
				Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
				HybridRecall: func(context.Context, string) bool {
					gateCalls++
					return false
				},
			})
			if _, err := f.Remember(ctx, RememberInput{
				Scope: ScopeSession, ScopeID: "s1",
				Action: ActionAdd, Content: "UI uses dark mode",
			}); err != nil {
				t.Fatal(err)
			}
			before := emb.count()
			_, err := f.Recall(ctx, RecallQuery{
				Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
				Query: "dark theme preference", Limit: 5, AgentID: agentID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if gateCalls != 0 {
				t.Fatalf("blank AgentID must not call hybrid gate, got %d calls", gateCalls)
			}
			if emb.count() <= before {
				t.Fatalf("blank AgentID must bypass false gate and embed: before=%d after=%d", before, emb.count())
			}
		})
	}
}

func TestFacade_HybridRecall_EmptyQuery_NoEmbed(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"hello": {1, 0}}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
	})
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	before := emb.count()
	if _, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "   ", Limit: 5, AgentID: "a1",
	}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != before {
		t.Fatalf("empty query must not embed: before=%d after=%d", before, emb.count())
	}
}

func TestFacade_HybridRecall_EmbedError_TripsAndTruncates(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"item": {1, 0}}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
	})
	for i := 0; i < 6; i++ {
		if _, err := f.Remember(ctx, RememberInput{
			Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
			Action: ActionAdd, Content: "item shared " + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	emb.mu.Lock()
	emb.err = errors.New("embed unavailable")
	emb.mu.Unlock()
	before := emb.count()

	limit := 2
	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "item", Limit: limit, AgentID: "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > limit {
		t.Fatalf("fail-open must truncate to limit=%d, got %d", limit, len(hits))
	}
	if emb.count() != before+1 {
		t.Fatalf("first failing recall must embed once: before=%d after=%d", before, emb.count())
	}

	afterTrip := emb.count()
	if _, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "item", Limit: limit, AgentID: "a1",
	}); err != nil {
		t.Fatal(err)
	}
	if emb.count() != afterTrip {
		t.Fatalf("embedTripped must skip further embeds: %d -> %d", afterTrip, emb.count())
	}
}

func TestFacade_HybridRecall_EmbedTimeout_DoesNotTrip(t *testing.T) {
	ctx := context.Background()
	old := hybridEmbedTimeout
	hybridEmbedTimeout = 30 * time.Millisecond
	defer func() { hybridEmbedTimeout = old }()

	// Seed with a working embedder via write path, then swap to waitCtx for recall.
	seedEmb := &fakeEmbedder{byKey: map[string][]float32{"shared": {1, 0}}}
	idx := NewInMemoryUnitVectorIndex()
	sess := NewSessionMemory()
	f := NewFacade(FacadeConfig{
		Session: sess, UnitVectors: idx, UnitEmbedder: seedEmb,
	})
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "shared token",
	}); err != nil {
		t.Fatal(err)
	}

	wait := &waitCtxEmbedder{}
	f.unitEmbedder = wait

	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "shared", Limit: 2, AgentID: "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("timeout fail-open must still return LIKE hits")
	}
	if wait.count() != 1 {
		t.Fatalf("first recall embed calls=%d want 1", wait.count())
	}

	if _, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "shared", Limit: 2, AgentID: "a1",
	}); err != nil {
		t.Fatal(err)
	}
	if wait.count() != 2 {
		t.Fatalf("timeout must not trip: second recall embed calls=%d want 2", wait.count())
	}
}

func TestFacade_HybridRecall_Cache_SameQueryEmbedsOnce(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{
		"dark mode":             {1, 0},
		"dark theme preference": {0.99, 0.01},
	}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
	})
	if _, err := f.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
		Action: ActionAdd, Content: "UI uses dark mode",
	}); err != nil {
		t.Fatal(err)
	}
	before := emb.count()
	q := RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "dark theme preference", Limit: 5, AgentID: "a1",
	}
	if _, err := f.Recall(ctx, q); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Recall(ctx, q); err != nil {
		t.Fatal(err)
	}
	if emb.count() != before+1 {
		t.Fatalf("same agentID+query must embed once on recall: before=%d after=%d", before, emb.count())
	}
}

func TestFacade_HybridRecall_FailOpen_TruncatesToLimit(t *testing.T) {
	ctx := context.Background()
	emb := &fakeEmbedder{byKey: map[string][]float32{"token": {1, 0}}}
	f := NewFacade(FacadeConfig{
		Session: NewSessionMemory(), UnitVectors: NewInMemoryUnitVectorIndex(), UnitEmbedder: emb,
	})
	for i := 0; i < 8; i++ {
		if _, err := f.Remember(ctx, RememberInput{
			Scope: ScopeSession, ScopeID: "s1", AgentID: "a1",
			Action: ActionAdd, Content: "token row " + string(rune('0'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	emb.mu.Lock()
	emb.err = errors.New("boom")
	emb.mu.Unlock()

	limit := 3
	hits, err := f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits,
		Query: "token", Limit: limit, AgentID: "a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > limit {
		t.Fatalf("LIKE returned 2*limit candidates must truncate: len=%d limit=%d", len(hits), limit)
	}
	if len(hits) == 0 {
		t.Fatal("expected LIKE fail-open hits")
	}
}

// waitCtxEmbedder blocks until ctx is done, then returns ctx.Err().
type waitCtxEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (e *waitCtxEmbedder) Embed(ctx context.Context, _ string, _ []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (e *waitCtxEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestFacade_SharedEmbedTripped(t *testing.T) {
	ctx := context.Background()
	tripped := &atomic.Bool{}
	idx := NewInMemoryUnitVectorIndex()
	_ = idx.Upsert(ctx, UnitVectorEntry{Scope: ScopeSession, ScopeID: "s1", UnitID: "u1", Vector: []float32{1, 0}})
	emb := &fakeEmbedder{err: errors.New("no embed")}
	f := NewFacade(FacadeConfig{
		Session:      NewSessionMemory(),
		UnitVectors:  idx,
		UnitEmbedder: emb,
		EmbedTripped: tripped,
	})
	_, _ = f.Recall(ctx, RecallQuery{
		Scope: ScopeSession, ScopeID: "s1", Source: SourceUnits, Query: "q", AgentID: "a1",
	})
	if !tripped.Load() {
		t.Fatal("shared breaker not tripped by Facade")
	}
	if f.vectorReady() {
		t.Fatal("Facade must observe shared tripped breaker")
	}
}
