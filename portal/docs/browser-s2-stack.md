## Enable

Browser tools are **opt-in** (default off):

1. **Per agent (UI):** Agent 编辑 → 运行时工具 → 勾选「浏览器」(`runtime_tools.browser_enabled`)
2. **Process-wide:** `SATH_BROWSER_ENABLED=true`（或旧名 `BROWSER_ENABLED`）

Either path OR-merges; restart Portal after changing env.

## Confirm

| Action | Default |
|--------|---------|
| `browser_navigate` | **no confirm** (set `SATH_BROWSER_CONFIRM_NAVIGATE=true` to require token) |
| `browser_click` / `browser_type` | confirm_token when PendingStore wired |

## Image collection tips

Prefer `browser_navigate` → `browser_get_images` (filters SVG/UI chrome). Avoid `browser_cdp` Runtime.evaluate for scraping imgs.


`navigate` / `snapshot` / `click` / `type` / `scroll` / `back` / `press` / `get_images` / `console` / `vision` / **`cdp`** / **`dialog`**

- `browser_snapshot` may include `pending_dialogs` when a native JS dialog is open.
- `browser_dialog`: `action=accept|dismiss`, optional `prompt_text` / `dialog_id`.
- `browser_cdp`: raw CDP `method` + `params` on the **current tab** (MVP ignores `target_id` / `frame_id`).

## Download

| Env | Default | Behavior |
|-----|---------|----------|
| `SATH_BROWSER_DOWNLOAD` | `deny` | CDP denies downloads |
| `SATH_BROWSER_DOWNLOAD=workspace` | — | Save under `{workspace}/downloads/` |

## Vision LLM

| Env | Default | Behavior |
|-----|---------|----------|
| `SATH_VISION_ENABLED` | on | Set `0`/`false` to disable |
| `SATH_VISION_PROVIDER` / `SATH_VISION_MODEL` / `SATH_VISION_API_KEY` / `SATH_VISION_BASE_URL` | agent model | Optional dedicated vision client |

- `browser_vision(question=...)` or `analyze=true` → screenshot + LLM `analysis` when wired.
- `vision_analyze(path=..., question=...)` for workspace images.

## Still out of scope

- Multi-tab / cross-origin `frame_id` CDP routing
