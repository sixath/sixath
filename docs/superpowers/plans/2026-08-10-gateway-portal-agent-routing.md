# Gateway Portal Agent Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Portal the source of truth for inbound channel Agent defaults/allowlists, and support explicit rebind via `force_new` (new session) plus Gateway slash commands and API fields.

**Architecture:** Extend Portal `channels.allowed_agents` and rewrite `ChannelPeerUsecase.Resolve` to validate against Portal channel config, reject silent agent switches (`AGENT_BOUND`), and upsert peer mappings on `force_new`. Gateway stops trusting yaml `default_agent` for routing truth; it parses `/agent|/new|/unbind|/agents`, calls Runtime Resolve/DeleteBinding/ListAgents, and invalidates peer cache.

**Tech Stack:** Go (Portal Kratos + GORM, Gateway stdlib), MySQL migration, React ChannelForm, existing `/runtime/v1` service token.

**Spec:** `docs/superpowers/specs/2026-08-10-gateway-portal-agent-routing-design.md`

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/migrations/014_channel_allowed_agents.sql` | Add `allowed_agents` JSON column |
| `portal/internal/data/model/channel.go` | Model field `AllowedAgents` |
| `portal/internal/biz/channel.go` + usecase/data | CRUD + validation `default ∈ allowed` |
| `portal/internal/biz/channel_peer.go` | Resolve decision table + DeleteBinding |
| `portal/internal/data/channel_peer_mysql.go` | `Upsert`, `Delete` |
| `portal/internal/runtime/http.go` + `service.go` | Resolve body fields; DELETE binding; GET channel agents |
| `gateway/internal/runtimeclient/client.go` | `ForceNew`, `Reason`; DeleteBinding; ListChannelAgents |
| `gateway/internal/session/router.go` | `Invalidate(channel, peer)` |
| `gateway/internal/command/parse.go` (new) | Slash-command parser |
| `gateway/internal/adapter/webhook.go` / `wecom_bot.go` | Command intercept + Resolve without yaml agent truth |
| `web/src/pages/ChannelForm.tsx` + `web/src/api/client.ts` | Edit `allowed_agents` |
| `gateway/README.md` + inbound spec note | Deprecate yaml `default_agent` as routing truth |

---

### Task 1: Migration + Channel model `allowed_agents`

**Files:**
- Create: `portal/migrations/014_channel_allowed_agents.sql`
- Modify: `portal/internal/data/model/channel.go`
- Modify: `portal/internal/biz/channel.go` (`ChannelCreate`, `ChannelMeta`)
- Modify: `portal/internal/data/channel_mysql.go` (map field on create/get/update/list)
- Test: `portal/internal/biz/channel_allowed_agents_test.go` (create after Task 2 if validation lives in usecase; for this task only model round-trip via data test optional)

- [ ] **Step 1: Add migration**

```sql
-- portal/migrations/014_channel_allowed_agents.sql
ALTER TABLE channels
  ADD COLUMN allowed_agents JSON NULL COMMENT 'agent UUID allowlist; empty/null => only default_agent' AFTER default_agent;
```

- [ ] **Step 2: Extend model**

In `portal/internal/data/model/channel.go` add:

```go
AllowedAgents StringSlice `gorm:"column:allowed_agents;type:json"`
```

In `biz.ChannelCreate` / `ChannelMeta` add `AllowedAgents []string`.

- [ ] **Step 3: Map in `channel_mysql.go`**

On Create/Update/Get/List copy `AllowedAgents` ↔ `model.Channel.AllowedAgents` (nil → empty slice in biz).

- [ ] **Step 4: Commit**

```bash
git add portal/migrations/014_channel_allowed_agents.sql portal/internal/data/model/channel.go portal/internal/biz/channel.go portal/internal/data/channel_mysql.go
git commit -m "feat(portal): add channels.allowed_agents column and model wiring"
```

---

### Task 2: Channel usecase validation for allowlist

**Files:**
- Modify: `portal/internal/biz/channel_usecase.go`
- Create: `portal/internal/biz/channel_allowed_agents_test.go`
- Modify: channel HTTP handlers / service JSON if they omit the new field (`portal/internal/service/channel.go`, server handlers)

- [ ] **Step 1: Write failing tests**

```go
func TestChannelCreate_DefaultMustBeInAllowedWhenNonEmpty(t *testing.T) {
	// usecase with fake repo; Create with DefaultAgent=a1, AllowedAgents=[a2]
	// expect BadRequest INVALID_ARGUMENT
}

func TestChannelCreate_EmptyAllowedMeansDefaultOnly(t *testing.T) {
	// AllowedAgents empty, DefaultAgent=a1 → ok; stored AllowedAgents empty
}

