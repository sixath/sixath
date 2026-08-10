# Gateway Message Auto-Route Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let inbound messages auto-select a channel allowlisted Agent via `@name|@id` or a Portal light classifier, then `force_new` on switch and run this message’s Turn immediately.

**Architecture:** Portal stores three channel flags and exposes `POST /runtime/v1/channels/{id}/route` (single-candidate short-circuit + aux LLM JSON classifier with fail-open). Gateway parses `@` against `ListChannelAgents`, otherwise calls `route`, then Resolve (retry with `force_new` on `AGENT_BOUND`) and Turn. Slash commands stay first and unchanged.

**Tech Stack:** Go (Portal Kratos + GORM, Gateway stdlib), MySQL migration `015`, React ChannelForm, existing Runtime service token, growth aux LLM via `model.NewModelFromConfig`.

**Spec:** `docs/superpowers/specs/2026-08-10-gateway-message-auto-route-design.md`

**Repo root:** `D:\workspace\github\sixath` (do not use nested `E:\workspace\github\sixath\sixath`).

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/migrations/015_channel_auto_route_flags.sql` | `auto_route_enabled/mention/classifier` BOOL DEFAULT 1 |
| `portal/internal/data/model/channel.go` | Model bool fields |
| `portal/internal/biz/channel.go` + `channel_usecase.go` + `channel_mysql.go` | CRUD map + defaults |
| `portal/internal/service/channel.go` (+ proto/JSON if needed) | Admin API expose flags |
| `web/src/pages/ChannelForm.tsx` + `web/src/api/client.ts` | Three checkboxes |
| `portal/internal/runtime/http.go` + `service.go` | `POST .../route`; agents reply + flags + description |
| `portal/internal/biz/agent_route.go` (+ `_test.go`) | Route decision table + classifier parse |
| `portal/internal/runtime/route_llm.go` | Wire growth aux model Completer |
| `gateway/internal/mention/parse.go` (+ `_test.go`) | Longest-name `@` match + strip |
| `gateway/internal/runtimeclient/client.go` | `RouteChannel`; agents reply flags |
| `gateway/internal/adapter/autoroute.go` (+ `_test.go`) | Shared prepare: flags → mention → route → resolve plan |
| `gateway/internal/adapter/resolve_rebind.go` (+ `_test.go`) | Resolve; on `AGENT_BOUND` invalidate + `force_new` |
| `gateway/internal/adapter/webhook.go` / `wecom_bot.go` | Insert after slash, before Resolve |
| `gateway/README.md` | Document auto-route |

---

### Task 1: Migration + Channel model auto-route flags

**Files:**
- Create: `portal/migrations/015_channel_auto_route_flags.sql`
- Modify: `portal/internal/data/model/channel.go`
- Modify: `portal/internal/biz/channel.go` (`ChannelCreate`, `ChannelMeta`)
- Modify: `portal/internal/data/channel_mysql.go` (Create/Update/Get/List mapping)
- Test: extend `portal/internal/biz/channel_allowed_agents_test.go` fake repo fields if needed

- [ ] **Step 1: Add migration**

```sql
-- portal/migrations/015_channel_auto_route_flags.sql
ALTER TABLE channels
  ADD COLUMN auto_route_enabled TINYINT(1) NOT NULL DEFAULT 1 COMMENT 'master auto-route switch' AFTER allowed_agents,
  ADD COLUMN auto_route_mention TINYINT(1) NOT NULL DEFAULT 1 COMMENT '@Agent mention routing' AFTER auto_route_enabled,
  ADD COLUMN auto_route_classifier TINYINT(1) NOT NULL DEFAULT 1 COMMENT 'classifier when no @' AFTER auto_route_mention;
```

- [ ] **Step 2: Extend model + biz structs**

```go
// model.Channel
AutoRouteEnabled    bool `gorm:"column:auto_route_enabled;not null;default:1"`
AutoRouteMention    bool `gorm:"column:auto_route_mention;not null;default:1"`
AutoRouteClassifier bool `gorm:"column:auto_route_classifier;not null;default:1"`

