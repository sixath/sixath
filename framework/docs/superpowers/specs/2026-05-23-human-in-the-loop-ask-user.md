# Human-in-the-Loop：`ask_user` 工具与 `input_required` 协议

**版本**: 0.1（设计稿）  
**状态**: 待评审  
**范围**: `framework/tool`、`framework/agent`（最小改动）、`portal` SSE/API、`web` 前端  
**关联**: 现有 `execute_write` 两阶段确认、`confirm_required` SSE 事件（`portal/internal/service/chat_stream.go`）

---

## 1. 目标与非目标

### 1.1 目标

| ID | 目标 |
|----|------|
| H-O1 | Agent 可通过**结构化工具**向用户请求文本、密码、选择、确认，而非仅靠自然语言追问 |
| H-O2 | Portal SSE 推送 `input_required` 事件，前端渲染专用输入卡片 |
| H-O3 | 用户提交后，**下一轮** `Run` 自动注入 tool result，ReAct 循环继续；**不要求** Run 中途挂起 |
| H-O4 | 敏感字段（`password`）**不落库**、不进 transcript/记忆索引 |
| H-O5 | 与 `execute_write` / `confirm_required` **共用同一套 pending + token 心智模型** |

### 1.2 非目标（v0.1 不做）

- Run 级 checkpoint / `ResumeRun`（Layer 3，见 §8）
- 独立 `HumanInTheLoopAgent` 类型
- 跨 session 审批、多人会签
- IM 网关（Telegram 等）适配

---

## 2. 总体流程

```
Turn N — Agent Run
  Model → ask_user({ prompt, kind: "password", field: "ssh_password" })
  Tool  → { status: "pending", request_id, token, expires_in }
  Run   → 正常结束（StreamEventDone）
  Portal→ SSE input_required { input: {...} }
  UI    → 渲染密码输入卡片

Turn N+1 — User submits
  方式 A（推荐 v0.1）: POST .../messages 带 input_response metadata
  方式 B（兼容）:      用户自然语言回复 + metadata 绑定 request_id
  Portal→ 注入 synthetic tool message（fulfilled）
  Agent → 继续 ReAct，看到 password 已提供，调用 ssh_exec 等
```

**关键设计选择**：v0.1 采用 **「工具返回 pending → Run 结束 → 下轮注入 fulfilled result」**，与 `execute_write` 一致，**不修改** `ReActAgent` 主循环。

---

## 3. Framework：`ask_user` 工具

### 3.1 工具注册

- **包**: `framework/tool/ask_user.go`
- **名称**: `ask_user`
- **Toolset**: `ToolsetCore`（与 `execute_write` 同级，Hermes core 标签）
- **注册**: `RegisterAskUserTool(reg, cfg)`

### 3.2 参数 Schema

```json
{
  "type": "object",
  "required": ["prompt"],
  "properties": {
    "prompt": {
      "type": "string",
      "description": "Shown to the user; explain why this input is needed."
    },
    "kind": {
      "type": "string",
      "enum": ["text", "password", "select", "confirm"],
      "description": "Input widget type. Default: text."
    },
    "field": {
      "type": "string",
      "description": "Stable field key for this request (e.g. ssh_username). Used to match fulfillment."
    },
    "title": {
      "type": "string",
      "description": "Short card title; defaults from prompt."
    },
    "options": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Required when kind=select."
    },
    "required": {
      "type": "boolean",
      "description": "Default true."
    },
    "response_token": {
      "type": "string",
      "description": "Fulfillment token from a previous pending response; when set, returns fulfilled/cancelled/expired status instead of creating a new request."
    },
    "value": {
      "type": "string",
      "description": "User-provided value when fulfilling via tool (internal/CLI path); Portal uses metadata injection instead."
    },
    "cancelled": {
      "type": "boolean",
      "description": "When true with response_token, marks request cancelled."
    }
  }
}
```

### 3.3 返回 Schema

**Pending（首次调用，无 `response_token`）**

```json
{
  "status": "pending",
  "request_id": "req_8f3a...",
  "token": "tok_c4e1...",
  "kind": "password",
  "field": "ssh_password",
  "prompt": "请输入 SSH 密码以连接 10.0.0.1",
  "title": "SSH 密码",
  "expires_in": 600
}
```

**Fulfilled（Portal 注入或 CLI 带 token+value）**

```json
{
  "status": "fulfilled",
  "request_id": "req_8f3a...",
  "field": "ssh_password",
  "kind": "password",
  "value_redacted": true
}
```

> 模型侧 tool message 中**不包含**明文 `value`；仅 `fulfilled` + `field`。实际 secret 通过 `AskUserFulfillmentStore` 供**同 Run 内后续工具**读取（见 §3.6）。

