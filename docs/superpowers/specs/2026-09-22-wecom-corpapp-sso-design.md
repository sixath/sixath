# 企业微信 CorpApp SSO 登录与自动注册

**日期**: 2026-09-22  
**状态**: 设计待用户确认（方案 A 已口头确认）  
**方案**: Portal 自建企微 CorpApp Web 登录（`login_type=CorpApp`）；首次登录自动建用户  
**关联**:
- [`2026-07-25-email-invite-auth-design.md`](./2026-07-25-email-invite-auth-design.md)（邮箱+邀请；本期保留为高级入口）
- [`2026-07-24-web-login-design.md`](./2026-07-24-web-login-design.md)（Token 门禁）
- [`2026-07-23-user-resource-acl-design.md`](./2026-07-23-user-resource-acl-design.md)（Bearer → `caller_user_id`）
- 企微文档：[Web 登录组件](https://developer.work.weixin.qq.com/document/path/98174)、[获取访问用户身份](https://developer.work.weixin.qq.com/document/path/91023)、[获取 access_token](https://developer.work.weixin.qq.com/document/path/91039)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 登录形态 | **CorpApp SSO**（`https://login.work.weixin.qq.com/wwlogin/sso/login?login_type=CorpApp&…`） |
| 首次登录 | **自动注册**：以企微 `userid` 建 Portal 用户并发 Bearer |
| 组织归属 | **不加入任何组织**；登录后自行创建/加入 |
| 验邮横幅 | **企微用户视为已验证**（`email_verified=true`） |
| 登录页 | **企微为主 CTA**；邮箱密码 / Token 收入「高级登录」折叠 |
| 凭证配置 | **`auth` 段独立配置**（不复用 wecom_bot channel secret） |
| 实现位置 | **Portal Auth**；不经 Gateway |
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
4. 未配置 `wecom_corp_id` / `agent_id` / `secret` 时：隐藏企微按钮，高级登录仍可用。  
5. 前端主路径为企微；邮箱/Token 降级为高级。

### 2.2 非目标

| 项 | 说明 |
|----|------|
| 与邮箱账号绑定/合并 | 另开迭代 |
| 邀请码门槛 | 企微路径不要求 invite |
| Gateway / Bot 改绑 | 不动现有 wecom_bot |
| 强制业务 API 验邮墙 | 维持现网「仅横幅提示」；企微用户无横幅 |
| 通讯录全量同步 | 可选读成员姓名；失败则用 userid 作 display name |

---

## 3. 架构与时序

```text
Browser                Portal                         企微
  │                      │                              │
  │  GET /auth/wecom/start                              │
  │─────────────────────▶│  302 → wwlogin SSO           │
  │◀─────────────────────│─────────────────────────────▶│
  │                      │         用户确认/扫码         │
  │  GET /auth/wecom/callback?code&state                │
  │─────────────────────▶│                              │
  │                      │  gettoken(corpid,secret)     │
  │                      │─────────────────────────────▶│
  │                      │  auth/getuserinfo(code)      │
  │                      │─────────────────────────────▶│
  │                      │  upsert identity + issue token│
  │  302 → /login/wecom/callback?ticket=…             │
  │◀─────────────────────│                              │
  │  POST /auth/wecom/exchange {ticket}                 │
  │─────────────────────▶│  → AuthSession JSON          │
  │  applyLoginSession   │                              │
```

**为何用一次性 `ticket` 再换票**：避免 Bearer 出现在 URL/Referer；ticket 短 TTL（如 60s）、单次消费，存在 Portal 内存或 `user_tokens` 旁表均可（一期内存 map + 进程重启失效可接受；多实例则用 DB）。

**多实例**：若 Portal 水平扩展，state/ticket **必须**落库（见 §3.1）；单实例可先内存。

### 3.1 `oauth_states` / `login_tickets`（建议合一表 `auth_ephemeral`）

| 字段 | 说明 |
|------|------|
| `id` | 随机 token（state 或 ticket） |
| `kind` | `wecom_state` \| `wecom_ticket` |
| `payload_json` | state：`{nonce, created_at}`；ticket：`{user_id}` |
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

### 4.2 `users` 行为

- 自动注册：`id=UUID`，`name`=通讯录姓名或 `userid`，`email=null`，`password_hash=null`，`email_verified_at=now()`。  
- **不**写 `org_members`。  
- 签发 `user_tokens` 与邮箱登录相同。

### 4.3 不变

`orgs` / `org_invites` / Resource ACL / 中间件 Bearer 契约。

---

## 5. 配置

`portal/configs/config.yaml` → `auth`：

```yaml
# 企微 CorpApp Web 登录（均配置后启用）
# wecom_corp_id: "wwxxxxxxxx"      # 企业 CorpID（SSO 的 appid）
# wecom_agent_id: "1000096"        # 自建应用 AgentID
# wecom_secret: "****"             # 该应用 Secret（gettoken）
# wecom_redirect_uri: "https://portal.example.com/api/v1/auth/wecom/callback"
# 若省略 wecom_redirect_uri：用 public_base_url + "/api/v1/auth/wecom/callback"
# public_base_url: "https://portal.example.com"   # 已有；亦用于前端回跳绝对地址
```

`conf.proto` / `Auth` 增加对应字段；`ProvideAuthUsecase`：三者非空则 `wecomLoginEnabled=true`。

**企微管理后台**：应用「可信域名 / Web 授权回调域名」须包含 Portal 主机名；`redirect_uri` 须完全匹配。

---

## 6. API（均在 `/api/v1/auth/*`，全局 Auth 中间件放行；handler 内自校验）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/auth/wecom/start` | 若未启用 → 404/400；生成 `state` 入库；302 到 `wwlogin/sso/login?login_type=CorpApp&appid=&agentid=&redirect_uri=&state=` |
| GET | `/auth/wecom/callback` | 校验 `state`；`code` 换 `userid`；upsert 用户；写 ticket；302 → `{web_origin}/login/wecom/callback?ticket=`（`web_origin` 取 `public_base_url` 或配置 `wecom_web_origin`） |
| POST | `/auth/wecom/exchange` | `{ ticket }` → 与 login 相同的 `AuthSession` JSON；消费 ticket |
| GET | `/auth/wecom/status` | `{ enabled: bool }` 供前端决定是否展示主按钮（无 secret 泄露） |

换身份：

1. `GET https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=&corpsecret=`（缓存 `expires_in`）  
2. `GET https://qyapi.weixin.qq.com/cgi-bin/auth/getuserinfo?access_token=&code=`  
3. 企业成员必须返回 `userid`；仅 `openid`（非成员）→ `403`/`INVALID_WECOM_USER`，不建号  

可选：有通讯录权限时 `user/get` 补全姓名；失败不阻断登录。

---

## 7. 前端

### 7.1 登录页

- 主区：大按钮「企业微信登录」→ `window.location = /api/v1/auth/wecom/start`（或先 `status` 再跳转）。  
- 折叠「高级登录」：现有邮箱密码 + Token 表单。  
- `enabled=false`：不展示企微按钮，高级区默认展开。

### 7.2 `/login/wecom/callback`

- 读 `ticket` → `POST /auth/wecom/exchange` → `applyLoginSession` → `navigate(next)`。  
- 失败展示错误 + 回登录。

### 7.3 注册页

- 保留邀请注册（次要）；页眉可链回企微登录。

### 7.4 会话标记

- `email_verified=true` → 无「邮箱尚未验证」横幅。

---

## 8. 安全

| 项 | 要求 |
|----|------|
| `state` | 一次性、短 TTL、服务端校验 |
| `ticket` | 一次性、≤60s、仅 HTTPS 回调链使用 |
| `secret` / `access_token` | 仅服务端；禁止下发前端 |
| `redirect_uri` | 与配置及企微后台白名单一致 |
| CSRF | start→callback 靠 state；exchange 靠 ticket 熵 |

---

## 9. 错误与可观测

| 场景 | 行为 |
|------|------|
| 未配置企微 | `start` → 400 `WECOM_LOGIN_DISABLED`；UI 隐藏按钮 |
| state 无效/过期 | callback → 重定向前端错误页 |
| code 无效/非成员 | 前端可见错误文案 |
| gettoken 频率 | 进程内缓存 token |

日志：打 `userid` 哈希或后缀，避免明文 secret；`errcode` 原样记。

---

## 10. 测试要点

1. 单元：state/ticket 消费；upsert identity；非成员拒绝。  
2. 伪 HTTP：mock qyapi gettoken/getuserinfo。  
3. 前端：enabled 开关；高级折叠；callback 换票成功写入 session。  
4. 手动：企微后台配好可信域名后扫码/桌面确认走通。

---

## 11. 实现分期

| 里程碑 | 内容 |
|--------|------|
| M1 | conf + identity 表 + AuthUsecase WeCom 流 + API |
| M2 | 登录页主 CTA + callback 换票 + status |
| M3 | 文档/示例 config；可选通讯录姓名 |

---

## 12. 开放问题（实现前可默认）

| 问题 | 默认 |
|------|------|
| 多 Portal 实例 | ephemeral 表落 MySQL（与现网 DB 一致） |
| `public_base_url` 未配 | `wecom_redirect_uri` 必填；前端回跳用相对路径 `/login/wecom/callback`（同域反代） |
| 显示名 | 优先 `user/get` 的 `name`，否则 `userid` |
