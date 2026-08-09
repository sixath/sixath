# Session 上下文压缩 — 阶段二 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** L2 摘要成功后持久化 compact boundary；Agent 加载 `[summary + boundary 后消息]`，避免重复摘要并接近 Claude Code continuity。

**Architecture:** MySQL migration 扩展 `chat_sessions`；Framework `RunTrace` 增加 `LastL2Summary`；Portal 在 Run 结束后异步 `sessionRepo.Update`；加载路径注入 persisted summary 并传 `ListBudgetOpts.AfterTime`。

**Tech Stack:** Go、GORM、MySQL migration SQL、framework/agent RunTrace

**Spec:** [2026-05-26-session-context-compression-design.md](../specs/2026-05-26-session-context-compression-design.md) §4  
**前置:** [阶段一计划](./2026-05-26-session-context-compression-phase1.md) 全部完成并验收 A1–A5  
**非目标:** snipCompact、手动 compact API、前端 ContextOps 面板

---

## File Structure

| 文件 | 职责 |
|------|------|
| `portal/migrations/008_session_compact.sql` | **新建** — session compact 列 |
| `portal/internal/data/model/chat.go` | GORM 字段 |
| `portal/internal/biz/chat.go` | `ChatSession` compact 字段 |
| `portal/internal/data/chat_mysql.go` | Update/Get 映射 |
| `framework/model/metadata_sixath.go` | `OriginCompactBoundary` |
| `framework/agent/trace.go` | `RunTrace.LastL2Summary` |
| `framework/agent/context_ops.go` | L2 成功时写入 LastL2Summary |
| `portal/internal/conf/conf.proto` | `compact_persist_enabled`、`compact_persist_min_messages` |
| `portal/internal/chat/context_compression.go` | 持久化相关 settings |
| `portal/internal/service/chat_compact.go` | **新建** — 异步 persist + 加载注入 |
| `portal/internal/service/chat.go` | 调用 compact 加载/持久化 |
| `portal/internal/service/chat_compact_test.go` | **新建** |

---

### Task 1: Migration + 领域模型

**Files:**
- Create: `portal/migrations/008_session_compact.sql`
- Modify: `portal/internal/data/model/chat.go`, `portal/internal/biz/chat.go`, `portal/internal/data/chat_mysql.go`

- [ ] **Step 1: SQL**

```sql
ALTER TABLE chat_sessions
  ADD COLUMN compact_summary TEXT NULL,
  ADD COLUMN compact_summary_hash VARCHAR(64) NULL,
  ADD COLUMN compact_boundary_at DATETIME(3) NULL,
  ADD COLUMN compact_message_count INT NOT NULL DEFAULT 0;
```

- [ ] **Step 2: 模型字段 + Update 支持**

`biz.ChatSession` 增加 `CompactSummary`, `CompactSummaryHash`, `CompactBoundaryAt *time.Time`, `CompactMessageCount int`。

- [ ] **Step 3: Commit**

```bash
git add portal/migrations/008_session_compact.sql portal/internal/data/model/chat.go portal/internal/biz/chat.go portal/internal/data/chat_mysql.go
git commit -m "feat(portal): chat_sessions compact boundary columns"
```

---

### Task 2: RunTrace.LastL2Summary

**Files:**
- Modify: `framework/agent/trace.go`, `framework/agent/context_ops.go`, `framework/model/l2_runtime.go`（可选：返回 summary 文本）

- [ ] **Step 1: 字段**

```go
// RunTrace
LastL2Summary string `json:"last_l2_summary,omitempty"`
LastL2MiddleRemoved int `json:"last_l2_middle_removed,omitempty"`
```

- [ ] **Step 2: 在 `contextTraceMerge` 的 `l2_summarize` 分支**

从 `detail["summary_hash"]` 同批增加 `summary_text`（在 `l2_runtime.go` trace 调用处传入 summary 正文），写入 `trace.LastL2Summary` 与 `LastL2MiddleRemoved`。

- [ ] **Step 3: 单测 `context_ops_test.go`**

- [ ] **Step 4: Commit**

```bash
git add framework/agent/trace.go framework/agent/context_ops.go framework/model/l2_runtime.go framework/agent/context_ops_test.go
git commit -m "feat(agent): expose LastL2Summary on RunTrace for portal persist"
```

---

### Task 3: 配置 `compact_persist_enabled`

