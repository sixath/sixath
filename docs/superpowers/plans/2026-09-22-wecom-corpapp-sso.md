# WeCom CorpApp SSO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for optional tracking — checking boxes is not required to complete the work.

**Goal:** Add Portal CorpApp WeCom SSO: configure → SSO start/callback → auto-register by `userid` (no org) → ticket exchange → Bearer session; login page WeCom-primary.

**Architecture:** Extend `AuthUsecase` with WeCom flows. Persist OAuth `state`/`ticket` in MySQL `auth_ephemeral`; map `wecom`+`userid` → `users` via `user_identities`. Callback never puts Bearer in the URL — one-time ticket → `POST /exchange` → same `issueSession` as email login. Frontend: primary CTA + `/login/wecom/callback`.

**Tech Stack:** Go (Kratos, GORM), React/Vite, MySQL. Spec: `docs/superpowers/specs/2026-09-22-wecom-corpapp-sso-design.md`.

**Repos:** `portal/` + `web/` (+ root docs). Do not touch Gateway / wecom_bot.

---

## File map

| Area | Files |
|------|--------|
| Models / migrate | `portal/internal/data/model/identity.go` (or new `auth_ephemeral.go` / `user_identity.go`), `portal/migrations/017_wecom_corpapp_sso.sql`, `portal/internal/data/data.go` AutoMigrate |
| Conf | `portal/internal/conf/conf.proto` + regenerate/update `conf.pb.go`, `portal/configs/config.yaml`, `config.docker.yaml` |
| Repos | `portal/internal/biz/identity.go` (extend), `portal/internal/data/identity_mysql.go`, `portal/internal/data/auth_ephemeral_mysql.go`, fakes in `*_test.go` |
| WeCom HTTP client | `portal/internal/biz/wecom_oauth.go` (+ test with fake transport) |
| Auth usecase | `portal/internal/biz/auth_usecase.go`, `auth_usecase_test.go`, `acl_errors.go` (new reasons) |
| Wire | `portal/internal/biz/auth_usecase.go` `ProvideAuthUsecase`; `wire_gen.go` if ctor signature changes |
| HTTP | `portal/internal/server/auth_wecom_http.go` (+ test), `http.go` routes |
| Web API | `web/src/api/sessionAuth.ts` |
| Web UI | `web/src/pages/LoginPage.tsx` (+ css), `WecomCallbackPage.tsx`, `App.tsx` |

**Out of scope:** Email account linking, invite gate on WeCom path, embedded `ww.createWWLoginPanel`, ServiceApp, Gateway changes.

---

### Task 1: Schema + conf fields

**Files:**
- Modify: `portal/internal/data/model/identity.go` (append models) **or** Create: `portal/internal/data/model/user_identity.go`, `auth_ephemeral.go`
- Create: `portal/migrations/017_wecom_corpapp_sso.sql`
- Modify: `portal/internal/data/data.go` (AutoMigrate)
- Modify: `portal/internal/conf/conf.proto` (+ `conf.pb.go`)
- Modify: `portal/configs/config.yaml`, `portal/configs/config.docker.yaml`

- [ ] **Step 1: Add GORM models**

```go
// UserIdentity maps an external IdP subject to a Portal user.
type UserIdentity struct {
	Provider  string    `gorm:"column:provider;primaryKey;size:32"`
	Subject   string    `gorm:"column:subject;primaryKey;size:128"`
	UserID    string    `gorm:"column:user_id;size:36;not null;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}
func (UserIdentity) TableName() string { return "user_identities" }

