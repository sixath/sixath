# 联网搜索引用溯源（Phase 1）— Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agent 联网搜索时在 Chat 中实时展示「已浏览 N 个网页」来源面板，并强制模型在正文中用 Markdown 内联链接标注出处。

**Architecture:** `framework/tool/web` 解析工具 output 为来源列表；`SendMessageStream` 为每轮 run 创建独立 EventBus，订阅 `ToolExecuted` 推送 SSE `sources_browsed`；web 工具启用时注入 Citation Prompt；前端 `SourcesPanel` 渲染在 assistant 正文上方。

**Tech Stack:** Go 1.25、Kratos SSE、React 19 + Vite 8、react-markdown

**Spec 关联:** [2026-05-26-web-search-citation-design.md](../specs/2026-05-26-web-search-citation-design.md)  
**范围:** Phase 1 only（Phase 2 metadata 持久化不在本计划）

---

## File Structure

| 文件 | 职责 |
|------|------|
| `framework/tool/web/sources.go` | **新建** — 从 web 工具 output 提取 `SourceItem` |
| `framework/tool/web/sources_test.go` | **新建** — 解析单测 |
| `portal/internal/chat/web_citation.go` | **新建** — Citation prompt + `WebCitationEnabled` gate |
| `portal/internal/chat/web_citation_test.go` | **新建** — prompt gate 单测 |
| `portal/internal/chat/web_sources_stream.go` | **新建** — EventBus 订阅 → `ChatStreamEvent` |
| `portal/internal/chat/web_sources_stream_test.go` | **新建** — 订阅单测 |
| `portal/internal/service/chat_stream.go` | 新增 `sources_browsed` 事件类型与 payload |
| `portal/internal/service/chat.go` | runBus  wiring + citation prompt 注入 |
| `portal/internal/server/chat_sse.go` | SSE `sources_browsed` handler |
| `web/src/components/SourcesPanel.tsx` | **新建** — 折叠来源列表 |
| `web/src/components/SourcesPanel.css` | **新建** — 样式 |
| `web/src/api/client.ts` | 解析 SSE + callback |
| `web/src/pages/ChatPage.tsx` | 状态 + 渲染 SourcesPanel |
| `web/src/components/MarkdownContent.css` | 外链 `target="_blank"` |

**不改:** `framework/tool/web_tools.go`（output 结构已满足）、Phase 2 DB migration

---

### Task 1: 来源解析 `ExtractSourcesFromToolOutput`

**Files:**
- Create: `framework/tool/web/sources.go`
- Create: `framework/tool/web/sources_test.go`

- [ ] **Step 1: 写失败单测**

`framework/tool/web/sources_test.go`:

```go
package web

import "testing"

func TestExtractSourcesFromToolOutput_webSearch(t *testing.T) {
	out := &SearchResponse{
		Query: "贵州 生猪",
		Results: []SearchResult{
			{Title: "贵州畜牧业统计", URL: "https://gov.example/stats", SiteName: "现代畜牧网"},
			{Title: "", URL: "https://news.example/pig", SiteName: ""},
		},
	}
	items := ExtractSourcesFromToolOutput("web_search", out)
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Title != "贵州畜牧业统计" || items[0].URL != "https://gov.example/stats" {
		t.Fatalf("first: %#v", items[0])
	}
	if items[1].Title != "news.example" {
		t.Fatalf("fallback title: %q", items[1].Title)
	}
}

func TestExtractSourcesFromToolOutput_webExtract(t *testing.T) {
	out := map[string]any{
		"results": []any{
			map[string]any{"url": "https://a.test/page", "format": "markdown", "content": "x"},
			map[string]any{"url": "https://b.test/fail", "error": "timeout"},
		},
	}
	items := ExtractSourcesFromToolOutput("web_extract", out)
	if len(items) != 1 || items[0].URL != "https://a.test/page" {
		t.Fatalf("got %#v", items)
	}
}

func TestExtractSourcesFromToolOutput_dedupe(t *testing.T) {
	out := &SearchResponse{
		Results: []SearchResult{
			{Title: "A", URL: "https://dup.test"},
			{Title: "B", URL: "https://dup.test"},
		},
	}
	items := ExtractSourcesFromToolOutput("web_search", out)
	if len(items) != 1 {
		t.Fatalf("expected dedupe, got %#v", items)
	}
}
```

