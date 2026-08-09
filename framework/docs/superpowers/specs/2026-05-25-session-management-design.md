# 会话管理 — 设计规格

**版本**: 0.1  
**状态**: 待评审  
**日期**: 2026-05-25  
**方案**: 方案 1 — Portal 分层 API  
**关联**: [architecture_design.md](../../../../portal/docs/architecture_design.md) §5.4、[session-search-r1](./2026-05-19-session-search-r1.md)、[hermes-capability-gap-requirements](./2026-05-25-hermes-capability-gap-requirements.md)

---

## 1. 背景与目标

### 1.1 现状

| 层 | 状态 |
|----|------|
| Portal Chat API | 已有会话 CRUD、`ListMessages`、SSE 流式发送 |
| MySQL | `chat_sessions` / `chat_messages`；`parent_session_id` 已迁移（005） |
| Framework | `session_search` FTS sidecar 已落地（R1） |
| Web `chatApi` | 有会话 CRUD + 流式发送；**缺 `listMessages`** |
| Web UI | 无会话侧栏；**刷新不加载历史**；无全局历史页 |

### 1.2 目标（已确认决策）

| 决策项 | 选择 |
|--------|------|
| 总体档位 | **C** — Web 完整体验 + 后端 API 增强 |
| 导航 | **B** — Agent 优先；`/sessions` 为跨 Agent 历史页 |
| 搜索 | **B** — Chat 侧栏 MySQL 过滤；`/sessions` FTS |
| 实现方案 | **方案 1** — Portal 分层 API，Web 展示与路由 |

### 1.3 非目标（本期不做）

- Gateway / IM 渠道会话路由
- 侧栏 UI 按 `parent_session_id` 树形折叠（仅扁平 +「分支」标签）
- `session_search` LLM 摘要、trigram CJK（属 H-P1-D）
- 替换 MySQL 权威存储为纯 FTS sidecar

---

## 2. 架构与 API（§1）

### 2.1 组件边界

```
Web                          Portal                         Framework / Data
────────────────────────────────────────────────────────────────────────────
SessionSidebar ──► ListSessions(q, preview) ──► MySQL
SessionHistoryPage ──► ListAllSessions ────────► MySQL + agents JOIN
                   ──► SearchSessions ────────► sessionsearch/{agent_id}.db
ChatPage ──────────► ListMessages ────────────► MySQL
                   ──► SendMessageStream ───────► (existing)
Growth/Curator ────► CreateSession(parent=…) ─► parent_session_id
```

### 2.2 Proto 增量（`portal/api/chat/v1/chat.proto`）

**扩展 `SessionReply`：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `parent_session_id` | optional string | 父子链；空表示根会话 |
| `preview` | optional string | 末条 user/assistant 消息摘要，truncate 120 字 |
| `agent_name` | optional string | 仅 `ListAllSessions` / `SearchSessions` |

**扩展 `ListSessionsRequest`：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `q` | optional string | 标题 `LIKE`；非空时可含末条消息 `LIKE` |
| `include_preview` | optional bool | 默认 true |

**扩展 `CreateSessionRequest`：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `parent_session_id` | optional string | 可选；须存在且与 `agent_id` 一致 |

**新增 RPC：**

| RPC | HTTP | 说明 |
|-----|------|------|
| `ListAllSessions` | `GET /api/v1/sessions` | 跨 Agent 分页，`ORDER BY updated_at DESC` |
| `SearchSessions` | `GET /api/v1/sessions/search` | FTS；query 必填 |

**`SearchSessionsRequest`：**

| 字段 | 说明 |
|------|------|
| `query` | 必填，FTS 关键词 |
| `agent_id` | 可选，限定单 Agent |
| `limit` | 默认 20，最大 50 |

**`SearchSessionsReply` 条目：**

`session_id`, `root_session_id`, `agent_id`, `agent_name`, `title`, `preview`, `matched_snippets[]`, `updated_at` — 对齐 `framework/sessionsearch.SessionHit` + Agent 元数据。

### 2.3 Biz / Data

**`ChatSessionRepo` 扩展：**

- `ListByAgentFiltered(ctx, agentID, q, page, pageSize, includePreview)` — 现有 `ListByAgent` 增强
- `ListAll(ctx, page, pageSize, includePreview)` — `JOIN agents` 取 `name`
- `Create(ctx, agentID, title, parentSessionID)` — 校验 parent 存在且同 agent

**Preview 实现（MySQL）：**

子查询或 `LEFT JOIN` 取该 session 最后一条 `role IN ('user','assistant')` 的 `content`，应用层 truncate 120 字。

