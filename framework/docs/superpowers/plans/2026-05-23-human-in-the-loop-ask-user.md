# Human-in-the-Loop（ask_user + input_required）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现结构化 Human-in-the-Loop：Agent 通过 `ask_user` 工具请求用户输入，Portal 推送 `input_required` SSE，用户提交后下轮 Run 注入 synthetic tool message 继续执行；password 不落库。

**Architecture:** v0.1 完全对齐 `execute_write` / `confirm_required` 模式——工具返回 `pending`、Run 正常结束、Portal 从 `RunTrace.ToolCalls` 提取事件；**不修改** `ReActAgent` 主循环。密码经 `AskUserFulfillmentStore` + `SecretFromContext` 供后续工具读取，明文不进 LLM 上下文与 DB。

**Tech Stack:** Go 1.22+ / `github.com/sixath/framework`、Kratos portal、React 19 / TypeScript / Vite（web）

**Spec:** [`../specs/2026-05-23-human-in-the-loop-ask-user.md`](../specs/2026-05-23-human-in-the-loop-ask-user.md)

---

## File Structure

| 文件 | 职责 |
|------|------|
| `framework/tool/ask_user_store_memory.go` | InMemory pending + fulfillment store |
| `framework/tool/ask_user_context.go` | `SecretFromContext` / `WithSecretProvider` |
| `framework/tool/ask_user.go` | 工具注册与 Execute 逻辑 |
| `framework/tool/ask_user_test.go` | framework 单测 |
| `framework/tool/toolset.go` | 增加 `ask_user` → ToolsetCore |
| `framework/events/event.go` | 可选审计 Kind（InputRequested 等） |
| `portal/internal/service/chat_stream.go` | `inputRequestsFromResponse` + stream 事件 |
| `portal/internal/service/chat_stream_test.go` | Portal 提取单测 |
| `portal/internal/server/chat_sse.go` | SSE `input_required` 分支 |
| `portal/internal/chat/input_response.go` | synthetic messages + fulfillment 应用 |
| `portal/internal/chat/input_response_test.go` | synthetic message 单测 |
| `portal/internal/chat/ask_user_wiring.go` | session 级 store 单例 + RegisterAskUserTool |
| `portal/internal/service/chat.go` | SendMessage / SendMessageStream 处理 input_response |
| `portal/internal/chat/agent_builder.go` | BuildReActAgent 前注册 ask_user |
| `web/src/api/chatStream.ts` | `ChatInputRequest` + parse helper |
| `web/tests/chatStream.test.ts` | 前端 helper 单测 |
| `web/src/api/client.ts` | `onInputRequired` SSE 回调 |
| `web/src/pages/ChatPage.tsx` | InputCard UI + 提交逻辑 |
| `web/src/pages/ChatPage.css` | InputCard 样式 |

**v0.1 不修改:** `framework/agent/react_agent.go`

---

## Spec Coverage Map

| Spec § | Task |
|--------|------|
| H-O1, §3 ask_user 工具 | Task 1–2 |
| H-O4, §3.6 SecretFromContext | Task 3 |
| H-O2, §5.1–5.2 SSE | Task 4–5 |
| H-O3, §5.3 input_response | Task 6–7 |
| §6 Frontend | Task 8–9 |
| §7 System prompt | Task 7 |
| §9 安全（password 不落库） | Task 6–7, 9 |
| §10 测试清单 | 各 Task 内 |
| P5 checkpoint | **Out of scope** |

---

### Task 1: InMemory Stores

**Files:**
- Create: `framework/tool/ask_user_store_memory.go`
- Create: `framework/tool/ask_user_store_memory_test.go`

- [ ] **Step 1: Write the failing test**

Create `framework/tool/ask_user_store_memory_test.go`:

```go
package tool

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryAskUserPendingStore_SaveGetDelete(t *testing.T) {
	store := NewInMemoryAskUserPendingStore()
	ctx := context.Background()
	p := PendingInputRequest{
		RequestID: "req_1",
		Token:     "tok_1",
		SessionID: "sess_a",
		Field:     "ssh_password",
		Kind:      "password",
		CreatedAt: time.Now(),
	}
	if err := store.SavePending(ctx, "sess_a", p); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPending(ctx, "sess_a", "tok_1")
	if err != nil || got == nil || got.RequestID != "req_1" {
		t.Fatalf("GetPending: got=%#v err=%v", got, err)
	}
	if err := store.DeletePending(ctx, "sess_a", "tok_1"); err != nil {
		t.Fatal(err)
	}
	if got2, _ := store.GetPending(ctx, "sess_a", "tok_1"); got2 != nil {
		t.Fatalf("expected deleted, got %#v", got2)
	}
}

func TestInMemoryAskUserFulfillmentStore_PutGetDelete(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := context.Background()
	if err := store.PutSecret(ctx, "sess_a", "ssh_password", "secret123", time.Minute); err != nil {
		t.Fatal(err)
	}
	v, err := store.GetSecret(ctx, "sess_a", "ssh_password")
	if err != nil || v != "secret123" {
		t.Fatalf("GetSecret: v=%q err=%v", v, err)
	}
	if err := store.DeleteSecret(ctx, "sess_a", "ssh_password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSecret(ctx, "sess_a", "ssh_password"); err == nil {
		t.Fatal("expected error after delete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
cd d:\workspace\github\sixath\framework
go test ./tool -run InMemoryAskUser -v
```

Expected: FAIL — types/functions undefined.

- [ ] **Step 3: Implement stores**

Create `framework/tool/ask_user_store_memory.go` with `PendingInputRequest`, `AskUserPendingStore`, `AskUserFulfillmentStore` interfaces (from spec §3.4), plus `InMemoryAskUserPendingStore` and `InMemoryAskUserFulfillmentStore` using `sync.RWMutex` and key `sessionID + ":" + token` / `sessionID + ":" + field`. `GetSecret` returns error when missing.

- [ ] **Step 4: Run test to verify it passes**

Run:

```powershell
go test ./tool -run InMemoryAskUser -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```powershell
git add framework/tool/ask_user_store_memory.go framework/tool/ask_user_store_memory_test.go
git commit -m "feat(tool): add in-memory ask_user pending and fulfillment stores"
```

---

### Task 2: ask_user Tool

**Files:**
- Create: `framework/tool/ask_user.go`
- Create: `framework/tool/ask_user_test.go`
- Modify: `framework/tool/toolset.go` — add `"ask_user": ToolsetCore`

- [ ] **Step 1: Write failing tests**

Create `framework/tool/ask_user_test.go` (reuse `fakeTokenGen` from `execute_write_test.go`):

```go
func TestAskUser_PendingThenFulfill(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	fulfillStore := NewInMemoryAskUserFulfillmentStore()
	reg := tool.NewRegistry()
	cfg := &AskUserConfig{
		PendingStore:     pendingStore,
		FulfillmentStore: fulfillStore,
		TokenGen:         &fakeTokenGen{next: "tok_abc"},
		TTLSeconds:       600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")
	ctx = WithSecretProvider(ctx, fulfillStore)

	// propose
	res, err := tl.Execute(ctx, map[string]any{
		"prompt": "Enter SSH password",
		"kind":   "password",
		"field":  "ssh_password",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["status"] != "pending" || m["token"] != "tok_abc" {
		t.Fatalf("pending: %#v", res)
	}

	// fulfill via response_token
	res2, err := tl.Execute(ctx, map[string]any{
		"response_token": "tok_abc",
		"value":          "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := res2.(map[string]any)
	if m2["status"] != "fulfilled" || m2["value_redacted"] != true {
		t.Fatalf("fulfilled: %#v", res2)
	}
	if _, ok := m2["value"]; ok {
		t.Fatalf("password must not appear in tool result: %#v", m2)
	}
	secret, err := fulfillStore.GetSecret(ctx, "sess_1", "ssh_password")
	if err != nil || secret != "hunter2" {
		t.Fatalf("secret store: %q err=%v", secret, err)
	}
}

func TestAskUser_Cancelled(t *testing.T) { /* response_token + cancelled:true → status cancelled */ }
func TestAskUser_ExpiredToken(t *testing.T) { /* unknown token → status expired */ }
func TestAskUser_TextFulfilledIncludesValue(t *testing.T) {
	// kind=text → fulfilled result MAY include value for model (spec Q1 default)
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```powershell
go test ./tool -run TestAskUser -v
```

- [ ] **Step 3: Implement `ask_user.go`**

Key behaviors:
- `RegisterAskUserTool(reg, cfg)` — register with Parameters schema from spec §3.2
- Read `session_id` from `ctx.Value(ContextKeySessionID)`; error if empty on propose
- Propose path: generate `request_id` (`req_` + 8 hex), save pending with `ToolCallID` from ctx optional key or empty (portal fills on synthetic replay)
- Fulfill path: validate token, handle cancel, password → `FulfillmentStore.PutSecret`, text/select → include `value` in result
- Return maps matching spec §3.3

- [ ] **Step 4: Add toolset entry**

In `framework/tool/toolset.go`:

```go
"ask_user": ToolsetCore,
```

- [ ] **Step 5: Run tests — expect PASS**

```powershell
go test ./tool -run TestAskUser -v
```

- [ ] **Step 6: Commit**

```powershell
git add framework/tool/ask_user.go framework/tool/ask_user_test.go framework/tool/toolset.go
git commit -m "feat(tool): add ask_user tool with pending and fulfillment flow"
```

---

### Task 3: SecretFromContext Helper

**Files:**
- Create: `framework/tool/ask_user_context.go`
- Create: `framework/tool/ask_user_context_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestSecretFromContext(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := WithSecretProvider(context.Background(), store)
	_ = store.PutSecret(ctx, "sess_1", "ssh_password", "x", time.Minute)
	v, ok := SecretFromContext(ctx, "ssh_password")
	if !ok || v != "x" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
}
```

- [ ] **Step 2: Run — FAIL**

```powershell
go test ./tool -run TestSecretFromContext -v
```

- [ ] **Step 3: Implement**

```go
type secretProviderKey struct{}

func WithSecretProvider(ctx context.Context, store AskUserFulfillmentStore) context.Context {
	return context.WithValue(ctx, secretProviderKey{}, store)
}

func SecretFromContext(ctx context.Context, field string) (string, bool) {
	store, _ := ctx.Value(secretProviderKey{}).(AskUserFulfillmentStore)
	if store == nil {
		return "", false
	}
	sid, _ := ctx.Value(ContextKeySessionID).(string)
	if sid == "" {
		return "", false
	}
	v, err := store.GetSecret(ctx, sid, field)
	if err != nil {
		return "", false
	}
	return v, true
}
```

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

```powershell
git commit -m "feat(tool): add SecretFromContext for ask_user fulfillment"
```

---

### Task 4: Portal — Extract input_required from Trace

**Files:**
- Modify: `portal/internal/service/chat_stream.go`
- Modify: `portal/internal/service/chat_stream_test.go`

- [ ] **Step 1: Write failing test**

Append to `chat_stream_test.go`:

```go
func TestInputRequestsFromResponseExtractsPendingAskUser(t *testing.T) {
	resp := &agent.Response{
		Text: "Need your password.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolCallID: "call_1",
					ToolName:   "ask_user",
					Result: map[string]any{
						"status":     "pending",
						"request_id": "req_abc",
						"token":      "tok_xyz",
						"kind":       "password",
						"field":      "ssh_password",
						"prompt":     "Enter SSH password",
						"title":      "SSH Password",
						"expires_in": 600,
					},
				}},
			},
		},
	}
	items := inputRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("got %d", len(items))
	}
	got := items[0]
	if got.Token != "tok_xyz" || got.Kind != "password" || got.ID != "call_1:tok_xyz" {
		t.Fatalf("%#v", got)
	}
}

func TestStreamEventsFromResponse_InputBeforeConfirm(t *testing.T) {
	resp := &agent.Response{
		Text: "Need input.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{
					{ToolCallID: "c1", ToolName: "ask_user", Result: map[string]any{
						"status": "pending", "request_id": "r1", "token": "t1",
						"kind": "text", "field": "username", "prompt": "Username?",
					}},
					{ToolCallID: "c2", ToolName: "execute_write", Result: map[string]any{
						"status": "pending", "token": "wt", "dsl": "DELETE FROM x",
					}},
				},
			},
		},
	}
	events := streamEventsFromResponse(resp)
	if len(events) != 3 {
		t.Fatalf("want chunk+input+confirm, got %d: %#v", len(events), events)
	}
	if events[1].Type != ChatStreamEventInputRequired || events[2].Type != ChatStreamEventConfirmRequired {
		t.Fatalf("order wrong: %#v", events)
	}
}
```

- [ ] **Step 2: Run — FAIL**

```powershell
cd d:\workspace\github\sixath\portal
go test ./internal/service -run InputRequests -v
```

- [ ] **Step 3: Extend `chat_stream.go`**

Add:
- `ChatStreamEventInputRequired`
- `ChatInputRequest` struct (spec §5.1)
- `Input *ChatInputRequest` on `ChatStreamEvent`
- `inputRequestsFromResponse(resp)` — mirror `confirmationRequestsFromResponse`
- Update `streamEventsFromResponse`: chunk → input_required* → confirm_required*

Severity: `password` → `"warning"`, else `"default"`.

- [ ] **Step 4: Run — PASS**

```powershell
go test ./internal/service -run "InputRequests|StreamEventsFromResponse" -v
```

- [ ] **Step 5: Commit**

---

### Task 5: Portal — SSE input_required

**Files:**
- Modify: `portal/internal/server/chat_sse.go`
- Modify: `portal/internal/server/chat_sse_test.go` (if exists, else add minimal test)

- [ ] **Step 1: Add SSE branch**

In `SendMessageSSE` switch:

```go
case service.ChatStreamEventInputRequired:
	if event.Input != nil {
		writeSSEEvent(ctx, "input_required", map[string]any{"input": event.Input})
	}
```

- [ ] **Step 2: Manual smoke**

Start portal, trigger mock response with pending ask_user trace, verify SSE contains `event: input_required`.

- [ ] **Step 3: Commit**

---

### Task 6: Portal — input_response Helper + Synthetic Messages

**Files:**
- Create: `portal/internal/chat/input_response.go`
- Create: `portal/internal/chat/input_response_test.go`
- Create: `portal/internal/chat/ask_user_wiring.go`

- [ ] **Step 1: Write failing test**

```go
func TestBuildSyntheticAskUserMessages_Fulfilled(t *testing.T) {
	pending := tool.PendingInputRequest{
		ToolCallID: "call_1",
		Token:      "tok_1",
		Field:      "ssh_password",
		Kind:       "password",
		Prompt:     "Enter password",
	}
	msgs := BuildSyntheticAskUserMessages(pending, SyntheticAskUserOutcomeFulfilled)
	if len(msgs) != 2 {
		t.Fatalf("want assistant+tool, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[1].Role != "tool" {
		t.Fatal("roles wrong")
	}
	if strings.Contains(msgs[1].Content, "hunter2") {
		t.Fatal("tool content must not contain secret")
	}
}

func TestUserMessagePlaceholderForInput(t *testing.T) {
	got := UserMessagePlaceholderForInput("ssh_password")
	if got != "[input provided: ssh_password]" {
		t.Fatalf("%q", got)
	}
}
```

- [ ] **Step 2: Run — FAIL**

```powershell
go test ./internal/chat -run SyntheticAskUser -v
```

- [ ] **Step 3: Implement `input_response.go`**

```go
type SyntheticAskUserOutcome int
const (
	SyntheticAskUserOutcomeFulfilled SyntheticAskUserOutcome = iota
	SyntheticAskUserOutcomeCancelled
)

type InputResponse struct {
	Token     string `json:"token"`
	RequestID string `json:"request_id"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	Cancelled bool   `json:"cancelled"`
}

func UserMessagePlaceholderForInput(field string) string {
	return fmt.Sprintf("[input provided: %s]", field)
}

func BuildSyntheticAskUserMessages(p tool.PendingInputRequest, outcome SyntheticAskUserOutcome) []model.Message { /* spec §5.3 */ }

func ApplyInputResponse(ctx context.Context, sessionID string, ir InputResponse, pendingStore tool.AskUserPendingStore, fulfillStore tool.AskUserFulfillmentStore) (tool.PendingInputRequest, SyntheticAskUserOutcome, error) { /* validate token, put secret, delete pending */ }
```

- [ ] **Step 4: Implement `ask_user_wiring.go`**

Session-scoped singleton stores (process-local for v0.1):

```go
var defaultAskUserPending = tool.NewInMemoryAskUserPendingStore()
var defaultAskUserFulfill = tool.NewInMemoryAskUserFulfillmentStore()

func RegisterAskUserTools(reg *tool.Registry) error {
	return tool.RegisterAskUserTool(reg, &tool.AskUserConfig{
		PendingStore:     defaultAskUserPending,
		FulfillmentStore: defaultAskUserFulfill,
		TokenGen:         tool.RandomTokenGenerator{},
		TTLSeconds:       600,
	})
}

func AskUserPendingStore() tool.AskUserPendingStore { return defaultAskUserPending }
func AskUserFulfillmentStore() tool.AskUserFulfillmentStore { return defaultAskUserFulfill }
```

- [ ] **Step 5: Run — PASS**

- [ ] **Step 6: Commit**

---

### Task 7: Portal — SendMessage Integration + System Prompt

**Files:**
- Modify: `portal/internal/service/chat.go`
- Modify: `portal/internal/server/chat_sse.go` — extend bind body for `input_response`
- Modify: `portal/internal/chat/agent_builder.go` — call `RegisterAskUserTools`

- [ ] **Step 1: Write failing test**

`portal/internal/service/chat_input_test.go`:

```go
func TestSendMessage_PasswordNotPersistedInContent(t *testing.T) {
	// table-driven: when input_response with password value submitted,
	// CreateMessage content must equal "[input provided: ssh_password]" not plaintext
}
```

- [ ] **Step 2: Register tool in agent build path**

In `BuildRegistry` or `BuildReActAgent` setup (after reg created):

```go
if err := RegisterAskUserTools(reg); err != nil { return nil, err }
```

- [ ] **Step 3: Extend SSE HTTP body struct**

In `chat_sse.go`:

```go
var body struct {
	Content       string                 `json:"content"`
	InputResponse *chat.InputResponse    `json:"input_response"`
}
```

Allow empty `content` when `input_response` present.

- [ ] **Step 4: Shared helper in ChatService**

```go
func (s *ChatService) prepareMessagesForTurn(ctx context.Context, sessionID, content string, ir *chat.InputResponse, history []*biz.ChatMessage, systemPrompt string) ([]model.Message, string, error) {
	userContent := content
	if ir != nil {
		outcome, pending, err := chat.ApplyInputResponse(ctx, sessionID, *ir, ...)
		// userContent = chat.UserMessagePlaceholderForInput(pending.Field)
		// append synthetic messages before user message
	}
	// build messages slice...
}
```

Wire into `SendMessage` and `SendMessageStream`.

- [ ] **Step 5: Bind SecretProvider on runCtx**

```go
runCtx = tool.WithSecretProvider(runCtx, chat.AskUserFulfillmentStore())
```

- [ ] **Step 6: Append ask_user system prompt snippet**

In `BuildEffectiveSystemPromptForTurn` or dedicated helper when ask_user registered — spec §7 text.

- [ ] **Step 7: Run tests**

```powershell
go test ./internal/service ./internal/chat -v
```

- [ ] **Step 8: Commit**

---

### Task 8: Frontend — Stream Helpers

**Files:**
- Modify: `web/src/api/chatStream.ts`
- Create: `web/tests/chatStream-input.test.ts`
- Modify: `web/package.json` — ensure test script covers new file

- [ ] **Step 1: Write failing tests**

```ts
test('parseInputRequiredPayload accepts valid input events', () => {
  const parsed = parseInputRequiredPayload({
    input: {
      request_id: 'req_1',
      token: 'tok_1',
      kind: 'password',
      field: 'ssh_password',
      title: 'SSH Password',
      prompt: 'Enter password',
      expires_in: 600,
    },
  })
  assert.equal(parsed?.token, 'tok_1')
  assert.equal(parsed?.kind, 'password')
})

test('buildInputResponseBody omits password from visible content', () => {
  const body = buildInputSubmitBody({
    request_id: 'req_1',
    token: 'tok_1',
    field: 'ssh_password',
    kind: 'password',
    title: 't',
    prompt: 'p',
  }, 'secret')
  assert.equal(body.content, '')
  assert.equal(body.input_response?.value, 'secret')
})
```

- [ ] **Step 2: Run — FAIL**

```powershell
cd d:\workspace\github\sixath\web
npm test
```

- [ ] **Step 3: Implement helpers in `chatStream.ts`**

Add `ChatInputRequest`, `parseInputRequiredPayload`, `buildInputSubmitBody`.

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 9: Frontend — InputCard UI

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/ChatPage.tsx`
- Modify: `web/src/pages/ChatPage.css`

- [ ] **Step 1: Extend `sendMessageStream` callbacks**

```ts
onInputRequired?: (input: ChatInputRequest) => void
```

Parse `event: input_required` in SSE loop.

- [ ] **Step 2: Add state + handlers in ChatPage**

Mirror confirmation card pattern from `2026-04-28-page-confirmation.md`:

```ts
interface ChatInputItem extends ChatInputRequest {
  messageKey: string
  status: 'pending' | 'submitting' | 'submitted' | 'cancelled'
  draft?: string
}
```

- [ ] **Step 3: Render InputCard by kind**

- `text` / `password`: input field (password uses `type="password"`)
- `select`: `<select>` from `options`
- `confirm`: Yes/Cancel buttons → value `"yes"` or cancelled

Submit via fetch POST with `buildInputSubmitBody` — **do not** put password in visible chat input.

- [ ] **Step 4: CSS** — `.chat-input-card`, `.chat-input-card-warning` for password

- [ ] **Step 5: Verify**

```powershell
npm test
npm run build
```

- [ ] **Step 6: Commit**

---

### Task 10: End-to-End Verification

**Files:** none new

- [ ] **Step 1: Framework tests**

```powershell
cd d:\workspace\github\sixath\framework
go test ./tool -v
```

Expected: all ask_user tests PASS

- [ ] **Step 2: Portal tests**

```powershell
cd d:\workspace\github\sixath\portal
go test ./internal/service ./internal/chat ./internal/server -v
```

Expected: PASS

- [ ] **Step 3: Frontend**

```powershell
cd d:\workspace\github\sixath\web
npm test
npm run build
```

Expected: PASS

- [ ] **Step 4: Manual E2E checklist (spec §10.3)**

1. 配置 Agent 可使用 `ask_user`（core toolset）
2. 发送「帮我 SSH 到某主机」类 prompt，观察模型调用 `ask_user`
3. SSE 收到 `input_required`，UI 显示密码卡片
4. 提交后下轮 Agent 继续；DB `chat_messages` 中 user 内容为 `[input provided: ssh_password]`，无明文
5. （若 Task 11 已做）`ssh_exec` 成功连接

- [ ] **Step 5: Update spec status**

In spec front matter: `状态: 已实施（v0.1）` after merge.

---

### Task 11（P4 可选）: ssh_exec 读取 SecretFromContext

**Files:**
- Modify: `framework/tool/ssh_exec.go`（或等价 SSH 工具文件）
- Test: 对应 `*_test.go`

- [ ] **Step 1: Test — password from context when arg empty**

- [ ] **Step 2: In Execute, before connect:**

```go
if cfg.Password == "" {
	if v, ok := SecretFromContext(ctx, "ssh_password"); ok {
		cfg.Password = v
	}
}
```

- [ ] **Step 3: Run tool tests — PASS**

- [ ] **Step 4: Commit**

---

## Milestone Summary

| 里程碑 | Tasks | 交付物 |
|--------|-------|--------|
| **M1 — Framework core** | 1–3 | `ask_user` 工具可单测通过 |
| **M2 — Portal 协议** | 4–7 | SSE `input_required` + input_response 闭环 |
| **M3 — Frontend** | 8–9 | InputCard UI |
| **M4 — 验收** | 10 | 全量测试 + 手动 E2E |
| **M5 — 工具集成（可选）** | 11 | ssh_exec 读 secret |

**预估 PR 拆分:** M1 → framework PR；M2 → portal PR；M3 → web PR；M5 独立小 PR。

---

## Self-Review

**Spec coverage:** H-O1–H-O5、§3–§7、§9–§10 均有对应 Task；P5 checkpoint 明确排除。

**Placeholder scan:** 无 TBD；Task 11 标为可选 P4。

**Type consistency:** `ChatInputRequest` / `InputResponse` / `PendingInputRequest` 字段名与 spec §5.1、§5.3 一致；`input` vs `confirmation` SSE payload 对称。

**开放问题默认（spec §12）:**
- Q1: text fulfilled 含 value — Task 2 单测 `TestAskUser_TextFulfilledIncludesValue`
- Q2: 同 field 新 pending 覆盖旧 token — Task 2 `SavePending` 同 session+field 时 delete 旧条目
- Q3: v0.1 仅 Run 完成后提取 — Task 4–5
- Q4: v0.1 HTTP JSON 扩展 body，不改 proto — Task 7 Step 3

---

## Execution Handoff

Plan complete and saved to `framework/docs/superpowers/plans/2026-05-23-human-in-the-loop-ask-user.md`.

**Two execution options:**

1. **Subagent-Driven（推荐）** — 每个 Task 派发独立 subagent，Task 间人工/代理 review
2. **Inline Execution** — 本会话按 Task 1→10 顺序执行，每里程碑 checkpoint

**Which approach?**
