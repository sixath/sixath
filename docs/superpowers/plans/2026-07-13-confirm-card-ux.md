# 危险确认卡 UX 优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use checkbox (`- [x]`) syntax. **Skip git commits unless the user asks.**

**Goal:** 危险确认失败可机读（`error_code` + 中文）、skill_manage 顶替可观测、前端旧卡作废/倒计时/等 `confirm_result` 再改状态，消除「假 Confirmed」。

**Architecture:** framework PendingStore 增加 tombstone + 统一 confirm 错误 map；Portal SSE 增加 `confirm_result` 并为 skill_manage 下发 `resource_key=action:name`；Web 按 resource_key 作废旧卡，`submitConfirmation` 等待 `confirm_result`。

**Tech Stack:** Go（framework/portal）、React/TS（web）、现有 SSE 协议

**Spec:** [docs/superpowers/specs/2026-07-13-confirm-card-ux-design.md](../specs/2026-07-13-confirm-card-ux-design.md)

---

## 文件地图

| 文件 | 职责 |
|------|------|
| Modify `framework/tool/skillops/skill_manage_pending.go` | tombstone；`LookupTombstone` / Delete 写 already_used |
| Modify `framework/tool/skillops/skill_manager_tool.go` | confirm 失败返回中文 + `error_code` |
| Modify `framework/tool/terminal_tool.go` / `file_tools.go` / `browser_tools.go` | 同上（expired/not_found） |
| Modify `framework/tool/data/execute_write.go` | confirm 改 result map + error_code |
| Modify `portal/internal/service/chat_stream.go` | `ChatStreamEventConfirmResult`；confirmation 填 `resource_key` / `expires_at` |
| Modify `portal/internal/service/chat.go` | `streamSkillManageConfirm` 发 confirm_result |
| Modify `portal/internal/server/chat_sse.go` | 序列化 confirm_result 事件 |
| Modify `web/src/api/chatStream.ts` + `client.ts` | 解析 resource_key / confirm_result |
| Modify `web/src/pages/ChatPage.tsx` | 状态机、倒计时、作废、submitConfirmation |
| Tests | 各层对应 `*_test.go` / `web/tests/chatStream.test.ts` |

---

### Task 1: skill_manage tombstone + error_code

**Files:**
- Modify: `framework/tool/skillops/skill_manage_pending.go`
- Modify: `framework/tool/skillops/skill_manager_tool.go`
- Modify: `framework/tool/skillops/skill_manage_pending_test.go`
- Modify: `framework/tool/skillops/skill_manager_tool_test.go`

- [x] **Step 1: 扩展 pending 测试（先红）**

在 `skill_manage_pending_test.go` 增加：

```go
func TestInMemorySkillManagePendingStore_TombstoneSuperseded(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "old", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "new", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	reason, ok := store.TombstoneReason(ctx, "sess", "old")
	if !ok || reason != "superseded" {
		t.Fatalf("tombstone: ok=%v reason=%q", ok, reason)
	}
}

func TestInMemorySkillManagePendingStore_TombstoneAlreadyUsed(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "t1", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.ConsumePending(ctx, "sess", "t1") // 仅成功消费写 already_used
	reason, ok := store.TombstoneReason(ctx, "sess", "t1")
	if !ok || reason != "already_used" {
		t.Fatalf("tombstone: ok=%v reason=%q", ok, reason)
	}
}

func TestInMemorySkillManagePendingStore_ExpireDeleteNoAlreadyUsed(t *testing.T) {
	store := NewInMemorySkillManagePendingStore()
	ctx := context.Background()
	_ = store.SavePending(ctx, "sess", PendingSkillManage{
		Token: "t1", Action: "patch", Name: "s1", CreatedAt: time.Now(),
	})
	_ = store.DeletePending(ctx, "sess", "t1") // 过期清理不得写 already_used
	if _, ok := store.TombstoneReason(ctx, "sess", "t1"); ok {
		t.Fatal("plain DeletePending must not tombstone as already_used")
	}
}
```

并在 `skill_manager_tool_test.go` 增加 confirm 旧 token 返回 `error_code=superseded` 的用例（可复用现有 pending confirm 测试骨架）。

- [x] **Step 2: 跑测确认失败**

Run（在 `framework/`）:

```bash
go test ./tool/skillops/ -run Tombstone -count=1
```

Expected: FAIL（`TombstoneReason` / `ConsumePending` 未定义）