- [ ] **Step 2: 运行确认 FAIL**

```bash
cd framework && go test ./tool/web/... -run TestExtractSourcesFromToolOutput -count=1 -v
```

Expected: `undefined: ExtractSourcesFromToolOutput`

- [ ] **Step 3: 实现**

`framework/tool/web/sources.go`:

```go
package web

import (
	"net/url"
	"strings"
)

// SourceItem is a normalized citation source for UI and metadata.
type SourceItem struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
}

// ExtractSourcesFromToolOutput extracts citation sources from web_search / web_extract tool output.
func ExtractSourcesFromToolOutput(toolName string, output any) []SourceItem {
	switch toolName {
	case "web_search":
		return sourcesFromWebSearch(output)
	case "web_extract":
		return sourcesFromWebExtract(output)
	default:
		return nil
	}
}

func sourcesFromWebSearch(output any) []SourceItem {
	var results []SearchResult
	switch v := output.(type) {
	case *SearchResponse:
		if v == nil {
			return nil
		}
		results = v.Results
	case SearchResponse:
		results = v.Results
	case map[string]any:
		raw, _ := v["results"].([]any)
		for _, item := range raw {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			results = append(results, SearchResult{
				Title:    stringField(m, "title"),
				URL:      stringField(m, "url"),
				SiteName: stringField(m, "site_name"),
			})
		}
	default:
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]SourceItem, 0, len(results))
	for _, r := range results {
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = hostLabel(u)
		}
		out = append(out, SourceItem{Title: title, URL: u, SiteName: strings.TrimSpace(r.SiteName)})
	}
	return out
}

func sourcesFromWebExtract(output any) []SourceItem {
	m, ok := output.(map[string]any)
	if !ok {
		return nil
	}
	raw, _ := m["results"].([]any)
	seen := make(map[string]struct{})
	out := make([]SourceItem, 0, len(raw))
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row == nil {
			continue
		}
		if errMsg, _ := row["error"].(string); strings.TrimSpace(errMsg) != "" {
			continue
		}
		u := strings.TrimSpace(stringField(row, "url"))
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, SourceItem{Title: hostLabel(u), URL: u})
	}
	return out
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func hostLabel(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
```

- [ ] **Step 4: 运行确认 PASS**

```bash
cd framework && go test ./tool/web/... -run TestExtractSourcesFromToolOutput -count=1 -v
```

Expected: PASS

---

### Task 2: Citation Prompt 与启用 gate

**Files:**
- Create: `portal/internal/chat/web_citation.go`
- Create: `portal/internal/chat/web_citation_test.go`

- [ ] **Step 1: 写失败单测**

```go
package chat

import (
	"testing"

	"github.com/sixath/framework/tool"
)

func TestAppendWebCitationPrompt_whenEnabled(t *testing.T) {
	out := AppendWebCitationPrompt("base", true)
	if out == "base" || !stringsContains(out, "[页面标题](完整URL)") {
		t.Fatalf("expected citation snippet, got %q", out)
	}
}

func TestAppendWebCitationPrompt_whenDisabled(t *testing.T) {
	if AppendWebCitationPrompt("base", false) != "base" {
		t.Fatal("expected unchanged")
	}
}

func TestWebCitationEnabled_requiresToolRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	if WebCitationEnabled(true, reg) {
		t.Fatal("web tools flag alone should not enable without registered tool")
	}
}
```

（`stringsContains` 用 `strings.Contains` 即可，勿另建 helper 文件。）

- [ ] **Step 2: 运行确认 FAIL**

```bash
cd portal && go test ./internal/chat/... -run TestAppendWebCitationPrompt -count=1 -v
```

- [ ] **Step 3: 实现**

`portal/internal/chat/web_citation.go`:

```go
package chat

import (
	"github.com/sixath/framework/tool"
)

const webCitationPromptSnippet = `## 网络信息引用规则

当你使用 web_search 或 web_extract 获取信息并在回复中引用时：

