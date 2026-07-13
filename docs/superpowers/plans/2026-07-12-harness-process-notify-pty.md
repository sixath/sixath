# Process notify wake + pty status

> Skip git commits unless asked.

**Goal:** `notify_on_complete=true` 结束后唤醒 Agent（合成用户消息触发一轮 `SendMessage`）；`pty=true` 明确返回 not_supported（交互走 process write/submit）。

**非目标：** creack/pty 真伪终端；watch_patterns。