func TestChannelUpdate_RejectsDefaultOutsideAllowed(t *testing.T) {
	// existing allowed [a1]; update default to a2 → error
}
```

- [ ] **Step 2: Run tests (expect FAIL)**

```bash
cd portal && go test ./internal/biz/ -run TestChannelCreate_DefaultMustBeInAllowed -count=1
```

- [ ] **Step 3: Implement validation helper**

```go
func normalizeAllowedAgents(defaultAgent string, allowed []string) ([]string, error) {
	defaultAgent = strings.TrimSpace(defaultAgent)
	out := uniqueTrimmed(allowed)
	if len(out) == 0 {
		return out, nil // empty => default-only policy at Resolve time
	}
	if defaultAgent == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "default_agent required when allowed_agents is set")
	}
	if !contains(out, defaultAgent) {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "default_agent must be in allowed_agents")
	}
	return out, nil
}
```

Call from Create/Update before repo write. Expose `allowed_agents` on API JSON (snake_case).

- [ ] **Step 4: Run tests (expect PASS)**

```bash
cd portal && go test ./internal/biz/ -run 'TestChannelCreate_|TestChannelUpdate_Rejects' -count=1
```

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/channel_usecase.go portal/internal/biz/channel_allowed_agents_test.go portal/internal/service/channel.go
git commit -m "feat(portal): validate channel default_agent against allowed_agents"
```

---

### Task 3: Peer session repo Upsert + Delete

**Files:**
- Modify: `portal/internal/biz/channel_peer.go` (repo interface)
- Modify: `portal/internal/data/channel_peer_mysql.go`
- Modify: `portal/internal/biz/channel_peer_test.go` fake repo
- Create/extend tests in `portal/internal/data/` only if integration style exists; prefer fake in biz tests (Task 4)

- [ ] **Step 1: Extend interface**

```go
type ChannelPeerSessionRepo interface {
	Get(ctx context.Context, channelID, peerID string) (*ChannelPeerSession, error)
	Create(ctx context.Context, row *ChannelPeerSession) error
	Upsert(ctx context.Context, row *ChannelPeerSession) error
	Delete(ctx context.Context, channelID, peerID string) error
}
```

- [ ] **Step 2: Implement MySQL**

```go
func (r *channelPeerSessionRepo) Upsert(ctx context.Context, row *biz.ChannelPeerSession) error {
	// INSERT ... ON DUPLICATE KEY UPDATE session_id, agent_id, updated_at
	// or: Get; if not found Create; else Updates on channel_id+peer_id
}

func (r *channelPeerSessionRepo) Delete(ctx context.Context, channelID, peerID string) error {
	return r.db.WithContext(ctx).
		Where("channel_id = ? AND peer_id = ?", channelID, peerID).
		Delete(&model.ChannelPeerSession{}).Error
}
```

- [ ] **Step 3: Update fakes in `channel_peer_test.go`**

Implement `Upsert`/`Delete` on `fakeChannelPeerSessionRepo`.

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz/channel_peer.go portal/internal/data/channel_peer_mysql.go portal/internal/biz/channel_peer_test.go
git commit -m "feat(portal): peer session Upsert and Delete for rebind/unbind"
```

---

### Task 4: Rewrite ChannelPeer Resolve (core decision table)

**Files:**
- Modify: `portal/internal/biz/channel_peer.go`
- Modify: `portal/internal/biz/channel_peer_test.go`
- Wire: inject `ChannelRepo` into `ChannelPeerUsecase` (`biz.go`, `wire_gen` via ` mag wire`)

**Constructor becomes:**

```go
func NewChannelPeerUsecase(peerRepo ChannelPeerSessionRepo, sessionRepo ChatSessionRepo, channelRepo ChannelRepo) *ChannelPeerUsecase
```

**New Resolve signature:**

```go
type ChannelPeerResolveInput struct {
	ChannelID string
	PeerID    string
	AgentID   string // optional
	ForceNew  bool
	Reason    string
}