// biz.ChannelCreate / ChannelMeta — same three bool fields
```

On Create, if caller leaves zero-value unset semantics ambiguous: **default all three to `true`** when creating unless explicitly set false (use pointers in Create later only if API needs tri-state; for v1 set defaults in usecase Create: if JSON omitted, true).

- [ ] **Step 3: Map in `channel_mysql.go`**

Copy fields in `Create`, `channelModelToBiz`, and allow Update keys:
`auto_route_enabled`, `auto_route_mention`, `auto_route_classifier` (bool).

- [ ] **Step 4: Commit**

```bash
git add portal/migrations/015_channel_auto_route_flags.sql portal/internal/data/model/channel.go portal/internal/biz/channel.go portal/internal/data/channel_mysql.go
git commit -m "feat(portal): add channel auto_route_* flags"
```

---

### Task 2: Channel usecase + admin API + Web UI

**Files:**
- Modify: `portal/internal/biz/channel_usecase.go` (Create defaults; Update parse bools)
- Modify: `portal/internal/service/channel.go` (request/response map)
- Modify proto/generated only if channel API is protobuf-driven for these fields; if HTTP uses structpb/map, follow existing `allowed_agents` pattern
- Modify: `web/src/api/client.ts` (`Channel`, `CreateChannelRequest`)
- Modify: `web/src/pages/ChannelForm.tsx`
- Test: `portal/internal/biz/channel_auto_route_flags_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestChannelCreate_AutoRouteFlagsDefaultTrue(t *testing.T) {
	// Create with only ChannelID/Type/DefaultAgent; expect all three AutoRoute* == true
}

func TestChannelUpdate_CanDisableMasterSwitch(t *testing.T) {
	// updates["auto_route_enabled"]=false → meta.AutoRouteEnabled==false
}
```

- [ ] **Step 2: Run (expect FAIL)**

```bash
cd D:\workspace\github\sixath\portal && go test ./internal/biz/ -run TestChannelCreate_AutoRouteFlags -count=1
```

- [ ] **Step 3: Implement usecase + service + UI**

UI: three checkboxes under Allowed Agents, labels:
- Enable auto-route
- @Agent mention
- Classifier (no @)

Default checked on New Channel.

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz portal/internal/service/channel.go web/src/api/client.ts web/src/pages/ChannelForm.tsx
git commit -m "feat(portal,web): expose auto_route channel flags in API and form"
```

---

### Task 3: ListChannelAgents returns flags + description

**Files:**
- Modify: `portal/internal/runtime/service.go` (`channelAgentItem`, `channelAgentsReply`, `listChannelAgents`)
- Modify: `portal/internal/runtime/sessions_test.go`
- Modify: `gateway/internal/runtimeclient/client.go` (`ChannelAgentItem`, `ChannelAgentsReply`)

- [ ] **Step 1: Extend reply shape**

```go
type channelAgentItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type channelAgentsReply struct {
	DefaultAgent        string             `json:"default_agent"`
	Agents              []channelAgentItem `json:"agents"`
	AutoRouteEnabled    bool               `json:"auto_route_enabled"`
	AutoRouteMention    bool               `json:"auto_route_mention"`
	AutoRouteClassifier bool               `json:"auto_route_classifier"`
}
```

Populate flags from `ch.AutoRoute*`. Truncate description to ~200 runes when filling from `AgentMeta.Description`.

- [ ] **Step 2: Update Portal + Gateway client tests**

Expect flags true for channels created with defaults; agent description present when set.

- [ ] **Step 3: Commit**

```bash
git add portal/internal/runtime gateway/internal/runtimeclient
git commit -m "feat(runtime): include auto_route flags and agent description in channel agents list"
```

---

