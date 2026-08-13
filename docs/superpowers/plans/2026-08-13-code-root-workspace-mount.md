# Code Root Workspace Mount Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pre-mount host code at `/mnt/codes:ro`, browse it via Portal API, and let AgentForm set workspace as full tree or `workspace/code` symlink (default).

**Architecture:** Compose bind-mount + `data.code_roots` allowlist; pure Go path helpers for safe browse/link; HTTP handlers next to existing agent routes; Web AgentForm mode switch calling browse + `workspace-link` after create.

**Tech Stack:** Go/Kratos portal, protobuf conf, docker-compose, React AgentForm, Vitest/Go tests.

**Spec:** `docs/superpowers/specs/2026-08-13-code-root-workspace-mount-design.md`

**Commits:** Only when the user explicitly asks (repo rule). Plan still lists logical commit boundaries.

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/internal/conf/conf.proto` (+ generated `conf.pb.go`) | `Data.code_roots` |
| `portal/configs/config.docker.yaml` | `code_roots: ["/mnt/codes"]` |
| `docker-compose.yml` | `${HOST_CODE_ROOT:-./codes}:/mnt/codes:ro` |
| `.env.example` | `HOST_CODE_ROOT` |
| `.gitignore` | optional `codes/` |
| `portal/internal/chat/code_roots.go` | Resolve roots, `SafeJoin`, `ListDirs`, `UnderRoot` |
| `portal/internal/chat/code_roots_test.go` | Path safety + list tests |
| `portal/internal/server/code_roots.go` | `GET code-roots`, `GET browse`, `POST workspace-link` |
| `portal/internal/server/http.go` | Register routes |
| `portal/internal/service/agent.go` | UploadSkill reject if workspace under code_roots |
| `web/src/api/client.ts` | API helpers |
| `web/src/pages/AgentForm.tsx` | Browse UI + modes |

---

### Task 1: conf `code_roots` + compose mount

**Files:**
- Modify: `portal/internal/conf/conf.proto` (`Data` message)
- Regenerate: `portal/internal/conf/conf.pb.go` via `make config`
- Modify: `portal/configs/config.docker.yaml`
- Modify: `docker-compose.yml` (portal volumes)
- Modify: `.env.example`
- Modify: `.gitignore` (add `/codes/`)

- [ ] **Step 1: Extend proto**

In `message Data`, after `data_root`:

```protobuf
  // code_roots are absolute container paths allowed for browse / workspace-link (e.g. /mnt/codes).
  repeated string code_roots = 4;
```

- [ ] **Step 2: Regenerate**

Run (from `portal/`):

```bash
make config
```

Expected: `conf.pb.go` contains `CodeRoots` / `GetCodeRoots()`.

- [ ] **Step 3: Docker config + compose + env**

`config.docker.yaml` under `data:`:

```yaml
  code_roots:
    - /mnt/codes
```

`docker-compose.yml` portal volumes add:

```yaml
      - ${HOST_CODE_ROOT:-./codes}:/mnt/codes:ro
```

`.env.example` add:

```env
# Host dir → portal /mnt/codes (read-only). Default ./codes
HOST_CODE_ROOT=./codes
```

`.gitignore` add `/codes/`.

- [ ] **Step 4: Ensure default host dir exists for compose**

```bash
mkdir -p codes
```

(Optional README one-liner later; not required for M0.)

---

### Task 2: Path helpers (TDD)

**Files:**
- Create: `portal/internal/chat/code_roots.go`
- Create: `portal/internal/chat/code_roots_test.go`

- [ ] **Step 1: Write failing tests**

```go
package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnderRoot_OK(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := ResolveUnderRoot(root, "a/b")
	if err != nil || abs != sub {
		t.Fatalf("abs=%q err=%v", abs, err)
	}
}

func TestUnderRoot_RejectDotDot(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveUnderRoot(root, "../x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnderRoot_RejectAbsPath(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveUnderRoot(root, root); err == nil {
		t.Fatal("expected error")
	}
}

func TestListDirs_OnlyDirectories(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "d1"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "f1"), []byte("x"), 0o644)
	ents, err := ListCodeDirs(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name != "d1" {
		t.Fatalf("%+v", ents)
	}
}

