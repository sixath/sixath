# Portal Remote Terminal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a browser-based remote terminal (xterm.js) to Portal that connects to Windows VMs via the existing `/runCmd` HTTP endpoint, with Portal tracking working directory per session.

**Architecture:** Browser (xterm.js) ↔ WebSocket ↔ Portal (Terminal Session Manager) ↔ HTTP POST /runCmd ↔ Target Windows VM (:53000). Session state is in-memory only. Portal resolves vmid → IP via MySQL, tracks `$PWD` per session, and prepends `cd <dir> &&` to commands.

**Tech Stack:** Go (Kratos http handlers + gorilla/websocket + GORM MySQL), React + TypeScript + xterm.js

---

## File Structure

| File | Create/Modify | Responsibility |
|------|--------------|----------------|
| `portal/internal/terminal/manager.go` | Create | Session store, cd parsing, command execution, VM IP lookup |
| `portal/internal/terminal/manager_test.go` | Create | Unit tests for cd parsing and command construction |
| `portal/internal/server/terminal_ws.go` | Create | WebSocket upgrade handler + REST handlers (create/get/delete session) |
| `portal/internal/server/http.go` | Modify | Register 4 terminal routes |
| `portal/cmd/backend/wire_gen.go` | Modify | Wire terminal.Manager into NewHTTPServer |
| `portal/cmd/backend/wire.go` | Modify | Add terminal.Manager Wire provider set |
| `web/src/api/terminal.ts` | Create | API client for terminal sessions |
| `web/src/pages/TerminalPage.tsx` | Create | Terminal page with xterm.js + WebSocket |
| `web/src/App.tsx` | Modify | Add /terminal route, sidebar nav item, breadcrumb |
| `web/package.json` | Modify | Add @xterm/xterm, @xterm/addon-fit |

---

### Task 1: Backend — Terminal Session Manager

**Files:**
- Create: `portal/internal/terminal/manager.go`
- Create: `portal/internal/terminal/manager_test.go`
- Modify: `portal/go.mod` (add gorilla/websocket — required by Task 2 but added now)

**Context:** The Manager is an in-memory session store that handles command execution against remote VMs. It parses `cd` commands to track working directory, constructs full commands with `cd /d <dir> &&` prefix, and sends them via HTTP POST to `/runCmd`. VM IP lookup uses the same MySQL query as `framework/tool/vm_run_cmd.go`: `SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?`.

- [ ] **Step 1: Add gorilla/websocket dependency**

```bash
cd portal && go get github.com/gorilla/websocket
```

- [ ] **Step 2: Write failing tests for cd parsing and command construction**

Create `portal/internal/terminal/manager_test.go`:

```go
package terminal

import (
	"testing"
)

func TestParseCDCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantDir  string
		wantIsCD bool
	}{
		{"cd D:\\logs", "D:\\logs", true},
		{"cd /d D:\\logs", "D:\\logs", true},
		{"cd C:\\Users", "C:\\Users", true},
		{"dir", "", false},
		{"type file.txt", "", false},
		{"cd", "", true},            // cd alone → no dir change
		{"  cd  D:\\data  ", "D:\\data", true},
		{"CD D:\\LOGS", "D:\\LOGS", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotDir, gotIsCD := parseCDCommand(tt.input)
			if gotDir != tt.wantDir || gotIsCD != tt.wantIsCD {
				t.Fatalf("parseCDCommand(%q) = (%q, %v), want (%q, %v)", tt.input, gotDir, gotIsCD, tt.wantDir, tt.wantIsCD)
			}
		})
	}
}

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		workDir  string
		input    string
		want     string
		wantDir  string
	}{
		{"D:\\logs", "dir", "cd /d D:\\logs && dir", "D:\\logs"},
		{"C:\\", "type file.txt", "cd /d C:\\ && type file.txt", "C:\\"},
		{"D:\\logs", "cd D:\\data", "cd /d D:\\data", "D:\\data"},
		{"D:\\logs", "cd /d E:\\work", "cd /d E:\\work", "E:\\work"},
		{"C:\\", "cd", "cd /d C:\\", "C:\\"}, // cd alone stays in same dir
		{"D:\\a", "cd b", "cd /d D:\\a\\b", "D:\\a\\b"}, // relative cd
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, gotDir := buildCommand(tt.workDir, tt.input)
			if got != tt.want || gotDir != tt.wantDir {
				t.Fatalf("buildCommand(%q, %q) = (%q, %q), want (%q, %q)", tt.workDir, tt.input, got, gotDir, tt.want, tt.wantDir)
			}
		})
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	mgr := NewManager(nil, 0) // no DB, TTL=0 means all expired
	mgr.sessions["s1"] = &Session{ID: "s1", LastUsed: time.Now().Add(-1 * time.Hour)}
	mgr.sessions["s2"] = &Session{ID: "s2", LastUsed: time.Now()}
	mgr.cleanupExpired()
	if _, ok := mgr.sessions["s2"]; ok {
		t.Fatal("s2 should not be cleaned up since TTL=0 means immediate expiry")
	}
}
```