### Task 4: Portal `RouteChannel` decision logic (fake Completer)

**Files:**
- Create: `portal/internal/biz/agent_route.go`
- Create: `portal/internal/biz/agent_route_test.go`
- Modify: `portal/internal/biz/channel_peer.go` — add `GetBinding(ctx, channelID, peerID) (*ChannelPeerSession, error)` that does **not** create (wraps `peerRepo.Get`; map not-found cleanly)

- [ ] **Step 1: Define types + Completer interface**

```go
package biz

type RouteConfidence string // "high" | "low"
type RouteSource string     // "classifier" | "default" | "current"

type AgentRouteInput struct {
	ChannelID string
	PeerID    string // optional
	Text      string
}

type AgentRouteResult struct {
	AgentID    string
	Confidence RouteConfidence
	Source     RouteSource
	Reason     string
}

type RouteCandidate struct {
	ID          string
	Name        string
	Description string
}

type RouteCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

type AgentRouteUsecase struct {
	channels  ChannelRepo
	peers     ChannelPeerSessionRepo // or thin interface with Get only
	agents    interface{ GetForSession(context.Context, string) (*AgentMeta, error) }
	complete  RouteCompleter // nil => always fail-open without LLM
	timeout   time.Duration  // default 3s
}
```

- [ ] **Step 2: Failing tests**

```go
func TestRoute_SingleCandidate_NoLLM(t *testing.T) { /* Allowed empty → default only; Completer must not be called; high/default */ }
func TestRoute_ClassifierHigh_InAllowlist(t *testing.T) { /* fake Completer returns {"agent_id":"b","confidence":"high"} */ }
func TestRoute_ClassifierLow_FailOpenCurrent(t *testing.T) { /* binding exists → current + low */ }
func TestRoute_ClassifierTimeout_FailOpen(t *testing.T) { /* Completer blocks > timeout */ }
func TestRoute_BadJSON_FailOpen(t *testing.T) {}
func TestRoute_NonAllowlistID_FailOpen(t *testing.T) {}
func TestRoute_ChannelMissing_NotFound(t *testing.T) {}
```

- [ ] **Step 3: Run (expect FAIL)**

```bash
cd D:\workspace\github\sixath\portal && go test ./internal/biz/ -run TestRoute_ -count=1
```

- [ ] **Step 4: Implement `Route`**

Logic:
1. Load channel; 404 if missing.
2. Build candidate IDs = allowed or `[default]`.
3. If `len(candidates)==1` → return that id, high, `default`, reason `single_candidate` (no LLM).
4. Resolve `current`: if peer_id set, `GetBinding`; else empty. Fail-open target = current || default.
5. If Completer nil → fail-open immediately.
6. Else build prompt (candidates JSON + user text); `context.WithTimeout(3s)`; parse strict JSON; validate id ∈ candidates and confidence high → return classifier/high; else fail-open.

Prompt skeleton (keep short):

```text
Pick the best agent for the user message. Reply ONLY JSON: {"agent_id":"<uuid>","confidence":"high"|"low"}
Candidates:
[{"id":"...","name":"...","description":"..."}]
User:
<text>
```

- [ ] **Step 5: Tests PASS + commit**

```bash
git add portal/internal/biz/agent_route.go portal/internal/biz/agent_route_test.go portal/internal/biz/channel_peer.go
git commit -m "feat(portal): AgentRouteUsecase with fail-open classifier"
```

---

### Task 5: Runtime HTTP `POST .../route` + LLM wiring

**Files:**
- Modify: `portal/internal/runtime/http.go` — register route
- Modify: `portal/internal/runtime/service.go` — `handleRoute`, inject `AgentRouteUsecase`
- Modify: `portal/internal/runtime/sessions_test.go` — HTTP tests with fake Completer
- Create: `portal/internal/runtime/route_llm.go` — build Completer from `conf.Growth.GetLlm()` aux-first (same as growth)
- Modify: `portal/cmd/backend/wire.go` + regenerate `wire_gen.go` (`cd portal && go generate ./cmd/backend` or project’s wire command)