1. **必须标注出处**：每个关键事实、数字、日期、政策表述，使用 Markdown 链接格式 [页面标题](完整URL) 标注来源。
2. **优先权威来源**：政府官网、行业协会、原始统计数据优先于转载/自媒体。
3. **多源冲突**：若来源矛盾，分别标注并说明差异，不要静默合并。
4. **数据缺失**：找不到可靠来源时，明确写「未找到官方公开数据」，不要编造。
5. **链接有效性**：只使用工具返回结果中的 URL，不要猜测或构造链接。`

// AppendWebCitationPrompt appends citation rules when web citation is active for this turn.
func AppendWebCitationPrompt(prompt string, enabled bool) string {
	if !enabled {
		return prompt
	}
	if prompt == "" {
		return webCitationPromptSnippet
	}
	return prompt + "\n\n---\n\n" + webCitationPromptSnippet
}

// WebCitationEnabled is true when agent web tools flag is on and web_search or web_extract is registered.
func WebCitationEnabled(webToolsFlag bool, reg *tool.Registry) bool {
	if !webToolsFlag || reg == nil {
		return false
	}
	_, hasSearch := reg.Get("web_search")
	_, hasExtract := reg.Get("web_extract")
	return hasSearch || hasExtract
}
```

- [ ] **Step 4: 运行确认 PASS**

```bash
cd portal && go test ./internal/chat/... -run "TestAppendWebCitationPrompt|TestWebCitationEnabled" -count=1 -v
```

---

### Task 3: SSE 事件类型

**Files:**
- Modify: `portal/internal/service/chat_stream.go`

- [ ] **Step 1: 扩展 ChatStreamEvent**

在 `ChatStreamEventType` 常量区追加：

```go
ChatStreamEventSourcesBrowsed ChatStreamEventType = "sources_browsed"
```

新增类型（同文件或 `chat_sources.go`，推荐同文件保持 SSE 类型集中）：

```go
type WebSourceItem struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
}

type ChatSourcesBrowsedPayload struct {
	Tool    string          `json:"tool"`
	Query   string          `json:"query,omitempty"`
	Sources []WebSourceItem `json:"sources"`
}
```

在 `ChatStreamEvent` struct 追加：

```go
SourcesBrowsed *ChatSourcesBrowsedPayload `json:"sources_browsed,omitempty"`
```

- [ ] **Step 2: 编译验证**

```bash
cd portal && go build ./...
```

Expected: success

---

### Task 4: EventBus 订阅推送 sources

**Files:**
- Create: `portal/internal/chat/web_sources_stream.go`
- Create: `portal/internal/chat/web_sources_stream_test.go`

- [ ] **Step 1: 写失败单测**

```go
package chat

import (
	"context"
	"testing"
	"time"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool/web"
	"backend/internal/service"
)

func TestSubscribeWebSourcesDuringRun_emitsOnToolExecuted(t *testing.T) {
	bus := events.NewBus()
	ch := make(chan service.ChatStreamEvent, 4)
	unsub := SubscribeWebSourcesDuringRun(context.Background(), bus, ch)
	defer unsub()

	bus.Publish(context.Background(), events.Event{
		Kind: events.ToolExecuted,
		Payload: map[string]any{
			"tool": "web_search",
			"input": map[string]any{"query": "test"},
			"output": &web.SearchResponse{
				Results: []web.SearchResult{{Title: "Hit", URL: "https://x.test"}},
			},
		},
	})

	select {
	case ev := <-ch:
		if ev.Type != service.ChatStreamEventSourcesBrowsed {
			t.Fatalf("type=%s", ev.Type)
		}
		if ev.SourcesBrowsed == nil || len(ev.SourcesBrowsed.Sources) != 1 {
			t.Fatalf("payload=%#v", ev.SourcesBrowsed)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for sources event")
	}
}
```

- [ ] **Step 2: 运行确认 FAIL**

```bash
cd portal && go test ./internal/chat/... -run TestSubscribeWebSourcesDuringRun -count=1 -v
```

- [ ] **Step 3: 实现**

`portal/internal/chat/web_sources_stream.go`:

```go
package chat

import (
	"context"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/tool/web"
	"backend/internal/service"
)

// SubscribeWebSourcesDuringRun listens for web tool ToolExecuted events and pushes sources_browsed to ch.
func SubscribeWebSourcesDuringRun(ctx context.Context, bus *events.Bus, ch chan<- service.ChatStreamEvent) func() {
	if bus == nil || ch == nil {
		return func() {}
	}
	return bus.Subscribe(true, func(c context.Context, e events.Event) {
		if e.Kind != events.ToolExecuted {
			return
		}
		toolName, _ := e.Payload["tool"].(string)
		if toolName != "web_search" && toolName != "web_extract" {
			return
		}
		output := e.Payload["output"]
		items := web.ExtractSourcesFromToolOutput(toolName, output)
		if len(items) == 0 {
			return
		}
		payload := &service.ChatSourcesBrowsedPayload{
			Tool:    toolName,
			Sources: make([]service.WebSourceItem, 0, len(items)),
		}
		if input, ok := e.Payload["input"].(map[string]any); ok {
			if q, _ := input["query"].(string); q != "" {
				payload.Query = q
			}
		}
		for _, it := range items {
			payload.Sources = append(payload.Sources, service.WebSourceItem{
				Title: it.Title, URL: it.URL, SiteName: it.SiteName,
			})
		}
		ev := service.ChatStreamEvent{Type: service.ChatStreamEventSourcesBrowsed, SourcesBrowsed: payload}
		select {
		case ch <- ev:
		case <-ctx.Done():
		default:
			// drop if consumer blocked; avoid deadlock
		}
	})
}
```

- [ ] **Step 4: 运行确认 PASS**

```bash
cd portal && go test ./internal/chat/... -run TestSubscribeWebSourcesDuringRun -count=1 -v
```

---

### Task 5: SendMessageStream 接入 runBus

**Files:**
- Modify: `portal/internal/service/chat.go`

- [ ] **Step 1: 注入 citation prompt**

将（约 L494）：

```go
effectivePrompt := chat.AppendAskUserToolPrompt(chat.BuildEffectiveSystemPromptForTurn(agentMeta.SystemPrompt, skillsIdx, userContent))
```

改为：

```go
flags := chat.RuntimeToolsForAgent(agentMeta)
basePrompt := chat.BuildEffectiveSystemPromptForTurn(agentMeta.SystemPrompt, skillsIdx, userContent)
basePrompt = chat.AppendWebCitationPrompt(basePrompt, chat.WebCitationEnabled(flags.WebToolsEnabled, reg))
effectivePrompt := chat.AppendAskUserToolPrompt(basePrompt)
```

- [ ] **Step 2: runBus + 订阅（在 `go func()` 内、`a.Run` 之前）**

在 `reg := tool.NewRegistry()` 之后、`BuildRegistry` 之后，追加：

```go
runBus := events.NewBus()
reg.SetEventBus(runBus)
```

将 `a := chat.BuildReActAgent(...)` 改为传入 runBus：

```go
a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory,
	append(s.growthReActOptions(), agent.WithReActEventBus(runBus))...)
```

在 `go func()` 内、`a.Run` 之前：

```go
runCtx, cancelRun := context.WithCancel(runCtx)
defer cancelRun()
unsubSources := chat.SubscribeWebSourcesDuringRun(runCtx, runBus, ch)
defer unsubSources()
```

**DebugRun 兼容：** 当 `agentMeta.DebugRun` 时，现有逻辑会 `events.SetDefaultBus(bus)` 并订阅 debug。改为：
- debug 仍订阅 `prevBus` 的全局替换逻辑不变
- **同时** debug bus 与 runBus 分离：debug 继续用独立 `bus` 订阅；工具事件走 `runBus`（已在 reg 上）
- 即：`BuildRegistry` 后立刻 `reg.SetEventBus(runBus)` 覆盖 `BuildRegistry` 内的 `DefaultBus` 设置

- [ ] **Step 3: 编译 + 现有测试**

```bash
cd portal && go test ./internal/service/... -count=1
cd portal && go test ./internal/chat/... -count=1
```

Expected: PASS（或仅与本次无关的 skip）

---

### Task 6: SSE handler

**Files:**
- Modify: `portal/internal/server/chat_sse.go`

- [ ] **Step 1: 处理 sources_browsed**

在 `switch event.Type` 中 `ChatStreamEventDebug` 之前追加：

```go
case service.ChatStreamEventSourcesBrowsed:
	if event.SourcesBrowsed != nil {
		writeSSEEvent(ctx, "sources_browsed", map[string]any{
			"sources_browsed": event.SourcesBrowsed,
		})
	}
```

- [ ] **Step 2: 编译**

```bash
cd portal && go build ./...
```

---

### Task 7: 前端 SSE 解析

**Files:**
- Modify: `web/src/api/client.ts`

