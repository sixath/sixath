# 联网搜索引用溯源 — 设计规格

**版本**: 0.1  
**状态**: 待评审  
**日期**: 2026-05-26  
**方案**: DeepSeek 双层式（C）— Phase 1 实时来源面板 + 正文内联链接；Phase 2 历史持久化  
**关联**: [hermes-capability-gap-requirements](./2026-05-25-hermes-capability-gap-requirements.md)、`framework/tool/web/`、`portal/internal/server/chat_sse.go`

---

## 1. 背景与目标

### 1.1 问题

Agent 使用 `web_search` / `web_extract` 生成报告时，正文常出现「根据 **贵州省农业农村厅** 官方监测数据」等表述，但**无可点击出处**，用户无法溯源证伪。

工具层已返回完整 URL（`SearchResult.URL`、`web_extract.url`），前端 `MarkdownContent` 也支持 GFM 链接渲染；缺口在于：

1. **无过程透明**：搜索/浏览了哪些页面不可见（DeepSeek「Browsed N pages」）
2. **无引用约束**：System prompt 未强制模型标注来源
3. **无结构化 UI**：来源面板不依赖模型输出，无法保证稳定展示

### 1.2 已确认决策

| 决策项 | 选择 |
|--------|------|
| 引用 UX | **C** — DeepSeek 双层式 |
| 第一层 | 搜索过程中折叠展示「已浏览 N 个网页」，标题可点外链 |
| 第二层 | 正文关键数据用 Markdown 内联链接 `[标题](url)` |
| 实现策略 | **Phase 1** 实时 SSE + Prompt；**Phase 2** 消息 metadata 持久化 |
| 角标脚注 | **不做**（内联链接足够，实现更简单） |

### 1.3 非目标（本期不做）

- Perplexity 式 `[1]` 角标 + 底部参考文献编号体系
- 自动检测正文链接是否与工具结果 URL 一致（LLM 审计）
- PDF 全文提取后的页码级引用
- 非 web 工具来源（数据库查询、文件读取）的统一引用框架

---

## 2. 架构与数据流

### 2.1 组件边界

```
Web ChatPage                    Portal                           Framework
─────────────────────────────────────────────────────────────────────────────
SourcesPanel ◄── SSE sources_browsed ◄── ChatService.SendMessageStream
     │                                      │
     │                              subscribe ToolExecuted (EventBus)
     │                                      │
MarkdownContent ◄── SSE chunk ◄──────── a.Run(ReActAgent)
     ▲                                      │
     │                              web_search / web_extract
Citation Prompt ◄── BuildEffectiveSystemPromptForTurn (+ web tools gate)
```

### 2.2 运行时序（Phase 1）

```
1. 用户发送消息
2. ChatService 构建 Agent；若 agent.web_tools_enabled 且后端 API key 已配置：
   a. 注入 WebCitationPrompt 到 effective system prompt
   b. 在 a.Run 之前订阅 EventBus ToolExecuted
3. Agent 调用 web_search
4. ToolExecuted 发布 → 订阅者解析 output → ch <- sources_browsed
5. SSE 推送 sources_browsed → 前端 SourcesPanel 实时更新
6. Agent 继续推理，最终输出带 [标题](url) 的正文
7. a.Run 结束 → streamEventsFromResponse 推送 chunk
8. SSE done → SaveAssistantMessage(content only)
```

**关键约束**：当前 `SendMessageStream` 在 `a.Run()` 完成前**不推送 chunk**（`RunStream` 仍注释）。`sources_browsed` 必须在 `a.Run` 并行订阅 EventBus 实现，使用与 `DebugRun` 相同模式，但**不依赖 debug 开关**。

### 2.3 启用条件

引用能力在以下条件**同时满足**时启用：

| 条件 | 来源 |
|------|------|
| Agent `web_tools_enabled == true` | `RuntimeToolsForAgent` |
| 进程级 web 后端可用 | `web_search` CheckFn 通过（BOCHA/TAVILY key） |
| 本轮 Agent 实际注册了 `web_search` 或 `web_extract` | Registry |

不满足时：不注入 Citation Prompt、不订阅来源事件、前端不渲染 SourcesPanel。

---

## 3. Phase 1 — 后端

### 3.1 新增 SSE 事件

**文件**: `portal/internal/service/chat_stream.go`

```go
const ChatStreamEventSourcesBrowsed ChatStreamEventType = "sources_browsed"

type WebSourceItem struct {
    Title    string `json:"title"`
    URL      string `json:"url"`
    SiteName string `json:"site_name,omitempty"`
}

type ChatSourcesBrowsedEvent struct {
    Tool  string          `json:"tool"`            // web_search | web_extract
    Query string          `json:"query,omitempty"` // web_search 时有值
    Sources []WebSourceItem `json:"sources"`
}
```

**ChatStreamEvent** 扩展字段：