- [ ] **Step 1: Register**

```go
r.POST("/runtime/v1/channels/{channel_id}/route", svc.wrap(svc.handleRoute))
```

Request/response:

```go
type routeRequest struct {
	Text   string `json:"text"`
	PeerID string `json:"peer_id"`
}
// Response = AgentRouteResult JSON fields
```

- [ ] **Step 2: HTTP tests**

- single candidate → 200 high/default, Completer unused  
- multi + fake high → classifier  
- missing channel → 404 `CHANNEL_NOT_FOUND` (match existing error style)

- [ ] **Step 3: Wire Completer**

If growth LLM config missing, leave Completer nil (fail-open). Do not crash Portal boot.

When calling the model, set `temperature=0` and a small `max_tokens` (e.g. 64–128) if `ModelConfig` / Generate options support them; otherwise document the limitation in a code comment and keep the Completer prompt “JSON only”.

- [ ] **Step 4: Commit**

```bash
git add portal/internal/runtime portal/cmd/backend
git commit -m "feat(runtime): POST channel route endpoint with aux LLM"
```

---

### Task 6: Gateway `@` mention parser

**Files:**
- Create: `gateway/internal/mention/parse.go`
- Create: `gateway/internal/mention/parse_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestParse_LongestNameFirst(t *testing.T) {
	// candidates: "ops", "ops-bot"; text "@ops-bot hello" → id of ops-bot, stripped "hello"
}
func TestParse_UUID(t *testing.T) { /* @uuid */ }
func TestParse_CaseInsensitive(t *testing.T) {}
func TestParse_UnknownMention_NoHit(t *testing.T) { /* @foo stays; Hit=false */ }
func TestParse_FirstOnly(t *testing.T) { /* two valid @ → first */ }
func TestParse_NoAt(t *testing.T) {}
```

- [ ] **Step 2: Implement**

```go
type Candidate struct{ ID, Name string }
type Result struct {
	Hit     bool
	AgentID string
	Stripped string // text with mention removed
}

func Parse(text string, cands []Candidate) Result
```

Rules: scan for `@` tokens; match id exact or name EqualFold; prefer longest matching name; strip that token + adjacent spaces.

- [ ] **Step 3: Commit**

```bash
git add gateway/internal/mention
git commit -m "feat(gateway): parse and strip @Agent mentions against allowlist"
```

---

### Task 7: Gateway runtimeclient `RouteChannel`

**Files:**
- Modify: `gateway/internal/runtimeclient/client.go`
- Modify: `gateway/internal/runtimeclient/client_test.go`

- [ ] **Step 1: Types + method**

```go
type RouteRequest struct {
	Text   string `json:"text"`
	PeerID string `json:"peer_id,omitempty"`
}
type RouteReply struct {
	AgentID    string `json:"agent_id"`
	Confidence string `json:"confidence"`
	Source     string `json:"source"`
	Reason     string `json:"reason"`
}

func (c *Client) RouteChannel(ctx context.Context, channelID string, req RouteRequest) (*RouteReply, error)
```

Path: `POST /runtime/v1/channels/{id}/route`

- [ ] **Step 2: Test with httptest**

- [ ] **Step 3: Commit**

```bash
git add gateway/internal/runtimeclient
git commit -m "feat(gateway): runtimeclient RouteChannel"
```

---

### Task 8: Gateway `prepareAutoRoute` + webhook/wecom wiring

**Files:**
- Create: `gateway/internal/adapter/autoroute.go`
- Create: `gateway/internal/adapter/autoroute_test.go`
- Modify: `gateway/internal/adapter/webhook.go`
- Modify: `gateway/internal/adapter/wecom_bot.go`
- Modify: `gateway/internal/adapter/wecom_bot_test.go` (+ webhook tests if present)
- Modify: `gateway/internal/adapter/commands.go` only if sharing helpers (prefer not)

