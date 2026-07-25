# 邮箱登录与邀请注册设计（Phase 2）

**日期**: 2026-07-25  
**状态**: Phase 2 已落地（portal/web `feature/email-invite-auth`；待合并 main）  
**方案**: Portal 内聚 Auth（邮箱+密码+邀请）；OIDC 仅预留  
**关联**:  
- `docs/superpowers/specs/2026-07-24-web-login-design.md`（Phase 1 Token 门禁）  
- `docs/superpowers/specs/2026-07-23-user-resource-acl-design.md`（Bearer → `caller_user_id`）  
- `portal/internal/data/model/identity.go`、`portal/internal/server/middleware/auth.go`  
- `web/src/pages/LoginPage.tsx`、`web/src/api/auth.ts`  

---

## 1. 背景与问题

Phase 1 已提供全屏 Token 登录与路由守卫。产品需要邮箱密码登录与邀请链接注册；当前无密码字段、无邀请表、无创建 Org 的 HTTP/UI（仅有 `CreateOrg` 仓储与 bootstrap 默认 org）。

已确认选择：

| 项 | 选择 |
|----|------|
| 身份后端 | Portal 自建；架构预留日后 OIDC |
| 登录标识 | 邮箱 |
| 注册 | 邀请链接（创建时可选单次 / 可复用） |
| 发邀请 | 仅 org `owner`（及 bootstrap） |
| 创建 Org | 本期 API + 简单 Web |
| 验邮 | 可配置：默认关；配置 SMTP 后开启 |
| 登录后凭证 | 内部 Bearer → `user_tokens`（与 ACL 一致） |
| Phase 1 Token 表单 | 降为「开发者登录」 |

---

## 2. 目标与非目标

### 2.1 目标

1. 邮箱 + 密码登录；邀请链接注册。  
2. 登录/注册成功签发内部 Bearer；业务 API 契约不变。  
3. 任意已登录用户可创建 Org（创建者自动 `owner`）。  
4. Org owner 创建/列表/撤销邀请（`max_uses` + 过期）。  
5. 验邮可选（SMTP）；未配置时不挡登录。  
6. Web：登录主路径改邮箱密码；`/register`；组织与邀请页；Token 入口降级。  
7. 规格预留 OIDC 换发边界（本期不实现 `/auth/oidc/*`）。

### 2.2 非目标

| 项 | 说明 |
|----|------|
| OIDC / 外部 IdP | 另开迭代 |
| 找回密码 | 可后补 |
| 强制验邮墙 | 开启 SMTP 后仅提示，不强制阻断业务 API |
| Public Hub | 另开 spec |
| 细粒度 RBAC UI | owner/member 足够 |

---

## 3. 数据模型

### 3.1 `users` 扩展

| 字段 | 说明 |
|------|------|
| `email` | unique，可空（兼容仅 bootstrap/token 用户）；登录用户必填 |
| `password_hash` | 可空；有邮箱密码登录时必填；算法 bcrypt 或 argon2id |
| `email_verified_at` | nullable；SMTP 关闭时注册可直接视为已验证或保持 null（不强制） |
| `name` | 保留；默认可用邮箱 `@` 前前缀 |

### 3.2 `org_invites`

| 字段 | 说明 |
|------|------|
| `id` | UUID PK |
| `org_id` | 目标组织 |
| `token_hash` | SHA-256 hex（明文仅创建时返回一次） |
| `created_by` | 创建者 user_id |
| `max_uses` | `1` = 单次；`>1` 或约定 `0` 表示可复用上限（实现取：`0` 表示无限，或 UI 传 N；**规格约定：`max_uses=1` 单次，`max_uses=0` 无限可复用，`max_uses>1` 有限可复用**） |
| `used_count` | 已成功注册次数 |
| `expires_at` | nullable；null = 不过期 |
| `revoked_at` | nullable |
| `created_at` | |

### 3.3 可选 `email_verify_tokens`

仅 SMTP 开启时使用：`token_hash`、`user_id`、`expires_at`。

### 3.4 不变

`user_tokens`、`orgs`、`org_members`、Resource ACL 表与中间件契约。

---

## 4. API