```go
Sources *ChatSourcesBrowsedEvent `json:"sources,omitempty"`
```

### 3.2 来源解析（framework 层）

**新文件**: `framework/tool/web/sources.go`

```go
// ExtractSourcesFromToolOutput 从 web_search / web_extract 工具 output 提取来源列表。
func ExtractSourcesFromToolOutput(toolName string, output any) []WebSourceItem
```

解析规则：

| 工具 | 输入结构 | 提取 |
|------|----------|------|
| `web_search` | `*web.SearchResponse` 或 `map` | 每条 `results[]` → title, url, site_name |
| `web_extract` | `map` with `results[]` | 每条成功项 → title  fallback 为 url  host，url |

- 跳过 `error` 字段非空的 extract 项
- URL 去重（同 URL 保留首次）
- title 为空时用 URL hostname

单元测试覆盖 bocha 响应样例与 web_extract 多 URL 场景。

### 3.3 EventBus 订阅

**新文件**: `portal/internal/chat/web_sources_stream.go`

```go
// SubscribeWebSourcesDuringRun 在 ctx 存活期间订阅 ToolExecuted，匹配 web 工具后写入 ch。
// 返回 unsubscribe 函数；调用方在 a.Run 返回后 defer unsubscribe()。
func SubscribeWebSourcesDuringRun(ctx context.Context, ch chan<- service.ChatStreamEvent) func()
```

实现要点：

- 使用**独立** EventBus 实例或进程默认 Bus（与 DebugRun 隔离：DebugRun 替换全局 Bus，来源订阅应挂在**本次 run 专用 bus** 或 reg 上的 bus）
- 推荐：`SendMessageStream` 为每次 run 创建 `runBus := events.NewBus()`，`reg.SetEventBus(runBus)`，`BuildReActAgent(..., WithReActEventBus(runBus))`，避免 DebugRun 全局替换冲突
- 仅处理 `events.ToolExecuted` 且 `payload.tool ∈ {web_search, web_extract}`
- 非阻塞写入 `ch`（select + default 或 buffered channel，防死锁）

### 3.4 Citation System Prompt

**新文件**: `framework/templates/web_citation_prompt.go`（或 `portal/internal/chat/web_citation.go`）

当 web 工具启用时追加到 effective system prompt：

```markdown
## 网络信息引用规则

当你使用 web_search 或 web_extract 获取信息并在回复中引用时：

1. **必须标注出处**：每个关键事实、数字、日期、政策表述，使用 Markdown 链接格式 `[页面标题](完整URL)` 标注来源。
2. **优先权威来源**：政府官网、行业协会、原始统计数据优先于转载/自媒体。
3. **多源冲突**：若来源矛盾，分别标注并说明差异，不要 silently 合并。
4. **数据缺失**：找不到可靠来源时，明确写「未找到官方公开数据」，不要编造。
5. **链接有效性**：只使用工具返回结果中的 URL，不要猜测或构造链接。
```

注入点：`BuildEffectiveSystemPromptForTurn` 之后、`AppendAskUserToolPrompt` 之前，由 `chat.AppendWebCitationPrompt(prompt, webToolsActive bool)` 完成。

### 3.5 SSE Handler

**文件**: `portal/internal/server/chat_sse.go`

```go
case service.ChatStreamEventSourcesBrowsed:
    if event.Sources != nil {
        writeSSEEvent(ctx, "sources_browsed", map[string]any{"sources_browsed": event.Sources})
    }
```

### 3.6 SendMessageStream 改造要点

**文件**: `portal/internal/service/chat.go`

在 `go func()` 内、`a.Run` 之前：

1. 判断 `webCitationEnabled(agentMeta, reg)`
2. 若启用：创建 runBus、SetEventBus、SubscribeWebSourcesDuringRun
3. `defer` 恢复 bus / unsubscribe

**不改变**现有 chunk 推送时机（仍为 run 结束后一次性 chunk）；Phase 1 的「实时感」来自 `sources_browsed` 在 run 中途推送。

---

## 4. Phase 1 — 前端

### 4.1 SSE 客户端

