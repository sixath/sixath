# Portal Remote Terminal Design

## Overview

Add a browser-based remote terminal to Portal that connects to Windows VMs (identified by `vmid`) via the existing `/runCmd` HTTP endpoint. The browser runs xterm.js, Portal acts as a WebSocket intermediary that tracks working directory per session and translates terminal input into cmd commands.

**Architecture:** Browser (xterm.js) ↔ WebSocket ↔ Portal (Terminal Session Manager) ↔ HTTP POST /runCmd ↔ Target Windows VM (:53000)

**Target VMs:** Windows machines running a `/runCmd` service on port 53000, accepting `POST {"cmd":"..."}` and returning stdout text.

## Architecture

```
┌──────────────┐     WebSocket      ┌────────────┐     HTTP POST      ┌──────────────┐
│  Browser     │ ◄────────────────► │   Portal   │ ◄────────────────► │  Windows VM  │
│  (xterm.js)  │   wss://portal/    │  (Kratos)  │  http://host:53000 │  (:53000)    │
│              │   api/v1/terminal/ │            │  /runCmd           │  /runCmd     │
│              │   sessions/{id}/ws │            │                    │              │
└──────────────┘                    └────────────┘                    └──────────────┘
                                           │
                                           ▼
                                   MySQL (vmid → IP lookup)
```

## Backend Design

### Terminal Session Manager

File: `portal/internal/terminal/manager.go`

Pure in-memory session store (no persistence needed — sessions are ephemeral).

```go
type Session struct {
    ID        string
    VMID      int64
    Host      string     // resolved IP from vmid lookup
    Port      int        // default 53000
    WorkDir   string     // tracked working directory, e.g. "D:\\logs"
    CreatedAt time.Time
    LastUsed  time.Time
    mu        sync.Mutex // protects concurrent command execution per session
}

type Manager struct {
    mu       sync.Mutex
    sessions map[string]*Session
    lookup   VMIPLookup       // MySQL query: SELECT mgr_ipv4_address FROM t_game_virtual_machine_info WHERE vmid = ?
    client   *http.Client
    idleTTL  time.Duration    // 30 minutes, idle sessions are cleaned up
}
```

**VM IP Lookup:** Reuses the same MySQL lookup pattern as `framework/tool/vm_run_cmd.go` — query `t_game_virtual_machine_info` by vmid.

**Session ID:** UUID v4, generated on creation.

### Command Execution Flow

Each user input goes through:

1. Parse the input for `cd` commands (both `cd <dir>` and `cd /d <dir>`)
2. Update `WorkDir` if it was a cd command
3. Construct the full command: `cd /d <WorkDir> && <user_input>` (skip cd prefix if it was a cd command — just update state)
4. HTTP POST `{"cmd":"<full_command>"}` to `http://<host>:<port>/runCmd`
5. Return stdout to client via WebSocket

**cd command detection:** Regex `^cd\s+(.*)` — if the command starts with `cd`, extract the target directory, update `WorkDir`, and don't forward the command to the VM (or forward just `cd /d <dir>` without `&&` suffix).