### 4.1 公开（Auth 中间件放行）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | `{ email, password }` → `{ token, user_id, email, orgs[{id,name,role}], email_verified }` |
| POST | `/api/v1/auth/register` | `{ email, password, invite }`：校验邀请 → 建用户 → 入 org（`member`）→ `used_count++` → 签发 Bearer；SMTP 开则发验邮 |
| GET | `/api/v1/auth/invites/{token}` | 预览：`org_name`、是否有效（不消耗次数） |
| POST | `/api/v1/auth/verify-email` | `{ token }` |

登录失败：统一 401，不区分无用户/密码错。  
邀请无效/过期/用尽/撤销：400。  
邮箱冲突：409。

### 4.2 需登录

| 方法 | 路径 | 权限 |
|------|------|------|
| POST | `/api/v1/orgs` | 任意登录用户；body `{ name }`；创建者 `owner` |
| GET | `/api/v1/orgs` | 当前用户所属列表 |
| POST | `/api/v1/orgs/{id}/invites` | owner；body `{ max_uses, expires_in_hours? }`；响应含**一次**明文 `invite_token` 与拼好的 path |
| GET | `/api/v1/orgs/{id}/invites` | owner；无明文 |
| DELETE | `/api/v1/orgs/{id}/invites/{invite_id}` | owner 撤销 |
| 已有 | members / user tokens | 保持 |

业务 API 仍：`Authorization: Bearer` + 可选 `X-Org-Id`。

### 4.3 Auth 中间件

在 webhook 之外增加公开前缀放行：`/api/v1/auth/`（login/register/invite preview/verify-email）。

---

## 5. Web

### 5.1 公开页（无壳）

| 路由 | 行为 |
|------|------|
| `/login` | 主：邮箱+密码 → `saveCredentials`；次要折叠：「开发者 Token 登录」 |
| `/register` | `?invite=` 必填语义；预览 org；邮箱+密码+确认 |
| `/verify-email` | SMTP 开启时邮件链落地：读 `?token=` → 调 `verify-email` API → 提示结果并链回登录 |

### 5.2 壳内

| 路由 | 行为 |
|------|------|
| `/orgs` | 所属 org 列表、新建、选择当前 Org（写 `sixath-org-id`） |
| `/orgs/:id` | owner：邀请 CRUD、复制链接；非 owner 只读 |

设置页：链到组织；保留 Token/Org 高级项。  
门禁 / 退出 / 401 / `sixath-auth-gate`：沿用 Phase 1。  
SMTP 开启时：登录后可轻提示未验证邮箱（不强制墙）。

**登录后 Org 选择默认策略**

1. 若响应 `orgs.length === 1`：自动写入 `sixath-org-id`。  
2. 若多个：保留已存 org（若仍在列表中），否则清空并引导用户在 `/orgs` 选择（不强制打断 `next` 跳转）。  
3. 若零个：清空 org；用户可去「新建组织」。

---

## 6. 配置

| 项 | 说明 |
|----|------|
| 现有 `auth.bootstrap_*` | 保留 |
| 可选 `auth.bootstrap_email` / `bootstrap_password` | 给**现有** bootstrap 用户补邮箱/密码（upsert），非另建账号 |
| `auth.smtp_*` 或 env | host/port/user/pass/from；未配 = 验邮关 |
| Web `VITE_*` | 无强制变更；登录改走 API |

密码哈希成本使用安全默认（bcrypt cost ≥ 12 或等价 argon2id）。

---

## 7. OIDC 预留（不实现）

日后：`/auth/oidc/callback` → 映射/创建 `users`（按 email）→ 换发内部 Bearer 写入 `user_tokens`。  
`org_invites` 与 Org API 可复用。本期无 IdP 依赖。

---

## 8. 测试计划

- 单测：密码校验、邀请 `max_uses`/过期/撤销、非 owner 403、login/register 签发 token。  
- 中间件：`/api/v1/auth/*` 无 Bearer 可访问；业务路径仍要 Bearer。  
- 手工：建 org → 单次/可复用邀请 → 注册登录 → 开发者 Token 入口仍可用；无 SMTP 时注册不依赖邮件。

---

## 9. 决策记录

| 决策 | 结论 |
|------|------|
| 实现形状 | Portal 内聚 Auth |
| 邀请形态 | 单次 + 可复用（`max_uses`） |
| 发邀请 | org owner |
| 建 Org | API + Web |
| 验邮 | 可配置，默认关 |
| 凭证 | 内部 Bearer |
| OIDC | 仅预留 |