Add `import "time"` to the import block.

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd portal && go test ./internal/terminal/ -v -run 'TestParse|TestBuild|TestCleanup' 2>&1 | tail -20
```

Expected: compilation errors — `parseCDCommand`, `buildCommand`, `NewManager` not defined.

- [ ] **Step 4: Implement Terminal Session Manager**

Create `portal/internal/terminal/manager.go`:

```go
package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var cdRe = regexp.MustCompile(`(?i)^\s*cd\s+(.*)`)

type Session struct {
	ID        string
	VMID      int64
	Host      string
	Port      int
	WorkDir   string
	CreatedAt time.Time
	LastUsed  time.Time
	mu        sync.Mutex
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	db       *gorm.DB
	client   *http.Client
	idleTTL  time.Duration
}

func NewManager(db *gorm.DB, idleTTL time.Duration) *Manager {
	if idleTTL <= 0 {
		idleTTL = 30 * time.Minute
	}
	m := &Manager{
		sessions: make(map[string]*Session),
		db:       db,
		client:   &http.Client{Timeout: 30 * time.Second},
		idleTTL:  idleTTL,
	}
	go m.cleanupLoop()
	return m
}

func (m *Manager) Create(VMID int64, port int, workdir string) (*Session, error) {
	if VMID <= 0 {
		return nil, fmt.Errorf("vmid must be positive")
	}
	if port < 1 || port > 65535 {
		port = 53000
	}
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		workdir = "C:\\"
	}

	host, err := m.lookupIP(VMID)
	if err != nil {
		return nil, err
	}

	s := &Session{
		ID:        uuid.NewString(),
		VMID:      VMID,
		Host:      host,
		Port:      port,
		WorkDir:   workdir,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	return ok
}

// Execute runs a command on the session identified by id.
func (m *Manager) Execute(id, input string) (string, error) {
	s := m.Get(id)
	if s == nil {
		return "", fmt.Errorf("session not found")
	}
	return s.execute(m.client, input)
}

func (m *Manager) lookupIP(vmid int64) (string, error) {
	if m.db == nil {
		return "", fmt.Errorf("database not available for VM IP lookup")
	}
	var ip string
	err := m.db.Raw("SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?", vmid).Scan(&ip).Error
	if err != nil {
		return "", fmt.Errorf("VM IP lookup failed: %w", err)
	}
	if ip == "" {
		return "", fmt.Errorf("VM_NOT_FOUND: no IP found for vmid %d", vmid)
	}
	return ip, nil
}

func (s *Session) execute(client *http.Client, input string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cmd, newDir := buildCommand(s.WorkDir, input)
	s.WorkDir = newDir
	s.LastUsed = time.Now()

	body := map[string]string{"cmd": cmd}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("http://%s:%d/runCmd", s.Host, s.Port)
	resp, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return string(out), nil
}

func parseCDCommand(input string) (dir string, isCD bool) {
	m := cdRe.FindStringSubmatch(input)
	if m == nil {
		return "", false
	}
	dir = strings.TrimSpace(m[1])
	dir = strings.TrimPrefix(dir, "/d ")
	dir = strings.TrimSpace(dir)
	return dir, true
}

func buildCommand(workDir, input string) (cmd, newDir string) {
	cdDir, isCD := parseCDCommand(input)
	if isCD {
		if cdDir == "" {
			newDir = workDir
		} else if len(cdDir) >= 2 && cdDir[1] == ':' {
			newDir = cdDir // absolute: D:\logs
		} else {
			// relative: cd subdir → join
			newDir = strings.TrimRight(workDir, "\\") + "\\" + cdDir
		}
		return "cd /d " + newDir, newDir
	}
	return "cd /d " + workDir + " && " + input, workDir
}