**`SearchSessions`（Service 层）：**

1. 若 `agent_id` 非空：对该 agent 调 `sessionsearch.GetSessionSearchManager` + `Search`
2. 否则：`agentUC.List` 取全部 agent ID，逐个 Search，合并按 `UpdatedAt` 降序，截断 `limit`
3. 每条 hit 补全 `agent_name`（`agentUC.GetByID` 或批量 map）
4. FTS 未启用或索引缺失：返回空列表 + `ret.message` 提示（非 5xx）

**`CreateSession` 校验：**

- `parent_session_id` 非空 → `GetByID(parent)` 必须存在
- `parent.AgentID == agent_id`，否则 `INVALID_ARGUMENT`

### 2.4 现有 API 保持不变

- `SendMessage` / stream、`DeleteSession` 级联删消息、索引同步（现有 `RegisterSessionSearchTools`）不改契约

---

## 3. Web UI（§2，已确认）

### 3.1 路由

| 路由 | 组件 | 说明 |
|------|------|------|
| `/` | `ChatHome` + `SessionSidebar` + `ChatPage` | `?agent=&session=` |
| `/sessions` | `SessionHistoryPage` | 全局历史 + FTS |
| `/agents/:id/chat/:sessionId` | `ChatPage` | 深链保留 |

`App.tsx` 侧栏新增：**会话历史** → `/sessions`。

### 3.2 布局

对话页：Agent 顶栏 + 左栏 `SessionSidebar`（240px，可折叠）+ 主区 `ChatPage`。

### 3.3 侧栏交互

| 操作 | API |
|------|-----|
| 新建 | `createSession` |
| 切换 | URL 更新 + `listMessages` |
| 搜索 | `listSessions({ q })` debounce 300ms |
| 重命名 | `updateSession` |
| 删除 | `confirm` + `deleteSession`；删当前 session 则切首条或清空 |

列表项：标题 + preview + 相对时间；`parent_session_id` 非空显示「分支」标签。**扁平列表，不树折叠。**

### 3.4 ChatPage 增强

- `sessionId` 变化 → `listMessages` + loading skeleton
- 首条 user 消息成功后，title 为「新对话」→ 取 content 前 30 字 `updateSession`
- 切换 session / 卸载 → `abort` 进行中的 stream

### 3.5 SessionHistoryPage

- 空 query：`listAllSessions` 分页，「加载更多」
- 有 query：`searchSessions` debounce 300ms
- 可选 Agent 筛选 → `searchSessions({ agentId })`
- 「打开」→ `/?agent={id}&session={sid}`

### 3.6 `chatApi` 增量

```typescript
listMessages(sessionId: string, limit?: number)
listSessions(agentId, opts?: { page?, pageSize?, q?, includePreview? })
listAllSessions(opts?: { page?, pageSize? })
searchSessions(opts: { query: string; agentId?: string; limit?: number })
```

归一化：`normalizeSession()` — `parentSessionId`, `agentName`, `matchedSnippets`, snake/camel 双读。

### 3.7 `data-testid`

`session-sidebar-new`, `session-sidebar-search`, `session-item-{id}`, `session-rename-{id}`, `session-delete-{id}`, `sessions-history-search`, `sessions-open-{id}`

---

## 4. 数据流与状态（§3）

### 4.1 URL 为侧栏选中态的单一来源（首页）

```
searchParams.agent / searchParams.session
       ↓
SessionSidebar selectedId
       ↓
ChatPage sessionId prop
```

`ChatHome.updateUrl(aId, sId?)` 保留；侧栏点击调用 `onNavigate`。

### 4.2 侧栏数据流

```
agentId 变化 → listSessions(agentId, { pageSize: 50, includePreview: true })
q 变化（debounce）→ listSessions(agentId, { q })
新建/删除/重命名 → 刷新列表 + 修正 URL
```

### 4.3 ChatPage 消息流

```
sessionId 设置
  → abort 旧 stream
  → listMessages(sessionId)
  → setMessages(items)
  → 用户发送 → sendMessageStream（现有）
  → onDone → 若首条 user 且 title 默认 → updateSession(autoTitle)
```

### 4.4 深链 `/agents/:id/chat/:sessionId`

与首页共用 `ChatPage`；侧栏仅在 `ChatHome` 展示。深链页可无侧栏，或二期在 `ChatPage` 加折叠侧栏（**本期深链页不强制侧栏**，避免双布局维护）。

### 4.5 错误处理