func (uc *ChannelPeerUsecase) Resolve(ctx context.Context, in ChannelPeerResolveInput) (*ChannelPeerResolveResult, error)
```

Keep a thin wrapper `Resolve(ctx, channelID, peerID, agentID string)` calling `ForceNew:false` **only if** needed for compile during migration; prefer updating all callers in same task.

- [ ] **Step 1: Write failing table tests**

```go
func TestResolve_NoMappingCreates(t *testing.T) { /* ... */ }
func TestResolve_SameAgentContinues(t *testing.T) { /* ... */ }
func TestResolve_DifferentAgentWithoutForceNew_AgentBound(t *testing.T) {
	// expect reason AGENT_BOUND / Conflict
}
func TestResolve_ForceNewRebindsMapping(t *testing.T) {
	// old session id retained in fake session repo; mapping points to new
}
func TestResolve_AgentNotAllowed(t *testing.T) { /* allowed=[a1], request a2 */ }
func TestResolve_EmptyAllowed_OnlyDefault(t *testing.T) { /* ... */ }
func TestResolve_ChannelNotFound(t *testing.T) { /* ... */ }
func TestResolve_OmitsAgentUsesDefault(t *testing.T) { /* ... */ }
```

Define exported errors (or kratos reasons):

```go
var (
	ErrChannelNotFound  = kratosErrors.NotFound("CHANNEL_NOT_FOUND", "channel not found")
	ErrAgentNotAllowed  = kratosErrors.Forbidden("AGENT_NOT_ALLOWED", "agent not allowed for channel")
	ErrAgentBound       = kratosErrors.Conflict("AGENT_BOUND", "peer already bound to another agent; use force_new")
)
```

- [ ] **Step 2: Run tests (expect FAIL)**

```bash
cd portal && go test ./internal/biz/ -run TestResolve_ -count=1
```

- [ ] **Step 3: Implement Resolve**

Pseudo:

```go
ch, err := uc.channelRepo.GetByChannelID(ctx, in.ChannelID)
// not found → ErrChannelNotFound
agentID := strings.TrimSpace(in.AgentID)
if agentID == "" {
	agentID = ch.DefaultAgent
}
if agentID == "" {
	return nil, BadRequest agent_id required
}
if !agentAllowed(ch, agentID) {
	return nil, ErrAgentNotAllowed
}
existing, err := uc.peerRepo.Get(...)
if existing != nil && !in.ForceNew {
	if existing.AgentID == agentID {
		return existing mapping, created=false
	}
	return nil, ErrAgentBound
}
// create session with PeerUserID
// if existing == nil: Create mapping; else Upsert mapping
```

```go
func agentAllowed(ch *ChannelMeta, agentID string) bool {
	if len(ch.AllowedAgents) == 0 {
		return agentID == ch.DefaultAgent
	}
	return contains(ch.AllowedAgents, agentID)
}
```

- [ ] **Step 4: Add DeleteBinding**

```go
func (uc *ChannelPeerUsecase) DeleteBinding(ctx context.Context, channelID, peerID string) error {
	return uc.peerRepo.Delete(ctx, channelID, peerID)
}
```

- [ ] **Step 5: `cd portal && go generate ./cmd/backend` or `wire`** so `NewChannelPeerUsecase` gets `ChannelRepo`.

- [ ] **Step 6: Run tests PASS + fix runtime compile breakages by updating `PeerResolver` interface in Task 5 if needed in same commit.**

```bash
cd portal && go test ./internal/biz/ -run 'TestResolve_|TestChannelPeer' -count=1
```

- [ ] **Step 7: Commit**

```bash
git add portal/internal/biz/channel_peer.go portal/internal/biz/channel_peer_test.go portal/cmd/backend/wire_gen.go
git commit -m "feat(portal): Resolve allowlist, AGENT_BOUND, and force_new rebind"
```

---

### Task 5: Runtime HTTP — resolve fields, delete binding, list agents

**Files:**
- Modify: `portal/internal/runtime/service.go` (`PeerResolver` interface, `resolve`, handlers)
- Modify: `portal/internal/runtime/http.go` (routes)
- Modify: `portal/internal/runtime/sessions_test.go`

- [ ] **Step 1: Extend request/response types**

```go
type resolveRequest struct {
	ChannelID string `json:"channel_id"`
	PeerID    string `json:"peer_id"`
	AgentID   string `json:"agent_id"`
	ForceNew  bool   `json:"force_new"`
	Reason    string `json:"reason"`
}
```

Update `PeerResolver`:

```go
Resolve(ctx context.Context, in biz.ChannelPeerResolveInput) (*biz.ChannelPeerResolveResult, error)
DeleteBinding(ctx context.Context, channelID, peerID string) error
```

- [ ] **Step 2: Failing HTTP tests**

- POST resolve with force_new hits peer fake with ForceNew true  
- POST resolve conflicting agent without force_new → 409 AGENT_BOUND  
- DELETE `/runtime/v1/sessions/binding?channel_id=&peer_id=` → 200  
- GET `/runtime/v1/channels/{channel_id}/agents` → `{ default_agent, agents:[{id,name}] }` filtered by allowlist (need AgentRepo or minimal name from agent usecase — inject read-only lister)

For list agents: ChannelPeerUsecase or new small helper on runtime Service using `channelUC.GetByChannelID` + `agentUC.Get` per id (skip missing).

- [ ] **Step 3: Register routes**

```go
r.DELETE("/runtime/v1/sessions/binding", svc.wrap(svc.handleDeleteBinding))
r.GET("/runtime/v1/channels/{channel_id}/agents", svc.wrap(svc.handleListChannelAgents))
```

- [ ] **Step 4: Run**

```bash
cd portal && go test ./internal/runtime/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add portal/internal/runtime/
git commit -m "feat(portal): runtime resolve force_new, delete binding, list channel agents"
```

---

### Task 6: Gateway runtime client + session cache invalidate

**Files:**
- Modify: `gateway/internal/runtimeclient/client.go`
- Modify: `gateway/internal/runtimeclient/client_test.go`
- Modify: `gateway/internal/session/router.go`
- Create: `gateway/internal/session/router_test.go` (if missing)

- [ ] **Step 1: Extend client**

```go
type ResolveRequest struct {
	ChannelID string `json:"channel_id"`
	PeerID    string `json:"peer_id"`
	AgentID   string `json:"agent_id,omitempty"`
	ForceNew  bool   `json:"force_new,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func (c *Client) DeleteBinding(ctx context.Context, channelID, peerID string) error
func (c *Client) ListChannelAgents(ctx context.Context, channelID string) (*ChannelAgentsReply, error)
```

Parse Portal error JSON for `reason` when present (if runtime returns kratos error body); keep HTTPError status mapping.

- [ ] **Step 2: Router Invalidate**

```go
func (r *Router) Invalidate(channelID, peerID string) {
	r.mu.Lock()
	delete(r.cache, cacheKey{channelID, peerID})
	r.mu.Unlock()
}
```

Call Invalidate after successful force_new resolve and after DeleteBinding (adapters do this).

- [ ] **Step 3: Tests + commit**

```bash
cd gateway && go test ./internal/runtimeclient/ ./internal/session/ -count=1
git add gateway/internal/runtimeclient gateway/internal/session
git commit -m "feat(gateway): runtime force_new client fields and peer cache invalidate"
```

---

### Task 7: Gateway slash-command parser

**Files:**
- Create: `gateway/internal/command/parse.go`
- Create: `gateway/internal/command/parse_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestParse_AgentSwitch(t *testing.T) {
	c, ok := Parse("/agent zone-4100-agent")
	// ok, Kind=Agent, Target="zone-4100-agent"
}
func TestParse_AgentsList(t *testing.T) { Parse("/agents"); Parse("/agent") }
func TestParse_New(t *testing.T)        { Parse("/new") }
func TestParse_Unbind(t *testing.T)     { Parse("/unbind") }
func TestParse_UnknownSlash(t *testing.T) {
	c, ok := Parse("/foo"); // Kind=Unknown
}
func TestParse_NotCommand(t *testing.T) {
	_, ok := Parse("hello"); // ok=false
}
```

- [ ] **Step 2: Implement**

```go
type Kind int
const (
	KindNone Kind = iota
	KindAgentSwitch
	KindAgentList
	KindNew
	KindUnbind
	KindUnknown
)

type Command struct {
	Kind   Kind
	Target string // for AgentSwitch
}

func Parse(text string) (Command, bool)
```

Trim; require leading `/`; case-insensitive command names; `/agent` with no args → list.

- [ ] **Step 3: Commit**

```bash
cd gateway && go test ./internal/command/ -count=1
git add gateway/internal/command
git commit -m "feat(gateway): parse inbound slash commands for agent routing"
```

---

### Task 8: Wire commands into webhook + wecom_bot

**Files:**
- Modify: `gateway/internal/adapter/webhook.go`
- Modify: `gateway/internal/adapter/wecom_bot.go`
- Modify: `gateway/internal/adapter/webhook_test.go`
- Modify: `gateway/internal/adapter/wecom_bot_test.go`

**Behavior:**

1. After normalize, `command.Parse(text)`.
2. If command:
   - list → `ListChannelAgents`, reply text, return (no Turns)
   - switch → resolve target id (match id or name from list); `Resolve(ForceNew:true, AgentID)`; Invalidate; reply; return
   - new → `Resolve(ForceNew:true, AgentID:"")` (Portal default or current — spec: current mapping agent or default; pass empty agent_id so Portal default **or** pass previous mapped agent: prefer empty → Portal default for `/new` simplicity **OR** fetch binding via resolve without force first — **spec says current mapping or default**. Implement: try Resolve force_new=false with empty agent to learn current; if AGENT_BOUND impossible; actually continue mapping returns current agent; then ForceNew with that agent_id. Simpler approach for `/new`: `ForceNew:true` + omit agent_id → Portal uses default. **Follow spec table: `/new` uses current mapping agent or default.** Implement helper `resolveAgentForNew`:
   - GET binding by Resolve without force with empty agent_id → returns current agent if mapped, else creates with default (bad). Better: add Runtime `GET binding` — **YAGNI**: `/new` = `force_new=true` + empty `agent_id` → Portal default only. Document deviation OR pass last known from cache reply. **Plan lock:** `/new` sends `force_new=true` and `agent_id` omitted (Portal default). Update README accordingly if stricter “same agent new session” needed later.
3. Non-command: `Resolve` with `AgentID` omitted (Portal default), `ForceNew:false`. **Do not** send `ch.DefaultAgent` from yaml.
4. Webhook JSON optional `agent_id` / `force_new` merged into Resolve for non-command messages.
5. Map HTTP 403/404/409 to user-facing Chinese/English short strings.

- [ ] **Step 1: Tests for command path (wecom or webhook)**

Fake Sessions + Runtime; assert `/agent` does not call TurnsFinal; assert Resolve ForceNew.

- [ ] **Step 2: Implement adapter changes**

- [ ] **Step 3: Run**

```bash
cd gateway && go test ./internal/adapter/ ./internal/command/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git add gateway/internal/adapter gateway/internal/command
git commit -m "feat(gateway): intercept slash commands and resolve via Portal allowlist"
```

---

### Task 9: Web ChannelForm allowed_agents UI

**Files:**
- Modify: `web/src/api/client.ts` (Channel types: `allowed_agents?: string[]`)
- Modify: `web/src/pages/ChannelForm.tsx`

- [ ] **Step 1: Multi-select or checkbox list of agents**

State `allowedAgents: string[]`; on load from `channel.allowed_agents`; on submit send `allowed_agents`. Ensure `default_agent` is included when list non-empty (UI auto-check default).

- [ ] **Step 2: Manual smoke** — create/edit channel in UI (dev). No Jest required if project lacks component tests; keep change minimal.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/client.ts web/src/pages/ChannelForm.tsx
git commit -m "feat(web): edit channel allowed_agents allowlist"
```

---

### Task 10: Docs + yaml migration notes + E2E checklist

**Files:**
- Modify: `gateway/README.md`
- Modify: `gateway/configs/channels.yaml` (comment that `default_agent` is ignored for routing when Portal configured; keep field optional for backward compat during rollout — Gateway code must not require it)
- Modify: `docs/superpowers/specs/2026-08-10-gateway-portal-agent-routing-design.md` status → 实现中 / 已规划
- Optionally add short note to `docs/superpowers/specs/2026-08-09-inbound-gateway-design.md` linking this spec

- [ ] **Step 1: Document Portal channel rows for `demo-webhook` / `sixath4` with same channel_id and default/allowed**

- [ ] **Step 2: Commit**

```bash
git add gateway/README.md gateway/configs/channels.yaml docs/superpowers/specs/
git commit -m "docs: Portal-owned agent routing ops and yaml deprecation"
```

---

### Task 11: Integration smoke (manual / script)

- [ ] Apply migration `014` on dev MySQL (or rely on AutoMigrate if Channel model already migrates — prefer explicit SQL + AutoMigrate both).
- [ ] Ensure Portal `channels` row: `channel_id=sixath4`, `default_agent=<uuid>`, `allowed_agents=[uuid1,uuid2]`.
- [ ] Restart Portal + Gateway.
- [ ] WeCom or webhook: `/agents` lists; `/agent <other>` rebinds; next normal message uses new agent; `/unbind` then message creates fresh default session.
- [ ] Confirm yaml `default_agent` change without Portal change does **not** alter routing.

---

## Spec coverage self-check

| Spec requirement | Task |
|------------------|------|
| `allowed_agents` on Portal channels | 1–2 |
| default ∈ allowed validation | 2 |
| Resolve decision table / AGENT_BOUND / force_new | 4–5 |
| Delete binding endpoint | 5–6, 8 |
| List channel agents | 5–6, 8 |
| Slash commands | 7–8 |
| Webhook API agent_id/force_new | 8 |
| ChannelForm UI | 9 |
| yaml default_agent not routing truth | 8, 10 |
| Cache invalidate | 6, 8 |
| Tests listed in spec §7 | 4, 5, 7, 8 |

**Note:** `/new` locked to `force_new` + omitted `agent_id` (Portal default) for YAGNI; same-agent fresh session can be a follow-up.
