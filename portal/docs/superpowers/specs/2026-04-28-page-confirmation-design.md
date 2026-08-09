# Page Confirmation Design

## Scope

Support confirmation mechanisms in two places:

- Chat confirmations for agent-triggered risky actions, starting with pending write operations that already return a confirmation token.
- Reusable page confirmations for destructive or high-impact UI actions such as delete and manual run.

The first implementation should prioritize the chat confirmation path. Page-level confirmations should be added through a reusable frontend component so existing pages can adopt it incrementally.

## Current Context

The chat UI lives in `web/src/pages/ChatPage.tsx` and consumes the streaming endpoint `POST /api/v1/sessions/{session_id}/messages/stream`.

The streaming adapter is `portal/internal/server/chat_sse.go`. It currently emits only:

- `chunk` with text content
- `done`
- `error`

The framework already has an `execute_write` flow that proposes pending writes and returns a token-shaped response with:

- `status: "pending"`
- `token`
- `dsl`
- `expires_in`

That pending response is currently flattened into assistant text by the agent run. The page cannot recognize it as a structured confirmation request.

## Recommended Approach

Add a small structured confirmation protocol on top of the existing SSE stream.

When the backend detects a pending confirmation in agent response metadata or emitted tool/debug events, it should emit a `confirm_required` SSE event. The UI should render that event as a confirmation card in the conversation. Confirming sends a follow-up message that includes the token, allowing the agent/tool flow to complete the action. Cancelling records a user-visible cancellation in the chat without executing the action.

For ordinary pages, introduce a shared `ConfirmDialog` component in `web/src/components`. Page actions can call it before invoking existing delete or run APIs.

## Chat SSE Contract

Add a new SSE event:

```json
{
  "id": "optional stable request id",
  "kind": "execute_write",
  "title": "Confirm write operation",
  "description": "Review the operation before it is executed.",
  "token": "confirmation token",
  "dsl": "SQL or write DSL",
  "expires_in": 300,
  "severity": "danger"
}
```

Event name: `confirm_required`.

The frontend must ignore malformed confirmation events and continue rendering normal text.

## Frontend Chat Behavior

`ChatPage` should maintain confirmation card state alongside streamed messages.

When `confirm_required` is received:

- Attach the confirmation to the current assistant turn.
- Show the DSL in a compact preformatted block.
- Show expiry information when provided.
- Provide `Confirm` and `Cancel` actions.

When the user confirms:

- Disable the card while submitting.
- Send a follow-up chat message that clearly includes the token and intent to execute the pending operation.
- Mark the card as confirmed once the follow-up send starts successfully.

When the user cancels:

- Mark the card as cancelled.
- Do not send the token back for execution.

## Page Dialog Behavior

Create a reusable confirmation dialog component with:

- `open`
- `title`
- `description`
- `confirmLabel`
- `cancelLabel`
- `variant`: `default | danger`
- `loading`
- `onConfirm`
- `onCancel`

Initial integration targets:

- Delete actions on list/detail pages.
- Manual run actions for cron tasks.

The dialog should use existing CSS variables and keep the current application style.

## Error Handling

- If confirmation submission fails, keep the card actionable and show the error near the card.
- If the token has expired, show backend error as a normal assistant/error message.
- If the stream ends without a confirmation event, current chat behavior remains unchanged.
- If a page dialog action fails, close only after the action succeeds; otherwise keep the dialog open and show the page's existing error path when available.

## Testing

Frontend:

- Unit or component coverage for parsing `confirm_required`.
- Confirmation card renders token, DSL, expiry, and disabled states.
- Dialog confirm/cancel callbacks fire once.

Backend:

- SSE helper emits valid `confirm_required` event JSON.
- Existing `chunk`, `done`, and `error` behavior remains unchanged.

Manual verification:

- Start web and portal locally.
- Trigger a pending write proposal.
- Confirm from the chat card and verify the follow-up message sends.
- Cancel from the chat card and verify no execution follow-up is sent.
- Verify at least one destructive page action uses the shared dialog.
