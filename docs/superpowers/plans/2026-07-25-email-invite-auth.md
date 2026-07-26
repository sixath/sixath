# Email Login + Invite Registration (Phase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Portal-native email/password login, invite-link registration (single-use or reusable), Org create API+UI, optional SMTP email verify — while keeping Bearer ACL and Phase 1 developer Token login.

**Architecture:** Extend `users` + new `org_invites` (+ optional `email_verify_tokens`). New `AuthUsecase` handles login/register/verify and issues opaque Bearer into `user_tokens`. Org/invite HTTP handlers require caller; Auth middleware skips `/api/v1/auth/`. Web Login becomes email-first; `/register`, `/orgs`, `/verify-email` added. SMTP behind a `Mailer` interface (noop when unset).

**Tech Stack:** Go (Kratos, GORM, `golang.org/x/crypto/bcrypt`), React/Vite web, MySQL. Spec: `docs/superpowers/specs/2026-07-25-email-invite-auth-design.md`.

**Repos:** Work in `portal/` and `web/` (separate git repos if submodules). Docs commits in monorepo root.

---

## File map

| Area | Files |
|------|--------|
| Models / migrate | `portal/internal/data/model/identity.go`, `portal/migrations/008_email_invite_auth.sql`, AutoMigrate in `data.go` |
| Conf | `portal/internal/conf/conf.proto` (+ regenerate `conf.pb.go`), `configs/config.yaml` |
| Password | `portal/internal/biz/password.go` (+ test) |
| Identity / invites | `portal/internal/biz/identity.go`, `portal/internal/data/identity_mysql.go`, `portal/internal/data/invite_mysql.go`, `portal/internal/data/bootstrap.go` |
| Auth usecase | `portal/internal/biz/auth_usecase.go` (+ test) |
| Org/invite API | Extend `portal/internal/biz/acl_api.go` or new `org_usecase.go`; handlers `portal/internal/server/auth_http.go`, `org_http.go`; `http.go` routes |
| Middleware | `portal/internal/server/middleware/auth.go` (+ test) |
| Mailer | `portal/internal/biz/mailer.go` (noop + SMTP stub) |
| Wire | `portal/cmd/backend/wire.go` / providers |
| Web | `web/src/api/sessionAuth.ts` (+ Bearer org APIs in `client.ts` or `orgApi.ts`); `LoginPage.tsx`; `RegisterPage.tsx`; `VerifyEmailPage.tsx`; `OrgListPage.tsx`; `OrgDetailPage.tsx`; `App.tsx` nav |

**Out of scope:** OIDC, password reset, Playwright e2e, forcing email verification on business APIs.

---

### Task 1: Schema + conf fields

**Files:**
- Modify: `portal/internal/data/model/identity.go`
- Create: `portal/migrations/008_email_invite_auth.sql`
- Modify: `portal/internal/data/data.go` (AutoMigrate new models)
- Modify: `portal/internal/conf/conf.proto` (+ `make` / `protoc` regenerate as project usually does)
- Modify: `portal/configs/config.yaml` (optional bootstrap_email/password, smtp commented)

- [ ] **Step 1: Extend User model**

```go
Email             string     `gorm:"column:email;size:255;uniqueIndex"`
PasswordHash      string     `gorm:"column:password_hash;size:255"`
EmailVerifiedAt   *time.Time `gorm:"column:email_verified_at"`
```

- [ ] **Step 2: Add OrgInvite (+ EmailVerifyToken) models** in same file or `invite.go`

Fields per spec §3.2 / §3.3. Table names `org_invites`, `email_verify_tokens`.

- [ ] **Step 3: SQL migration 008** — ALTER users ADD columns; CREATE org_invites; CREATE email_verify_tokens (indexes on token_hash, org_id).

- [ ] **Step 4: Conf Auth message** — add optional `bootstrap_email`, `bootstrap_password`, and nested or flat `smtp_host`, `smtp_port`, `smtp_user`, `smtp_password`, `smtp_from`. Regenerate pb.

- [ ] **Step 5: Commit (portal)**

```bash
cd portal
git add internal/data/model internal/data/data.go migrations/008_email_invite_auth.sql internal/conf configs/config.yaml
git commit -m "feat(data): email/password fields and org_invites schema"
```

---

### Task 2: Password helpers (TDD)

**Files:**
- Create: `portal/internal/biz/password.go`
- Create: `portal/internal/biz/password_test.go`

- [ ] **Step 1: Failing tests** — Hash then Compare ok; wrong password fails; empty password Hash errors.

- [ ] **Step 2: Implement with bcrypt cost 12**

```go
func HashPassword(plain string) (string, error)
func CheckPassword(hash, plain string) bool
```

Add `golang.org/x/crypto` to `go.mod` via `go get`.

