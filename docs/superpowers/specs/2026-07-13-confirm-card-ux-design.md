# 危险确认卡 UX 优化设计

**日期**: 2026-07-13  
**状态**: 已落地（feat/confirm-card-ux）  
**方案**: B — 后端结构化错误 + 前端统一消费  
**触发**: 用户在 300s 内确认 `skill_manage` patch，仍收到 `invalid token: not found (superseded, already used, or server restarted)`；旧确认卡可点、倒计时为静态文案、卡片被误标 Confirmed。

**关联**:
- `framework/tool/skillops/skill_manage_pending.go`（同 session+action+name 顶替旧 token）
- `portal/internal/service/chat.go` → `streamSkillManageConfirm`
- `web/src/pages/ChatPage.tsx` 确认卡状态机

---

## 1. 目标与非目标

### 1.1 目标

1. **所有危险确认卡**（`skill_manage` / `execute_write` / `terminal` / `workspace_file` / `browser`）在 token 无效时返回稳定 `error_code` + 中文 `error`。
2. 前端：新卡到达时作废同资源旧卡；倒计时实时；确认结果以 SSE `confirm_result` 为准，失败不假装成功。
3. 用户点旧卡时，立刻看懂「已被新提案替换」，而不是英文 token 堆栈式文案。

### 1.2 非目标（本期不做）

| 项 | 说明 |
|----|------|
| SSE `confirm_superseded` 推送（方案 C） | 后续增强 |
| pending 持久化 / 多副本共享 | 仍进程内 InMemory |
| 改默认 TTL（300s） | 保持 |
| 为非 `skill_manage` 补齐 Portal 专用 `Apply*Confirm` 短路 | 现状仅 skill_manage 有服务端直确认；本期 framework 全 kind 结构化错误 + 前端卡 UX 对齐；**`confirm_result` 仅 skill_manage 短路路径保证发出** |

---

## 2. 问题根因

1. **顶替**：`InMemorySkillManagePendingStore.SavePending` 会删除同 session + action + name 的旧 token；UI 仍保留旧卡。
2. **错误半结构化**：`expired` 已有独立英文 `"token expired"`；**无法区分**的是 superseded / already_used / restart（都挤在 `invalid token: not found (...)`）。
3. **前端状态机竞态**：`handleSend` 调用 `sendMessageStream` 后**立即返回** `AbortController`，并不等待 `onDone`。`handleConfirmAction` 在 `await handleSend(...)` 后几乎立刻无条件标 `confirmed`，与流式结果无关。静态 `Expires in 300s` 亦非倒计时。

---

## 3. 后端设计

### 3.1 统一错误形状

确认路径（`confirm_*` / `confirm_token`）在失败时返回：

```json
{
  "error": "<中文说明>",
  "error_code": "superseded | expired | already_used | not_found"
}
```

约定：

| error_code | 何时 | 中文 error（可微调） |
|------------|------|----------------------|
| `superseded` | tombstone 标明曾被更新提案顶替 | 确认已失效：已被更新的提案替换，请确认最新卡片 |
| `expired` | `CreatedAt + TTL` 超时 | 确认已过期，请让助手重新发起操作 |
| `already_used` | 成功 confirm 后写入 tombstone，二次点击命中 | 该确认已使用过 |
| `not_found` | 其它缺失（重启、未知 token） | 确认已失效（可能已被替换、已使用或服务重启），请重新发起 |

`execute_write` 当前 `confirmWrite` 返回 Go `error`；本期改为与其它工具一致的 **result map**（`error` + `error_code`），单测锁定。

### 3.2 skill_manage 顶替可观测（tombstone 本期必做）

在 `InMemorySkillManagePendingStore` 增加短生命周期 **tombstone**（session+token → reason）：

- `SavePending` 删除旧 token 时写入 `superseded`
- 成功 `confirm` 后 `DeletePending` **必须**写入 `already_used`（二次点击有明确文案）
- `GetPending` miss 时查 tombstone → 对应 `error_code`
- tombstone TTL ≥ confirm TTL（默认 300s），或带上限清理，避免泄漏

其它 PendingStore（terminal / workspace_file / browser）本期**不强制**顶替；至少对 `expired` / `not_found` 返回结构化字段。后续加顶替时复用 tombstone 模式。

### 3.3 Portal：`confirm_result` SSE（定案，不做 A/B 摇摆）

`streamSkillManageConfirm`：

1. 解析工具结果 `error` / `error_code`
2. 发 chunk（人类可读，失败前缀 `技能确认失败: …`）
3. **必须**再发 SSE 事件 `confirm_result`：

```json
{
  "ok": false,
  "kind": "skill_manage",
  "token": "<token>",
  "error": "<中文>",
  "error_code": "superseded"
}
```

成功：`{ "ok": true, "kind": "skill_manage", "token": "..." }`（可附带摘要字段）。

非 `skill_manage` 确认走 LLM/其它路径时，本期**不保证** `confirm_result`；前端对这些 kind 仅做本地作废/倒计时，不假装有对称结果事件。

### 3.4 `resource_key`（skill_manage 强制）

Portal 构造 `confirm_required` 时，对 `skill_manage` **必须**下发：

```text
resource_key = "<action>:<name>"   // 与服务端顶替键一致
```

