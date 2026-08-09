package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// McpPoolOptions configures idle eviction for stdio MCP processes.
type McpPoolOptions struct {
	IdleTTL time.Duration
}

// McpProcessPool caches stdio MCP clients by server id with refcount + idle TTL.
type McpProcessPool struct {
	mu       sync.Mutex
	opts     McpPoolOptions
	entries  map[string]*mcpPoolEntry // active entry per server id
	retiring map[string]*mcpPoolEntry // detached entries still leased; key = id+"\x00"+fp
	stopCh   chan struct{}
	stopped  sync.Once
}

type mcpPoolEntry struct {
	client      mcpClient
	serverID    string
	fingerprint string
	refcount    int
	lastUsed    time.Time
	tools       []Tool
}

func retiringKey(serverID, fp string) string {
	return serverID + "\x00" + fp
}

// stdioClientFactory creates a stdio mcpClient for a config. Tests may replace it.
var stdioClientFactory = func(cfg *McpConfig) (mcpClient, error) {
	return newMark3labsStdioClient(cfg)
}

var (
	defaultMcpPool     *McpProcessPool
	defaultMcpPoolOnce sync.Once
)

// NewMcpProcessPool creates a process pool and starts the idle sweeper.
func NewMcpProcessPool(opts McpPoolOptions) *McpProcessPool {
	if opts.IdleTTL <= 0 {
		opts.IdleTTL = 5 * time.Minute
	}
	p := &McpProcessPool{
		opts:     opts,
		entries:  make(map[string]*mcpPoolEntry),
		retiring: make(map[string]*mcpPoolEntry),
		stopCh:   make(chan struct{}),
	}
	go p.sweepLoop()
	return p
}

// DefaultMcpProcessPool returns the process-wide pool (IdleTTL from SATH_MCP_STDIO_IDLE_TTL or 5m).
func DefaultMcpProcessPool() *McpProcessPool {
	defaultMcpPoolOnce.Do(func() {
		ttl := 5 * time.Minute
		if s := strings.TrimSpace(os.Getenv("SATH_MCP_STDIO_IDLE_TTL")); s != "" {
			if d, err := time.ParseDuration(s); err == nil && d > 0 {
				ttl = d
			}
		}
		defaultMcpPool = NewMcpProcessPool(McpPoolOptions{IdleTTL: ttl})
	})
	return defaultMcpPool
}