- [ ] **Step 3: `go test ./internal/biz/ -run Password -count=1`** → PASS

- [ ] **Step 4: Commit** `feat(biz): bcrypt password hash helpers`

---

### Task 3: Identity + invite repos

**Files:**
- Modify: `portal/internal/biz/identity.go` (User fields; repo methods)
- Modify: `portal/internal/data/identity_mysql.go`
- Create: `portal/internal/data/invite_mysql.go`
- Modify: fakes in `*_test.go` as compile requires

- [ ] **Step 1: Extend `User` biz struct** with Email, PasswordHash, EmailVerifiedAt

- [ ] **Step 2: IdentityRepo methods**

```go
GetUserByEmail(ctx, email string) (*User, error)
CreateUserWithPassword(ctx, id, name, email, passwordHash string) (*User, error)
SetEmailVerified(ctx, userID string, at time.Time) error
SetUserEmailPassword(ctx, userID, email, passwordHash string) error // bootstrap
ListUserOrgs(ctx, userID string) ([]OrgMembership, error) // id,name,role
```

- [ ] **Step 3: InviteRepo interface** (new file `biz/invite.go` or on IdentityRepo)

```go
CreateInvite(...) (*OrgInvite, plainToken string, error)
GetInviteByTokenHash(ctx, hash string) (*OrgInvite, error)
ListInvitesByOrg(ctx, orgID string) ([]*OrgInvite, error)
IncrementInviteUsed(ctx, id string) error
RevokeInvite(ctx, id string) error
```

Preview validity helper: not revoked, not expired, `max_uses==0 || used_count < max_uses`.

Also add `EmailVerifyTokenRepo` (or methods on IdentityRepo): `CreateVerifyToken`, `ConsumeVerifyToken` — used by Task 6 when SMTP is on.

Register must increment `used_count` atomically (transaction or conditional `UPDATE ... WHERE used_count < max_uses OR max_uses=0`) so single-use invites cannot double-register under concurrency.

- [ ] **Step 4: GORM impl + unit tests with sqlite/mysql fake if project pattern allows; else table-driven pure helpers for invite validity**

- [ ] **Step 5: Commit** `feat(data): identity email lookup and org invite repo`

---

### Task 4: AuthUsecase login/register (TDD)

**Files:**
- Create: `portal/internal/biz/auth_usecase.go`
- Create: `portal/internal/biz/auth_usecase_test.go`
- Create: `portal/internal/biz/mailer.go` (interface + NoopMailer)

- [ ] **Step 1: Write tests with fake repos**

Cases:
1. Login success → returns non-empty token; CheckPassword path
2. Login bad password → Unauthorized (same as unknown email)
3. Register valid invite → user member of org, used_count++, token issued
4. Register reused single-use invite → BadRequest
5. Register duplicate email → Conflict

- [ ] **Step 2: Implement AuthUsecase**

```go
Login(ctx, email, password) (*AuthSession, error)
Register(ctx, email, password, invitePlain) (*AuthSession, error)
PreviewInvite(ctx, invitePlain) (*InvitePreview, error)
VerifyEmail(ctx, tokenPlain) error
```

`AuthSession`: Token, UserID, Email, Orgs, EmailVerified.  
Issue token: `crypto/rand` 32 bytes → base64/hex plain; `IssueToken` / UpsertTokenHash.  
Register role: `member`.  
Mailer: if SMTP configured later, `SendVerifyEmail`; Task 4 can inject Noop.

- [ ] **Step 3: Tests PASS**

- [ ] **Step 4: Commit** `feat(biz): email login and invite registration usecase`

---

### Task 5: Org create/list + invite HTTP API

**Files:**
- Modify: `portal/internal/biz/acl_api.go` (or `org_usecase.go`)
- Create: `portal/internal/server/auth_http.go`, `org_http.go` (handlers mirroring `acl_api.go` style)
- Modify: `portal/internal/server/http.go` routes
- Modify: `portal/internal/server/middleware/auth.go` + `auth_test.go`

- [ ] **Step 1: Middleware** — skip auth when path has prefix `/api/v1/auth/` (in addition to webhooks). Test: login path without Bearer reaches handler (or middleware allows).

- [ ] **Step 2: Org usecase methods**

```go
CreateOrg(ctx, name) (*Org, error) // caller owner
ListMyOrgs(ctx) ([]OrgMembership, error)
CreateInvite(ctx, orgID, maxUses int, expiresInHours int) (plain, inviteMeta, error) // owner only
ListInvites / RevokeInvite
```

- [ ] **Step 3: Register routes** on `srv.Route("/")`:

```
POST /api/v1/auth/login
POST /api/v1/auth/register
GET  /api/v1/auth/invites/{token}
POST /api/v1/auth/verify-email
POST /api/v1/orgs
GET  /api/v1/orgs
POST /api/v1/orgs/{id}/invites
GET  /api/v1/orgs/{id}/invites
DELETE /api/v1/orgs/{id}/invites/{invite_id}
```

JSON shapes match spec §4. Invite create response includes `invite_token` and `invite_path` (`/register?invite=`).

- [ ] **Step 4: Wire providers** in `cmd/backend`

- [ ] **Step 5: `go test ./internal/...` + manual curl login/register against local**

- [ ] **Step 6: Commit** `feat(api): auth login/register and org invite endpoints`

---

### Task 6: Bootstrap email/password + SMTP Mailer

**Files:**
- Modify: `portal/internal/data/bootstrap.go`
- Create: `portal/internal/biz/smtp_mailer.go` (or `internal/data/smtp_mailer.go`)
- Modify: config yaml example

- [ ] **Step 1: Bootstrap** — if `bootstrap_email`+`bootstrap_password` set, `SetUserEmailPassword` on bootstrap user (hash password).

- [ ] **Step 2: Mailer** — `NewMailer(auth *conf.Auth) Mailer`: if smtp host empty → Noop; else net/smtp send. Verify link base URL from config or relative path only in email body (`/verify-email?token=`).

- [ ] **Step 3: AuthUsecase Register calls mailer when not noop; create email_verify_tokens row.

- [ ] **Step 4: Commit** `feat(portal): bootstrap email credentials and optional SMTP mailer`

---

### Task 7: Web auth API + Login/Register/VerifyEmail

**Files (web repo):**
- Create/Modify: `web/src/api/sessionAuth.ts` (login/register/preview/verify — **no** Bearer required; use raw `fetch` to `/api/v1/auth/...`)
- Modify: `web/src/api/auth.ts` — helper `applyLoginSession(session)` applying org-select rules from spec §5
- Modify: `web/src/pages/LoginPage.tsx` (+ css)
- Create: `web/src/pages/RegisterPage.tsx`, `VerifyEmailPage.tsx`
- Modify: `web/src/App.tsx` public routes

- [ ] **Step 1: sessionAuth API** + unit test for `pickOrgIdAfterLogin(orgs, stored)` pure function

- [ ] **Step 2: LoginPage** — email/password primary; submit → login API → `applyLoginSession` → navigate next. Collapsible developer Token form (existing).

- [ ] **Step 3: RegisterPage** — require invite query; preview; email + password + confirm password (client-side match); register; apply session. User `name` defaults to email local-part (server also sets).

- [ ] **Step 4: VerifyEmailPage** — public route. Optional: after email login, if `email_verified===false` show a muted banner (no hard block).

- [ ] **Step 5: `npm test` && `npm run build`**

- [ ] **Step 6: Commit (web)** `feat(web): email login and invite registration pages`

---

### Task 8: Web Org list/detail + nav

**Files:**
- Create: `web/src/pages/OrgListPage.tsx`, `OrgDetailPage.tsx`
- Modify: `web/src/api/client.ts` or `orgApi.ts` (Bearer required)
- Modify: `App.tsx` sidebar link「组织」
- Modify: `SettingsPage.tsx` link to `/orgs`

- [ ] **Step 1: OrgList** — list, create form, select current org button

- [ ] **Step 2: OrgDetail** — if owner: create invite (max_uses 1 / 0 / N, expires hours), copy link, list, revoke; else read-only

- [ ] **Step 3: Build + manual checklist**

- [ ] **Step 4: Commit** `feat(web): org create and invite management UI`

---

### Task 9: Docs status + acceptance

**Files:**
- Modify: `docs/superpowers/specs/2026-07-25-email-invite-auth-design.md` status → Phase 2 已落地
- Optional: tick plan checkboxes

- [ ] **Step 1: Portal `go test ./...` (or `./internal/...`)**  
- [ ] **Step 2: Web `npm test` && `npm run build`**  
- [ ] **Step 3: Manual** — bootstrap email login OR token → create org → single-use invite → register second user → reusable invite → developer token still works; no SMTP required  
- [ ] **Step 4: Commit docs** `docs: mark email invite auth Phase 2 as landed`

---

## Manual acceptance

- [ ] Email login with bootstrap_email/password  
- [ ] Create org + single-use invite → register → login  
- [ ] Reusable invite (`max_uses=0`) works twice  
- [ ] Non-owner cannot create invite (403)  
- [ ] Developer Token login still works  
- [ ] Phase 1 logout/401 gate still works  

---

## Execution notes

- Prefer `@superpowers:subagent-driven-development` with portal worktree `feature/email-invite-auth` and web worktree `feature/email-invite-auth`.  
- Do not start OIDC.  
- Keep invite plaintext out of logs and list APIs.
