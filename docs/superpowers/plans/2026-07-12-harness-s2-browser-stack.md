# S2 余量：Browser B2 全栈 + 下载策略

> Skip git commits unless asked.

**Goal:** 补齐 Hermes H-P2-B2 余量 6 工具（合计 10）+ 下载策略横切；**不做** H-P2-B3（`browser_cdp` / `browser_dialog`）。

**Architecture:** 扩展 `browser.Backend`；工具注册与四件套同 CheckFn/SSRF/session；下载默认 deny，可配 workspace 落盘。

---

## 工具（+6 → 10）

| 工具 | 要点 |
|------|------|
| `browser_scroll` | direction up/down |
| `browser_back` | history back |
| `browser_press` | key Enter/Tab/… |
| `browser_get_images` | img url+alt |
| `browser_console` | logs + optional JS expression |
| `browser_vision` | 截图落盘；本阶段不做 LLM 分析（返回 path + hint） |

## 下载策略

| Mode | 行为 |
|------|------|
| `deny`（默认） | CDP `setDownloadBehavior` deny |
| `workspace` | 允许下载到 `{workspace}/downloads/`（路径守卫 + 大小上限） |

Env：`SATH_BROWSER_DOWNLOAD=deny|workspace`（Portal 接线可选）。

## 非目标

`browser_cdp`、`browser_dialog`、vision LLM、下载 confirm 卡片。
