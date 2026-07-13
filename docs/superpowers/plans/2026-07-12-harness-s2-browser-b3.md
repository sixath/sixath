# S2 B3：browser_cdp + browser_dialog

> Skip git commits unless asked.

**Goal:** 补齐 Hermes H-P2-B3，使 browser 工具达到 12 个。

**Architecture:**
- `Backend` 增 `CDP` / `HandleDialog` / 快照附带 `PendingDialogs`
- chromedp：Listen `JavascriptDialogOpening`；`HandleJavaScriptDialog`；`Target.Execute` 发任意 CDP method
- Fake：可测 pending dialog + CDP 回显
- 工具：`browser_cdp`、`browser_dialog`；snapshot 结果含 `pending_dialogs`

**非目标：** frame_id 跨域 supervisor、多 tab target_id 完整路由（MVP 仅当前 tab；`target_id`/`frame_id` 参数接受但未接时返回 hint）。