**文件**: `web/src/api/client.ts`

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
```

`sendMessageStream` callbacks 新增：

```typescript
onSourcesBrowsed?: (payload: SourcesBrowsedPayload) => void
```

解析 `event: sources_browsed`。

### 4.2 SourcesPanel 组件

**新文件**: `web/src/components/SourcesPanel.tsx`

行为：

- 默认**折叠**；标题「已浏览 N 个网页」+ 展开箭头
- 展开后列表：每条 `[↗ title](url)` 新标签打开，`site_name` 作副标题（可选）
- 同一 assistant 消息内多次 `sources_browsed` **合并去重**（按 url）
- 流式结束后保持展示；新用户消息时清空

样式：轻量卡片，位于 assistant 消息正文**上方**（与 DeepSeek 一致）。

### 4.3 ChatPage 集成

**文件**: `web/src/pages/ChatPage.tsx`

- assistant placeholder 消息扩展本地 state：`sources?: WebSourceItem[]`
- `onSourcesBrowsed` 更新当前 assistant 消息的 sources
- 渲染顺序：`SourcesPanel` → `MarkdownContent` → confirm/input cards

`ChatMessage` 类型 Phase 1 **仅前端内存**持有 sources，不持久化。

### 4.4 Markdown 链接

无需改动 `MarkdownContent`；确保 CSS 中 `a` 标签有 `target="_blank"` + `rel="noopener noreferrer"`（可在 `.markdown-content a` 规则添加）。

---

## 5. Phase 2 — 历史持久化（后续）

### 5.1 数据库

**迁移**: `chat_messages` 增加 `metadata JSON NULL`

```json
{
  "sources": [
    { "title": "...", "url": "...", "site_name": "..." }
  ]
}
```

### 5.2 保存时机

`SaveAssistantMessage` 扩展签名或从 run 上下文收集本轮全部 sources（RunTrace.ToolCalls 兜底），写入 metadata。

`streamEventsFromResponse` 结束时可额外发送 `sources_summary`（可选，与 browsed 合并为最终列表）。

### 5.3 API

`MessageReply` / `ListMessages` 返回 `metadata`；前端 `normalizeChatMessage` 解析 sources，历史消息渲染同一 `SourcesPanel`。

---

## 6. 错误处理

| 场景 | 行为 |
|------|------|
| web_search 返回 error | 不推送 sources_browsed；模型按 prompt 说明未找到 |
| web_extract 部分 URL 失败 | 仅推送成功项 |
| EventBus 订阅写入 ch 阻塞 | 非阻塞 drop + 日志 warn |
| Agent 未启用 web tools | 全流程跳过，零开销 |
| 模型未加链接 | SourcesPanel 仍展示浏览记录；正文质量靠 prompt，Phase 2 可考虑 eval |

---

## 7. 测试计划

### 7.1 单元测试

- `ExtractSourcesFromToolOutput`：bocha JSON、tavily、tavily map、extract 混合成败
- `AppendWebCitationPrompt`：gate 开/关
- `SubscribeWebSourcesDuringRun`：mock bus 发布 ToolExecuted

### 7.2 集成测试

- `chat_stream_test.go`：`streamEventsFromResponse` 不变；新增 sources 订阅 E2E（mock web_search tool）
- `hermes_p0_e2e_test.go`：web_search 后响应 metadata 含 URL（已有）

### 7.3 前端

- Playwright：mock SSE `sources_browsed` → 面板可见、链接 href 正确、折叠交互
- 手测：贵州生猪行情类 query，确认面板 + 正文链接

---

## 8. 文件清单（Phase 1）

| 操作 | 路径 |
|------|------|
| 新增 | `framework/tool/web/sources.go` |
| 新增 | `framework/tool/web/sources_test.go` |
| 新增 | `portal/internal/chat/web_citation.go` |
| 新增 | `portal/internal/chat/web_sources_stream.go` |
| 新增 | `web/src/components/SourcesPanel.tsx` |
| 新增 | `web/src/components/SourcesPanel.css` |
| 修改 | `portal/internal/service/chat_stream.go` |
| 修改 | `portal/internal/service/chat.go` |
| 修改 | `portal/internal/server/chat_sse.go` |
| 修改 | `web/src/api/client.ts` |
| 修改 | `web/src/pages/ChatPage.tsx` |
| 修改 | `web/src/components/MarkdownContent.css`（外链样式） |

Phase 2 额外：`portal/internal/data/migrations/`, `chat.proto`, `SaveAssistantMessage`, `client.ts` ChatMessage。

---

## 9. 验收标准

### Phase 1

- [ ] 启用 web tools 的 Agent 联网问答时，搜索完成后**流式过程中**出现「已浏览 N 个网页」面板
- [ ] 面板内每条来源标题可点击，打开正确 URL
- [ ] 正文中关键数据含 Markdown 链接（抽检 3 个场景）
- [ ] 未启用 web tools 的 Agent 无面板、无 citation prompt 注入
- [ ] Debug 模式与来源面板可同时工作，互不干扰

### Phase 2

- [ ] 刷新页面后历史 assistant 消息仍显示来源面板
- [ ] metadata 中 sources 与当时浏览一致

---

## 10. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 模型仍不写链接 | Citation prompt + SourcesPanel 保底；后续 eval / 更强模型 |
| DebugRun 全局 Bus 替换冲突 | 每 run 独立 runBus（§3.3） |
| chunk 非真流式，用户长时间只看面板 | Phase 1 可接受；长期恢复 RunStream |
| 来源过多 UI 噪音 | 折叠默认 + 去重；单轮上限 display 20 条 |