**Cancelled / Expired**

```json
{ "status": "cancelled", "request_id": "req_8f3a...", "field": "ssh_password" }
{ "status": "expired",   "request_id": "req_8f3a...", "field": "ssh_password" }
```

### 3.4 配置与依赖

```go
// framework/tool/ask_user.go

type AskUserConfig struct {
    Store    AskUserPendingStore
    TokenGen TokenGenerator          // 复用 execute_write 的 RandomTokenGenerator
    TTLSeconds int                   // 默认 600
}

type PendingInputRequest struct {
    RequestID   string    `json:"request_id"`
    Token       string    `json:"token"`
    SessionID   string    `json:"session_id"`
    ToolCallID  string    `json:"tool_call_id"`
    Kind        string    `json:"kind"`
    Field       string    `json:"field"`
    Prompt      string    `json:"prompt"`
    Title       string    `json:"title"`
    Options     []string  `json:"options,omitempty"`
    Required    bool      `json:"required"`
    CreatedAt   time.Time `json:"created_at"`
}

type AskUserPendingStore interface {
    SavePending(ctx context.Context, sessionID string, p PendingInputRequest) error
    GetPending(ctx context.Context, sessionID, token string) (*PendingInputRequest, error)
    DeletePending(ctx context.Context, sessionID, token string) error
}

// 会话内短期 secret 槽；仅内存或加密 Redis，禁止写 MySQL chat_messages
type AskUserFulfillmentStore interface {
    PutSecret(ctx context.Context, sessionID, field, value string, ttl time.Duration) error
    GetSecret(ctx context.Context, sessionID, field string) (string, error)
    DeleteSecret(ctx context.Context, sessionID, field string) error
}
```

**v0.1 Store 实现**：

| 实现 | 用途 |
|------|------|
| `InMemoryAskUserStore` | 单进程 dev/test |
| `SessionScopedAskUserStore` | portal 注入，key = `session_id` + token |

`session_id` 从 `ctx.Value(tool.ContextKeySessionID)` 读取（与 `execute_write` 一致）。

### 3.5 Execute 逻辑（伪代码）

```
if response_token != "":
    pending = store.GetPending(session_id, response_token)
    if pending == nil → return expired
    if cancelled → delete pending; return cancelled
    if value == "" → return error
    if kind == password → fulfillmentStore.PutSecret(session_id, field, value, ttl)
    delete pending
    return fulfilled (no plaintext in result)

// 新建 pending
token = tokenGen.NewToken()
request_id = "req_" + shortHex()
save pending
return pending response
```

### 3.6 后续工具读取 Secret

需要密码的工具（如 `ssh_exec`）不通过 LLM 上下文传参，而读 fulfillment store：

```go
// framework/tool/ssh_exec.go（示例扩展）
if pass, ok := askuser.SecretFromContext(ctx, "ssh_password"); ok {
    cfg.Password = pass
}
```

新增 context helper：

```go
// framework/tool/ask_user_context.go
func SecretFromContext(ctx context.Context, field string) (string, bool)
func WithSecretProvider(ctx context.Context, p SecretProvider) context.Context
```

Portal 在 `Run` 前将 `AskUserFulfillmentStore` 绑到 ctx。

---

## 4. Framework：事件与 Trace

### 4.1 不新增 StreamEventType（v0.1）

沿用现有模式：**Portal 从 `RunTrace.ToolCalls` 提取 pending**，与 `confirmationRequestsFromResponse` 对称。

可选：在 `events/event.go` 增加审计 Kind（非必须 v0.1）：

```go
InputRequested  Kind = "agent.input.requested"
InputFulfilled  Kind = "agent.input.fulfilled"
InputCancelled  Kind = "agent.input.cancelled"
```

工具 `Execute` 内 `bus.Publish`；payload **不得**含 secret 明文。

### 4.2 ToolCallRecord 约定

`ask_user` pending 结果进入 `trace.ToolCalls[].Result`，Portal 扫描条件：

```go
call.ToolName == "ask_user"
result["status"] == "pending"
result["token"] != ""
result["request_id"] != ""
```

---

## 5. Portal：SSE 与 API

### 5.1 扩展 `ChatStreamEvent`

```go
// portal/internal/service/chat_stream.go

const ChatStreamEventInputRequired ChatStreamEventType = "input_required"

type ChatStreamEvent struct {
    // ...existing...
    Input *ChatInputRequest
}

type ChatInputRequest struct {
    ID          string   `json:"id"`           // tool_call_id:token
    RequestID   string   `json:"request_id"`
    Token       string   `json:"token"`
    Kind        string   `json:"kind"`         // text|password|select|confirm
    Field       string   `json:"field"`
    Title       string   `json:"title"`
    Prompt      string   `json:"prompt"`
    Options     []string `json:"options,omitempty"`
    Required    bool     `json:"required"`
    ExpiresIn   int      `json:"expires_in,omitempty"`
    Severity    string   `json:"severity"`     // default | warning
}
```