- [ ] **Step 1: 类型与 parser**

在 `ChatMessage` 附近追加：

```typescript
export interface WebSourceItem {
  title: string
  url: string
  site_name?: string
}

export interface SourcesBrowsedPayload {
  tool: 'web_search' | 'web_extract'
  query?: string
  sources: WebSourceItem[]
}

function parseSourcesBrowsedPayload(d: Record<string, unknown>): SourcesBrowsedPayload | null {
  const raw = d.sources_browsed
  if (!isRecord(raw)) return null
  const tool = raw.tool as string
  if (tool !== 'web_search' && tool !== 'web_extract') return null
  const sourcesRaw = raw.sources
  if (!Array.isArray(sourcesRaw)) return null
  const sources: WebSourceItem[] = []
  for (const item of sourcesRaw) {
    if (!isRecord(item)) continue
    const title = typeof item.title === 'string' ? item.title : ''
    const url = typeof item.url === 'string' ? item.url : ''
    if (!url) continue
    sources.push({
      title: title || url,
      url,
      site_name: typeof item.site_name === 'string' ? item.site_name : undefined,
    })
  }
  if (sources.length === 0) return null
  return {
    tool,
    query: typeof raw.query === 'string' ? raw.query : undefined,
    sources,
  }
}
```

- [ ] **Step 2: sendMessageStream callback**

`sendMessageStream` 的 callbacks 类型增加：

```typescript
onSourcesBrowsed?: (payload: SourcesBrowsedPayload) => void
```

在 SSE 解析 `data:` 分支追加：

```typescript
else if (curEvent === 'sources_browsed') {
  const payload = parseSourcesBrowsedPayload(d)
  if (payload) callbacks.onSourcesBrowsed?.(payload)
}
```

- [ ] **Step 3: 类型检查**

```bash
cd web && npm run build
```

Expected: success

---

### Task 8: SourcesPanel 组件

**Files:**
- Create: `web/src/components/SourcesPanel.tsx`
- Create: `web/src/components/SourcesPanel.css`

- [ ] **Step 1: 实现组件**

`SourcesPanel.tsx`:

```tsx
import { useMemo, useState } from 'react'
import type { WebSourceItem } from '../api/client'
import './SourcesPanel.css'

interface SourcesPanelProps {
  sources: WebSourceItem[]
}

export function SourcesPanel({ sources }: SourcesPanelProps) {
  const [open, setOpen] = useState(false)
  const unique = useMemo(() => {
    const seen = new Set<string>()
    const out: WebSourceItem[] = []
    for (const s of sources) {
      if (!s.url || seen.has(s.url)) continue
      seen.add(s.url)
      out.push(s)
    }
    return out
  }, [sources])

  if (unique.length === 0) return null

  return (
    <div className="sources-panel">
      <button type="button" className="sources-panel-toggle" onClick={() => setOpen((v) => !v)}>
        <span>已浏览 {unique.length} 个网页</span>
        <span aria-hidden>{open ? '▾' : '▸'}</span>
      </button>
      {open && (
        <ul className="sources-panel-list">
          {unique.map((s) => (
            <li key={s.url}>
              <a href={s.url} target="_blank" rel="noopener noreferrer" className="sources-panel-link">
                <span className="sources-panel-ext" aria-hidden>↗</span>
                {s.title}
              </a>
              {s.site_name ? <span className="sources-panel-site">{s.site_name}</span> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
```

`SourcesPanel.css`（简洁卡片，与 chat 风格一致）：

```css
.sources-panel {
  margin-bottom: 0.75rem;
  border: 1px solid var(--border, #e2e8f0);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-accent, #f8fafc);
  font-size: 0.85rem;
}
.sources-panel-toggle {
  display: flex;
  width: 100%;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem 0.75rem;
  border: none;
  background: transparent;
  cursor: pointer;
  color: var(--muted, #64748b);
  font: inherit;
}
.sources-panel-list {
  margin: 0;
  padding: 0 0.75rem 0.5rem;
  list-style: none;
}
.sources-panel-link {
  color: var(--primary, #2563eb);
  text-decoration: none;
}
.sources-panel-link:hover {
  text-decoration: underline;
}
.sources-panel-site {
  display: block;
  font-size: 0.75rem;
  color: var(--muted, #94a3b8);
}
```

