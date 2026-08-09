# Page Confirmation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add chat confirmation cards for pending agent actions and a reusable page confirmation dialog for destructive or high-impact UI actions.

**Architecture:** The backend converts pending `execute_write` tool results from the agent trace into structured chat stream events. The frontend stream parser exposes those events to `ChatPage`, which renders per-turn confirmation cards and sends a follow-up token message when confirmed. Ordinary pages use a shared `ConfirmDialog` component instead of `window.confirm`.

**Tech Stack:** Go 1.25/Kratos in `portal`, React 19/TypeScript/Vite in `web`, Node 22 built-in test runner for lightweight TypeScript helper tests.

---

## File Structure

- Create `portal/internal/service/chat_stream.go`: chat stream event types plus confirmation extraction from `agent.Response`.
- Create `portal/internal/service/chat_stream_test.go`: tests for extracting pending `execute_write` confirmations.
- Modify `portal/internal/service/chat.go`: return typed stream events from `SendMessageStream`.
- Modify `portal/internal/server/chat_sse.go`: emit `chunk`, `confirm_required`, `done`, and `error` SSE events from typed stream events.
- Create `web/src/api/chatStream.ts`: frontend confirmation event types, runtime validation, and confirm-message builder.
- Create `web/tests/chatStream.test.ts`: Node test coverage for confirmation payload parsing and follow-up message generation.
- Modify `web/src/api/client.ts`: add `onConfirmRequired` callback support to `sendMessageStream`.
- Modify `web/src/pages/ChatPage.tsx`: store and render confirmation cards, wire confirm/cancel behavior.
- Modify `web/src/pages/ChatPage.css`: confirmation card layout and states.
- Create `web/src/components/ConfirmDialog.tsx`: reusable modal dialog.
- Create `web/src/components/ConfirmDialog.css`: modal overlay and danger/default styles.
- Modify selected page files in `web/src/pages`: replace `window.confirm` in `ToolList.tsx`, `AgentList.tsx`, `ChannelList.tsx`, `CronTaskList.tsx`, and `CronTaskDetail.tsx`.
- Modify `web/package.json`: add a lightweight `test` script using Node's built-in test runner.

## Task 1: Backend Confirmation Extraction

**Files:**
- Create: `portal/internal/service/chat_stream.go`
- Create: `portal/internal/service/chat_stream_test.go`

- [ ] **Step 1: Write the failing test**

Create `portal/internal/service/chat_stream_test.go`:

```go
package service

import (
	"testing"

	"github.com/sixath/framework/agent"
)

func TestConfirmationRequestsFromResponseExtractsPendingExecuteWrite(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm before execution.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status":     "pending",
						"token":      "abc123",
						"dsl":        "UPDATE users SET active = 0 WHERE id = 7",
						"expires_in": 300,
					},
				}},
			},
		},
	}

	items := confirmationRequestsFromResponse(resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(items))
	}
	got := items[0]
	if got.Kind != "execute_write" || got.Token != "abc123" {
		t.Fatalf("unexpected confirmation identity: %#v", got)
	}
	if got.DSL != "UPDATE users SET active = 0 WHERE id = 7" {
		t.Fatalf("unexpected dsl: %q", got.DSL)
	}
	if got.ExpiresIn != 300 || got.Severity != "danger" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestConfirmationRequestsFromResponseIgnoresMalformedResults(t *testing.T) {
	resp := &agent.Response{
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status": "pending",
						"dsl":    "DELETE FROM users",
					},
				}},
			},
		},
	}

	if got := confirmationRequestsFromResponse(resp); len(got) != 0 {
		t.Fatalf("expected malformed confirmation to be ignored, got %#v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```powershell
go test ./internal/service -run ConfirmationRequestsFromResponse
```

Expected: FAIL because `confirmationRequestsFromResponse` is undefined.

- [ ] **Step 3: Add the minimal implementation**

Create `portal/internal/service/chat_stream.go`:

```go
package service

