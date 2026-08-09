# Process notify wake

When `terminal(background=true, notify_on_complete=true)` exits:

1. Framework fires `ProcessNotifyHandler` and sets `notify_pending` (also visible on `process` poll).
2. Portal publishes `agent.process.notify` on the event bus.
3. Unless `SATH_PROCESS_NOTIFY_WAKE=0`, Portal runs a synthetic `SendMessage` on that chat session so the Agent continues.

Session busy (another turn in flight): wake is skipped (logged); poll still shows notify until consumed.

## pty

`terminal(pty=true)` allocates a real PTY via [aymanbagabas/go-pty](https://github.com/aymanbagabas/go-pty) (Unix pty / Windows ConPTY).

- Foreground: runs attached to the PTY and returns combined stdout.
- Background: `process` poll/log shows `pty: true`; use `write` / `submit` / `close` for interactive stdin (close sends EOT on PTY).
