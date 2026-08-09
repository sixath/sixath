package lsp

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeServer struct {
	ensureReadyCount atomic.Int32
	closeCount       atomic.Int32
}

func (f *fakeServer) EnsureReady(ctx context.Context, root string) error {
	f.ensureReadyCount.Add(1)
	return nil
}

func (f *fakeServer) Definition(ctx context.Context, root, relPath string, pos Position) ([]Location, error) {
	return []Location{{Repo: "test", File: "a.go", Line: 1, Character: 0}}, nil
}

func (f *fakeServer) References(ctx context.Context, root, relPath string, pos Position, includeDeclaration bool) ([]Location, error) {
	return nil, nil
}

func (f *fakeServer) Close(ctx context.Context) error {
	f.closeCount.Add(1)
	return nil
}

func TestPool_ReusesServerForSameRoot(t *testing.T) {
	t.Parallel()

	var factoryCalls atomic.Int32
	var lastFake fakeServer

	factory := func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		factoryCalls.Add(1)
		return &lastFake, nil
	}

	pool := NewPool(factory, ServerOpts{})
	ctx := context.Background()
	root := t.TempDir()

	srv1, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	srv2, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if srv1 != srv2 {
		t.Fatal("expected same server instance")
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("factory calls = %d, want 1", factoryCalls.Load())
	}
	if lastFake.ensureReadyCount.Load() != 1 {
		t.Fatalf("EnsureReady calls = %d, want 1", lastFake.ensureReadyCount.Load())
	}

	locs, err := srv1.Definition(ctx, root, "a.go", Position{Line: 0})
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].File != "a.go" {
		t.Fatalf("unexpected locations: %+v", locs)
	}
}

func TestNormalizeRoot_WindowsPathVariants(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}

	dir := t.TempDir()
	vol := filepath.VolumeName(dir)
	if vol == "" {
		t.Skip("no volume in temp dir")
	}

	backslash := vol + `\pool-test\foo`
	slash := vol + `/pool-test/foo`

	if got := NormalizeRoot(backslash); got != NormalizeRoot(slash) {
		t.Fatalf("normalize mismatch: %q vs %q", NormalizeRoot(backslash), NormalizeRoot(slash))
	}
}

func TestPool_FactoryFailureThenRebuild(t *testing.T) {
	t.Parallel()

	var factoryCalls atomic.Int32
	root := t.TempDir()
	ctx := context.Background()

	factory := func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		n := factoryCalls.Add(1)
		if n == 1 {
			return nil, errors.New("factory failed")
		}
		return &fakeServer{}, nil
	}

	pool := NewPool(factory, ServerOpts{})

	if _, err := pool.Get(ctx, root); err == nil {
		t.Fatal("expected factory error")
	}
	if _, err := pool.Get(ctx, root); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if factoryCalls.Load() != 2 {
		t.Fatalf("factory calls = %d, want 2", factoryCalls.Load())
	}
}

func TestPool_MarkDeadThenRebuild(t *testing.T) {
	t.Parallel()

	var factoryCalls atomic.Int32
	root := t.TempDir()
	ctx := context.Background()

	factory := func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		factoryCalls.Add(1)
		return &fakeServer{}, nil
	}

	pool := NewPool(factory, ServerOpts{})

	srv1, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	pool.MarkDead(root)

	srv2, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("second Get after MarkDead: %v", err)
	}
	if srv1 == srv2 {
		t.Fatal("expected new server after MarkDead")
	}
	if factoryCalls.Load() != 2 {
		t.Fatalf("factory calls = %d, want 2", factoryCalls.Load())
	}
}

func TestPool_CloseThenGetErrors(t *testing.T) {
	t.Parallel()

	pool := NewPool(func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		return &fakeServer{}, nil
	}, ServerOpts{})

	root := t.TempDir()
	ctx := context.Background()

	if _, err := pool.Get(ctx, root); err != nil {
		t.Fatalf("Get before Close: %v", err)
	}
	if err := pool.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := pool.Get(ctx, root); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected ErrPoolClosed, got %v", err)
	}
}

func TestPool_ConcurrentGetSameRoot(t *testing.T) {
	t.Parallel()

	var factoryCalls atomic.Int32
	releaseFactory := make(chan struct{})

	factory := func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		if factoryCalls.Add(1) == 1 {
			<-releaseFactory
		}
		return &fakeServer{}, nil
	}

	pool := NewPool(factory, ServerOpts{})
	root := t.TempDir()
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	servers := make([]LanguageServer, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			servers[i], errs[i] = pool.Get(ctx, root)
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	close(releaseFactory)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Get[%d]: %v", i, err)
		}
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("factory calls = %d, want 1", factoryCalls.Load())
	}
	first := servers[0]
	for i, srv := range servers[1:] {
		if srv != first {
			t.Fatalf("server[%d] != server[0]", i+1)
		}
	}
}

func TestPool_CloseRacesInFlightGet(t *testing.T) {
	t.Parallel()

	releaseFactory := make(chan struct{})
	var created fakeServer

	pool := NewPool(func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error) {
		<-releaseFactory
		return &created, nil
	}, ServerOpts{})

	root := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := pool.Get(context.Background(), root)
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(releaseFactory)

	if err := <-done; !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("expected ErrPoolClosed, got %v", err)
	}
	if created.closeCount.Load() != 1 {
		t.Fatalf("close count = %d, want 1 (no leaked server)", created.closeCount.Load())
	}
}