import (
	"fmt"

	"github.com/sixath/framework/agent"
)

type ChatStreamEventType string

const (
	ChatStreamEventChunk           ChatStreamEventType = "chunk"
	ChatStreamEventConfirmRequired ChatStreamEventType = "confirm_required"
	ChatStreamEventError           ChatStreamEventType = "error"
)

type ChatStreamEvent struct {
	Type         ChatStreamEventType
	Content      string
	Error        string
	Confirmation *ChatConfirmationRequest
}

type ChatConfirmationRequest struct {
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Token       string `json:"token"`
	DSL         string `json:"dsl"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
	Severity    string `json:"severity"`
}

func confirmationRequestsFromResponse(resp *agent.Response) []ChatConfirmationRequest {
	if resp == nil || resp.Metadata == nil {
		return nil
	}
	trace, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || trace == nil {
		return nil
	}
	items := make([]ChatConfirmationRequest, 0, 1)
	for _, call := range trace.ToolCalls {
		if call.ToolName != "execute_write" {
			continue
		}
		result, ok := call.Result.(map[string]any)
		if !ok {
			continue
		}
		status, _ := result["status"].(string)
		token, _ := result["token"].(string)
		dsl, _ := result["dsl"].(string)
		if status != "pending" || token == "" || dsl == "" {
			continue
		}
		items = append(items, ChatConfirmationRequest{
			ID:          fmt.Sprintf("%s:%s", call.ToolCallID, token),
			Kind:        "execute_write",
			Title:       "Confirm write operation",
			Description: "Review the operation before it is executed.",
			Token:       token,
			DSL:         dsl,
			ExpiresIn:   intFromAny(result["expires_in"]),
			Severity:    "danger",
		})
	}
	return items
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```powershell
go test ./internal/service -run ConfirmationRequestsFromResponse
```

Expected: PASS.

## Task 2: Backend Stream Events and SSE Output

**Files:**
- Modify: `portal/internal/service/chat.go`
- Modify: `portal/internal/server/chat_sse.go`
- Test: `portal/internal/service/chat_stream_test.go`

- [ ] **Step 1: Add a failing test for stream event ordering helper**

Extend `portal/internal/service/chat_stream_test.go`:

```go
func TestStreamEventsFromResponseIncludesTextBeforeConfirmation(t *testing.T) {
	resp := &agent.Response{
		Text: "Please confirm.",
		Metadata: map[string]any{
			"trace": &agent.RunTrace{
				ToolCalls: []agent.ToolCallRecord{{
					ToolName: "execute_write",
					Result: map[string]any{
						"status": "pending",
						"token":  "tok",
						"dsl":    "DELETE FROM orders WHERE id = 1",
					},
				}},
			},
		},
	}

	events := streamEventsFromResponse(resp)
	if len(events) != 2 {
		t.Fatalf("expected text and confirmation events, got %#v", events)
	}
	if events[0].Type != ChatStreamEventChunk || events[0].Content != "Please confirm." {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[1].Type != ChatStreamEventConfirmRequired || events[1].Confirmation == nil {
		t.Fatalf("unexpected confirmation event: %#v", events[1])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```powershell
go test ./internal/service -run StreamEventsFromResponse
```

Expected: FAIL because `streamEventsFromResponse` is undefined.

- [ ] **Step 3: Add `streamEventsFromResponse`**

Append to `portal/internal/service/chat_stream.go`:

```go
func streamEventsFromResponse(resp *agent.Response) []ChatStreamEvent {
	if resp == nil {
		return nil
	}
	events := make([]ChatStreamEvent, 0, 1)
	if resp.Text != "" {
		events = append(events, ChatStreamEvent{Type: ChatStreamEventChunk, Content: resp.Text})
	}
	for _, item := range confirmationRequestsFromResponse(resp) {
		confirmation := item
		events = append(events, ChatStreamEvent{
			Type:         ChatStreamEventConfirmRequired,
			Confirmation: &confirmation,
		})
	}
	return events
}
```

- [ ] **Step 4: Update `SendMessageStream` to return typed events**

In `portal/internal/service/chat.go`, change:

```go
func (s *ChatService) SendMessageStream(ctx context.Context, req *chatv1.SendMessageRequest) (<-chan string, string, error)
```

to:

```go
func (s *ChatService) SendMessageStream(ctx context.Context, req *chatv1.SendMessageRequest) (<-chan ChatStreamEvent, string, error)
```

Change the channel declaration:

```go
ch := make(chan ChatStreamEvent, 32)
```

For debug event output, send chunk events:

```go
case ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: string(e.Kind) + "[" + string(msg) + "]\r\n"}:
```

For run errors:

```go
ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
```

For successful responses:

```go
for _, event := range streamEventsFromResponse(resp) {
	ch <- event
}
```

- [ ] **Step 5: Update SSE adapter**

In `portal/internal/server/chat_sse.go`, change the stream loop:

```go
for event := range ch {
	switch event.Type {
	case service.ChatStreamEventChunk:
		full.WriteString(event.Content)
		writeSSEEvent(ctx, "chunk", map[string]any{"content": event.Content})
	case service.ChatStreamEventConfirmRequired:
		if event.Confirmation != nil {
			writeSSEEvent(ctx, "confirm_required", map[string]any{"confirmation": event.Confirmation})
		}
	case service.ChatStreamEventError:
		writeSSEEvent(ctx, "error", map[string]any{"error": event.Error})
	default:
		if event.Content != "" {
			full.WriteString(event.Content)
			writeSSEEvent(ctx, "chunk", map[string]any{"content": event.Content})
		}
	}
	if f, ok := ctx.Response().(http.Flusher); ok {
		f.Flush()
	}
}
```

- [ ] **Step 6: Verify backend tests**

Run:

```powershell
go test ./internal/service
```

Expected: PASS.

## Task 3: Frontend Stream Parsing Helpers

**Files:**
- Create: `web/src/api/chatStream.ts`
- Create: `web/tests/chatStream.test.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Add the failing frontend tests**

Create `web/tests/chatStream.test.ts`:

```ts
import assert from 'node:assert/strict'
import test from 'node:test'
import { buildConfirmMessage, parseConfirmRequiredPayload } from '../src/api/chatStream.ts'

test('parseConfirmRequiredPayload accepts valid confirmation events', () => {
  const parsed = parseConfirmRequiredPayload({
    confirmation: {
      kind: 'execute_write',
      title: 'Confirm write operation',
      description: 'Review the operation before it is executed.',
      token: 'abc123',
      dsl: 'DELETE FROM users WHERE id = 1',
      expires_in: 300,
      severity: 'danger',
    },
  })

  assert.equal(parsed?.token, 'abc123')
  assert.equal(parsed?.dsl, 'DELETE FROM users WHERE id = 1')
  assert.equal(parsed?.expires_in, 300)
})

test('parseConfirmRequiredPayload rejects malformed events', () => {
  assert.equal(parseConfirmRequiredPayload({ confirmation: { token: 'abc123' } }), null)
  assert.equal(parseConfirmRequiredPayload({}), null)
})

test('buildConfirmMessage includes token and execution intent', () => {
  const message = buildConfirmMessage({
    kind: 'execute_write',
    title: 'Confirm write operation',
    description: 'Review the operation before it is executed.',
    token: 'abc123',
    dsl: 'UPDATE users SET active = 0',
    severity: 'danger',
  })

  assert.match(message, /confirm_token/)
  assert.match(message, /abc123/)
  assert.match(message, /execute/)
})
```

Modify `web/package.json` scripts:

```json
"test": "node --test --experimental-strip-types tests/*.test.ts"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run:

```powershell
& 'D:\Program Files\nodejs\npm.cmd' test
```

Expected: FAIL because `src/api/chatStream.ts` does not exist.

- [ ] **Step 3: Add the helper module**

Create `web/src/api/chatStream.ts`:

```ts
export interface ChatConfirmationRequest {
  id?: string
  kind: string
  title: string
  description: string
  token: string
  dsl: string
  expires_in?: number
  severity: 'default' | 'danger'
}

export function parseConfirmRequiredPayload(payload: unknown): ChatConfirmationRequest | null {
  if (!isRecord(payload)) return null
  const raw = payload.confirmation
  if (!isRecord(raw)) return null
  const kind = stringValue(raw.kind)
  const title = stringValue(raw.title)
  const description = stringValue(raw.description)
  const token = stringValue(raw.token)
  const dsl = stringValue(raw.dsl)
  if (!kind || !title || !description || !token || !dsl) return null
  const severity = raw.severity === 'danger' ? 'danger' : 'default'
  const expires = typeof raw.expires_in === 'number' ? raw.expires_in : undefined
  return { id: stringValue(raw.id) || undefined, kind, title, description, token, dsl, expires_in: expires, severity }
}

export function buildConfirmMessage(request: ChatConfirmationRequest): string {
  return `Please execute the pending ${request.kind} operation with confirm_token: ${request.token}`
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}
```

- [ ] **Step 4: Run frontend tests**

Run:

```powershell
& 'D:\Program Files\nodejs\npm.cmd' test
```

Expected: PASS.

## Task 4: Frontend API Callback and Chat Confirmation Card

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/pages/ChatPage.tsx`
- Modify: `web/src/pages/ChatPage.css`

- [ ] **Step 1: Update `sendMessageStream` callback shape**

In `web/src/api/client.ts`, import:

```ts
import { parseConfirmRequiredPayload, type ChatConfirmationRequest } from './chatStream'
```

Change callback type:

```ts
callbacks: {
  onChunk: (text: string) => void
  onDone: () => void
  onError: (err: string) => void
  onConfirmRequired?: (confirmation: ChatConfirmationRequest) => void
}
```

Inside SSE parsing, add:

```ts
else if (curEvent === 'confirm_required') {
  const confirmation = parseConfirmRequiredPayload(d)
  if (confirmation) callbacks.onConfirmRequired?.(confirmation)
}
```

- [ ] **Step 2: Add chat confirmation state**

In `web/src/pages/ChatPage.tsx`, import:

```ts
import { buildConfirmMessage, type ChatConfirmationRequest } from '../api/chatStream'
```

Add local types:

```ts
interface ChatConfirmationItem extends ChatConfirmationRequest {
  messageKey: string
  status: 'pending' | 'confirming' | 'confirmed' | 'cancelled'
  error?: string
}
```

Add state:

```ts
const [confirmations, setConfirmations] = useState<ChatConfirmationItem[]>([])
```

- [ ] **Step 3: Attach stream confirmation events to the current assistant turn**

Before creating `assistantPlaceholder`, create a stable key:

```ts
const assistantKey = `${sid}-assistant-${Date.now()}`
```

Set placeholder id to that key:

```ts
id: assistantKey,
```

In `sendMessageStream` callbacks:

```ts
onConfirmRequired: (confirmation) => {
  setConfirmations((prev) => [
    ...prev,
    { ...confirmation, messageKey: assistantKey, status: 'pending' },
  ])
},
```

- [ ] **Step 4: Add confirm/cancel handlers**

Add:

```ts
const handleConfirmAction = async (item: ChatConfirmationItem) => {
  if (item.status !== 'pending') return
  setConfirmations((prev) => prev.map((c) => c === item ? { ...c, status: 'confirming', error: undefined } : c))
  const previousInput = input
  setInput(buildConfirmMessage(item))
  try {
    await handleSend(buildConfirmMessage(item))
    setConfirmations((prev) => prev.map((c) => c === item ? { ...c, status: 'confirmed' } : c))
  } catch (e) {
    setInput(previousInput)
    setConfirmations((prev) => prev.map((c) => c === item ? { ...c, status: 'pending', error: (e as Error).message } : c))
  }
}

const handleCancelAction = (item: ChatConfirmationItem) => {
  setConfirmations((prev) => prev.map((c) => c === item ? { ...c, status: 'cancelled' } : c))
}
```

To make this compile, change `handleSend` to accept an optional override:

```ts
const handleSend = async (overrideContent?: string) => {
  const content = (overrideContent ?? input).trim()
```

Only clear visible input when no override was supplied:

```ts
if (!overrideContent) setInput('')
```

- [ ] **Step 5: Render confirmation cards under assistant messages**

Inside each assistant message content block, render:

```tsx
{confirmations.filter((c) => c.messageKey === (m.id || m.created_at + m.role + idx)).map((c) => (
  <div key={`${c.messageKey}-${c.token}`} className={`chat-confirm-card chat-confirm-card-${c.severity}`}>
    <div className="chat-confirm-title">{c.title}</div>
    <div className="chat-confirm-description">{c.description}</div>
    <pre className="chat-confirm-dsl">{c.dsl}</pre>
    {c.expires_in ? <div className="chat-confirm-meta">Expires in {c.expires_in}s</div> : null}
    {c.error ? <div className="chat-confirm-error">{c.error}</div> : null}
    <div className="chat-confirm-actions">
      <button className="btn btn-danger btn-sm" disabled={c.status !== 'pending'} onClick={() => handleConfirmAction(c)}>
        {c.status === 'confirming' ? 'Confirming...' : c.status === 'confirmed' ? 'Confirmed' : 'Confirm'}
      </button>
      <button className="btn btn-secondary btn-sm" disabled={c.status !== 'pending'} onClick={() => handleCancelAction(c)}>
        {c.status === 'cancelled' ? 'Cancelled' : 'Cancel'}
      </button>
    </div>
  </div>
))}
```

- [ ] **Step 6: Add card styles**

Append to `web/src/pages/ChatPage.css`:

```css
.chat-confirm-card {
  margin-top: 1rem;
  padding: 0.875rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg);
}

.chat-confirm-card-danger {
  border-color: var(--destructive);
  background: var(--destructive-subtle);
}

.chat-confirm-title {
  font-weight: 600;
  color: var(--text-strong);
}

.chat-confirm-description,
.chat-confirm-meta {
  margin-top: 0.25rem;
  color: var(--muted);
  font-size: 0.85rem;
}

.chat-confirm-dsl {
  margin: 0.75rem 0 0;
  max-height: 180px;
  overflow: auto;
  white-space: pre-wrap;
}

.chat-confirm-error {
  margin-top: 0.5rem;
  color: var(--destructive);
  font-size: 0.85rem;
}

.chat-confirm-actions {
  margin-top: 0.75rem;
  display: flex;
  gap: 0.5rem;
}
```

- [ ] **Step 7: Verify frontend**

Run:

```powershell
& 'D:\Program Files\nodejs\npm.cmd' test
& 'D:\Program Files\nodejs\npm.cmd' run build
```

Expected: both PASS.

## Task 5: Shared Page Confirmation Dialog

**Files:**
- Create: `web/src/components/ConfirmDialog.tsx`
- Create: `web/src/components/ConfirmDialog.css`
- Modify: `web/src/pages/ToolList.tsx`
- Modify: `web/src/pages/AgentList.tsx`
- Modify: `web/src/pages/ChannelList.tsx`
- Modify: `web/src/pages/CronTaskList.tsx`
- Modify: `web/src/pages/CronTaskDetail.tsx`

- [ ] **Step 1: Create the dialog component**

Create `web/src/components/ConfirmDialog.tsx`:

```tsx
import './ConfirmDialog.css'

export interface ConfirmDialogProps {
  open: boolean
  title: string
  description: string
  confirmLabel?: string
  cancelLabel?: string
  variant?: 'default' | 'danger'
  loading?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  variant = 'default',
  loading = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  if (!open) return null
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-dialog-title">
        <h2 id="confirm-dialog-title">{title}</h2>
        <p>{description}</p>
        <div className="confirm-dialog-actions">
          <button type="button" className="btn btn-secondary" disabled={loading} onClick={onCancel}>
            {cancelLabel}
          </button>
          <button type="button" className={`btn ${variant === 'danger' ? 'btn-danger' : ''}`} disabled={loading} onClick={onConfirm}>
            {loading ? 'Processing...' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Create dialog styles**

Create `web/src/components/ConfirmDialog.css`:

```css
.confirm-dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1rem;
  background: rgba(15, 23, 42, 0.35);
}

.confirm-dialog {
  width: min(420px, 100%);
  padding: 1.25rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg);
  box-shadow: 0 18px 50px rgba(15, 23, 42, 0.22);
}

.confirm-dialog h2 {
  margin: 0;
  color: var(--text-strong);
  font-size: 1rem;
}

.confirm-dialog p {
  margin: 0.75rem 0 0;
  color: var(--muted);
  line-height: 1.5;
}

.confirm-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.75rem;
  margin-top: 1.25rem;
}
```

- [ ] **Step 3: Replace `window.confirm` in list pages**

For each list page, add:

```ts
import { ConfirmDialog } from '../components/ConfirmDialog'
```

Add state:

```ts
const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
const [confirmLoading, setConfirmLoading] = useState(false)
```

Change delete button handlers to `setPendingDelete({ id, name })`.

Add a dialog near the end of the returned JSX:

```tsx
<ConfirmDialog
  open={!!pendingDelete}
  title="Delete item"
  description={pendingDelete ? `Delete "${pendingDelete.name}"? This action cannot be undone.` : ''}
  confirmLabel="Delete"
  variant="danger"
  loading={confirmLoading}
  onCancel={() => setPendingDelete(null)}
  onConfirm={async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await toolApi.delete(pendingDelete.id)
      setPendingDelete(null)
      loadTools()
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }}
/>
```

Use the page-specific API and reload function:

- `ToolList.tsx`: `toolApi.delete`, `loadTools`
- `AgentList.tsx`: `agentApi.delete`, `loadAgents`
- `ChannelList.tsx`: `channelApi.delete`, `loadChannels`
- `CronTaskList.tsx`: `cronApi.delete`, `loadTasks`

- [ ] **Step 4: Add run confirmation for cron**

In `CronTaskList.tsx`, add:

```ts
const [pendingRun, setPendingRun] = useState<{ id: string; name: string } | null>(null)
```

Change run buttons to `setPendingRun({ id: task.id, name: task.name })`.

Add a `ConfirmDialog` with `title="Run task now"`, `confirmLabel="Run"`, and `onConfirm` calling `cronApi.run(pendingRun.id)`.

In `CronTaskDetail.tsx`, add `pendingRun` state and wrap `handleRun` behind the same dialog.

- [ ] **Step 5: Verify build**

Run:

```powershell
& 'D:\Program Files\nodejs\npm.cmd' run build
```

Expected: PASS.

## Task 6: End-to-End Verification

**Files:**
- No new files.

- [ ] **Step 1: Run backend tests**

Run:

```powershell
go test ./internal/service
```

Expected: PASS.

- [ ] **Step 2: Run frontend tests and build**

Run:

```powershell
& 'D:\Program Files\nodejs\npm.cmd' test
& 'D:\Program Files\nodejs\npm.cmd' run build
```

Expected: PASS.

- [ ] **Step 3: Inspect Git status**

Run in each repo:

```powershell
git status --short
```

Expected:

- `portal`: backend confirmation files plus this plan document.
- `web`: frontend stream helper, confirmation UI, dialog, selected page updates.
- `framework`: unchanged by this task.

## Self-Review

- Spec coverage: chat confirmation, SSE contract, frontend card behavior, shared page dialog, error handling, and verification are covered.
- Placeholder scan: no `TBD`, `TODO`, or unspecified implementation steps remain.
- Type consistency: backend uses `ChatConfirmationRequest`; frontend uses `ChatConfirmationRequest`; SSE event name is consistently `confirm_required`.
