# Web 登录门禁设计（Phase 1 Token + Phase 2 OIDC 预留）

**日期**: 2026-07-24  
**状态**: Phase 1 已落地（web feature/web-login-gate）  
**方案**: 分阶段 — Phase 1 全屏 Token 登录壳；Phase 2 OIDC（邮箱 + 邀请链接）另开实现  
**关联**:  
- `docs/superpowers/specs/2026-07-23-user-resource-acl-design.md`（Bearer → `caller_user_id`）  
- `web/src/api/auth.ts`、`web/src/pages/SettingsPage.tsx`  
- portal Auth 中间件（`Authorization: Bearer`、可选 `X-Org-Id`）  

---

## 1. 背景与问题

ACL 落地后，portal 要求 Bearer；web 目前仅有「设置」页粘贴 token，且 DEV 默认注入 `dev-bootstrap-token`，**没有真正的登录门禁**。用户期望邮箱密码 + 邀请链接，但现后端无密码库、无 OIDC。

已确认产品选择：

| 项 | 选择 |
|----|------|
| 终态认证 | OIDC（邮箱）+ 邀请链接注册进 org |
| 凭证形态（与 ACL 对齐） | Bearer Token + 可选 Org；前端 localStorage |
| 本期策略 | **分阶段 A**：先做登录 UI 壳（Token），再接 OIDC |
| 登录页布局 | **独立全屏**（无侧栏） |
| 本期不做 | 密码表、自建注册 API、邀请表、OIDC 接入 |

---

## 2. 目标与非目标

### 2.1 Phase 1 目标

1. 新增全屏 `/login`：Token（必填）+ Org ID（可选）；DEV 可一键填入 bootstrap。  
2. 路由守卫：无有效 token 时跳转 `/login?next=...`；已登录访问 `/login` 则进入 `next` 或 `/`。  
3. 退出：清除 localStorage 凭证，并避免 DEV bootstrap 在同会话内偷偷回填。  
4. API 401：友好提示并引导回登录（不自动发明 token）。  
5. 「设置」保留为更换凭证的高级入口；侧栏可显示登录态 / 退出。  
6. 登录页文案预留 Phase 2「邮箱登录 / 邀请注册」。

### 2.2 非目标（Phase 1）

| 项 | 说明 |
|----|------|
| 用户名/密码存储与校验 | Phase 2 / OIDC |
| 邀请链接注册 API | Phase 2 |
| HttpOnly Cookie Session | 本期仍 Bearer + localStorage |
| 修改 portal ACL 中间件契约 | 业务 API 仍只认 Bearer |
| 生产环境自动 bootstrap token | 仅 Vite `DEV` 且未退出时 |

---

## 3. 架构（Phase 1）

```
Browser
  ├─ /login          (公开，无应用壳)
  ├─ RequireAuth     (无 token → /login?next=)
  └─ App shell       (现有路由)
        └─ api/client → Authorization: Bearer + X-Org-Id
              └─ portal Auth middleware → caller_user_id
```

凭证解析顺序（沿用并收紧 `auth.ts`）：

1. 若本会话标记「门禁会话活跃抑制」（见下表）→ **返回空**（忽略 env 与 bootstrap）  
2. localStorage 显式 token（登录/设置写入）  
3. `VITE_API_TOKEN`（仅当未抑制）  
4. **仅当** Vite DEV **且** 未抑制 → `dev-bootstrap-token`  
5. 生产构建永不发明 token  

Org：localStorage → `VITE_ORG_ID`（可选；抑制会话时 org 仍可读，但不单独构成「已登录」）。

**门禁会话标记** `sessionStorage['sixath-auth-gate']=1`（名称实现可微调）：表示用户已主动退出或因 401 被踢回登录，在本浏览器标签会话内禁止一切自动凭证回退。