- [ ] **Step 2: build**

```bash
cd web && npm run build
```

---

### Task 9: ChatPage 集成

**Files:**
- Modify: `web/src/pages/ChatPage.tsx`

- [ ] **Step 1: 扩展本地消息 state**

在文件顶部（`ChatInputItem` 附近）追加：

```typescript
interface AssistantMessageExtras {
  sources?: WebSourceItem[]
}
```

使用 `Map<string, WebSourceItem[]>` 或给 messages 并行 state：

```typescript
const [messageSources, setMessageSources] = useState<Record<string, WebSourceItem[]>>({})
```

assistant placeholder 创建时使用 `assistantKey` 作为 key。

- [ ] **Step 2: onSourcesBrowsed 合并去重**

在 `chatApi.sendMessageStream` callbacks 中：

```typescript
onSourcesBrowsed: (payload) => {
  setMessageSources((prev) => {
    const existing = prev[assistantKey] ?? []
    const seen = new Set(existing.map((s) => s.url))
    const merged = [...existing]
    for (const s of payload.sources) {
      if (!seen.has(s.url)) {
        seen.add(s.url)
        merged.push(s)
      }
    }
    return { ...prev, [assistantKey]: merged }
  })
},
```

- [ ] **Step 3: 渲染**

在 assistant 分支、`MarkdownContent` **之前**：

```tsx
import { SourcesPanel } from '../components/SourcesPanel'
import type { WebSourceItem } from '../api/client'

// inside map:
const sources = messageSources[messageKey] ?? []
// ...
<SourcesPanel sources={sources} />
<MarkdownContent ...>
```

新用户消息开始时 `setMessageSources` 不必清全局；仅新 assistantKey 无来源即可。

- [ ] **Step 4: build**

```bash
cd web && npm run build
```

---

### Task 10: Markdown 外链样式

**Files:**
- Modify: `web/src/components/MarkdownContent.css`

- [ ] **Step 1: 追加规则**

```css
.markdown-content a {
  color: var(--primary, #2563eb);
}
.markdown-content a[target="_blank"] {
  /* react-markdown 默认不加 target；SourcesPanel 已加。正文链接靠 remark 或后处理 — 见 Step 2 */
}
```

- [ ] **Step 2: ReactMarkdown 外链（可选增强）**

在 `MarkdownContent.tsx` 的 `ReactMarkdown` 增加 `components`：

```tsx
a: ({ href, children }) => (
  <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>
),
```

两处 `ReactMarkdown`（正常 + huge retry）都加。

- [ ] **Step 3: build**

```bash
cd web && npm run build
```

---

### Task 11: 端到端验证

- [ ] **Step 1: Go 全量相关测试**

```bash
cd framework && go test ./tool/web/... -count=1
cd portal && go test ./internal/chat/... ./internal/service/... -count=1
```

Expected: PASS

- [ ] **Step 2: 手测清单**

1. Agent 开启 `web_tools_enabled`，配置 `BOCHA_API_KEY`
2. 提问：「贵州省2026年1月生猪价格」
3. 流式过程中出现「已浏览 N 个网页」，展开后链接可点
4. 正文含 `[...](http...)` 链接
5. 关闭 web tools 的 Agent 无面板

- [ ] **Step 3: （可选）Playwright mock SSE**

在 `web/e2e/helpers/mock-api.ts` 的 stream mock 中插入 `sources_browsed` event，断言 `SourcesPanel` 可见。

---

## Spec 覆盖自检

| Spec 要求 | Task |
|-----------|------|
| ExtractSourcesFromToolOutput | Task 1 |
| Citation Prompt + gate | Task 2 |
| SSE sources_browsed | Task 3, 6 |
| EventBus 订阅 | Task 4, 5 |
| runBus 隔离 Debug | Task 5 |
| SourcesPanel UI | Task 8, 9 |
| 正文 Markdown 链接 | Task 2, 10 |
| Phase 2 metadata | **不在本计划** |

---

## 执行选项

Plan 已保存至 `framework/docs/superpowers/plans/2026-05-26-web-search-citation.md`。

**1. Subagent-Driven（推荐）** — 每个 Task 派生子 agent，任务间 review，迭代快  

**2. Inline Execution** — 本会话按 Task 顺序直接实现，批次间 checkpoint  

你选哪种方式开始？
