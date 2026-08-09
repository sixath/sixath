package lsp

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ErrPoolClosed is returned when Get is called on a closed pool.
var ErrPoolClosed = errors.New("lsp pool closed")

type entry struct {
	mu     sync.Mutex
	server LanguageServer
}

// Pool reuses LanguageServer instances keyed by normalized workspace root.
type Pool struct {
	factory ServerFactory
	opts    ServerOpts

	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
}

// NewPool creates a per-registry language server pool.
func NewPool(factory ServerFactory, opts ServerOpts) *Pool {
	return &Pool{
		factory: factory,
		opts:    opts,
		entries: make(map[string]*entry),
	}
}

// NormalizeRoot canonicalizes a workspace root for pool lookup.
func NormalizeRoot(root string) string {
	cleaned := filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}

// Get returns a ready language server for root, creating one if needed.
func (p *Pool) Get(ctx context.Context, root string) (LanguageServer, error) {
	key := NormalizeRoot(root)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	e, ok := p.entries[key]
	if !ok {
		e = &entry{}
		p.entries[key] = e
	}
	p.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.server != nil {
		return e.server, nil
	}

	srv, err := p.factory(ctx, root, p.opts)
	if err != nil {
		return nil, err
	}

	if err := srv.EnsureReady(ctx, root); err != nil {
		_ = srv.Close(ctx)
		return nil, err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = srv.Close(ctx)
		return nil, ErrPoolClosed
	}
	if _, ok := p.entries[key]; !ok {
		p.entries[key] = e
	}
	e.server = srv
	p.mu.Unlock()

	return srv, nil
}

// MarkDead removes and closes the server for root so the next Get rebuilds it.
func (p *Pool) MarkDead(root string) {
	key := NormalizeRoot(root)

	p.mu.Lock()
	e, ok := p.entries[key]
	if ok {
		delete(p.entries, key)
	}
	p.mu.Unlock()
	if !ok {
		return
	}

	e.mu.Lock()
	srv := e.server
	e.server = nil
	e.mu.Unlock()

	if srv != nil {
		_ = srv.Close(context.Background())
	}
}

// Close closes all pooled servers and marks the pool unusable.
func (p *Pool) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	entries := make([]*entry, 0, len(p.entries))
	for _, e := range p.entries {
		entries = append(entries, e)
	}
	p.entries = make(map[string]*entry)
	p.mu.Unlock()

	var firstErr error
	for _, e := range entries {
		if !e.mu.TryLock() {
			// In-flight Get holds the entry lock; it will Close the server after factory.
			continue
		}
		srv := e.server
		e.server = nil
		e.mu.Unlock()
		if srv == nil {
			continue
		}
		if err := srv.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