| 场景 | localStorage token/org | `sixath-auth-gate` | 随后 `getApiToken()` |
|------|------------------------|--------------------|----------------------|
| 登录页提交成功 | 写入用户输入 | **删除** 标记 | 用刚写入的 token |
| 设置页保存 token | 写入 | **删除** 标记 | 用刚写入的 token |
| 用户点退出 | **清除** token（及 org） | **设置** 标记 | 空（即使有 `VITE_API_TOKEN` / DEV bootstrap） |
| API 401（含 JSON `request` 与 SSE/`sendMessageStream`） | **清除** token | **设置** 标记 | 空；跳转 `/login?next=...` |
| 新标签 / 刷新（无标记） | 不变 | 无 | 按 2→3→4 正常解析（DEV 可 bootstrap） |

说明：抑制只挡**自动**回退；用户在登录页再次粘贴/一键 bootstrap 并提交后必须清标记，否则无法重新进入。

---

## 4. Web 页面与路由

| 路由 | 壳 | 行为 |
|------|----|------|
| `/login` | 无侧栏全屏 | 表单提交 → `setStoredToken` / `setStoredOrgId` → `navigate(next \|\| '/')`；清除「已退出」标记 |
| 其他业务路由 | 现有壳 | 包在 `RequireAuth` 内 |
| `/settings` | 壳内 | 继续可改 token/org；可提供退出按钮 |

**登录页字段**

- Bearer Token（必填）  
- Org ID（可选）  
- DEV：按钮「使用本地 bootstrap」  
- Phase 2 占位：禁用或说明性「邮箱登录 / 邀请注册（即将推出）」

**退出**

- 按 §3 决策表：清 localStorage 凭证 + 设置 `sixath-auth-gate` + 跳转 `/login`  

**401**

- `client.ts` 的 JSON `request` **与** SSE/`sendMessageStream` **一律**：清 localStorage token、设置 `sixath-auth-gate`、跳转 `/login?next=...`（若当前已在 `/login` 则不循环跳转）  
- 错误文案友好（如「凭证无效，请重新登录」），不 dump JSON；引导「前往登录页」而非「打开设置」  

---

## 5. 错误与边界

| 场景 | 行为 |
|------|------|
| 空 token 提交 | 前端拦截 |
| API 401 | 见 §3 表与 §4；必须回登录且无自动回填 |
| 非法 Org | 沿用 portal `INVALID_ORG`；Phase 1 登录页不强制预检 |
| 用户退出 | 见 §3 表；DEV/env 均不得静默回填 |

---

## 6. Phase 2 预留（本期仅规格，不实现）

后续另开 plan/spec，预期方向：

1. 接入 OIDC IdP；登录页主路径改为「邮箱登录」。  
2. 邀请链接：`/register?invite=...` → IdP 注册或 portal 换票后加入目标 org。  
3. Portal：验 JWT **或** 换发内部 Bearer，写入/映射 `user_tokens` / `users`（email）。  
4. 业务 API 契约尽量不变：`Authorization: Bearer <internal-or-access-token>`。  
5. Phase 1 的 Token 表单可降级为「高级 / 开发者登录」。

---

## 7. 测试计划（Phase 1）

- 单元：`getApiToken` / `authHeaders`；退出或 401 后（含存在 `VITE_API_TOKEN` 时）返回空；登录/设置保存后清除 gate；生产无 bootstrap。  
- 路由：gate 抑制或无 token → `/login`；有有效 token 可进 `/sessions`。  
- 手工：bootstrap 登录 → 会话历史；退出 → 再访问需登录（即使 `.env` 有 `VITE_API_TOKEN`）；设置页改 token 仍可用；伪造坏 token 触发 401 → 回登录。  


---

## 8. 决策记录

| 决策 | 结论 |
|------|------|
| 认证终态 | OIDC + 邀请链接 |
| 本期 | Token 全屏登录壳 + 守卫 |
| 凭证存储 | localStorage Bearer（方案 A） |
| 布局 | 独立全屏（非壳内） |
| 设置页 | 保留 |
| 密码自建 | 不做，交给 Phase 2 OIDC |