// AuthEphemeral holds one-time OAuth state / exchange tickets.
type AuthEphemeral struct {
	ID          string     `gorm:"column:id;primaryKey;size:64"`
	Kind        string     `gorm:"column:kind;size:32;not null;index"` // wecom_state | wecom_ticket
	PayloadJSON string     `gorm:"column:payload_json;type:text;not null"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;not null;index"`
	ConsumedAt  *time.Time `gorm:"column:consumed_at"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
}
func (AuthEphemeral) TableName() string { return "auth_ephemeral" }
```

- [ ] **Step 2: SQL migration `017_wecom_corpapp_sso.sql`**

```sql
CREATE TABLE IF NOT EXISTS user_identities (
  provider VARCHAR(32) NOT NULL,
  subject VARCHAR(128) NOT NULL,
  user_id VARCHAR(36) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (provider, subject),
  INDEX idx_user_identities_user_id (user_id)
);

CREATE TABLE IF NOT EXISTS auth_ephemeral (
  id VARCHAR(64) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  payload_json TEXT NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  consumed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  INDEX idx_auth_ephemeral_kind (kind),
  INDEX idx_auth_ephemeral_expires (expires_at)
);
```

Note: `users.email` is already nullable `*string` with unique index — multiple NULLs are OK in MySQL. No ALTER needed on `users`.

- [ ] **Step 3: AutoMigrate** — add `&model.UserIdentity{}, &model.AuthEphemeral{}` to the list in `data.go`.

- [ ] **Step 4: Conf Auth fields** in `conf.proto` (next field numbers after `public_base_url = 12`):

```protobuf
  string wecom_corp_id = 13;
  string wecom_agent_id = 14;
  string wecom_secret = 15;
  string wecom_redirect_uri = 16;
```

Regenerate `conf.pb.go` the same way this repo last updated Auth (protoc or hand-extend getters/`GetWecomCorpId` etc. so YAML keys `wecom_corp_id` bind). Add commented examples to `config.yaml` / `config.docker.yaml` per spec §5.

- [ ] **Step 5: Commit**

```bash
git add portal/internal/data/model portal/internal/data/data.go portal/migrations/017_wecom_corpapp_sso.sql portal/internal/conf portal/configs
git commit -m "feat(data): user_identities and auth_ephemeral for WeCom SSO"
```

---

### Task 2: Identity + ephemeral repos (TDD)

**Files:**
- Modify: `portal/internal/biz/identity.go`
- Modify: `portal/internal/data/identity_mysql.go`
- Create: `portal/internal/data/auth_ephemeral_mysql.go` (+ `_test.go` with sqlite AutoMigrate like other data tests)
- Update all `IdentityRepo` fakes: `auth_usecase_test.go`, `identity_test.go`, `acl_api_test.go`, etc.

- [ ] **Step 1: Extend `IdentityRepo`**

```go
// WeCom / IdP users: email and password_hash NULL; email_verified_at set.
CreateUserForIdentity(ctx context.Context, name string, verifiedAt time.Time) (*User, error)
DeleteUser(ctx context.Context, userID string) error // orphan rollback only

GetIdentity(ctx context.Context, provider, subject string) (userID string, err error) // ErrNotFound if missing
CreateIdentity(ctx context.Context, provider, subject, userID string) error // ErrConflict on duplicate PK
```

Optional: keep ephemeral on a separate interface:

```go
type AuthEphemeralRepo interface {
	Put(ctx context.Context, id, kind, payloadJSON string, expiresAt time.Time) error
	// Consume deletes or marks consumed atomically; returns payload or ErrNotFound if missing/expired/already used.
	Consume(ctx context.Context, id, kind string, now time.Time) (payloadJSON string, err error)
}
```

- [ ] **Step 2: Implement MySQL**

`CreateUserForIdentity`: insert `users` with `Email=nil`, `PasswordHash=nil`, `EmailVerifiedAt=&verifiedAt`, `Name=name`.

`CreateIdentity`: insert; map MySQL duplicate → `pkgErrors.ErrConflict`.

`Consume`: transaction `SELECT … FOR UPDATE` (or single `UPDATE … SET consumed_at=now WHERE id=? AND kind=? AND consumed_at IS NULL AND expires_at > ?` then read payload). Prefer conditional UPDATE + check `RowsAffected` so concurrent consume fails cleanly.

- [ ] **Step 3: Tests**

- Put then Consume succeeds once; second Consume → NotFound
- Expired row → NotFound
- CreateIdentity duplicate → Conflict
- CreateUserForIdentity → Email empty / EmailVerifiedAt set

Run: `cd portal && go test ./internal/data/ -run 'Ephemeral|Identity|WeCom|UserIdentity' -count=1`

- [ ] **Step 4: Wire provider** — add `ProvideAuthEphemeralRepo(data *Data) biz.AuthEphemeralRepo` (or equivalent) in `data` package so Task 4–5 can inject it without a compile gap. List it in `portal/cmd/backend/wire.go` providers if present; otherwise note for Task 5.

- [ ] **Step 5: Commit** `feat(data): WeCom identity and auth_ephemeral repos`

---

### Task 3: WeCom OAuth client (TDD)

**Files:**
- Create: `portal/internal/biz/wecom_oauth.go`
- Create: `portal/internal/biz/wecom_oauth_test.go`

- [ ] **Step 1: Interface + real client**

```go
type WeComOAuthClient interface {
	GetUserID(ctx context.Context, code string) (userid string, err error)
	// optional later: GetUserName(ctx, userid) (string, error)
}

type weComOAuthClient struct {
	corpID, secret string
	httpClient     *http.Client
	// process-local token cache
	mu        sync.Mutex
	token     string
	tokenExp  time.Time
}
```

Endpoints (spec §6):
1. `GET https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=&corpsecret=`
2. `GET https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo?access_token=&code=`

Parse JSON `errcode`/`errmsg`; non-zero → error. Require non-empty `userid`; if only `openid` → return sentinel that usecase maps to `INVALID_WECOM_USER`.

Cache `access_token` until `expires_in - 60s`.

- [ ] **Step 2: Tests with `httptest.Server`** — token then userinfo success; errcode≠0; missing userid.

Run: `cd portal && go test ./internal/biz/ -run WeComOAuth -count=1`

- [ ] **Step 3: Commit** `feat(biz): WeCom gettoken/getuserinfo client`

---

### Task 4: AuthUsecase WeCom flows (TDD)

**Files:**
- Modify: `portal/internal/biz/auth_usecase.go`
- Modify: `portal/internal/biz/auth_usecase_test.go`
- Modify: `portal/internal/biz/acl_errors.go`

- [ ] **Step 1: Errors**

```go
ErrWeComLoginDisabled = kratosErrors.BadRequest("WECOM_LOGIN_DISABLED", "wecom login is not configured")
ErrInvalidWeComUser   = kratosErrors.Forbidden("INVALID_WECOM_USER", "not a corp member")
ErrInvalidState       = kratosErrors.BadRequest("INVALID_STATE", "invalid or expired state")
ErrInvalidCode        = kratosErrors.BadRequest("INVALID_CODE", "wecom code exchange failed")
ErrInvalidTicket      = kratosErrors.BadRequest("INVALID_TICKET", "invalid or expired ticket")
```

- [ ] **Step 2: Extend `AuthUsecase` ctor**

Add fields: `ephemeral AuthEphemeralRepo`, `wecom WeComOAuthClient` (nil if disabled), `wecomEnabled bool`, `corpID`, `agentID`, `redirectURI`, `publicBaseURL string`.

Update `ProvideAuthUsecase` to read conf, build client when corp+agent+secret set and redirect resolvable (`wecom_redirect_uri` or `public_base_url + "/api/v1/auth/wecom/callback"`).

Update `NewAuthUsecase` / all test call sites, or add `NewAuthUsecaseWithWeCom(...)` to avoid churn — prefer extending `ProvideAuthUsecase` + optional nils in tests.

Helper:

```go
func sanitizeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}
```

- [ ] **Step 3: Failing tests (fake repos + fake WeCom client)**

1. `WeComEnabled` false when conf incomplete  
2. `StartWeCom` when disabled → `WECOM_LOGIN_DISABLED`  
3. `StartWeCom` puts `wecom_state` with sanitized `next`; returns SSO URL containing `login_type=CorpApp`, `appid`, `agentid`, `redirect_uri`, `state`  
4. `HandleWeComCallback` invalid/expired state → `INVALID_STATE`  
5. First login: GetUserID → CreateUserForIdentity(name=`userid`) + CreateIdentity → ticket; no AddMember  
6. Second login: GetIdentity hits → ticket for same user_id  
7. Concurrent CreateIdentity Conflict → GetIdentity + DeleteUser orphan; still returns ticket  
8. No userid from client → `INVALID_WECOM_USER`  
9. GetUserID transport/API failure → `INVALID_CODE`  
10. `ExchangeWeComTicket` → `issueSession`: `email==""`, `email_verified==true`, `orgs` from ListUserOrgs (empty first; non-empty after fake AddMember)  
11. Ticket single-use

- [ ] **Step 4: Implement methods**

```go
func (uc *AuthUsecase) WeComEnabled() bool
func (uc *AuthUsecase) StartWeCom(ctx context.Context, next string) (ssoURL string, err error)
// HandleWeComCallback returns ticket + next for frontend redirect (or error).
func (uc *AuthUsecase) HandleWeComCallback(ctx context.Context, code, state string) (ticket, next string, err error)
func (uc *AuthUsecase) ExchangeWeComTicket(ctx context.Context, ticket string) (*AuthSession, error)
```

SSO URL base: `https://login.work.weixin.qq.com/wwlogin/sso/login` with query `login_type=CorpApp&appid=&agentid=&redirect_uri=&state=`.

State TTL ~10m; ticket TTL ~60s. IDs: crypto/rand 32 bytes hex.

Upsert algorithm (spec §4.1):
1. GetIdentity; if found → use userID  
2. Else CreateUserForIdentity with **name = userid** (M1; optional `user/get` name is M3) → CreateIdentity  
3. On CreateIdentity Conflict → DeleteUser(newUser) → GetIdentity again  
4. Put ticket `{user_id}`  
5. Never call AddMember / bootstrap_org  

Map WeCom client errors: empty userid → `ErrInvalidWeComUser`; other GetUserID failures → `ErrInvalidCode`.

Exchange: Consume ticket → GetUser → `issueSession` (unchanged).

- [ ] **Step 5: Run tests**

`cd portal && go test ./internal/biz/ -run 'AuthUsecase|WeCom' -count=1` → PASS

- [ ] **Step 6: Commit** `feat(biz): WeCom SSO start/callback/exchange`

---

### Task 5: HTTP handlers + routes

**Files:**
- Create: `portal/internal/server/auth_wecom_http.go`
- Create: `portal/internal/server/auth_wecom_http_test.go`
- Modify: `portal/internal/server/http.go`
- Wire: if `ProvideAuthUsecase` signature only still `(IdentityRepo, InviteRepo, *conf.Auth)`, inject ephemeral repo inside Provide via new provider, **or** add `AuthEphemeralRepo` to `ProvideAuthUsecase` and update `wire.go` / run `wire` / edit `wire_gen.go`.

- [ ] **Step 1: Handlers**

| Method | Path | Behavior |
|--------|------|----------|
| GET | `/api/v1/auth/wecom/status` | `{ "enabled": bool }` |
| GET | `/api/v1/auth/wecom/start` | query `next`; 302 Location=ssoURL; disabled → 400 `WECOM_LOGIN_DISABLED` |
| GET | `/api/v1/auth/wecom/callback` | query `code`,`state`; success 302 → `{public_base_url}/login/wecom/callback?ticket=&next=` (relative `/login/wecom/callback?...` if no public_base_url); on biz error 302 → `/login/wecom/callback?error=CODE` mapping reason |
| POST | `/api/v1/auth/wecom/exchange` | body `{ "ticket" }` → `toAuthSessionResponse` |

Use raw `http.ResponseWriter` redirect for 302 (Kratos `ctx.Response()`), same pattern as any existing redirect handlers if present; else:

```go
w := ctx.Response()
http.Redirect(w, ctx.Request(), loc, http.StatusFound)
return nil
```

Error code mapping: use `errors.FromError(err).Reason` when present (`INVALID_STATE`, etc.); else `INTERNAL`.

- [ ] **Step 2: Register routes** in `http.go` next to other auth routes (already under `/api/v1/auth/` → middleware skip).

- [ ] **Step 3: Handler tests** with fake usecase or lightweight stubs — disabled start 400; status JSON; exchange 200 shape.

- [ ] **Step 4: Commit** `feat(server): WeCom SSO HTTP endpoints`

---

### Task 6: Web — API + WeCom callback page

**Files:**
- Modify: `web/src/api/sessionAuth.ts`
- Create: `web/src/pages/WecomCallbackPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: API helpers**

```ts
export async function wecomStatus(): Promise<{ enabled: boolean }> {
  return authFetch<{ enabled: boolean }>('/auth/wecom/status')
}

export async function exchangeWecomTicket(ticket: string): Promise<AuthSession> {
  return authFetch<AuthSession>('/auth/wecom/exchange', {
    method: 'POST',
    body: JSON.stringify({ ticket }),
  })
}

/** Full-page navigation to Portal start (must leave SPA so 302 reaches WeCom). */
export function wecomStartHref(next: string): string {
  const q = new URLSearchParams({ next: next || '/' })
  return `${API_BASE}/auth/wecom/start?${q}`
}
```

- [ ] **Step 2: `WecomCallbackPage`**

- Read `ticket`, `error`, `next` from search params (sanitize `next` like LoginPage).  
- If `error`: show Chinese message map (`INVALID_STATE` → 「登录状态已失效，请重试」 etc.) + link to `/login`.  
- If `ticket`: call `exchangeWecomTicket` → `applyLoginSession` → `navigate(next)`.  
- Else: failure message.

- [ ] **Step 3: Route** `<Route path="/login/wecom/callback" element={<WecomCallbackPage />} />` (outside auth gate if Login is).

- [ ] **Step 4: Commit** `feat(web): WeCom ticket exchange callback page`

---

### Task 7: Web — Login page WeCom-primary UX

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/pages/LoginPage.css` (minimal)

- [ ] **Step 1: On mount** `wecomStatus()` → `enabled` state (default false while loading).

- [ ] **Step 2: Layout**

- If enabled: primary button 「企业微信登录」 → `window.location.assign(wecomStartHref(next))` (full navigation).  
- `<details>` 「高级登录」 containing existing email form + Token form (move Token out of separate details into the same advanced block, or keep nested — either OK if email+token are both “advanced”).  
- If disabled: hide WeCom button; advanced `<details open>` by default.

- [ ] **Step 3: Copy** — change muted subtitle when WeCom enabled to something like 「使用企业微信登录」; keep invite register link.

- [ ] **Step 4: Manual smoke** — without conf, advanced open; with conf (local), button hits start (expect 302 or 400).

- [ ] **Step 5: Commit** `feat(web): WeCom-primary login page`

---

### Task 8: Spec status + smoke checklist

**Files:**
- Modify: `docs/superpowers/specs/2026-09-22-wecom-corpapp-sso-design.md` — status →「实现完成（待配置启用）」

- [ ] **Step 1: Update spec status line** (→「实现完成（待配置启用）」)
- [ ] **Step 2: Verify commands**

```bash
cd portal && go test ./internal/biz/ ./internal/data/ ./internal/server/ -count=1
cd web && npm test -- --run 2>/dev/null || true   # if no unit tests, skip
```

- [ ] **Step 3: Commit** `docs: mark WeCom CorpApp SSO spec planned/in progress`

---

## Manual E2E (after deploy)

1. Set `wecom_corp_id` / `wecom_agent_id` / `wecom_secret` / `wecom_redirect_uri` (or `public_base_url`).  
2. WeCom admin: trusted domain + OAuth callback URL match.  
3. First login → new user, no org, no verify banner.  
4. Create org in UI → logout → WeCom again → same user, org restored in session.  
5. Misconfigured → status `enabled:false`, advanced login only.

---

## Execution notes

- Do **not** reuse wecom_bot channel secret.  
- Do **not** add user to `bootstrap_org_id` on WeCom register.  
- `issueSession` is the only session builder — keeps org list correct on re-login.  
- Prefer TDD order inside each task; commit after each task.