**Files:**
- Modify: `portal/internal/conf/conf.proto`, `context_compression.go`, `main.go`

- [ ] **Step 1: proto 字段**

```protobuf
  bool compact_persist_enabled = 11;
  int32 compact_persist_min_messages = 12; // default 10
```

- [ ] **Step 2: Settings 结构体 + defaults**

- [ ] **Step 3: Commit**

---

### Task 4: 异步 persist

**Files:**
- Create: `portal/internal/service/chat_compact.go`

- [ ] **Step 1: `persistCompactIfNeeded`**

```go
func (s *ChatService) persistCompactIfNeeded(sessionID string, resp *agent.Response) {
	cfg := chat.ContextCompressionSettings()
	if !cfg.CompactPersistEnabled || resp == nil {
		return
	}
	tr, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || tr == nil || strings.TrimSpace(tr.LastL2Summary) == "" {
		return
	}
	if tr.LastL2MiddleRemoved < cfg.CompactPersistMinMessages {
		return
	}
	summary := tr.LastL2Summary
	hash := tr.ContextOps.L2SummaryHash
	removed := tr.LastL2MiddleRemoved
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		now := time.Now()
		_, err := s.chatUC.UpdateSessionCompact(ctx, sessionID, biz.SessionCompactUpdate{
			Summary: summary, Hash: hash, BoundaryAt: now, MiddleRemoved: removed,
		})
		if err != nil {
			s.log.Errorf("persist compact failed session_id=%s err=%v", sessionID, err)
		}
	}()
}
```

`ChatUsecase.UpdateSessionCompact` 封装 repo Update。

- [ ] **Step 2: 在 SendMessage/Stream 成功路径调用**

- [ ] **Step 3: 单测（mock UC）**

- [ ] **Step 4: Commit**

---

### Task 5: 加载路径注入 summary

**Files:**
- Modify: `portal/internal/service/chat.go`, `chat_agent_history.go`

- [ ] **Step 1: 读取 session compact state**

```go
func (s *ChatService) buildAgentMessages(ctx context.Context, session *biz.ChatSession, effectivePrompt string, history []*biz.ChatMessage) []model.Message {
	messages := make([]model.Message, 0, len(history)+3)
	if effectivePrompt != "" {
		messages = append(messages, model.Message{Role: "system", Content: effectivePrompt})
	}
	if session != nil && strings.TrimSpace(session.CompactSummary) != "" {
		messages = append(messages, model.Message{
			Role:    "system",
			Content: "[记忆中段摘要 / L2]\n" + session.CompactSummary,
			Metadata: map[string]any{model.MetadataKeySixathOrigin: model.OriginL2Handoff},
		})
	}
	for _, h := range history {
		if h.Role == "system" {
			continue
		}
		messages = append(messages, model.Message{Role: h.Role, Content: h.Content})
	}
	return messages
}
```

- [ ] **Step 2: `agentHistoryMessages` 传 `AfterTime: session.CompactBoundaryAt`**

- [ ] **Step 3: 验收 B1–B5**

Run integration / manual：长会话 → DB `compact_summary` 非空；连续多轮 auxiliary 调用次数不线性增长。

- [ ] **Step 4: Commit**

```bash
git add portal/internal/service/chat.go portal/internal/service/chat_agent_history.go portal/internal/service/chat_compact.go portal/internal/service/chat_compact_test.go
git commit -m "feat(portal): persist and load session compact boundary"
```

---

### Task 6（可选）: boundary 标记消息 + UI

**Files:**
- Modify: `ChatMessageMetadata` 或 generic JSON map；Web 历史渲染

- [ ] **Step 1: persist 成功后 `CreateMessage` system 标记**

`OriginCompactBoundary`

- [ ] **Step 2: Web 折叠展示**（若本期要做 UI）

---

## Spec Coverage（自检）

| Spec §4 | Task |
|---------|------|
| migration 008 | Task 1 |
| LastL2Summary | Task 2 |
| compact_persist_enabled | Task 3 |
| 异步 persist | Task 4 |
| 加载 summary + AfterTime | Task 5 |
| B1–B5 | Task 5 |
| boundary 标记消息 | Task 6 可选 |

---

## Execution Handoff

**前置:** 完成阶段一全部 Task 后再开始本计划。

**Two execution options:** Subagent-Driven（推荐）或 Inline Execution。