func (m *Manager) cleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-m.idleTTL)
	for id, s := range m.sessions {
		if s.LastUsed.Before(cutoff) {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.cleanupExpired()
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd portal && go test ./internal/terminal/ -v -run 'TestParse|TestBuild|TestCleanup' 2>&1 | tail -20
```

Expected: all 3 tests PASS.

- [ ] **Step 6: Build check**

```bash
cd portal && go build ./internal/terminal/ 2>&1
```

- [ ] **Step 7: Commit**

```bash
git add portal/internal/terminal/manager.go portal/internal/terminal/manager_test.go portal/go.mod portal/go.sum
git commit -m "feat(terminal): add Terminal Session Manager with cd parsing and VM IP lookup

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Backend — HTTP handlers, WebSocket upgrade, routes, Wire DI

**Files:**
- Create: `portal/internal/server/terminal_ws.go`
- Modify: `portal/internal/server/http.go` (add 4 routes)
- Modify: `portal/cmd/backend/wire_gen.go` (wire terminal.Manager)
- Modify: `portal/cmd/backend/wire.go` (wire provider)

**Context:** The WebSocket handler upgrades HTTP to WebSocket using gorilla/websocket, reads text messages as commands, executes them via the Manager, and writes output back. REST handlers follow the existing `runWithMiddleware` + `ctx.Vars()` pattern. Wire DI passes `*gorm.DB` from `dataData.DB()` to `terminal.NewManager`.

- [ ] **Step 1: Write WebSocket handler and REST handlers**

Create `portal/internal/server/terminal_ws.go`:

```go
package server

import (
	"context"
	"net/http"

	"backend/internal/terminal"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type createTerminalSessionRequest struct {
	VMID    int64  `json:"vmid"`
	Port    int    `json:"port"`
	WorkDir string `json:"workdir"`
}

type terminalSessionResponse struct {
	SessionID string `json:"session_id"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	WorkDir   string `json:"workdir"`
}

// TerminalWSHandler upgrades GET /api/v1/terminal/sessions/{id}/ws to WebSocket.
func TerminalWSHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		if id == "" {
			return kratoshttp.BadRequest("INVALID_ARGUMENT", "session_id is required")
		}
		sess := mgr.Get(id)
		if sess == nil {
			return kratoshttp.NotFound("SESSION_NOT_FOUND", "session not found or expired")
		}

		r, _ := kratoshttp.RequestFromServerContext(ctx)
		w, _ := kratoshttp.ResponseFromServerContext(ctx)
		if r == nil || w == nil {
			return kratoshttp.InternalServerError("UPGRADE_FAILED", "cannot get underlying request/response")
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				_ = conn.WriteJSON(map[string]string{"type": "closed", "reason": "connection closed"})
				break
			}
			if msgType != websocket.TextMessage {
				continue
			}

			input := string(msg)
			output, execErr := mgr.Execute(id, input)
			if execErr != nil {
				_ = conn.WriteJSON(map[string]string{"type": "error", "message": execErr.Error()})
			} else {
				_ = conn.WriteJSON(map[string]string{"type": "stdout", "data": output})
			}
		}
		return nil
	}
}

// CreateTerminalSessionHandler serves POST /api/v1/terminal/sessions.
func CreateTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createTerminalSessionRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s, err := mgr.Create(body.VMID, body.Port, body.WorkDir)
			if err != nil {
				return nil, err
			}
			return terminalSessionResponse{
				SessionID: s.ID,
				Host:      s.Host,
				Port:      s.Port,
				WorkDir:   s.WorkDir,
			}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// GetTerminalSessionHandler serves GET /api/v1/terminal/sessions/{id}.
func GetTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			s := mgr.Get(id)
			if s == nil {
				return nil, kratoshttp.NotFound("SESSION_NOT_FOUND", "session not found or expired")
			}
			return terminalSessionResponse{
				SessionID: s.ID,
				Host:      s.Host,
				Port:      s.Port,
				WorkDir:   s.WorkDir,
			}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// DeleteTerminalSessionHandler serves DELETE /api/v1/terminal/sessions/{id}.
