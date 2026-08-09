package tool

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeMcpClient struct {
	closed atomic.Bool
}

func (f *fakeMcpClient) Initialize(ctx context.Context) error { return nil }
func (f *fakeMcpClient) ListTools(ctx context.Context) ([]Tool, error) {
	return []Tool{{Name: "echo", Description: "echo"}}, nil
}
func (f *fakeMcpClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return "ok", nil
}
func (f *fakeMcpClient) Close(ctx context.Context) error {
	f.closed.Store(true)
	return nil
}

func withFakeStdioFactory(t *testing.T, spawns *int32) {
	t.Helper()
	old := stdioClientFactory
	t.Cleanup(func() { stdioClientFactory = old })
	stdioClientFactory = func(cfg *McpConfig) (mcpClient, error) {
		atomic.AddInt32(spawns, 1)
		return &fakeMcpClient{}, nil
	}
}

func TestMcpProcessPool_ReusesSameFingerprint(t *testing.T) {
	var spawns int32
	withFakeStdioFactory(t, &spawns)
	p := NewMcpProcessPool(McpPoolOptions{IdleTTL: time.Hour})
	t.Cleanup(p.stop)

	cfg := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Backend:   "mark3labs",
	}
	_, release1, err := p.Acquire(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, release2, err := p.Acquire(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&spawns); got != 1 {
		t.Fatalf("spawns=%d want 1", got)
	}
	release1()
	release2()
}

func TestMcpProcessPool_FingerprintChangeRespawns(t *testing.T) {
	var spawns int32
	withFakeStdioFactory(t, &spawns)
	p := NewMcpProcessPool(McpPoolOptions{IdleTTL: time.Hour})
	t.Cleanup(p.stop)

	cfg := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Env:       map[string]string{"FOO": "a"},
		Backend:   "mark3labs",
	}
	_, release1, err := p.Acquire(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	release1()

	cfg2 := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Env:       map[string]string{"FOO": "b"},
		Backend:   "mark3labs",
	}
	_, release2, err := p.Acquire(context.Background(), cfg2)
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if got := atomic.LoadInt32(&spawns); got != 2 {
		t.Fatalf("spawns=%d want 2", got)
	}
}

func TestMcpProcessPool_FingerprintChangeWithActiveLease(t *testing.T) {
	var spawns int32
	clients := make([]*fakeMcpClient, 0, 2)
	old := stdioClientFactory
	t.Cleanup(func() { stdioClientFactory = old })
	stdioClientFactory = func(cfg *McpConfig) (mcpClient, error) {
		atomic.AddInt32(&spawns, 1)
		f := &fakeMcpClient{}
		clients = append(clients, f)
		return f, nil
	}

	p := NewMcpProcessPool(McpPoolOptions{IdleTTL: time.Hour})
	t.Cleanup(p.stop)

	cfgA := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Env:       map[string]string{"FOO": "1"},
		Backend:   "mark3labs",
	}
	cliA, releaseA, err := p.Acquire(context.Background(), cfgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Fatalf("clients=%d", len(clients))
	}
	first := clients[0]

	cfgB := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Env:       map[string]string{"FOO": "2"},
		Backend:   "mark3labs",
	}
	cliB, releaseB, err := p.Acquire(context.Background(), cfgB)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&spawns); got != 2 {
		t.Fatalf("spawns=%d want 2", got)
	}
	if first.closed.Load() {
		t.Fatal("first client closed while lease still held")
	}
	if _, err := cliA.CallTool(context.Background(), "echo", nil); err != nil {
		t.Fatalf("first client CallTool after fingerprint replace: %v", err)
	}
	if cliA == cliB {
		t.Fatal("expected distinct clients for different fingerprints")
	}

	releaseA()
	if !first.closed.Load() {
		t.Fatal("first client should close when its last retiring lease is released")
	}
	second := clients[1]
	if second.closed.Load() {
		t.Fatal("second client should still be active")
	}
	releaseB()
	// Active entry stays until idle TTL; not required to close immediately.
	_ = second
}

func TestMcpProcessPool_IdleEvicts(t *testing.T) {
	var spawns int32
	withFakeStdioFactory(t, &spawns)
	p := NewMcpProcessPool(McpPoolOptions{IdleTTL: 20 * time.Millisecond})
	t.Cleanup(p.stop)

	cfg := &McpConfig{
		Id:        "s1",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"x"},
		Backend:   "mark3labs",
	}
	_, release, err := p.Acquire(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	release()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		p.sweep()
		p.mu.Lock()
		_, still := p.entries[cfg.Id]
		p.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, release2, err := p.Acquire(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if got := atomic.LoadInt32(&spawns); got != 2 {
		t.Fatalf("spawns=%d want 2 after idle eviction", got)
	}
}

func TestStdioMcp_ListAndCall(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("testdata", "mcp_stdio_fixture.js"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &McpConfig{
		Id:        "fixture",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{fixture},
		Backend:   "mark3labs",
	}
	p := NewMcpProcessPool(McpPoolOptions{IdleTTL: time.Minute})
	t.Cleanup(p.stop)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cli, release, err := p.Acquire(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tools, err := cli.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools=%+v", tools)
	}
	p.StoreTools(cfg.Id, tools)

	out, err := cli.CallTool(ctx, "echo", map[string]any{"text": "hello-stdio"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-stdio") {
		t.Fatalf("call result=%q", out)
	}

	cached, ok := p.CachedTools(cfg.Id)
	if !ok || len(cached) != 1 || cached[0].Name != "echo" {
		t.Fatalf("cached=%v ok=%v", cached, ok)
	}
}