- [x] **Step 3: 实现 tombstone + confirm 错误**

`skill_manage_pending.go` 要点：

- store 增加 `tombstones map[string]tombstoneEntry`（key=`sessionID:token`，含 `reason` + `at`）
- `SavePending` 删旧 token 时 `tombstones[old]=superseded`
- **`DeletePending` 只删 pending，不写 tombstone**（过期分支继续用它）
- 新增 **`ConsumePending(ctx, sessionID, token) error`**：删除 pending 并写 `already_used`；仅成功 confirm 路径调用
- 导出 `TombstoneReason(ctx, sessionID, token) (string, bool)`，过期 tombstone 视为 miss
- TTL：默认 300s（与 ConfirmTTL 对齐）

`confirmSkillManage`：成功后改调 `ConsumePending`（勿用裸 `DeletePending`）。`pending == nil` 时：

```go
if reason, ok := cfg.PendingStore.TombstoneReason(ctx, sessionID, token); ok {
  switch reason {
  case "superseded":
    return map[string]any{"error": "确认已失效：已被更新的提案替换，请确认最新卡片", "error_code": "superseded"}, nil
  case "already_used":
    return map[string]any{"error": "该确认已使用过", "error_code": "already_used"}, nil
  }
}
return map[string]any{"error": "确认已失效（可能已被替换、已使用或服务重启），请重新发起", "error_code": "not_found"}, nil
```

`token expired` 改为中文 + `"error_code": "expired"`（过期仍 `DeletePending`，不写 already_used）。

接口 `SkillManagePendingStore` 增加 `TombstoneReason` + `ConsumePending`（InMemory 实现；测试 fake 一并改）。

- [x] **Step 4: 跑测通过**

```bash
go test ./tool/skillops/ -count=1
```

Expected: PASS

---

### Task 2: 其它危险确认路径 error_code

**Files:**
- Modify: `framework/tool/terminal_tool.go`（`confirmTerminal`）
- Modify: `framework/tool/file_tools.go`（`confirmWorkspaceFile`）
- Modify: `framework/tool/browser_tools.go`（`confirmBrowserAction`）
- Modify: `framework/tool/data/execute_write.go`（`confirmWrite` → result map）
- Modify/Create 对应 `*_test.go`

- [x] **Step 1: 各路径最小失败用例（先红）**

对每个工具断言：非法 token → map 含 `error_code=not_found`（或 execute_write 对齐后的等价 map）；过期 → `expired`。

- [x] **Step 2: 实现中文 error + error_code**

文案与 spec §3.1 表一致。`execute_write`：`confirmWrite` 改为返回 `map[string]any`（与其它工具一致），更新调用方与测试（原 `errors.New("execute_write: invalid or expired token")` 等）。

- [x] **Step 3: 跑测**

```bash
go test ./tool/ ./tool/data/ -count=1
```

Expected: PASS（若包路径不同，按 repo 实际调整）

---

### Task 3: Portal confirm_result + resource_key

**Files:**
- Modify: `portal/internal/service/chat_stream.go`
- Modify: `portal/internal/service/chat.go`（`streamSkillManageConfirm`）
- Modify: `portal/internal/server/chat_sse.go`
- Modify: `portal/internal/service/chat_stream_test.go`

- [x] **Step 1: 扩展类型与测试（先红）**

`ChatStreamEventType` 增加 `ChatStreamEventConfirmResult = "confirm_result"`。

```go
type ConfirmResultPayload struct {
	OK        bool   `json:"ok"`
	Kind      string `json:"kind"`
	Token     string `json:"token"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type ChatConfirmationRequest struct {
	// ...existing...
	ResourceKey string `json:"resource_key,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"` // RFC3339
}
```

`skillManageConfirmationFromCall`：`ResourceKey = action + ":" + name`；若有 CreatedAt/TTL 可填 `ExpiresAt`（无 CreatedAt 时仅 `ExpiresIn`）。

测试：`confirmationRequestsFromResponse` 对 skill_manage 断言 `ResourceKey == "patch:archive-move-ops"`（用 fixture name）。

- [x] **Step 2: streamSkillManageConfirm 发事件**

```go
// 伪代码
ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: text}
ch <- ChatStreamEvent{Type: ChatStreamEventConfirmResult, ConfirmResult: &ConfirmResultPayload{
  OK: ok, Kind: "skill_manage", Token: cr.Token,
  Error: errMsg, ErrorCode: code,
}}
```

`ChatStreamEvent` 增加 `ConfirmResult *ConfirmResultPayload`。

- [x] **Step 3: chat_sse 写出事件**

在 SSE loop 对 `ChatStreamEventConfirmResult`：

```go
writeSSEEvent(ctx, "confirm_result", map[string]any{"confirm_result": event.ConfirmResult})
```

（字段形状与现有 `confirm_required` 包一层对象的风格保持一致即可。）

- [x] **Step 4: 跑 portal 测**

```bash
cd portal && go test ./internal/service/ -run Confirm -count=1
```

Expected: PASS

---

### Task 4: Web 解析与确认状态机

**Files:**
- Modify: `web/src/api/chatStream.ts`
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/ChatPage.tsx`
- Modify: `web/tests/chatStream.test.ts`
- 如有样式：`web` 现有 confirm card CSS

