package chat

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/memory"
)

func TestStartUnitVectorBackfill_OnceNoOpWithoutIndex(t *testing.T) {
	t.Cleanup(func() {
		restoreTestVectorDefaults(t)
		SetMemoryAgentGetter(nil)
		resetUnitVectorBackfillForTest()
		memoryEmbedTripped.Store(false)
	})
	resetUnitVectorBackfillForTest()
	SetMemoryVectorConfig(&config.MemoryVector{Provider: "none"})

	var launches atomic.Int32
	backfillLaunch = func(memory.SessionUnitsBackend, memory.UnitVectorIndex, memory.UnitEmbedder) {
		launches.Add(1)
	}

	sess := memory.NewSessionMemory()
	StartUnitVectorBackfill(sess)
	StartUnitVectorBackfill(sess)
	if backfillStartN != 1 {
		t.Fatalf("Once should arm once, got %d", backfillStartN)
	}
	if launches.Load() != 0 {
		t.Fatal("provider=none must not launch backfill")
	}
}

func TestStartUnitVectorBackfill_RunsOnceWithIndex(t *testing.T) {
	resetUnitVectorBackfillForTest()
	dir := t.TempDir()
	// Register after TempDir so LIFO closes the sqlite handle before TempDir RemoveAll.
	t.Cleanup(func() {
		restoreTestVectorDefaults(t)
		SetMemoryAgentGetter(nil)
		resetUnitVectorBackfillForTest()
		memoryEmbedTripped.Store(false)
	})
	SetMemoryVectorDataRoot(dir)
	SetMemoryVectorConfig(nil) // sqlite
	SetMemoryAgentGetter(stubAgentGetter{})

	var launches atomic.Int32
	backfillLaunch = func(memory.SessionUnitsBackend, memory.UnitVectorIndex, memory.UnitEmbedder) {
		launches.Add(1)
	}

	sess := memory.NewSessionMemory()
	StartUnitVectorBackfill(sess)
	StartUnitVectorBackfill(sess)

	if backfillStartN != 1 {
		t.Fatalf("startN=%d", backfillStartN)
	}
	if launches.Load() != 1 {
		t.Fatalf("launches=%d", launches.Load())
	}
}

func TestDynamicUnitEmbedder_UnavailableWrapsSentinel(t *testing.T) {
	t.Cleanup(func() {
		SetMemoryAgentGetter(nil)
		storedExtractionYAML = nil
	})
	SetMemoryAgentGetter(nil)
	storedExtractionYAML = nil

	e := &dynamicUnitEmbedder{}
	_, err := e.Embed(context.Background(), "", []string{"hi"})
	if !errors.Is(err, memory.ErrEmbedModelUnavailable) {
		t.Fatalf("want ErrEmbedModelUnavailable, got %v", err)
	}
}

func TestUnitBackfiller_UnavailableDoesNotTripSharedBreaker(t *testing.T) {
	t.Cleanup(func() {
		memoryEmbedTripped.Store(false)
	})
	memoryEmbedTripped.Store(false)
	ctx := context.Background()
	sess := memory.NewSessionMemory()
	idx := memory.NewInMemoryUnitVectorIndex()
	defer idx.Close()
	_, err := sess.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeSession, ScopeID: "s1", AgentID: "ag1",
		Action: memory.ActionAdd, Content: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	emb := &dynamicUnitEmbedder{} // no getter / aux → unavailable
	bf := memory.NewUnitBackfiller(memory.BackfillConfig{
		Units: sess, Index: idx, Embedder: emb, BatchSleep: 0,
		Scopes: []memory.Scope{memory.ScopeSession}, EmbedTripped: memoryEmbedTripped,
	})
	st, err := bf.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tripped || memoryEmbedTripped.Load() {
		t.Fatalf("unavailable model must skip without trip: stats=%+v", st)
	}
	if st.Skipped < 1 {
		t.Fatalf("expected Skipped, stats=%+v", st)
	}
}