func TestWorkspaceUnderAnyRoot(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	_ = os.Mkdir(ws, 0o755)
	if !WorkspaceUnderCodeRoots(ws, []string{root}) {
		t.Fatal("expected true")
	}
	if WorkspaceUnderCodeRoots(t.TempDir(), []string{root}) {
		t.Fatal("expected false")
	}
}

func TestUnderRoot_RejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skip("symlink not permitted:", err)
	}
	if _, err := ResolveUnderRoot(root, "escape"); err == nil {
		t.Fatal("expected symlink escape to fail")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd portal && go test ./internal/chat/ -run "TestUnderRoot|TestListDirs|TestWorkspaceUnder" -count=1
```

- [ ] **Step 3: Implement helpers**

```go
// code_roots.go — outline
package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxCodeBrowseDepth   = 32
	MaxCodeBrowseEntries = 500
	WorkspaceCodeLink    = "code"
)

type CodeDirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"` // relative to root
	Type string `json:"type"` // "dir"
}

func NormalizeCodeRoots(roots []string) []string { /* trim, skip empty, abs Clean */ }

func ResolveUnderRoot(root, rel string) (string, error) {
	// reject abs rel; Clean; reject ".." elements;
	// join; EvalSymlinks if exists; ensure prefix of EvalSymlinks(root)
}

func ListCodeDirs(root, rel string) ([]CodeDirEntry, error) { /* ResolveUnderRoot; ReadDir; dirs only; cap */ }

func WorkspaceUnderCodeRoots(workspace string, roots []string) bool { /* EvalSymlinks + prefix */ }
```

Prefix check: ensure `target == root || strings.HasPrefix(target, root+string(os.PathSeparator))` after both cleaned/eval'd.

- [ ] **Step 4: Run tests — expect PASS**

```bash
cd portal && go test ./internal/chat/ -run "TestUnderRoot|TestListDirs|TestWorkspaceUnder" -count=1
```

---

### Task 3: Browse HTTP API

**Files:**
- Create: `portal/internal/server/code_roots.go`
- Modify: `portal/internal/server/http.go` (inject `codeRoots []string` or conf accessor)
- Create: `portal/internal/server/code_roots_test.go` (httptest style like other handlers)

Wire: `NewHTTPServer` already receives dependencies — pass `codeRoots` from `data.GetCodeRoots()` in `cmd/backend` / server provider (follow existing conf injection pattern in `portal/cmd/backend` and `portal/internal/server/http.go`).

- [ ] **Step 1: Handlers**

```go
// GET /api/v1/code-roots → {"roots":[...]} existing only
// GET /api/v1/code-roots/browse?root=&path= → {root,path,entries}
```

Auth: inside each handler call `runWithMiddleware(ctx, func...)` — same pattern as `TranscriptSearchHandler` / `http_middleware_run.go`. Do **not** wrap at route registration (signature is `(ctx, fn)`, not a middleware decorator).

Wire: add `ProvideCodeRoots` (mirror `ProvideDataRoot`) and update `NewHTTPServer` / `wire_gen.go` (`make generate` if needed).

- [ ] **Step 2: Register in `http.go`**

```go
r.GET("/api/v1/code-roots", CodeRootsListHandler(codeRoots))
r.GET("/api/v1/code-roots/browse", CodeRootsBrowseHandler(codeRoots))
```

Handlers themselves invoke `runWithMiddleware`.

- [ ] **Step 3: Test list + browse happy path + 400 on escape**

```bash
cd portal && go test ./internal/server/ -run CodeRoots -count=1
```

---

### Task 4: `workspace-link` API

**Files:**
- Modify: `portal/internal/server/code_roots.go`
- Modify: `portal/internal/server/http.go`
- Need: Agent getter (biz usecase) — inject `AgentService` or thin usecase callback like hub handlers

- [ ] **Step 1: Handler**

`POST /api/v1/agents/{agent_id}/workspace-link`  
Body: `{"target":"/mnt/codes/foo"}`  
Steps:
1. Inside handler: `runWithMiddleware` (like TranscriptSearch)
2. Load agent via injected **`AgentUsecase.GetForEdit`** (not AgentService.Get — upload path uses usecase edit check)
3. Validate `target` under `code_roots`
4. **`os.MkdirAll(agent.Workspace, 0o755)`** — CreateAgent only stores path in DB and does **not** create the directory; symlink parent must exist
5. `link := filepath.Join(agent.Workspace, chat.WorkspaceCodeLink)`
6. If link exists and points elsewhere → 409; if same target → 200 noop
7. `os.Symlink(absTarget, link)` (absolute container path)

- [ ] **Step 2: Register route**

```go
r.POST("/api/v1/agents/{agent_id}/workspace-link", AgentWorkspaceLinkHandler(agentUC, codeRoots))
```

Handler body uses `runWithMiddleware`.
- [ ] **Step 3: Unit test with temp workspace + temp root**

```bash
cd portal && go test ./internal/server/ -run WorkspaceLink -count=1
```

---

### Task 5: Reject skill upload when workspace under code_roots

**Files:**
- Modify: `portal/internal/service/agent.go` `UploadSkillPackage`
- Modify: wire so AgentService knows `codeRoots` (constructor field or package-level setter like MEA — prefer constructor/field)

- [ ] **Step 1: Guard**

```go
if chat.WorkspaceUnderCodeRoots(agent.Workspace, s.codeRoots) {
  return reply 400 "workspace is under read-only code root; use subdirectory mode (workspace/code)"
}
```

- [ ] **Step 2: Test** (table or httptest if existing upload tests; else small unit on helper already covered + one service test if easy)

---

### Task 6: Web API client + AgentForm (M1)

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/AgentForm.tsx`
- Modify: `web/src/pages/AgentDetail.tsx` (hide upload if workspace under `/mnt/codes` — optional M2; **do minimal check**: `workspace.startsWith` any known root from `codeRoots()` fetch)