- [x] **Step 1: chatStream 类型与测试**

扩展：

```ts
export interface ChatConfirmationRequest {
  // existing...
  resource_key?: string
  expires_at?: string
}

export interface ConfirmResultPayload {
  ok: boolean
  kind: string
  token: string
  error?: string
  error_code?: string
}

export function parseConfirmResultPayload(payload: unknown): ConfirmResultPayload | null
```

`parseConfirmRequiredPayload` 透传 `resource_key` / `expires_at`。

测试覆盖：解析成功；skill_manage 带 resource_key。

- [x] **Step 2: client.sendMessageStream 增加 onConfirmResult**

解析 SSE event `confirm_result` → `callbacks.onConfirmResult?.(parsed)`。

- [x] **Step 3: ChatPage 状态机**

1. `status`: `pending | confirming | confirmed | cancelled | superseded | expired | failed`
2. `onConfirmRequired`：按 spec §4.2 作废同 `resource_key`（skill_manage 必须用服务端键）旧 pending
3. 倒计时：优先 `expires_at`，否则 `receivedAt + expires_in`；归零 → `expired`
4. **抽 `submitConfirmation(item)`，必须自管 stream（不要复用「fire-and-forget」的 `handleSend`）**：
   - 直接调 `chatApi.sendMessageStream(sid, '', callbacks, { confirm_response })`（或让 `handleSend` 在 confirm 场景返回 `Promise`）
   - 在本次调用的 `callbacks` 里挂 `onConfirmResult` / `onDone` / `onError`，用 `Promise` + resolver 等待终态（token 匹配）
   - skill_manage：收到匹配 `confirm_result` → resolve；`onDone` 仍无结果 → `failed`
   - 其它 kind：`onDone` → `confirmed`（已提交）；`onError` → `failed`
   - 用户消息占位 `[confirmed: kind]`、assistant 占位、timeline 等可抽共享 helper，避免与普通发送逻辑分叉过多
5. **删除** `await handleSend(...); updateConfirmation(..., confirmed)` 无条件成功路径

按钮：非 `pending` 禁用 Confirm；展示 `error` / 状态文案（已替换 / 已过期 / 失败原因）。

- [x] **Step 4: 跑前端测**

```bash
cd web && npm test -- chatStream.test.ts
```

Expected: PASS（按项目实际 test runner 调整）

---

### Task 5: 回归与手工验收

- [x] **Step 1: framework + portal 全量相关测**

```bash
cd framework && go test ./tool/... ./tool/skillops/... ./tool/data/... -count=1
cd portal && go test ./internal/service/ ./internal/chat/ -count=1
```

- [x] **Step 2: 手工（若本地 portal/web 已起）**

1. 触发两次同 skill patch 提案 → 旧卡「已替换」不可点  
2. 点最新卡 → 成功；聊天无「确认失败」矛盾  
3. 故意拖过期或点旧 token（若可）→ 中文 + 对应状态  

- [x] **Step 3: 更新 spec 状态为「已落地」**（实现完成后）

将 `docs/superpowers/specs/2026-07-13-confirm-card-ux-design.md` 头部状态改为已落地。

---

## 验收清单

- [x] 二次 patch 旧 token → `error_code=superseded`
- [x] 过期 → `expired`；二次确认 → `already_used`
- [x] SSE `confirm_result` 成功/失败都发
- [x] skill_manage `resource_key=action:name`
- [x] 前端等结果再改状态；无假 Confirmed
- [x] 倒计时到期 → expired