提取函数（与 `confirmationRequestsFromResponse` 并列）：

```go
func inputRequestsFromResponse(resp *agent.Response) []ChatInputRequest
func streamEventsFromResponse(resp *agent.Response) []ChatStreamEvent
// 顺序：chunk(s) → input_required* → confirm_required*
```

### 5.2 SSE 事件

```
event: input_required
data: {"input": { ... ChatInputRequest ... }}
```

`chat_sse.go` switch 增加 `ChatStreamEventInputRequired` 分支。

### 5.3 用户提交：扩展 SendMessage

**Proto / HTTP body**（向后兼容，字段可选）：

```json
{
  "content": "",
  "input_response": {
    "token": "tok_c4e1...",
    "request_id": "req_8f3a...",
    "field": "ssh_password",
    "value": "••••••",
    "cancelled": false
  }
}
```

- `content` 可为空（纯结构化提交）
- 若同时有 `content` 与 `input_response`，**优先处理 `input_response`**

**Portal 处理流程**（`SendMessage` / `SendMessageStream` 共用 helper）：

```go
func (s *ChatService) applyInputResponse(ctx context.Context, sessionID string, ir *InputResponse) error
```

1. 校验 token + session + 未过期
2. `fulfillmentStore.PutSecret(sessionID, field, value, ttl)`（password）或暂存 plain text（text/select）
3. **不**将 `value` 写入 `chat_messages`；user 消息存占位符：

   ```
   [input provided: ssh_password]
   ```

4. 构建 **synthetic messages** 追加到当轮 `agent.Request`：

```go
// assistant tool call（从历史 trace 恢复或从 pending store 重建 metadata）
model.Message{
    Role: "assistant",
    Metadata: map[string]any{"tool_calls": []model.ToolCall{{
        ID:   pending.ToolCallID,
        Name: "ask_user",
        Arguments: {...original...},
    }}},
}
// tool result — fulfilled，无明文
model.Message{
    Role:    "tool",
    Content: `{"status":"fulfilled","field":"ssh_password","value_redacted":true}`,
    Metadata: map[string]any{
        "tool_name":    "ask_user",
        "tool_call_id": pending.ToolCallID,
    },
}
// 可选 user 自然语言
model.Message{Role: "user", Content: "已提供 SSH 密码。"}
```

5. 正常调用 `a.Run(...)`

**Cancel**：

```json
{ "input_response": { "token": "...", "cancelled": true } }
```

→ synthetic tool result `{ "status": "cancelled" }`，Agent 决定重试或放弃。

### 5.4 Metadata 注入（可选快捷路径）

除 HTTP body 外，支持 `Request.Metadata["input_responses"]`：

```go
[]map[string]any{
    {"token": "...", "field": "...", "value": "..."},
}
```

供 CLI / 自动化测试使用；Portal HTTP 路径转成同一结构。

---

## 6. Frontend 协议

### 6.1 TypeScript 类型

```ts
// web/src/api/chatStream.ts

export interface ChatInputRequest {
  id?: string
  request_id: string
  token: string
  kind: 'text' | 'password' | 'select' | 'confirm'
  field: string
  title: string
  prompt: string
  options?: string[]
  required?: boolean
  expires_in?: number
  severity?: 'default' | 'warning'
}

export function parseInputRequiredPayload(payload: unknown): ChatInputRequest | null
```

### 6.2 SSE 解析

`client.ts` 增加 `onInputRequired?: (input: ChatInputRequest) => void`，处理 `event: input_required`。

### 6.3 UI 卡片

| kind | 组件 |
|------|------|
| `text` | `<input type="text">` |
| `password` | `<input type="password" autocomplete="off">` |
| `select` | `<select>` |
| `confirm` | Confirm / Cancel 按钮（无 value 字段，`value="yes"` / cancelled） |

提交调用：

```ts
await chatApi.sendMessage(sessionId, {
  content: '',
  input_response: {
    token: input.token,
    request_id: input.request_id,
    field: input.field,
    value: formValue,
  },
})
```

**禁止**把 password 填入可见 chat input 或 echo 到 message list。

### 6.4 与 confirm_required 共存

同一 assistant turn 可同时有：

- `input_required`（缺参数）
- `confirm_required`（危险写操作）

渲染顺序：先 input cards，再 confirm cards（或按 SSE 到达顺序）。

---

## 7. System Prompt 指引

