# 企业微信 CorpApp SSO 登录与自动注册

**日期**: 2026-09-22  
**状态**: 实现完成（待配置启用）；实现计划见 `docs/superpowers/plans/2026-09-22-wecom-corpapp-sso.md`  
**方案**: Portal 自建企微 CorpApp Web 登录（`login_type=CorpApp`）；首次登录自动建用户  
**关联**:
- [`2026-07-25-email-invite-auth-design.md`](./2026-07-25-email-invite-auth-design.md)（邮箱+邀请；**登录页信息架构由本规格覆盖**，见 §2.3）
- [`2026-07-24-web-login-design.md`](./2026-07-24-web-login-design.md)（Token 门禁）
- [`2026-07-23-user-resource-acl-design.md`](./2026-07-23-user-resource-acl-design.md)（Bearer → `caller_user_id`）
- 企微文档：[Web 登录组件](https://developer.work.weixin.qq.com/document/path/98174)、[获取访问用户身份](https://developer.work.weixin.qq.com/document/path/91023)、[获取 access_token](https://developer.work.weixin.qq.com/document/path/91039)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 登录形态 | **CorpApp SSO**（`https://login.work.weixin.qq.com/wwlogin/sso/login?login_type=CorpApp&…`） |
| 首次登录 | **自动注册**：以企微 `userid` 建 Portal 用户并发 Bearer |
| 组织归属 | **不加入任何组织**；登录后自行创建/加入（沿用邮箱规格：`orgs.length===0` → 清空当前 org、可去新建） |
| 验邮横幅 | **企微用户视为已验证**（`email_verified=true`） |
| 登录页 | **企微为主 CTA**；邮箱密码 / Token 收入「高级登录」折叠（**覆盖** Phase 2「邮箱为主」UX） |
| 凭证配置 | **`auth` 段独立配置**（不复用 wecom_bot channel secret） |
| 实现位置 | **Portal Auth**；不经 Gateway |
| ephemeral | **一律落 MySQL `auth_ephemeral`**（含单实例；简化实现） |
| 非目标（一期） | 绑定已有邮箱账号；强制邀请；嵌入式 `ww.createWWLoginPanel`（可二期）；第三方 ServiceApp |

---

## 1. 问题

- Portal Web 仅有邮箱密码、邀请注册与开发者 Token；无企业身份登录。
- 已有企微能力是 **Bot 入站 / Webhook 出站**，不能当作 Web SSO。
- 现网参考形态：`login.work.weixin.qq.com/wwlogin/sso/login/?login_type=CorpApp&appid=…&agentid=…&redirect_uri=…`（桌面端确认 / 浏览器继续 / 可切扫码）。
- SMTP 未配置时邮箱验邮不可用；企微登录应避免再逼用户「去验证」。

---

## 2. 目标与非目标

### 2.1 目标

1. 配置齐全时，登录页一键跳转企微 CorpApp SSO，回调后签发与现网一致的内部 Bearer。  
2. 首次成功：按 `userid` 自动建用户；**不**入 org；`email_verified=true`。  
3. 再次登录：同一 `userid` 命中既有映射，直接发 Bearer。  
4. 未配置 `wecom_corp_id` / `wecom_agent_id` / `wecom_secret` 时：隐藏企微按钮，高级登录默认展开。  
5. 前端主路径为企微；邮箱/Token 降级为高级。

### 2.2 非目标

| 项 | 说明 |
|----|------|
| 与邮箱账号绑定/合并 | 另开迭代 |
| 邀请码门槛 | 企微路径不要求 invite |
| Gateway / Bot 改绑 | 不动现有 wecom_bot |
| 强制业务 API 验邮墙 | 维持现网「仅横幅提示」；企微用户无横幅 |
| 通讯录全量同步 | 可选读成员姓名；失败则用 userid 作 display name |

### 2.3 对既有规格的覆盖

| 既有规格 | 本规格覆盖点 |
|----------|----------------|
| 邮箱 auth §5.1「登录主路径=邮箱密码」 | **改为企微主 CTA**；邮箱+Token 同属「高级登录」 |
| 邮箱 auth §3.1「登录用户必填 email」 | **IdP/企微用户例外**：允许 `email=NULL`；MySQL `UNIQUE(email)` 允许多 NULL |
| 邮箱 auth §5.2零 org | **沿用**：`orgs=[]` → 清空 org 上下文，引导去组织页创建/加入；**禁止**静默挂 `bootstrap_org_id` |

---

## 3. 架构与时序

```text
Browser                Portal                         企微
  │                      │                              │
  │  GET /api/v1/auth/wecom/start?next=/…               │
  │─────────────────────▶│  302 → wwlogin SSO           │
  │◀─────────────────────│─────────────────────────────▶│
  │                      │         用户确认/扫码         │
  │  GET /api/v1/auth/wecom/callback?code&state         │
  │─────────────────────▶│                              │
  │                      │  gettoken(corpid,secret)     │
  │                      │─────────────────────────────▶│
  │                      │  auth/getuserinfo(code)      │
  │                      │─────────────────────────────▶│
  │                      │  upsert identity + ticket    │
  │  302 → /login/wecom/callback?ticket=…&next=…      │
  │◀─────────────────────│                              │
  │  POST /api/v1/auth/wecom/exchange {ticket}          │
  │─────────────────────▶│  → AuthSession JSON          │
  │  applyLoginSession   │                              │
```

**为何用一次性 `ticket` 再换票**：避免 Bearer 出现在 URL/Referer。

### 3.1 表 `auth_ephemeral`（M1 必做）

| 字段 | 说明 |
|------|------|
| `id` | 随机 token（state 或 ticket） |
| `kind` | `wecom_state` \| `wecom_ticket` |
| `payload_json` | state：`{next}`；ticket：`{user_id}` |
| `expires_at` | state ~10min；ticket ~60s |
| `consumed_at` | nullable；非空表示已用 |

---

## 4. 数据模型

### 4.1 `user_identities`（新表）

| 字段 | 说明 |
|------|------|
| `provider` | 固定 `wecom`（PK 部分） |
| `subject` | 企微 `userid`（PK 部分；互联企业格式按企微原样存储） |
| `user_id` | → `users.id` |
| `created_at` / `updated_at` | |

唯一约束：`(provider, subject)`；同一 `user_id` 可有多 provider（二期邮箱绑定预留）。

**并发**：双 callback 同时插入时，捕获唯一冲突 → **改为 SELECT 既有映射**再发 ticket，不得 500。若本请求先插入了 `users` 行、再因 identity 唯一冲突落败，应**删除或回滚该 orphan user**，避免双开用户。

### 4.2 `users` 行为

- 自动注册：`id=UUID`，`name`=通讯录姓名或 `userid`，`email=NULL`，`password_hash=NULL`，`email_verified_at=now()`。  
- **例外说明**：相对邮箱规格「登录用户必填 email」，企微/IdP 用户允许无邮箱。  
- **不**写 `org_members`。  
- 签发 `user_tokens` 与邮箱登录相同。

### 4.3 不变

`orgs` / `org_invites` / Resource ACL / 中间件 Bearer 契约。

---

## 5. 配置

`portal/configs/config.yaml` → `auth`：

```yaml
# 企微 CorpApp Web 登录
# wecom_corp_id: "wwxxxxxxxx"      # 企业 CorpID（SSO 的 appid）
# wecom_agent_id: "1000096"        # 自建应用 AgentID
# wecom_secret: "****"             # 该应用 Secret（gettoken）
# wecom_redirect_uri: "https://portal.example.com/api/v1/auth/wecom/callback"
# public_base_url: "https://portal.example.com"
```

### 5.1 启用条件与 URL 拼装

| 条件 | 行为 |
|------|------|
| `wecom_corp_id` + `wecom_agent_id` + `wecom_secret` 均非空 | `enabled=true` |
| 否则 | `enabled=false`；`start` → **400** `WECOM_LOGIN_DISABLED` |
| `wecom_redirect_uri` | 优先；否则 `public_base_url` + `/api/v1/auth/wecom/callback`；二者皆空 → 视为未启用 |
| 前端回跳 origin | `public_base_url`；未配则 302 相对路径 `/login/wecom/callback?…`（要求 Web 与 API 同域反代） |

**企微管理后台**：应用「可信域名 / Web 授权回调域名」须包含 Portal 主机名；`redirect_uri` 须完全匹配。

---

## 6. API（全局 Auth 中间件对 `/api/v1/auth/*` 放行）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/auth/wecom/status` | `{ "enabled": bool }` |
| GET | `/api/v1/auth/wecom/start?next=` | 校验 `next`（§6.2）；写 `wecom_state`；**302** → `wwlogin/sso/login?login_type=CorpApp&appid=&agentid=&redirect_uri=&state=`；未启用 → **400** `WECOM_LOGIN_DISABLED` |
| GET | `/api/v1/auth/wecom/callback?code=&state=` | 校验并消费 state；换 `userid`；upsert；写 ticket；**302** → `/login/wecom/callback?ticket=&next=` 或错误回流（§6.3） |
| POST | `/api/v1/auth/wecom/exchange` | body `{ "ticket": "…" }` → **AuthSession**（§6.1）；消费 ticket |

换身份：

1. `GET https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=&corpsecret=`（进程内按 `expires_in` 缓存）  
2. `GET https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo?access_token=&code=`  
3. 必须有 `userid`；仅 `openid` → **403** `INVALID_WECOM_USER`  

可选：`user/get` 补姓名；失败不阻断。

### 6.1 AuthSession 契约（企微）

与邮箱 login **同 shape**：`token` / `user_id` / `email` / `orgs` / `email_verified`。签发路径与邮箱一致：建/查用户后走同一 `issueSession`（含 `ListUserOrgs`），**不得**为企微单独写死会话字段。

**首次自动注册、尚未入任何组织时**的示例：

```json
{
  "token": "<bearer>",
  "user_id": "<uuid>",
  "email": "",
  "orgs": [],
  "email_verified": true
}
```

| 字段 | 企微约定 |
|------|----------|
| `email` | 无邮箱时为 **空字符串**（JSON 不省略；不发 `null`，便于现有 TS 类型） |
| `orgs` | **与邮箱 login 相同**：从 `org_members` / `ListUserOrgs` **实时加载**，不得写死。首次注册后通常为 `[]`；用户后续创建/加入组织后，**再次企微登录须返回实际成员列表**。仅当结果为空时，前端沿用既有「清空 org、可去新建」逻辑 |
| `email_verified` | **`true`**（企微用户恒为已验证） |
| `token` / `user_id` | 非空 |

前端：`applyLoginSession` / `pickOrgIdAfterLogin` 对空 `orgs` 清空 org；有 org 时与邮箱登录同一套选 org 逻辑。不得假定 `email` 含 `@`。

### 6.2 `next` 白名单

- 仅允许以 `/` 开头、不以 `//` 开头的相对路径（与现网 LoginPage `next` 规则一致）。  
- 非法或缺失 → 默认 `/`。  
- `start` 将合法 `next` 写入 state payload；callback 原样带回前端 query。

### 6.3 错误回流

callback 失败时 **302** 到：

`/login/wecom/callback?error={CODE}`

| CODE | 含义 |
|------|------|
| `WECOM_LOGIN_DISABLED` | 未配置（正常不应到 callback） |
| `INVALID_STATE` | state 缺失/过期/已消费 |
| `INVALID_CODE` | code 换身份失败 |
| `INVALID_WECOM_USER` | 非企业成员（无 userid） |
| `INTERNAL` | 其他服务端错误 |

前端根据 `error` 展示文案，并提供返回 `/login`。

---

## 7. 前端

### 7.1 登录页（覆盖 Phase 2 UX）

- 主区：大按钮「企业微信登录」→ 同域 `GET /api/v1/auth/wecom/start?next=…`（经反代；本地分端口时用 `public_base_url` 绝对地址）。  
- 折叠「高级登录」：邮箱密码 + Token。  
- `status.enabled=false`：隐藏企微按钮，高级区**默认展开**。

### 7.2 `/login/wecom/callback`

- 有 `ticket`：`POST /api/v1/auth/wecom/exchange` → `applyLoginSession` → `navigate(next)`。  
- 有 `error`：展示对应文案。  
- 两者皆无：视为失败。

### 7.3 注册页

- 保留邀请注册（次要）；可链回企微登录。

### 7.4 会话标记

- `email_verified=true` → 无验邮横幅。

---

## 8. 安全

| 项 | 要求 |
|----|------|
| `state` | 一次性、短 TTL、服务端校验 |
| `ticket` | 一次性、≤60s |
| `secret` / `access_token` | 仅服务端 |
| `redirect_uri` | 与配置及企微后台白名单一致 |
| `next` | 仅同站相对路径 |

---

## 9. 错误与可观测

| 场景 | HTTP / 行为 |
|------|-------------|
| 未配置 | `start` → **400** `WECOM_LOGIN_DISABLED` |
| state 无效 | callback → 302 `error=INVALID_STATE` |
| code / 非成员 | 302 `error=INVALID_CODE` / `INVALID_WECOM_USER` |
| gettoken 频率 | 进程内缓存 |

日志：userid 仅记后缀；secret 不入日志；企微 `errcode` 原样记。

---

## 10. 测试要点

1. 单元：state/ticket 消费；upsert + 唯一冲突重试（含 orphan 回滚）；非成员拒绝；AuthSession 字段（`email=""`、`email_verified=true`）。  
2. 伪 HTTP：mock gettoken/getuserinfo。  
3. 集成：已入 org 用户再次 WeCom login → `orgs` 非空，且 `applyLoginSession` 保留/自动选 org（不得清空）。  
4. 前端：enabled 开关；`next` 保留；error query。  
5. 手动：可信域名配置后 CorpApp 全流程。

---

## 11. 实现分期

| 里程碑 | 内容 |
|--------|------|
| M1 | conf + `user_identities` + `auth_ephemeral` + AuthUsecase + API |
| M2 | 登录页主 CTA + `/login/wecom/callback` + status |
| M3 | 示例 config；可选通讯录姓名 |

---

## 12. 默认值汇总

| 项 | 默认 |
|----|------|
| ephemeral 存储 | MySQL `auth_ephemeral` |
| 显示名 | `user/get.name` → 否则 `userid` |
| 无 `public_base_url` | 要求显式 `wecom_redirect_uri`；前端回跳相对路径 |
| 零 org | 清空 org；可建组织；不挂 bootstrap org |