func DeleteTerminalSessionHandler(mgr *terminal.Manager) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := ctx.Vars().Get("id")
t	_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			if !mgr.Delete(id) {
				return nil, kratoshttp.NotFound("SESSION_NOT_FOUND", "session not found")
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}
```

- [ ] **Step 2: Add routes to http.go**

In `portal/internal/server/http.go`, add import for `"backend/internal/terminal"` at line 7 (after other `backend/internal/...` imports). Then add `terminalMgr *terminal.Manager` parameter to `NewHTTPServer` signature (after `logger log.Logger`, before `) *httptransport.Server`):

```go
func NewHTTPServer(c *conf.Server, tool *service.ToolService, agent *service.AgentService, chat *service.ChatService, channelSvc *service.ChannelService, cronSvc *cron.CronService, channelUC *biz.ChannelUsecase, identityRepo biz.IdentityRepo, aclAPI *biz.ACLAPIUsecase, authUC *biz.AuthUsecase, mcpServer *service.McpServerService, proxy *service.ProxyService, runtimeSvc *runtime.Service, pinger DBPinger, agentUC *biz.AgentUsecase, codeRoots []string, logger log.Logger, terminalMgr *terminal.Manager) *httptransport.Server {
```

Then add the 4 routes after line 138 (after `r.POST("/api/v1/proxies/{id}/test", TestProxyHandler(proxy))`):

```go
	r.POST("/api/v1/terminal/sessions", CreateTerminalSessionHandler(terminalMgr))
	r.GET("/api/v1/terminal/sessions/{id}", GetTerminalSessionHandler(terminalMgr))
	r.DELETE("/api/v1/terminal/sessions/{id}", DeleteTerminalSessionHandler(terminalMgr))
	r.GET("/api/v1/terminal/sessions/{id}/ws", TerminalWSHandler(terminalMgr))
```

Import for `"backend/internal/server/middleware"` at line 15 is already present, no change needed there since TerminalWSHandler bypasses runWithMiddleware (WebSocket upgrade needs raw request/response).

- [ ] **Step 3: Wire DI — update wire_gen.go**

In `portal/cmd/backend/wire_gen.go`, after line 48 (after `v := data.ProvideCodeRoots(confData)`), add:

```go
	terminalManager := terminal.NewManager(dataData.DB(), 30*time.Minute)
```

Add import for `"backend/internal/terminal"` in the import block and `"time"` to the standard library imports.

Then modify the `NewHTTPServer` call (line 75) to include `terminalManager` as the last argument:

```go
	httpServer := server.NewHTTPServer(confServer, toolService, agentService, chatService, channelService, cronService, channelUsecase, identityRepo, aclapiUsecase, authUsecase, mcpServerService, proxyService, runtimeService, dataData, agentUsecase, v, logger, terminalManager)
```

- [ ] **Step 4: Wire DI — update wire.go**

In `portal/cmd/backend/wire.go`, add `terminal.NewManager` to the `wire.Build` call (after line 41, before `newApp`):

```go
			terminal.NewManager,
```

Also add `"backend/internal/terminal"` to the import block. Note: Wire code generation may not auto-resolve `terminal.NewManager`'s `*gorm.DB` dependency — if `go generate` is re-run, restore the `wire_gen.go` line from Step 3 manually.

- [ ] **Step 5: Build check**

```bash
cd portal && go build ./... 2>&1 | tail -15
```

Expected: BUILD SUCCESS (no errors).

- [ ] **Step 6: Run all terminal tests**

```bash
cd portal && go test ./internal/terminal/ -v 2>&1 | tail -10
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add portal/internal/server/terminal_ws.go portal/internal/server/http.go portal/cmd/backend/wire_gen.go portal/cmd/backend/wire.go portal/internal/terminal/manager.go
git commit -m "feat(terminal): add WebSocket handler, REST endpoints, routes, and Wire DI

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Frontend — xterm.js terminal page, API client, sidebar, breadcrumb

**Files:**
- Create: `web/src/api/terminal.ts`
- Create: `web/src/pages/TerminalPage.tsx`
- Modify: `web/src/App.tsx` (route, sidebar nav, breadcrumb)
- Modify: `web/package.json` (add @xterm/xterm, @xterm/addon-fit)

**Context:** The terminal page first shows a connection form (vmid + optional port + workdir), then opens an xterm.js terminal connected via WebSocket. The sidebar gets a "终端" nav item with icon 💻. Breadcrumb shows "远程终端".

- [ ] **Step 1: Install xterm.js packages**

```bash
cd web && npm install @xterm/xterm @xterm/addon-fit
```

- [ ] **Step 2: Create terminal API client**

Create `web/src/api/terminal.ts`:

```ts
import { authHeaders } from './auth'

const API_BASE = '/api/v1'

export interface TerminalSession {
  session_id: string
  host: string
  port: number
  workdir: string
}

export async function createTerminalSession(vmid: number, port?: number, workdir?: string): Promise<TerminalSession> {
  const res = await fetch(`${API_BASE}/terminal/sessions`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ vmid, port: port ?? 53000, workdir: workdir ?? 'C:\\' }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `create session failed: ${res.status}`)
  }
  return res.json()
}

export async function getTerminalSession(sessionId: string): Promise<TerminalSession> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`session not found: ${res.status}`)
  return res.json()
}

export async function deleteTerminalSession(sessionId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`delete session failed: ${res.status}`)
}

export function terminalWebSocketURL(sessionId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}/ws`
}
```

- [ ] **Step 3: Create TerminalPage component**

Create `web/src/pages/TerminalPage.tsx`:

```tsx
import { useEffect, useRef, useState, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { createTerminalSession, deleteTerminalSession, terminalWebSocketURL, type TerminalSession } from '../api/terminal'
import '@xterm/xterm/css/xterm.css'

export default function TerminalPage() {
  const termRef = useRef<HTMLDivElement>(null)
  const termInstance = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

  const [session, setSession] = useState<TerminalSession | null>(null)
  const [connecting, setConnecting] = useState(false)
  const [error, setError] = useState('')
  const [disconnected, setDisconnected] = useState(false)

  // Connection form state
  const [vmid, setVmid] = useState('')
  const [port, setPort] = useState('53000')
  const [workdir, setWorkdir] = useState('C:\\')

  const connectWS = useCallback((sess: TerminalSession) => {
    if (wsRef.current) {
      wsRef.current.close()
    }
    setDisconnected(false)

    const url = terminalWebSocketURL(sess.session_id)
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setDisconnected(false)
    }

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'stdout' && msg.data) {
          termInstance.current?.write(msg.data.replace(/\n/g, '\r\n'))
        } else if (msg.type === 'error') {
          termInstance.current?.writeln(`\r\n\x1b[31m[ERROR] ${msg.message}\x1b[0m`)
        } else if (msg.type === 'closed') {
          termInstance.current?.writeln(`\r\n\x1b[33m[连接已关闭: ${msg.reason || 'unknown'}]\x1b[0m`)
          setDisconnected(true)
        }
      } catch {
        termInstance.current?.write(event.data)
      }
    }

    ws.onclose = () => {
      termInstance.current?.writeln('\r\n\x1b[33m[连接已断开]\x1b[0m')
      setDisconnected(true)
    }

    ws.onerror = () => {
      termInstance.current?.writeln('\r\n\x1b[31m[WebSocket 错误]\x1b[0m')
    }
  }, [])

  const handleConnect = async () => {
    const vmidNum = parseInt(vmid, 10)
    if (isNaN(vmidNum) || vmidNum <= 0) {
      setError('请输入有效的 VM ID')
      return
    }
    setConnecting(true)
    setError('')
    try {
      const portNum = parseInt(port, 10) || 53000
      const sess = await createTerminalSession(vmidNum, portNum, workdir)
      setSession(sess)
      setConnecting(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建会话失败')
      setConnecting(false)
    }
  }

  const handleDisconnect = async () => {
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    if (session) {
      try {
        await deleteTerminalSession(session.session_id)
      } catch {}
    }
    termInstance.current?.dispose()
    termInstance.current = null
    setSession(null)
    setDisconnected(false)
  }

  const handleReconnect = () => {
    if (!session) return
    connectWS(session)
  }

  // Initialize xterm.js when session is set
  useEffect(() => {
    if (!session || !termRef.current) return

    if (termInstance.current) {
      termInstance.current.dispose()
    }

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#ffffff',
        selectionBackground: '#264f78',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.current)
    fitAddon.fit()
    fitAddonRef.current = fitAddon

    term.onData((data) => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(data)
      }
    })

    term.write(`Connected to VM ${session.host}:${session.port}\r\n`)
    term.write(`Working directory: ${session.workdir}\r\n\r\n`)

    termInstance.current = term

    connectWS(session)

    const handleResize = () => fitAddon.fit()
    window.addEventListener('resize', handleResize)
    return () => {
      window.removeEventListener('resize', handleResize)
      term.dispose()
      termInstance.current = null
    }
  }, [session, connectWS])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close()
      }
      if (session) {
        deleteTerminalSession(session.session_id).catch(() => {})
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 120px)' }}>
      <h1 className="page-title">远程终端</h1>

      {!session ? (
        <div className="section-card" style={{ maxWidth: 480, margin: '2rem auto', padding: '1.5rem' }}>
          <div className="form-group">
            <label>VM ID</label>
            <input
              type="text"
              value={vmid}
              onChange={(e) => setVmid(e.target.value)}
              placeholder="输入 VM ID"
              disabled={connecting}
            />
          </div>
          <div className="form-group">
            <label>端口</label>
            <input
              type="text"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="53000"
              disabled={connecting}
            />
          </div>
          <div className="form-group">
            <label>工作目录</label>
            <input
              type="text"
              value={workdir}
              onChange={(e) => setWorkdir(e.target.value)}
              placeholder="C:\"
              disabled={connecting}
            />
          </div>
          {error && <p className="error">{error}</p>}
          <button
            type="button"
            className="btn btn-primary"
            onClick={handleConnect}
            disabled={connecting}
            style={{ marginTop: '0.5rem', width: '100%' }}
          >
            {connecting ? '连接中…' : '连接'}
          </button>
        </div>
      ) : (
        <>
          <div style={{
            display: 'flex',
            gap: '1rem',
            alignItems: 'center',
            padding: '0.5rem 1rem',
            background: 'var(--bg-accent)',
            borderRadius: 'var(--radius-md)',
            marginBottom: '0.5rem',
            fontSize: '0.85rem',
            flexWrap: 'wrap',
          }}>
            <span>Host: <code>{session.host}</code></span>
            <span>Port: <code>{session.port}</code></span>
            <span>VMID: <code>{vmid}</code></span>
            <span>WorkDir: <code>{workdir}</code></span>
            <span style={{ marginLeft: 'auto', display: 'flex', gap: '0.5rem' }}>
              {disconnected && (
                <button type="button" className="btn btn-primary btn-sm" onClick={handleReconnect}>
                  重新连接
                </button>
              )}
              <button type="button" className="btn btn-sm" onClick={handleDisconnect} style={{ background: 'var(--danger)', color: '#fff' }}>
                断开
              </button>
            </span>
          </div>
          <div
            ref={termRef}
            style={{
              flex: 1,
              borderRadius: 'var(--radius-md)',
              overflow: 'hidden',
              minHeight: 400,
            }}
          />
        </>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Add route, sidebar, breadcrumb to App.tsx**

In `web/src/App.tsx`:

**4a. Add import (after line 27, after VerifyEmailPage):**
```tsx
import TerminalPage from './pages/TerminalPage'
```

**4b. Add breadcrumb case (after line 86, in the `else if` chain):**
```tsx
  } else if (segments[0] === 'terminal') {
    current = '远程终端'
    icon = '💻'
  }
```

**4c. Add sidebar nav item (after line 193, after the 组织 nav item):**
```tsx
            <NavLink to="/terminal" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">💻</span>
              终端
            </NavLink>
```

**4d. Add route (after line 285, after `<Route path="/settings" element={<SettingsPage />} />`):**
```tsx
            <Route path="/terminal" element={<TerminalPage />} />
```

- [ ] **Step 5: Build check**

```bash
cd web && npm run build 2>&1 | tail -15
```

Expected: BUILD SUCCESS (no TypeScript errors).

- [ ] **Step 6: Commit**

```bash
git add web/src/api/terminal.ts web/src/pages/TerminalPage.tsx web/src/App.tsx web/package.json web/package-lock.json
git commit -m "feat(web): add remote terminal page with xterm.js, sidebar nav, and breadcrumb

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Portal build + test**

```bash
cd portal && go build ./... && go test ./... 2>&1 | tail -25
```

Expected: build success, all tests pass.

- [ ] **Step 2: Web build**

```bash
cd web && npm run build 2>&1 | tail -10
```

Expected: build success.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "chore: portal remote terminal regression pass

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Verification

After all tasks complete:
1. **Sidebar** — verify "终端" nav item exists with 💻 icon
2. **Terminal page** (`/terminal`) — verify connection form appears (vmid, port, workdir)
3. **Session creation** — POST with valid vmid should create session and show terminal
4. **Command execution** — type `dir` in terminal, verify stdout appears
5. **cd tracking** — type `cd D:\`, then `dir`, verify output from D:\
6. **Disconnect** — click "断开" button, verify session is cleaned up
7. **Reconnect** — reconnect to existing session (within 30min), verify state preserved