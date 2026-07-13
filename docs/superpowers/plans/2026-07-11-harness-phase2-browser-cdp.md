# Phase 2：浏览器 CDP 最小集 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 harness Phase 2 S2：四个浏览器工具（`browser_navigate` / `browser_snapshot` / `browser_click` / `browser_type`）+ CheckFn + SSRF + BrowserSession 生命周期；无可用 CDP 时不出 schema。

**Architecture:** `BrowserBackend` 接口隔离真实 CDP（默认 [chromedp](https://github.com/chromedp/chromedp)）与测试 Fake；`BrowserSessionStore` 按 `session_id`（chat）持有一页；工具走现有 `Registry` + `CheckFn` + Toolset `browser`；危险导航靠 SSRF（复用 `ValidateOutboundURL`），不扩 Hermes 12 工具。ChatSession 结束时经 `ChatSessionHooks` 调用 `on_browser_session_end` 关页。

**Tech Stack:** Go；`github.com/chromedp/chromedp`（framework `go.mod`）；Portal opt-in flag；既有 `tool.CheckFn` / `ssrf.go` / `ChatSessionHookRegistry`。

**Spec:** `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md` §3.1 BrowserSession、§4.5 S2、§5 Phase 2、Q3  
**前置:** Phase 0（ToolHook、ChatSessionHooks）已落地。

> **Git：** 无仓库则跳过 Commit。  
> **非目标：** browser 全栈 10+、dialog、下载、S1 terminal 危险审批、G1。  
> **相对 Spec S2 的 Phase 2 收口（waiver）：** 四工具阶段 **不做** confirm 卡片、**不支持** 下载策略；导航安全靠 SSRF。gap Task 6 标「S2 最小集已落地」时注明全栈/confirm/下载仍 backlog。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/tool/browser/backend.go` | `Backend` 接口：Navigate/Snapshot/Click/Type/Close |
| Create `framework/tool/browser/session.go` | `SessionStore`：按 chat session_id 懒创建/复用/关闭 |
| Create `framework/tool/browser/fake_backend.go` | 测试用 Fake（记录调用、返回固定 snapshot） |
| Create `framework/tool/browser/chromedp_backend.go` | 真实实现：local Chrome 或 `CDP_URL` remote |
| Create `framework/tool/browser_tools.go` | 注册四工具 + CheckFn + Toolset |
| Create `framework/tool/browser_tools_test.go` | Fake 后端集成测 |
| Modify `framework/tool/toolset.go` | `ToolsetBrowser = "browser"`；名→toolset 映射 |
| Modify `framework/agent/chat_session_hook.go` 或 portal | Browser cleanup hook 注册 |
| Modify `portal/internal/chat/hermes_p0_flags.go`（或新建 browser flags） | `SATH_BROWSER_ENABLED` |
| Modify `portal/internal/chat/runtime_tools.go` / agent 注册 | opt-in `RegisterBrowserTools` |
| Modify gap spec | Phase 2 S2 状态 |

**依赖决策（写死）：** 使用 `chromedp`；测试默认 Fake，不要求本机装 Chrome。CI 可选 `-tags=chromedp_e2e` 跳过。

---

### Task 1: Backend 接口 + Fake + SessionStore

**Files:**
- Create: `framework/tool/browser/backend.go`
- Create: `framework/tool/browser/session.go`
- Create: `framework/tool/browser/fake_backend.go`
- Create: `framework/tool/browser/session_test.go`

```go
package browser

type Snapshot struct {
	URL     string
	Title   string
	Text    string            // accessibility / compact text
	Refs    map[string]string // refID -> brief description e.g. @e1
}

type Backend interface {
	Navigate(ctx context.Context, url string) (Snapshot, error)
	Snapshot(ctx context.Context, full bool) (Snapshot, error)
	Click(ctx context.Context, ref string) (Snapshot, error)
	Type(ctx context.Context, ref, text string) (Snapshot, error)
	Close(ctx context.Context) error
	Healthy(ctx context.Context) error // CheckFn
}

type SessionStore struct { /* sync.Map sessionID -> Backend */ }
func (s *SessionStore) GetOrCreate(sessionID string, factory func() (Backend, error)) (Backend, error)
func (s *SessionStore) Close(sessionID string) error
func (s *SessionStore) CloseAll() error
```

- [ ] **Step 1:** 测 SessionStore GetOrCreate 复用同一 backend；Close 后重建；Fake Navigate 记录 URL
- [ ] **Step 2–4:** TDD 实现
- [ ] **Step 5:** Commit `feat(tool/browser): add Backend interface, Fake, SessionStore`

---

### Task 2: 四工具注册 + CheckFn（Fake）

**Files:**
- Create: `framework/tool/browser_tools.go`
- Create: `framework/tool/browser_tools_test.go`
- Modify: `framework/tool/toolset.go`

**工具名（Hermes 对齐前缀）：**
| 工具 | 参数 |
|------|------|
| `browser_navigate` | `url`（必填） |
| `browser_snapshot` | `full`（bool，默认 false） |
| `browser_click` | `ref`（如 `@e1`） |
| `browser_type` | `ref`, `text` |

**行为：**
- 从 `ctx` 取 `session_id`（与现有 `tool.ContextKey` / Portal metadata 一致；若无则用 `"default"` 并文档说明）
- `CheckFn`：调用**进程级** `factory`/`Healthy` 探活（**禁止**在 ListForAPI 路径 `GetOrCreate` 建会话页）
- Env 优先级（写死）：`SATH_BROWSER_ENABLED`（Portal flag）为总开关；`BROWSER_CDP_URL` 仅后端连接；不再使用易混淆的独立 `BROWSER_ENABLED`（若读到则等同 SATH，并 Warn deprecate）
- `browser_navigate`：先 `ValidateOutboundURL(url)`（SSRF）；失败返回 permanent 错误 map
- 返回 JSON：`ok` + snapshot 字段；错误带 `error` / `error_code`

`RegisterBrowserTools(reg *Registry, store *SessionStore, factory func() (browser.Backend, error)) error`

- [ ] **Step 1:** 测：Healthy 失败 → ListForAPI 无 browser_*；成功 Navigate+Click 走 Fake；SSRF 拒绝内网 URL
- [ ] **Step 2–4:** 实现
- [ ] **Step 5:** Commit `feat(tool): register browser_navigate/snapshot/click/type with CheckFn`

---

### Task 3: chromedp Backend（真实 CDP）

**Files:**
- Create: `framework/tool/browser/chromedp_backend.go`
- Create: `framework/tool/browser/chromedp_backend_test.go`（可 `t.Skip` 无 Chrome）
- Modify: `framework/go.mod` — `go get github.com/chromedp/chromedp`

**配置（写死默认）：**
| Env / 字段 | 含义 |
|------------|------|
| `BROWSER_CDP_URL` | 非空则 attach remote CDP（ws://...） |
| 空 | 本地 headless Chrome（chromedp 默认 allocator） |

总开关只用 Portal `SATH_BROWSER_ENABLED`（见 Task 5）。

`Healthy`：短超时导航 `about:blank` 或 CDP version 探测。

Snapshot：优先 accessibility tree 文本 + 简易 ref 分配（`@e1`…）；Phase 2 不要求像素级完美，但 click/type 必须能用同一套 ref。

- [ ] **Step 1:** 单测用 build tag 或 SkipIfNoChrome
- [ ] **Step 2–4:** 实现最小 Navigate/Snapshot/Click/Type/Close
- [ ] **Step 5:** Commit `feat(tool/browser): chromedp Backend with local/remote CDP`

---

### Task 4: BrowserSession 结束钩子

**Files:**
- Modify: `portal/internal/service/chat.go` 或 `browser_session_hooks.go`
- Modify: framework 若需导出全局 store

**写死：**
1. Portal 进程级 `*browser.SessionStore`（或 ChatService 字段）
2. `ChatSessionHooks.Register`：`OnChatSessionEnd` → `store.Close(sessionID)`（错误只 Warn）
3. 与 Growth hook 共存（registry 多 hook）

- [ ] **Step 1:** 测 DeleteSession 后 Fake backend Close 被调用
- [ ] **Step 2–4:** 接线
- [ ] **Step 5:** Commit `feat(portal): close browser sessions on ChatSession end`

---

### Task 5: Portal opt-in 注册

**Files:**
- Modify: `portal/internal/chat/hermes_p0_flags.go`（或 `browser_flags.go`）— `BrowserEnabled`
- Modify: `RegisterAgentRuntimeTools` / chat 构建 registry 处
- Env: `SATH_BROWSER_ENABLED`（与 Hermes P0 flags 同风格）

默认 **false**；启用后 `RegisterBrowserTools` + chromedp factory。

- [ ] **Step 1:** 测 flag off → 无 browser 工具；on + Healthy ok → 有四工具名
- [ ] **Step 2–4:** 实现
- [ ] **Step 5:** Commit `feat(portal): opt-in browser tools via SATH_BROWSER_ENABLED`

---

### Task 6: 文档 + 回归

- [x] 更新 gap spec：S2 / Phase 2 已落地（四工具）；confirm/下载/全栈仍 backlog（waiver 已写明）
- [x] `toolsets-hermes-mapping.md` 增加 browser 行
- [x] 运行：
```bash
cd framework && go test ./tool/ ./tool/browser/ ./agent/ -count=1
cd portal && go test ./internal/service/ ./internal/chat/ -count=1
```
- [ ] Commit docs（本轮 NO git）

**Toolchain note:** framework/portal 要求 **Go 1.26**；真实 CDP 依赖 **chromedp**（`go.mod` 已引入）。CI/本机若 Go 版本偏低会直接无法 `go test`；无 Chrome 时 Fake 单测仍应绿，chromedp e2e 可 Skip。

---

## 完成定义

| 项 | 验收 |
|----|------|
| 四工具 | Fake 路径单测绿；schema 名正确 |
| CheckFn | Healthy 失败不出 API schema |
| SSRF | navigate 内网 URL 拒绝 |
| Session | 同 session_id 复用；DeleteSession Close |
| Opt-in | 默认不注册；flag 开启才出现 |
| 范围 | 无第 5 个 browser_* 工具 |

## 风险

| 风险 | 缓解 |
|------|------|
| chromedp 依赖/CI 无 Chrome | 默认 Fake 测；e2e Skip |
| ref 不稳定 | Phase 2 文档说明须 snapshot 后再 click |
| CDP 膨胀 | DoD 锁 4 工具 |
| session_id 缺失 | fallback `default` + 日志 |