- [ ] **Step 1: client helpers**

```ts
export const codeRootsApi = {
  list: () => request<{ roots: string[] }>('/code-roots'),
  browse: (root: string, path = '') =>
    request<{ root: string; path: string; entries: { name: string; path: string; type: string }[] }>(
      `/code-roots/browse?root=${encodeURIComponent(root)}&path=${encodeURIComponent(path)}`
    ),
}
// agentApi.workspaceLink(id, target) → POST `/agents/${id}/workspace-link`
// NOTE: request() already prefixes API_BASE `/api/v1` — do NOT include `/api/v1` in paths.
```

- [ ] **Step 2: AgentForm UI**

- Mode radio: default **`subdir`** (`挂到 workspace/code`) | `full` (`整仓作 Workspace`)
- Browse panel: list roots → click dirs → breadcrumb →「选择当前目录」
- **subdir:** require a selected browse target before submit; allow empty workspace string (server default); after `create`, use returned **`id`** → `workspaceLink(id, selectedTarget)`; on link failure show error (agent may already exist — allow retry)
- **full:** set workspace to absolute selected path; show ro warning; skip link
- Keep manual workspace input (advanced)

- [ ] **Step 3: AgentDetail (optional / M2)**

Defer hide skill-upload UI to M2 if time-boxed; Task 5 API guard is enough for M1 safety.

- [ ] **Step 4: Manual smoke** (docker or local with code_roots pointing at a temp dir)

```bash
# local: set code_roots in conf to a real path, restart portal
curl -sS -H "Authorization: Bearer $TOKEN" "$PORTAL/api/v1/code-roots"
curl -sS -H "Authorization: Bearer $TOKEN" "$PORTAL/api/v1/code-roots/browse?root=...&path="
```

---

### Task 7: Docs touch-up

**Files:**
- Modify: `docs/superpowers/specs/2026-08-13-code-root-workspace-mount-design.md` status → 实现中/已实现
- Optional: one paragraph in `README.md` or `portal/docs/` — only if README already documents compose env vars

---

## Verification checklist

```bash
cd portal && go test ./internal/chat/ -run "TestUnderRoot|TestListDirs|TestWorkspaceUnder" -count=1
cd portal && go test ./internal/server/ -run "CodeRoots|WorkspaceLink" -count=1
# Web: create agent in subdir mode, confirm workspace/code symlink inside data_root
# Upload skill on full-mode agent → 400
```

## Out of scope (do not implement)

- Dynamic Docker bind from UI
- `HOST_CODE_MODE=rw`
- Unlink API
- Browsing files (only dirs)
- Changing gateway volumes