// Fingerprint hashes transport + endpoint/command/args/env/backend for reuse detection.
func Fingerprint(cfg *McpConfig) string {
	if cfg == nil {
		return ""
	}
	h := sha256.New()
	writeFP := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	writeFP(strings.ToLower(strings.TrimSpace(cfg.Transport)))
	writeFP(strings.TrimSpace(cfg.Endpoint))
	writeFP(strings.TrimSpace(cfg.Command))
	writeFP(strings.ToLower(strings.TrimSpace(cfg.Backend)))
	for _, a := range cfg.Args {
		writeFP(a)
	}
	if len(cfg.Env) > 0 {
		keys := make([]string, 0, len(cfg.Env))
		for k := range cfg.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeFP(k)
			writeFP(cfg.Env[k])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Acquire returns a live client and a release func. Same id+fingerprint reuses the process.
func (p *McpProcessPool) Acquire(ctx context.Context, cfg *McpConfig) (mcpClient, func(), error) {
	if p == nil {
		return nil, nil, fmt.Errorf("mcp: process pool is nil")
	}
	if cfg == nil {
		return nil, nil, fmt.Errorf("mcp: config is nil")
	}
	if strings.TrimSpace(cfg.Id) == "" {
		return nil, nil, fmt.Errorf("mcp: server id is required for stdio pool")
	}
	fp := Fingerprint(cfg)

	p.mu.Lock()
	if e, ok := p.entries[cfg.Id]; ok {
		if e.fingerprint == fp {
			e.refcount++
			e.lastUsed = time.Now()
			cli := e.client
			p.mu.Unlock()
			return cli, p.makeRelease(cfg.Id, fp), nil
		}
		// Fingerprint changed: detach old entry. Close only if idle (refcount==0).
		var closeNow mcpClient
		delete(p.entries, cfg.Id)
		if e.refcount > 0 {
			p.retiring[retiringKey(e.serverID, e.fingerprint)] = e
		} else {
			closeNow = e.client
		}
		p.mu.Unlock()
		if closeNow != nil {
			_ = closeNow.Close(context.Background())
		}
	} else {
		p.mu.Unlock()
	}

	if err := ValidateStdioMcp(cfg.Command, cfg.Args, cfg.Env); err != nil {
		return nil, nil, err
	}
	cli, err := stdioClientFactory(cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := cli.Initialize(ctx); err != nil {
		_ = cli.Close(context.Background())
		return nil, nil, err
	}

	p.mu.Lock()
	if e, ok := p.entries[cfg.Id]; ok && e.fingerprint == fp {
		e.refcount++
		e.lastUsed = time.Now()
		existing := e.client
		p.mu.Unlock()
		_ = cli.Close(context.Background())
		return existing, p.makeRelease(cfg.Id, fp), nil
	}
	var closeNow mcpClient
	if e, ok := p.entries[cfg.Id]; ok {
		delete(p.entries, cfg.Id)
		if e.refcount > 0 {
			p.retiring[retiringKey(e.serverID, e.fingerprint)] = e
		} else {
			closeNow = e.client
		}
	}
	p.entries[cfg.Id] = &mcpPoolEntry{
		client:      cli,
		serverID:    cfg.Id,
		fingerprint: fp,
		refcount:    1,
		lastUsed:    time.Now(),
	}
	p.mu.Unlock()
	if closeNow != nil {
		_ = closeNow.Close(context.Background())
	}
	return cli, p.makeRelease(cfg.Id, fp), nil
}

func (p *McpProcessPool) makeRelease(serverID, fp string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			var closeNow mcpClient
			if e, ok := p.entries[serverID]; ok && e.fingerprint == fp {
				if e.refcount > 0 {
					e.refcount--
				}
				e.lastUsed = time.Now()
			} else if e, ok := p.retiring[retiringKey(serverID, fp)]; ok {
				if e.refcount > 0 {
					e.refcount--
				}
				e.lastUsed = time.Now()
				if e.refcount == 0 {
					closeNow = e.client
					delete(p.retiring, retiringKey(serverID, fp))
				}
			}
			p.mu.Unlock()
			if closeNow != nil {
				_ = closeNow.Close(context.Background())
			}
		})
	}
}

// CachedTools returns schema-cached tools for a server id, if present.
func (p *McpProcessPool) CachedTools(serverID string) ([]Tool, bool) {
	if p == nil || serverID == "" {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[serverID]
	if !ok || len(e.tools) == 0 {
		return nil, false
	}
	out := make([]Tool, len(e.tools))
	copy(out, e.tools)
	return out, true
}

// StoreTools caches tool schemas for a pooled server entry.
func (p *McpProcessPool) StoreTools(serverID string, tools []Tool) {
	if p == nil || serverID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[serverID]
	if !ok {
		return
	}
	e.tools = append([]Tool(nil), tools...)
}

func (p *McpProcessPool) sweepLoop() {
	interval := p.opts.IdleTTL / 2
	if interval < 5*time.Millisecond {
		interval = 5 * time.Millisecond
	}
	if interval > time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.sweep()
		}
	}
}

func (p *McpProcessPool) sweep() {
	p.mu.Lock()
	var toClose []mcpClient
	now := time.Now()
	for id, e := range p.entries {
		if e.refcount == 0 && now.Sub(e.lastUsed) > p.opts.IdleTTL {
			toClose = append(toClose, e.client)
			delete(p.entries, id)
		}
	}
	p.mu.Unlock()
	for _, c := range toClose {
		_ = c.Close(context.Background())
	}
}

// stop terminates the sweeper (test helper).
func (p *McpProcessPool) stop() {
	if p == nil {
		return
	}
	p.stopped.Do(func() {
		close(p.stopCh)
	})
	p.mu.Lock()
	var toClose []mcpClient
	for id, e := range p.entries {
		toClose = append(toClose, e.client)
		delete(p.entries, id)
	}
	for k, e := range p.retiring {
		toClose = append(toClose, e.client)
		delete(p.retiring, k)
	}
	p.mu.Unlock()
	for _, c := range toClose {
		_ = c.Close(context.Background())
	}
}