| 场景 | 行为 |
|------|------|
| `getSession` 404 | 清空 session query，toast |
| `listMessages` 失败 | 空状态 + 重试按钮 |
| `SearchSessions` FTS 关闭 | 空结果 + 说明文案 |
| 流式中 `invalid connection` | 保持现有 warn，不覆盖 messages |
| 删除当前 session | 导航到同 agent 无 session 或列表第一项 |

### 4.6 并发与一致性

- `updated_at`：发送消息成功后现有 `Touch` 逻辑保持；侧栏列表在 `onDone` 后可选 `refreshSessionList()`（轻量 invalidate）
- 删除会话：Portal 级联删消息 + `sessionsearch.RemoveSession`（现有路径）

---

## 5. `parent_session_id` 语义（§4）

### 5.1 写入方

| 写入方 | 场景 |
|--------|------|
| Portal `CreateSession` | API 显式传入（UI 首期不传） |
| Growth / Curator | 折叠产生子会话（已有数据层支持） |
| Web | 首期不暴露「从当前会话分叉」按钮 |

### 5.2 读取与展示

- API 一律返回 `parent_session_id`（proto + JSON）
- 侧栏：扁平列表；非空 parent → 「分支」标签
- FTS `SearchSessions`：沿用 `sessionsearch` 的 `RootSessionID` 折叠，去重展示

### 5.3 校验规则

- Parent 必须存在
- Parent 与 child 同一 `agent_id`
- 禁止 parent 指向自身；禁止环（可选：首期只校验 parent 存在 + 同 agent）

### 5.4 与 `session_search` 工具对齐

Agent 运行时 `session_search` 行为不变；Portal `SearchSessions` 为 **Web 只读包装**，不替代工具。

---

## 6. 测试与验收（§5）

### 6.1 Portal 单测 / 集成

| 用例 | 说明 |
|------|------|
| `ListSessions` + `q` | 标题匹配、消息正文匹配 |
| `ListSessions` + `include_preview` | preview 长度 ≤ 120 |
| `ListAllSessions` | 跨 agent 分页、含 `agent_name` |
| `SearchSessions` | mock index；单 agent / 全 agent |
| `CreateSession` + parent | 合法 parent；非法 agent 拒绝 |
| `DeleteSession` | 级联 + 索引删除（现有回归） |

### 6.2 Web E2E（Playwright）

| 文件 | 场景 |
|------|------|
| `session-sidebar.spec.ts` | Mock：新建、切换、搜索、重命名、删除 |
| `session-history.spec.ts` | Mock：ListAll、Search、打开跳转 |
| `session-live.spec.ts` | `E2E_LIVE=1`：创建会话 → 发消息 → 刷新 → 历史可见 |

### 6.3 验收清单（DoD）

- [ ] 首页选 Agent 后可见会话侧栏，可新建/切换/删/改名
- [ ] 刷新页面后消息历史从 `ListMessages` 恢复
- [ ] 首条消息后标题自动从「新对话」更新
- [ ] `/sessions` 可浏览全局列表并用 FTS 搜索
- [ ] `SearchSessions` 结果可点击打开对应对话
- [ ] `parent_session_id` API 可读写；Growth 写入子会话侧栏可见「分支」
- [ ] `portal` `go test ./...` 与 `web` E2E mock 全绿

---

## 7. 实施顺序建议

| 阶段 | 内容 | 估时 |
|------|------|------|
| **P1** | Proto + `make api` + Biz/Data `ListSessions` 增强 + `listMessages` Web | 1–2d |
| **P2** | `ListAllSessions` + `SearchSessions` + `CreateSession` parent | 1–2d |
| **P3** | `SessionSidebar` + `ChatHome` 布局 + `ChatPage` 历史/自动标题 | 1–2d |
| **P4** | `SessionHistoryPage` + 导航 + `chatApi` normalize | 1d |
| **P5** | E2E + 文档更新 | 0.5–1d |

**依赖**：P1 → P3；P2 → P4；P5 最后。

---

## 8. 风险与缓解

| 风险 | 缓解 |
|------|------|
| 全 agent FTS 扫描慢 | 首期 limit≤50；二期加全局索引或异步 |
| Preview 子查询性能 | 首期 page_size≤50；二期冗余 `last_message_preview` 列 |
| 深链页无侧栏 | 文档说明；首页为主入口 |
| FTS 与 MySQL 不一致 | 以 MySQL 为列表权威；FTS 仅搜索页 |

---

## 9. 修订记录

| 版本 | 日期 | 说明 |
|------|------|------|
| 0.1 | 2026-05-25 | 初稿：方案 1，决策 C/B/B，§1–§5 头脑风暴确认 |