**Initial WorkDir:** Configurable via API parameter, defaults to `C:\`.

### WebSocket Protocol

**Server → Client messages (JSON):**

```json
{"type":"stdout","data":"<command output text>"}
{"type":"stderr","data":"<error text>"}
{"type":"error","message":"<human-readable error>"}
{"type":"pong"}
{"type":"closed","reason":"<why the session ended>"}
```

**Client → Server messages:**

Any text message → treated as a command to execute (supports paste with newlines — each line is a separate command).

```json
{"type":"resize","cols":120,"rows":40}
```

The `resize` event is accepted and stored for future use (e.g., passing `mode con: cols=X lines=Y` to the VM), but initial implementation does not forward it to the VM.

### REST API

| Method   | Path                                    | Description            |
|----------|-----------------------------------------|------------------------|
| `POST`   | `/api/v1/terminal/sessions`             | Create a new session   |
| `GET`    | `/api/v1/terminal/sessions/{id}`        | Get session info       |
| `DELETE` | `/api/v1/terminal/sessions/{id}`        | Close and remove session|
| `GET`    | `/api/v1/terminal/sessions/{id}/ws`     | WebSocket upgrade      |

**POST /api/v1/terminal/sessions** request body:

```json
{
  "vmid": 12345,
  "port": 53000,
  "workdir": "D:\\"
}
```

Response:

```json
{
  "session_id": "uuid-v4",
  "host": "10.x.x.x",
  "port": 53000,
  "workdir": "D:\\"
}
```

### Authentication & Authorization

All terminal endpoints go through the existing Kratos auth middleware (`middleware.Auth`). The session is scoped to the authenticated user — only the creator can access it.

### Session Lifecycle

- **Created:** via POST, resolves vmid → IP, stores session in memory
- **Active:** WebSocket connected, commands flowing
- **Idle:** WebSocket disconnected but session still in memory (reconnect possible within idle TTL)
- **Expired:** No activity for 30 minutes → Manager goroutine cleans up

### Route Registration

In `http.go`, add routes inside the existing `r := srv.Route("/")` block:

```go
r.POST("/api/v1/terminal/sessions", CreateTerminalSessionHandler(terminalMgr))
r.GET("/api/v1/terminal/sessions/{id}", GetTerminalSessionHandler(terminalMgr))
r.DELETE("/api/v1/terminal/sessions/{id}", DeleteTerminalSessionHandler(terminalMgr))
r.GET("/api/v1/terminal/sessions/{id}/ws", TerminalWSHandler(terminalMgr))
```

### Dependency Injection (Wire)

Add `terminalMgr *terminal.Manager` to `NewHTTPServer` signature. Wire provider: `terminal.NewManager(db *gorm.DB)`.

### Error Handling

- vmid not found in MySQL → 404 "VM_NOT_FOUND"
- VM unreachable (HTTP timeout/refused) → the error is sent via WebSocket `{"type":"error",...}`, session stays open
- Session not found → 404 "SESSION_NOT_FOUND"
- Session expired → 410 "SESSION_EXPIRED"

## Frontend Design

### Dependencies

Install `@xterm/xterm` and `@xterm/addon-fit`:

```
npm install @xterm/xterm @xterm/addon-fit
```

### Terminal Page

File: `web/src/pages/TerminalPage.tsx`

Route: `/terminal`

Flow:
1. If no session, show a form: vmid input + optional port + optional workdir + "连接" button
2. POST to create session, get session_id
3. Open xterm.js terminal, connect WebSocket
4. Show VM host info in a status bar above the terminal
5. On disconnect, show "连接已断开" overlay with "重新连接" button

Connection status bar shows: host IP, vmid, current working directory (can be refreshed).

### Sidebar Entry

Add a "终端" nav item to the sidebar with icon `💻`:

```tsx
<NavLink to="/terminal" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
  <span className="nav-item__icon">💻</span>
  终端
</NavLink>
```

### Agent Detail Tab

Agent detail page already has tabs. If an agent has a vmid field or is associated with a VM, add a "终端" tab that auto-fills the vmid and creates a session. This is a future enhancement and not in the initial implementation.

### API Client

File: `web/src/api/terminal.ts`

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
    body: JSON.stringify({ vmid, port, workdir }),
  })
  if (!res.ok) throw new Error(`create session failed: ${res.status}`)
  return res.json()
}

export async function getTerminalSession(sessionId: string): Promise<TerminalSession> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`get session failed: ${res.status}`)
  return res.json()
}

export async function deleteTerminalSession(sessionId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`delete session failed: ${res.status}`)
}
```

### Breadcrumb

Add terminal entry to the Breadcrumb component:

```tsx
} else if (segments[0] === 'terminal') {
  current = '远程终端'
  icon = '💻'
}
```

## Session Reconnection

When WebSocket disconnects unexpectedly (network drop, page refresh):

1. Frontend detects `onclose` event
2. Shows "连接已断开" overlay with a "重新连接" button
3. On reconnect: call `GET /api/v1/terminal/sessions/{id}` to verify session still exists
4. If session exists: re-establish WebSocket (Portal preserves session state)
5. If session expired: redirect to new session form

Session state (vmid, host, workdir) is preserved server-side even while WebSocket is disconnected, for the idle TTL duration (30 minutes).

## Concurrency

Each session has a mutex (`Session.mu`) to serialize command execution — only one command runs at a time per session. This prevents race conditions on `WorkDir` updates.

## Testing

- **Unit tests:** Manager session CRUD, cd parsing, command construction
- **Integration tests:** WebSocket upgrade, command execution flow (with mock HTTP server)
- **Frontend:** No automated tests in initial implementation (manual testing via browser)

## Out of Scope

- Agent detail tab terminal (future enhancement)
- Resize forwarding to VM
- Session persistence across Portal restarts
- Multi-tab terminal within a single session
- File upload/download
- SSH transport (this is purely HTTP /runCmd based)