在 `BuildEffectiveSystemPromptForTurn` 或 Agent 默认 system 中追加（当注册了 `ask_user`）：

```markdown
## 用户输入工具 ask_user

- 需要用户提供凭据、选择环境、或明确确认时，调用 `ask_user`，不要只在文本里索要敏感信息。
- `kind=password` 用于密码/密钥；用户提交后通过安全通道传递，你不会在上下文中看到明文。
- 收到 `status=fulfilled` 后，继续调用后续工具；若 `cancelled` 或 `expired`，向用户说明并给出替代方案。
- 不要重复 ask_user 同一 `field`，除非上一次 cancelled/expired。
```

---

## 8. 分阶段路线图

| 阶段 | 内容 | 改动面 |
|------|------|--------|
| **P0** | `ask_user` 工具 + InMemory store + 单测 | `framework/tool` |
| **P1** | Portal `input_required` SSE + `inputRequestsFromResponse` | `portal/internal/service`, `chat_sse.go` |
| **P2** | `SendMessage` body `input_response` + synthetic tool messages | `portal`, `api/chat/v1` |
| **P3** | 前端 InputCard + password 不落库 | `web` |
| **P4** | `ssh_exec` 等工具读 `SecretFromContext` | `framework/tool` |
| **P5**（可选） | Run checkpoint / `InterruptPolicy` | `framework/agent` |

---

## 9. 安全与合规

| 项 | 要求 |
|----|------|
| 密码存储 | 仅 `AskUserFulfillmentStore`，TTL ≤ pending TTL，Run 结束可主动清除 |
| chat_messages | user 消息存占位符，不存明文 |
| transcript / memory index | 排除 `[input provided:*]` 与 tool result 中的 secret |
| 日志 / trace | `ToolStarted` payload 对 `kind=password` 打码 |
| 审计 | `InputFulfilled` 事件记录 field/session，不记录 value |

---

## 10. 测试清单

### 10.1 Framework

- `TestAskUser_PendingThenFulfill`
- `TestAskUser_ExpiredToken`
- `TestAskUser_Cancelled`
- `TestAskUser_PasswordNotInToolResultJSON`

### 10.2 Portal

- `TestInputRequestsFromResponseExtractsPendingAskUser`
- `TestStreamEventsFromResponse_InputBeforeConfirm`
- `TestApplyInputResponse_SyntheticToolMessage`
- `TestSendMessage_PasswordNotPersistedInContent`

### 10.3 E2E（手动）

1. Agent 调用 `ask_user` kind=password → SSE `input_required`
2. 前端提交 → 下轮 Agent 成功 `ssh_exec`
3. DB 中无 password 明文

---

## 11. 与 execute_write 对照

| 维度 | execute_write | ask_user |
|------|---------------|----------|
| 目的 | 确认危险写操作 | 收集用户输入 |
| Pending 字段 | token, dsl | token, request_id, kind, field, prompt |
| SSE 事件 | confirm_required | input_required |
| 用户提交 | 自然语言 + confirm_token | input_response 结构化 body |
| Fulfillment | 再次调用工具带 token | synthetic tool message + secret store |
| 明文可见性 | DSL 可见 | password 不可见 |

---

## 12. 开放问题

| # | 问题 | 建议默认 |
|---|------|----------|
| Q1 | plain `text` 是否也走 secret store？ | 否；仅 password 走 secret store，text 可在 fulfilled result 中返回给模型 |
| Q2 | 同一 field 多次 pending | 后一次覆盖前一次 pending，旧 token 失效 |
| Q3 | 流式 Run 中途 emit input_required | v0.1 仅 Run 完成后从 trace 提取；流式中途需 P5 |
| Q4 | proto 代码生成 | `SendMessageRequest` 增加 `InputResponse` message；或 v0.1 仅 HTTP JSON 扩展 |

---

## 13. 最小文件清单（实现时）

**Create**

- `framework/tool/ask_user.go`
- `framework/tool/ask_user_test.go`
- `framework/tool/ask_user_store_memory.go`
- `framework/tool/ask_user_context.go`
- `portal/internal/chat/input_response.go` — synthetic message 构建

**Modify**

- `portal/internal/service/chat_stream.go` — `InputRequired` 类型与提取
- `portal/internal/server/chat_sse.go` — SSE 分支
- `portal/internal/service/chat.go` — SendMessage 处理 input_response
- `portal/internal/chat/agent_builder.go` — RegisterAskUserTool
- `web/src/api/chatStream.ts` — parseInputRequiredPayload
- `web/src/api/client.ts` — onInputRequired
- `web/src/pages/ChatPage.tsx` — InputCard UI

**不修改（v0.1）**

- `framework/agent/react_agent.go` 主循环