可选同时下发 `expires_at`（RFC3339，基于 pending `CreatedAt + TTL`），前端倒计时优先用它，缺省再退回 `receivedAt + expires_in`。

其它 kind：尽量填稳定键（path / command / dsl 截断）；缺省时前端用「同 kind 仅最新 pending」启发式（见 §4.2）。

---

## 4. 前端设计

### 4.1 确认卡状态

```text
pending | confirming | confirmed | cancelled | superseded | expired | failed
```

| status | 含义 | Confirm 按钮 |
|--------|------|--------------|
| `pending` | 可确认 | 启用 |
| `confirming` | 等待 `confirm_result` / 流结束 | 禁用 |
| `confirmed` | 成功 | 禁用 |
| `cancelled` | 用户取消 | 禁用 |
| `superseded` | 被新提案或服务端顶替 | 禁用 |
| `expired` | 本地/服务端过期 | 禁用 |
| `failed` | `already_used` / `not_found` / 未知失败 | 禁用（展示 `error`） |

`error_code` → status 映射：

- `superseded` → `superseded`
- `expired` → `expired`
- `already_used` | `not_found` | 其它 → `failed`

### 4.2 新卡到达时作废旧卡

在 `onConfirmRequired`：

1. 取 `resource_key`（服务端优先）
2. **`skill_manage`**：同 `kind` + 同 `resource_key` 的旧 `pending` → `superseded`（与服务端 `action:name` 对齐，**禁止**仅用「同 kind 只留一张」作为 skill_manage 默认）
3. **其它 kind**：有 `resource_key` 则同上；否则同 `kind` 仅保留最新 pending，其余标 `superseded`
4. append 新卡；记录 `receivedAt`；若有 `expires_at` 一并保存

### 4.3 倒计时

- 优先 `expires_at`；否则 `receivedAt + expires_in * 1000`
- 归零 → `expired`，禁用按钮
- 不再展示静态 `Expires in 300s`

### 4.4 确认提交（必须等结果）

`handleConfirmAction` **不得**在流启动后立刻标 `confirmed`。

约定：

1. 提交前若已是 `superseded` / `expired` / `failed` → 直接 return
2. 标 `confirming`
3. 发起带 `confirm_response` 的 stream，等待终态（抽 `submitConfirmation(item)` 或让 confirm 场景返回 `Promise<ConfirmOutcome>`）
4. **按 kind 分流终态**（共享按钮路径时必须写死，避免卡在 `confirming` 或假 `confirmed`）：

| kind | 等待什么 | `onDone` 仍无结果时 |
|------|----------|---------------------|
| `skill_manage` | SSE `confirm_result`（`onConfirmResult`） | → `failed`（本期短路路径必发该事件；缺事件视为协议失败） |
| 其它危险 kind | 本期无 `confirm_result` 保证 | → **保持 `confirming` 至 `onDone`，然后标 `confirmed` 仅表示「已提交给助手」**；卡片 `error` 留空。不解析 chunk 猜成败（避免假阴性）。传输 `onError` → `failed` |

说明：其它 kind 的「已提交」与 skill_manage 的「已落盘成功」语义不同；UI 文案用 Confirmed / Submitted 区分更佳（实现计划可选），但 status 枚举可仍用 `confirmed` 表示终态已关闭。

5. skill_manage：`ok: true` → `confirmed`；`ok: false` → 按 §4.1 映射 + 中文 `error`
6. 传输层 `onError`（所有 kind）→ `failed` 或回 `pending`（可重试）；不得标成功

### 4.5 视觉

- `superseded` / `expired` / `failed`：禁用 Confirm；Cancel 可保留为「关闭/dismiss」→ `cancelled`
- 沿用现有 `chat-confirm-card-*`，meta/error 区展示说明

---

## 5. 兼容与测试

### 5.1 兼容

- 旧客户端忽略未知 SSE 事件 / 未知字段，仍可读 chunk 文案
- 工具 result 保留 `error` 字符串；新增 `error_code`

### 5.2 测试要点

| 层 | 用例 |
|----|------|
| framework | 二次 patch 旧 token → `superseded`；超时 → `expired`；确认后再确认 → `already_used` |
| framework | terminal / file / browser / execute_write：`expired` / `not_found` 带 `error_code` |
| portal | 失败/成功均发 `confirm_result`；`skill_manage` 的 `resource_key` = `action:name` |
| web | 同 `resource_key` 新卡作废旧卡；等 `confirm_result` 才改状态；失败不标 confirmed；倒计时归零 |

---

## 6. 实现顺序（供 plan 展开）

1. framework：tombstone（含 already_used）+ 各 confirm 路径 `error_code` / 中文 `error` + 单测  
2. portal：`confirm_result` SSE + `resource_key` / 可选 `expires_at` + skill_manage 确认流  
3. web：状态机、`submitConfirmation` 等待结果、倒计时、按 resource_key 作废旧卡  
4. 回归：hermes / skill_manage confirm e2e 绿

---

## 7. 成功标准

- 同 skill 连续两次 patch 后，旧卡不可点且标明已替换；再请求旧 token → `error_code=superseded` + 中文说明  
- 未超时、未顶替的卡确认仍成功  
- 不再出现「卡片已 Confirmed + 聊天里技能确认失败」  
- 倒计时可见且到期自动 `expired`  