**Resolve pattern (reuse `AGENT_BOUND`):**

```text
desiredAgent, turnText, reason := ...
resolved, err := Sessions.Resolve(..., AgentID: desired, ForceNew: false)
if HTTP 409 AGENT_BOUND:
  Invalidate
  Resolve(..., ForceNew: true, Reason: reason)
Turn(turnText)
```

When no auto desire: plain Resolve as today; Turn original text.

- [ ] **Step 1: `prepareAutoRoute` API**

```go
type autoRoutePlan struct {
	AgentID   string // empty => do not pin agent on Resolve
	ForceHint bool   // optional; Prefer AGENT_BOUND retry over always force_new
	Reason    string
	TurnText  string
	Source    string // mention|classifier|none
}

func prepareAutoRoute(ctx context.Context, rt *runtimeclient.Client, channelID, peerID, text string) autoRoutePlan
```

Steps inside (order matters — mention miss must **not** skip classifier):

1. Start with `TurnText=text`, AgentID empty (none).
2. `list, listErr := ListChannelAgents(...)`.
3. If `listErr == nil` and `!list.AutoRouteEnabled` → return none.
4. **Mention** (only when list OK **and** `AutoRouteMention`):
   - `mention.Parse(text, candidates)`.
   - On **hit** → return AgentID, TurnText=stripped, Reason=`auto_mention` (**do not** call classifier).
   - On **miss** → continue (do **not** `else if` away from classifier).
5. **Classifier** when `AutoRouteClassifier` is on:
   - If list OK and flag false → skip.
   - If list failed: **still try** `RouteChannel` (spec §6). If flags unknown because list failed, treat classifier as enabled.
   - Call `RouteChannel`; on 5xx/timeout/client error → **swallow**, warn log, return none (do not abort Turn).
   - If `confidence=="high"` and `agent_id!=""` → pin AgentID, Reason=`auto_classifier`.
6. Otherwise none.

Pseudo:

```go
plan := autoRoutePlan{TurnText: text}
list, listErr := rt.ListChannelAgents(ctx, channelID)
if listErr == nil && !list.AutoRouteEnabled {
	return plan
}
if listErr == nil && list.AutoRouteMention {
	if hit := mention.Parse(text, toCands(list)); hit.Hit {
		plan.AgentID, plan.TurnText, plan.Reason, plan.Source = hit.AgentID, hit.Stripped, "auto_mention", "mention"
		return plan
	}
}
classifierOn := listErr != nil || list.AutoRouteClassifier
if classifierOn {
	reply, err := rt.RouteChannel(ctx, channelID, RouteRequest{Text: text, PeerID: peerID})
	if err != nil {
		log.Printf("auto-route classifier: %v", err) // fail-open
		return plan
	}
	if reply.Confidence == "high" && strings.TrimSpace(reply.AgentID) != "" {
		plan.AgentID, plan.Reason, plan.Source = reply.AgentID, "auto_classifier", "classifier"
	}
}
return plan
```

- [ ] **Step 2: Unit-test prepare with fake httptest Portal**

Cover: mention hit; unknown @ falls through to route; classifier high; flags off; list failure still attempts route; route 5xx fail-open.

- [ ] **Step 3: Wire webhook**

After slash command block, before Resolve:

```go
plan := prepareAutoRoute(ctx, h.deps.Runtime, ev.ChannelID, ev.PeerID, ev.Content)
content := plan.TurnText
if content == "" {
	content = ev.Content
}
// Preserve phase-1 webhook body fields when auto-route did not pin an agent.
agentID := plan.AgentID
forceNew := false
reason := plan.Reason
if agentID == "" {
	agentID = ev.AgentID
	forceNew = ev.ForceNew
}
if forceNew {
	// existing path: Resolve with ForceNew; Invalidate after
	resolved, err := h.deps.Sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: ev.ChannelID, PeerID: ev.PeerID, AgentID: agentID, ForceNew: true, Reason: reason,
	})
	// ...
} else if plan.AgentID != "" {
	resolved, err := resolveMaybeRebind(ctx, h.deps.Sessions, ev.ChannelID, ev.PeerID, plan.AgentID, plan.Reason)
} else {
	resolved, err := h.deps.Sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: ev.ChannelID, PeerID: ev.PeerID, AgentID: agentID, ForceNew: forceNew,
	})
}
```

Use `content` for Turn. Never drop `ev.AgentID` / `ev.ForceNew` when `plan.AgentID` is empty.

- [ ] **Step 4: Wire wecom**

Same order as webhook: slash → `prepareAutoRoute(ctx, ..., n.QuestionText)` → `resolveMaybeRebind` (or plain Resolve when AgentID empty) → Turn.

Parse `@` on `n.QuestionText` (already bot-stripped). On mention strip, rebuild `RuntimeContent` via `wecom.FormatRuntimeContent(n.AskerName, n.AskerID, strippedQuestion)`. Classifier uses QuestionText; Turn uses updated RuntimeContent.

- [ ] **Step 5: Integration-style adapter tests**

1. `@Ops Bot hello` → resolve agent-2, force_new on conflict, turn content stripped  
2. no @ + route high → force_new path  
3. `/agent` still no turn  
4. `auto_route_enabled=false` → no route/mention calls (or ignore hits)

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/adapter
git commit -m "feat(gateway): wire @mention and classifier auto-route into inbound adapters"
```

---

### Task 9: Docs + local smoke checklist

**Files:**
- Modify: `gateway/README.md` (auto-route section; yaml still not routing truth)
- Modify: spec status line to「设计已确认；实现中」optional

- [ ] **Step 1: Document**

- Pipeline order  
- Flags  
- `POST .../route`  
- Fail-open  

- [ ] **Step 2: Smoke (manual after implement)**

```text
1. Apply migration 015
2. Channel with ≥2 allowed agents; flags on
3. WeCom/webhook: "@<name> ping" → new session on that agent; content without @
4. Plain "查告警" with stub/high classifier → switch
5. Kill LLM / bad key → still replies on current agent
6. auto_route_enabled=false → no auto switch
```

- [ ] **Step 3: Commit**

```bash
git add gateway/README.md
git commit -m "docs(gateway): document message-level auto-route"
```

---

## Resolve conflict helper (shared snippet)

Put in `gateway/internal/adapter/resolve_rebind.go` if both adapters need it:

```go
func resolveMaybeRebind(ctx context.Context, sessions *session.Router, channelID, peerID, agentID, reason string) (*runtimeclient.ResolveReply, error) {
	out, err := sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: channelID, PeerID: peerID, AgentID: agentID,
	})
	if err == nil {
		return out, nil
	}
	var he *runtimeclient.HTTPError
	if errors.As(err, &he) && he.StatusCode == 409 && strings.Contains(string(he.Body), "AGENT_BOUND") {
		sessions.Invalidate(channelID, peerID)
		return sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
			ChannelID: channelID, PeerID: peerID, AgentID: agentID,
			ForceNew: true, Reason: reason,
		})
	}
	return nil, err
}
```

When `agentID==""`, call plain Resolve (no rebind logic).

---

## Out of scope (do not implement)

- Keyword rules, multi-@ arbitration, user-visible “classifying…” UI  
- Routing outside allowlist  
- In-place session agent swap without `force_new`  

---

## Execution notes

- Prefer feature branch `feat/gateway-message-auto-route` off `main`.  
- TDD per task; commit after each green task.  
- After Task 5, Portal must boot without growth LLM (Completer nil).  
- Nested clone under `E:\...\sixath\sixath` is stale — never commit there.
