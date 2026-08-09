# Agent 工具/模型调用可视化 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 chat 页面像 Claude Code 一样实时展示本轮 Agent 的执行时间线（模型推理节点 + 工具调用节点，可展开看入参/结果/元数据），替代当前只能看到最终文本的黑盒。

**Architecture:** 复用框架已实现的 `ReActAgent.RunEvents()` 流式接口（发出类型安全的 `StreamEvent`，其中 `ToolCall` 即完整 `ToolCallRecord`）。Portal 的 `SendMessageStream` 从 `a.Run()` 切换到 `a.RunEvents()`，将工具事件映射为新的 `tool_call` SSE 事件；同时始终订阅 `events.Bus`（按 `RequestID` 过滤）拿 `ModelInvoked/ModelResponded` 映射为 `model_call` SSE 事件。前端按 `step` + 到达顺序合并成一条时间线。

**Tech Stack:** Go（framework + portal，Kratos HTTP/SSE），React + TypeScript（web），Go test / vitest。

**规格来源：** `portal/docs/superpowers/specs/2026-07-08-tool-call-visibility-design.md`

**命名对齐说明（相对规格）：** 规格草稿用 `prompt_tokens`/`completion_tokens`，但框架 `model.TokenUsage` 实际字段为 `InputTokens`/`OutputTokens`。本计划统一采用 `input_tokens`/`output_tokens`。模型名不由框架发出——Portal 已持有 `agentMeta.ModelConfig.Model`（`chat.go:394`），在映射 `model_call` 时由 Portal 直接盖章。

---

## 文件结构

**Framework（改动最小）**
- Modify: `framework/agent/react_agent.go` — 各 `emit(events.ModelResponded, …)` 补 `input_tokens`/`output_tokens`（从 `gen.TokenUsage`）。

**Portal（主要）**
- Modify: `portal/internal/service/chat_stream.go` — 新增 `ChatStreamEventToolCall`/`ChatStreamEventModelCall` 类型 + payload 结构体 + 截断辅助函数。
- Modify: `portal/internal/service/chat.go` — `SendMessageStream` 改用 `RunEvents`，始终订阅 bus 过滤模型事件，映射并推送新事件。
- Modify: `portal/internal/server/chat_sse.go` — 新增两个 SSE `case`。

**Web（主要）**
- Modify: `web/src/api/client.ts` — SSE 解析新增 `onToolCall`/`onModelCall` 回调 + payload 解析。
- Modify: `web/src/pages/ChatPage.tsx` — 时间线数据模型、事件归并、渲染。
- Modify: `web/src/pages/ChatPage.css` — 时间线样式。
- Create: `web/src/pages/toolVerbMap.ts` — 工具名 → 友好中文动词映射（纯函数，单独可测）。

**测试**
- `framework/agent/react_agent_events_token_test.go`（新）
- `portal/internal/service/chat_stream_toolcall_test.go`（新）
- `web/src/pages/toolVerbMap.test.ts`（新）
- `web/src/pages/timelineReducer.test.ts`（新）

---

## Task 1: 框架 — 模型事件补 token 字段

**Files:**
- Modify: `framework/agent/react_agent.go`（所有 `emit(events.ModelResponded, …)` 调用点：约 304、487、511、536、639、705、903 行）
- Test: `framework/agent/react_agent_events_token_test.go`（Create）

- [ ] **Step 1: 写失败测试**

新建 `framework/agent/react_agent_events_token_test.go`。测试订阅事件总线，跑一次带 fake 模型（返回已知 `TokenUsage`）的 ReAct，断言 `ModelResponded` 事件 payload 含 `input_tokens`/`output_tokens`。参考同目录 `react_agent_test.go` 现有的 fake model 与 bus 订阅写法。

```go
package agent

import (
	"context"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/model"
)

func TestRunEvents_ModelRespondedIncludesTokenUsage(t *testing.T) {
	bus := events.NewBus()
	var gotInput, gotOutput int
	var sawResponded bool
	bus.Subscribe(true, func(_ context.Context, e events.Event) {
		if e.Kind == events.ModelResponded {
			sawResponded = true
			if v, ok := e.Payload["input_tokens"].(int); ok {
				gotInput = v
			}
			if v, ok := e.Payload["output_tokens"].(int); ok {
				gotOutput = v
			}
		}
	})

	// fakeTokenModel 返回固定文本与 TokenUsage{InputTokens:11, OutputTokens:7}
	m := &fakeTokenModel{text: "hello", usage: &model.TokenUsage{InputTokens: 11, OutputTokens: 7}}
	a := NewReActAgent(m, nil, nil, ReActConfig{MaxSteps: 2}, WithReActEventBus(bus))

	ch, err := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	for range ch {
	}

	if !sawResponded {
		t.Fatal("expected ModelResponded event")
	}
	if gotInput != 11 || gotOutput != 7 {
		t.Fatalf("token usage not propagated: input=%d output=%d", gotInput, gotOutput)
	}
}

type fakeTokenModel struct {
	text  string
	usage *model.TokenUsage
}

func (f *fakeTokenModel) Chat(_ context.Context, _ []model.Message, _ ...model.CallOption) (model.Generation, error) {
	return model.Generation{Text: f.text, TokenUsage: f.usage}, nil
}
```

> 注意：`NewReActAgent` 的实际构造签名与 `WithReActEventBus` 请以 `react_agent.go` 现有定义为准；若 fake model 需实现更多接口方法（如 `ChatWithTools`），照 `react_agent_test.go` 里已有的 fake 补齐。此测试走 plain 路径（无 tools），命中 `runPlainEvents` 的 `ModelResponded`（约 487/511 行）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./agent/ -run TestRunEvents_ModelRespondedIncludesTokenUsage -v`
Expected: FAIL（当前 payload 无 `input_tokens`/`output_tokens`，`gotInput/gotOutput` 为 0）

- [ ] **Step 3: 加一个 helper 并在所有 ModelResponded emit 处使用**

在 `react_agent.go` 靠近 `beginModelInvocation` 处新增 helper：

```go
// modelRespondedPayload 构造 ModelResponded 事件 payload，附带 token 用量（若有）。
func modelRespondedPayload(gen model.Generation, step int) map[string]any {
	p := map[string]any{"text_length": len(gen.Text), "step": step}
	if gen.TokenUsage != nil {
		p["input_tokens"] = gen.TokenUsage.InputTokens
		p["output_tokens"] = gen.TokenUsage.OutputTokens
	}
	return p
}
```

将每处 `emit(events.ModelResponded, map[string]any{"text_length": len(gen.Text), "step": step})` 改为：

```go
emit(events.ModelResponded, modelRespondedPayload(gen, step))
```

对没有 `step` 变量的调用点（如 `runPlainEvents` 约 487 行、511 行 `len(text)` 处），用 `step = -1`：

```go
// 约 487 行（gen 可用）
emit(events.ModelResponded, modelRespondedPayload(gen, -1))
// 约 511 行（只有 text 字符串，无 gen）：保持原样或用 map，无 token 可加
emit(events.ModelResponded, map[string]any{"text_length": len(text), "step": -1})
```

> 用编辑器搜索 `events.ModelResponded` 逐一替换。511 行是流式累积后的收尾，无 `gen.TokenUsage`，保持无 token 字段即可（前端按缺省处理）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd framework && go test ./agent/ -run TestRunEvents_ModelRespondedIncludesTokenUsage -v`
Expected: PASS

- [ ] **Step 5: 跑整包回归**

Run: `cd framework && go test ./agent/ 2>&1 | tail -20`
Expected: PASS（无回归）

- [ ] **Step 6: 提交**

```bash
cd framework
git add agent/react_agent.go agent/react_agent_events_token_test.go
git commit -m "feat(agent): include token usage in ModelResponded events

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Portal — 新增 tool_call/model_call 事件类型与截断

**Files:**
- Modify: `portal/internal/service/chat_stream.go`
- Test: `portal/internal/service/chat_stream_toolcall_test.go`（Create）

- [ ] **Step 1: 写失败测试**

新建 `portal/internal/service/chat_stream_toolcall_test.go`，覆盖：(a) `ToolCallRecord` → `toolCallPayloadFromRecord` 映射正确；(b) 大字段被截断且 `Truncated=true`。

```go
package service

import (
	"strings"
	"testing"

	"github.com/sixath/framework/agent"
)

func TestToolCallPayloadFromRecord_MapsFields(t *testing.T) {
	rec := agent.ToolCallRecord{
		Step:       2,
		ToolCallID: "call_1",
		ToolName:   "execute_query",
		Arguments:  map[string]any{"sql": "SELECT 1"},
		Result:     map[string]any{"rows": 42},
		Allowed:    true,
		Decision:   "allowed",
		DurationMS: 128,
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	if p.ID != "call_1" || p.Step != 2 || p.ToolName != "execute_query" {
		t.Fatalf("basic fields wrong: %+v", p)
	}
	if p.Phase != "completed" || p.DurationMS != 128 || !p.Allowed {
		t.Fatalf("status fields wrong: %+v", p)
	}
}

func TestToolCallPayloadFromRecord_TruncatesLargeResult(t *testing.T) {
	big := strings.Repeat("x", 20*1024) // 20KB > 8KB 上限
	rec := agent.ToolCallRecord{
		ToolCallID: "call_2",
		ToolName:   "read_file",
		Result:     big,
	}
	p := toolCallPayloadFromRecord(rec, "completed")
	if !p.Truncated {
		t.Fatal("expected Truncated=true for oversized result")
	}
	s, _ := p.Result.(string)
	if len(s) > toolPayloadFieldLimit+64 { // 允许截断标记的少量额外字节
		t.Fatalf("result not truncated: len=%d", len(s))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd portal && go test ./internal/service/ -run TestToolCallPayload -v`
Expected: FAIL（`toolCallPayloadFromRecord`、`toolCallPayloadFieldLimit` 等未定义）

- [ ] **Step 3: 实现类型、payload、截断**

在 `chat_stream.go` 顶部 `const` 区新增事件类型：

```go
const (
	ChatStreamEventChunk           ChatStreamEventType = "chunk"
	ChatStreamEventConfirmRequired ChatStreamEventType = "confirm_required"
	ChatStreamEventInputRequired   ChatStreamEventType = "input_required"
	ChatStreamEventError           ChatStreamEventType = "error"
	ChatStreamEventDebug           ChatStreamEventType = "debug"
	ChatStreamEventToolCall        ChatStreamEventType = "tool_call"  // 新增
	ChatStreamEventModelCall       ChatStreamEventType = "model_call" // 新增
)

const toolPayloadFieldLimit = 8 * 1024 // 单字段截断上限（字节）
```

在 `ChatStreamEvent` 结构体新增两个可选指针字段：

```go
type ChatStreamEvent struct {
	Type         ChatStreamEventType
	Content      string
	Error        string
	Confirmation *ChatConfirmationRequest
	Input        *ChatInputRequest
	ToolCall     *ToolCallPayload  // 新增
	ModelCall    *ModelCallPayload // 新增
}
```

新增 payload 结构体与映射函数：

```go
type ToolCallPayload struct {
	ID         string `json:"id"`
	Step       int    `json:"step"`
	Phase      string `json:"phase"` // started|completed|failed
	ToolName   string `json:"tool_name"`
	Arguments  any    `json:"arguments,omitempty"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Allowed    bool   `json:"allowed"`
	Decision   string `json:"decision,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ModelCallPayload struct {
	Step         int    `json:"step"`
	Phase        string `json:"phase"` // invoked|responded
	Mode         string `json:"mode,omitempty"`
	Model        string `json:"model,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
}

// truncateField 将任意值转为 JSON，超过上限时截断并返回 truncated=true。
func truncateField(v any) (any, bool) {
	if v == nil {
		return nil, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v, false
	}
	if len(b) <= toolPayloadFieldLimit {
		return v, false
	}
	return string(b[:toolPayloadFieldLimit]) + "…[truncated]", true
}

func toolCallPayloadFromRecord(rec agent.ToolCallRecord, phase string) *ToolCallPayload {
	p := &ToolCallPayload{
		ID:         rec.ToolCallID,
		Step:       rec.Step,
		Phase:      phase,
		ToolName:   rec.ToolName,
		Error:      rec.Error,
		Allowed:    rec.Allowed,
		Decision:   rec.Decision,
		DurationMS: rec.DurationMS,
	}
	args, aTrunc := truncateField(rec.Arguments)
	res, rTrunc := truncateField(rec.Result)
	p.Arguments = args
	p.Result = res
	p.Truncated = aTrunc || rTrunc
	return p
}
```

确保 `chat_stream.go` import 了 `encoding/json`（若未 import 则加）。测试里 `toolCallPayloadFromRecord` 返回指针，测试中的 `p.ID` 访问需相应改为指针（已是指针，`p.ID` 合法）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd portal && go test ./internal/service/ -run TestToolCallPayload -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd portal
git add internal/service/chat_stream.go internal/service/chat_stream_toolcall_test.go
git commit -m "feat(portal): add tool_call/model_call stream event types with truncation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Portal — SendMessageStream 改用 RunEvents 并映射事件

**Files:**
- Modify: `portal/internal/service/chat.go`（`SendMessageStream`，约 366-605 行）

> 本任务以集成改造为主，逻辑分支多、难以对单函数写窄单测。以「保持现有流式测试全绿 + 手动 SSE 验证（Task 6）」作为验证手段。若 `chat_stream_test.go` 存在相关用例，改造后需保持通过。

- [ ] **Step 1: 先跑现有 stream 测试建立基线**

Run: `cd portal && go test ./internal/service/ -run Stream -v 2>&1 | tail -20`
Expected: PASS（记录当前通过用例，作为回归基线）

- [ ] **Step 2: 让 bus 订阅始终生效（不再仅 DebugRun）并过滤 RequestID**

改造 `SendMessageStream` 中约 405-443 行的订阅块。目标：无论是否 `DebugRun`，都订阅 bus 把 `ModelInvoked`/`ModelResponded` 映射成 `model_call` 事件；`DebugRun` 时**额外**保留原始 debug 转发。

将现有 `if agentMeta.DebugRun {` 块替换为始终建立的中转（保留 done/relayWg/prevBus 生命周期语义）：

```go
ch := make(chan ChatStreamEvent, 32)
done := make(chan struct{})
relay := make(chan ChatStreamEvent, 64)
var relayWg sync.WaitGroup
prevBus := events.DefaultBus()
bus := events.NewBus()
events.SetDefaultBus(bus)

rid := sessionID // 见下方说明：用于过滤本轮事件
debugRun := agentMeta.DebugRun

bus.Subscribe(true, func(_ context.Context, e events.Event) {
	// 模型事件 → 结构化 model_call
	if mc := modelCallEventFromBus(e, agentMeta.ModelConfig.Model); mc != nil {
		select {
		case relay <- ChatStreamEvent{Type: ChatStreamEventModelCall, ModelCall: mc}:
		case <-done:
		}
	}
	// DebugRun 额外保留原始 debug 文本
	if debugRun {
		msg, _ := json.Marshal(e.Payload)
		select {
		case relay <- ChatStreamEvent{Type: ChatStreamEventDebug, Content: string(e.Kind) + "[" + string(msg) + "]\r\n"}:
		case <-done:
		}
	}
})
relayWg.Add(1)
go func() {
	defer relayWg.Done()
	for {
		select {
		case <-done:
			return
		case ev, ok := <-relay:
			if !ok {
				return
			}
			select {
			case <-done:
				return
			case ch <- ev:
			}
		}
	}
}()
```

> **RequestID 过滤说明：** 框架 `RunEvents` 用 `requestID(req)` 生成 rid；若 `req.Metadata` 未显式带 request id，rid 可能是自动生成值，portal 侧拿不到同一个值。为稳妥，本轮通过「每轮新建独立 `events.NewBus()` 并 `SetDefaultBus`」实现隔离（现有 DebugRun 已用此法），因此该 bus 只会收到本轮事件，无需再按 rid 过滤。跨并发会话的隔离依赖 `SetDefaultBus` 的进程级串行——保持与现有 DebugRun 相同的行为边界，不在本任务扩大并发语义。

新增 `modelCallEventFromBus`（放在 `chat_stream.go`，紧邻 Task 2 的映射函数）：

```go
func modelCallEventFromBus(e events.Event, modelName string) *ModelCallPayload {
	var phase string
	switch e.Kind {
	case events.ModelInvoked:
		phase = "invoked"
	case events.ModelResponded:
		phase = "responded"
	default:
		return nil
	}
	p := &ModelCallPayload{Phase: phase, Model: modelName}
	if v, ok := e.Payload["step"].(int); ok {
		p.Step = v
	}
	if v, ok := e.Payload["mode"].(string); ok {
		p.Mode = v
	}
	if v, ok := e.Payload["message_count"].(int); ok {
		p.MessageCount = v
	}
	if v, ok := e.Payload["input_tokens"].(int); ok {
		p.InputTokens = v
	}
	if v, ok := e.Payload["output_tokens"].(int); ok {
		p.OutputTokens = v
	}
	return p
}
```

> 该函数需 import `github.com/sixath/framework/events`（chat_stream.go 顶部补 import）。

- [ ] **Step 3: 主 goroutine 改用 RunEvents 消费工具事件**

将约 557-603 行的运行 goroutine 改为优先走 `RunEvents`。替换 `resp, err := a.Run(...)` 那段为：

```go
go func() {
	defer func() {
		close(done)
		relayWg.Wait()
		events.SetDefaultBus(prevBus)
		close(ch)
	}()

	req := &agent.Request{
		Messages: messages,
		Metadata: prefetchRequestMetadata(sessionID, session.AgentID, agentMeta.Workspace),
	}

	ea, ok := a.(agent.EventStreamableAgent)
	if !ok {
		// 回退：不支持流式事件的 agent 保持原有 a.Run 行为
		resp, err := a.Run(runCtx, req)
		if err != nil {
			s.handleStreamRunError(ctx, sessionID, session.AgentID, streamSessionProvider, ch, err)
			return
		}
		for _, event := range streamEventsFromResponse(resp) {
			ch <- event
		}
		return
	}

	evCh, err := ea.RunEvents(runCtx, req)
	if err != nil {
		ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
		return
	}
	for ev := range evCh {
		switch ev.Type {
		case agent.StreamEventDelta:
			if ev.Text != "" {
				ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: ev.Text}
			}
		case agent.StreamEventToolStarted:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "started")}
			}
		case agent.StreamEventToolCompleted:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "completed")}
			}
		case agent.StreamEventToolFailed, agent.StreamEventPermissionDenied:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "failed")}
			}
		case agent.StreamEventError:
			s.handleStreamRunError(ctx, sessionID, session.AgentID, streamSessionProvider, ch, errors.New(ev.Error))
		case agent.StreamEventDone:
			// 结束由 channel 关闭驱动，done 事件无需额外处理
		}
	}
}()
```

> **保留 guardrail banner 逻辑：** 现有 `a.Run` 错误分支里的 `chat.DecomposeGuardrailRunError` 落库/banner 逻辑（约 582-598 行）抽取为方法 `handleStreamRunError`（新增在 chat.go），供上面回退路径与 `StreamEventError` 分支复用，避免重复。抽取时原样搬运现有逻辑，不改语义。

新增方法（把现有错误处理块搬进来）：

```go
func (s *ChatService) handleStreamRunError(ctx context.Context, sessionID, agentID string, provider chat.SessionProvider, ch chan<- ChatStreamEvent, err error) {
	isH, vis, persist, raw := chat.DecomposeGuardrailRunError(err)
	if isH && !raw && vis != "" {
		ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: vis}
		if persist {
			if gmsg, cerr := s.chatUC.CreateMessage(ctx, sessionID, "assistant", vis); cerr != nil {
				s.log.Errorf("stream persist guardrail banner failed: session_id=%s err=%v", sessionID, cerr)
			} else {
				s.notifyGrowthAssistantTurn(sessionID)
				go chat.NotifyMemorySessionDirty(ctx, sessionID, len(vis), 1, s.chatUC, s.agentUC, provider)
				chat.NotifySessionMessageIndexed(ctx, s.chatUC, sessionID, gmsg)
			}
		}
		return
	}
	s.log.Errorf("SendMessageStream run agent failed: session_id=%s agent_id=%s err=%v", sessionID, agentID, err)
	ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
}
```

> `provider chat.SessionProvider` 的确切类型以 `NewChatTranscriptProvider` 返回值为准；若类型不同，按实际签名调整参数类型。确保 chat.go 顶部 import 了 `errors`。

- [ ] **Step 4: 编译 + 跑现有 stream 测试确认无回归**

Run: `cd portal && go build ./... && go test ./internal/service/ -run Stream -v 2>&1 | tail -20`
Expected: 编译通过；Step 1 记录的基线用例仍 PASS

- [ ] **Step 5: 提交**

```bash
cd portal
git add internal/service/chat.go internal/service/chat_stream.go
git commit -m "feat(portal): stream tool_call/model_call via RunEvents

SendMessageStream 改用 RunEvents 消费工具事件，并始终订阅
事件总线映射模型事件；DebugRun 原始转发保留。

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Portal — SSE 层新增两个事件 case

**Files:**
- Modify: `portal/internal/server/chat_sse.go`（`for event := range ch` 的 switch，约 76-114 行）

- [ ] **Step 1: 在 switch 中新增 tool_call/model_call 分支**

在 `case service.ChatStreamEventDebug:` 之后、`default:` 之前插入：

```go
case service.ChatStreamEventToolCall:
	if event.ToolCall != nil {
		writeSSEEvent(ctx, "tool_call", map[string]any{"tool_call": event.ToolCall})
	}
case service.ChatStreamEventModelCall:
	if event.ModelCall != nil {
		writeSSEEvent(ctx, "model_call", map[string]any{"model_call": event.ModelCall})
	}
```

> 这两类事件不参与 `full`（assistant 文本累积）与 `hasContent` 判定——它们是过程元数据，不落进 assistant 消息正文。

- [ ] **Step 2: 编译确认**

Run: `cd portal && go build ./...`
Expected: 通过

- [ ] **Step 3: 提交**

```bash
cd portal
git add internal/server/chat_sse.go
git commit -m "feat(portal): emit tool_call/model_call SSE events

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Web — 工具名友好动词映射（纯函数）

**Files:**
- Create: `web/src/pages/toolVerbMap.ts`
- Test: `web/src/pages/toolVerbMap.test.ts`（Create）

- [ ] **Step 1: 写失败测试**

新建 `web/src/pages/toolVerbMap.test.ts`：

```ts
import { describe, it, expect } from 'vitest'
import { toolVerb } from './toolVerbMap'

describe('toolVerb', () => {
  it('maps known tools to Chinese verbs', () => {
    expect(toolVerb('read_file')).toBe('读取文件')
    expect(toolVerb('execute_query')).toBe('数据库查询')
    expect(toolVerb('web_search')).toBe('网页搜索')
  })
  it('falls back to raw name for unknown tools', () => {
    expect(toolVerb('some_custom_tool')).toBe('some_custom_tool')
  })
  it('handles empty input', () => {
    expect(toolVerb('')).toBe('工具调用')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/pages/toolVerbMap.test.ts`
Expected: FAIL（模块不存在）

- [ ] **Step 3: 实现映射**

新建 `web/src/pages/toolVerbMap.ts`：

```ts
const VERB_MAP: Record<string, string> = {
  read_file: '读取文件',
  write_file: '写入文件',
  execute_query: '数据库查询',
  execute_write: '数据库写入',
  web_search: '网页搜索',
  web_fetch: '抓取网页',
  skill_manage: '技能管理',
  ask_user: '询问用户',
  tool_search: '工具检索',
}

export function toolVerb(name: string): string {
  if (!name) return '工具调用'
  return VERB_MAP[name] ?? name
}
```

> 映射表按项目实际工具名补充；未知工具回退原始名符合规格「未映射回退显示原始 tool_name」。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/pages/toolVerbMap.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd web
git add src/pages/toolVerbMap.ts src/pages/toolVerbMap.test.ts
git commit -m "feat(web): add tool name to Chinese verb mapping

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Web — SSE 解析新增 onToolCall/onModelCall

**Files:**
- Modify: `web/src/api/client.ts`（callbacks interface 约 682-688 行；SSE 解析 switch 约 743-774 行）

- [ ] **Step 1: 新增 payload 类型与回调声明**

在 `client.ts` 中（靠近 `SourcesBrowsedPayload` 定义处）新增导出类型：

```ts
export interface ToolCallPayload {
  id: string
  step: number
  phase: 'started' | 'completed' | 'failed'
  tool_name: string
  arguments?: unknown
  result?: unknown
  error?: string
  allowed: boolean
  decision?: string
  duration_ms?: number
  truncated?: boolean
}

export interface ModelCallPayload {
  step: number
  phase: 'invoked' | 'responded'
  mode?: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  message_count?: number
}
```

在 callbacks interface（约 685-688 行，与 `onSourcesBrowsed` 并列）新增：

```ts
      onToolCall?: (payload: ToolCallPayload) => void
      onModelCall?: (payload: ModelCallPayload) => void
```

- [ ] **Step 2: 在 SSE switch 新增解析分支**

在 `else if (curEvent === 'sources_browsed')` 分支之后新增：

```ts
              else if (curEvent === 'tool_call') {
                const tc = d.tool_call as ToolCallPayload | undefined
                if (tc && typeof tc.id === 'string') callbacks.onToolCall?.(tc)
              }
              else if (curEvent === 'model_call') {
                const mc = d.model_call as ModelCallPayload | undefined
                if (mc && typeof mc.phase === 'string') callbacks.onModelCall?.(mc)
              }
```

- [ ] **Step 3: 编译/类型检查确认通过**

Run: `cd web && npx tsc --noEmit`
Expected: 无类型错误

- [ ] **Step 4: 提交**

```bash
cd web
git add src/api/client.ts
git commit -m "feat(web): parse tool_call/model_call SSE events

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Web — 时间线归并 reducer（纯函数）

**Files:**
- Create: `web/src/pages/timelineReducer.ts`
- Test: `web/src/pages/timelineReducer.test.ts`（Create）

- [ ] **Step 1: 写失败测试**

新建 `web/src/pages/timelineReducer.test.ts`：

```ts
import { describe, it, expect } from 'vitest'
import { applyToolCall, applyModelCall, finalizeTimeline, type TimelineNode } from './timelineReducer'
import type { ToolCallPayload, ModelCallPayload } from '../api/client'

const tc = (o: Partial<ToolCallPayload>): ToolCallPayload => ({
  id: 'c1', step: 0, phase: 'started', tool_name: 'read_file', allowed: true, ...o,
})
const mc = (o: Partial<ModelCallPayload>): ModelCallPayload => ({
  step: 0, phase: 'invoked', ...o,
})

describe('timelineReducer', () => {
  it('upserts a tool node by id, updating in place', () => {
    let nodes: TimelineNode[] = []
    nodes = applyToolCall(nodes, tc({ phase: 'started' }))
    nodes = applyToolCall(nodes, tc({ phase: 'completed', duration_ms: 128, result: { rows: 1 } }))
    const tools = nodes.filter((n) => n.kind === 'tool')
    expect(tools).toHaveLength(1)
    expect(tools[0].kind === 'tool' && tools[0].phase).toBe('completed')
    expect(tools[0].kind === 'tool' && tools[0].durationMs).toBe(128)
  })

  it('upserts a model node by step, invoked then responded', () => {
    let nodes: TimelineNode[] = []
    nodes = applyModelCall(nodes, mc({ step: 1, phase: 'invoked' }))
    nodes = applyModelCall(nodes, mc({ step: 1, phase: 'responded', input_tokens: 10, output_tokens: 5 }))
    const models = nodes.filter((n) => n.kind === 'model')
    expect(models).toHaveLength(1)
    expect(models[0].kind === 'model' && models[0].outputTokens).toBe(5)
  })

  it('marks in-progress nodes as interrupted on finalize', () => {
    let nodes: TimelineNode[] = []
    nodes = applyToolCall(nodes, tc({ id: 'c9', phase: 'started' }))
    nodes = finalizeTimeline(nodes)
    const t = nodes.find((n) => n.kind === 'tool' && n.id === 'c9')
    expect(t && t.kind === 'tool' && t.phase).toBe('interrupted')
  })

  it('sorts by step then arrival', () => {
    let nodes: TimelineNode[] = []
    nodes = applyModelCall(nodes, mc({ step: 2, phase: 'invoked' }))
    nodes = applyToolCall(nodes, tc({ id: 'a', step: 1, phase: 'started' }))
    expect(nodes[0].step).toBe(1)
    expect(nodes[1].step).toBe(2)
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/pages/timelineReducer.test.ts`
Expected: FAIL（模块不存在）

- [ ] **Step 3: 实现 reducer**

新建 `web/src/pages/timelineReducer.ts`：

```ts
import type { ToolCallPayload, ModelCallPayload } from '../api/client'

export type ToolNode = {
  kind: 'tool'
  id: string
  step: number
  seq: number
  phase: 'started' | 'completed' | 'failed' | 'interrupted'
  toolName: string
  arguments?: unknown
  result?: unknown
  error?: string
  allowed: boolean
  decision?: string
  durationMs?: number
  truncated?: boolean
}

export type ModelNode = {
  kind: 'model'
  step: number
  seq: number
  phase: 'invoked' | 'responded' | 'interrupted'
  mode?: string
  model?: string
  inputTokens?: number
  outputTokens?: number
  messageCount?: number
}

export type TimelineNode = ToolNode | ModelNode

let seqCounter = 0
const nextSeq = () => ++seqCounter

function sortNodes(nodes: TimelineNode[]): TimelineNode[] {
  return [...nodes].sort((a, b) => (a.step - b.step) || (a.seq - b.seq))
}

export function applyToolCall(nodes: TimelineNode[], p: ToolCallPayload): TimelineNode[] {
  const idx = nodes.findIndex((n) => n.kind === 'tool' && n.id === p.id)
  const patch: Partial<ToolNode> = {
    phase: p.phase,
    toolName: p.tool_name,
    arguments: p.arguments,
    result: p.result,
    error: p.error,
    allowed: p.allowed,
    decision: p.decision,
    durationMs: p.duration_ms,
    truncated: p.truncated,
  }
  if (idx >= 0) {
    const merged = { ...(nodes[idx] as ToolNode), ...patch }
    const copy = [...nodes]
    copy[idx] = merged
    return sortNodes(copy)
  }
  const node: ToolNode = {
    kind: 'tool', id: p.id, step: p.step, seq: nextSeq(),
    phase: p.phase, toolName: p.tool_name, arguments: p.arguments,
    result: p.result, error: p.error, allowed: p.allowed,
    decision: p.decision, durationMs: p.duration_ms, truncated: p.truncated,
  }
  return sortNodes([...nodes, node])
}

export function applyModelCall(nodes: TimelineNode[], p: ModelCallPayload): TimelineNode[] {
  const idx = nodes.findIndex((n) => n.kind === 'model' && n.step === p.step)
  const patch: Partial<ModelNode> = {
    phase: p.phase,
    mode: p.mode,
    model: p.model,
    inputTokens: p.input_tokens,
    outputTokens: p.output_tokens,
    messageCount: p.message_count,
  }
  if (idx >= 0) {
    const merged = { ...(nodes[idx] as ModelNode), ...patch }
    // invoked 不覆盖已到达的 responded token
    const copy = [...nodes]
    copy[idx] = merged
    return sortNodes(copy)
  }
  const node: ModelNode = {
    kind: 'model', step: p.step, seq: nextSeq(), phase: p.phase,
    mode: p.mode, model: p.model, inputTokens: p.input_tokens,
    outputTokens: p.output_tokens, messageCount: p.message_count,
  }
  return sortNodes([...nodes, node])
}

export function finalizeTimeline(nodes: TimelineNode[]): TimelineNode[] {
  return nodes.map((n) => {
    if (n.kind === 'tool' && n.phase === 'started') return { ...n, phase: 'interrupted' as const }
    if (n.kind === 'model' && n.phase === 'invoked') return { ...n, phase: 'interrupted' as const }
    return n
  })
}
```

> `seqCounter` 为模块级单调计数器，保证同 step 内按到达顺序稳定排序，跨消息不重置无影响（仅用于排序相对大小）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/pages/timelineReducer.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd web
git add src/pages/timelineReducer.ts src/pages/timelineReducer.test.ts
git commit -m "feat(web): add timeline merge reducer for tool/model events

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Web — ChatPage 接线时间线状态

**Files:**
- Modify: `web/src/pages/ChatPage.tsx`

> 本任务把 reducer 接到组件状态。以「类型检查通过 + 手动验证（Task 10）」为验证；组件级测试非本项目现有惯例，不强加。

- [ ] **Step 1: 为每条 assistant 消息引入 timeline 状态**

在 `ChatPage.tsx` 的消息类型/状态中，为当前正在流式接收的 assistant 消息增加 `timeline: TimelineNode[]` 字段。定位现有维护流式 assistant 文本的 state（与 `debugText`/chunk 累积相邻），并列新增：

```ts
import { applyToolCall, applyModelCall, finalizeTimeline, type TimelineNode } from './timelineReducer'
// ...
const [timeline, setTimeline] = useState<TimelineNode[]>([])
const timelineRef = useRef<TimelineNode[]>([])
```

- [ ] **Step 2: 在流式回调里接线 reducer**

在调用 `api.sendMessageStream`（或等价流式方法）传入 callbacks 处，新增：

```ts
onToolCall: (p) => {
  timelineRef.current = applyToolCall(timelineRef.current, p)
  setTimeline(timelineRef.current)
},
onModelCall: (p) => {
  timelineRef.current = applyModelCall(timelineRef.current, p)
  setTimeline(timelineRef.current)
},
```

在 `onDone`/`onError` 收尾处调用 finalize，并将 timeline 固化进该条消息：

```ts
// onDone 内
timelineRef.current = finalizeTimeline(timelineRef.current)
setTimeline(timelineRef.current)
// 将 timelineRef.current 存入刚完成的 assistant 消息对象（跟随现有消息落地逻辑）
```

发起新一轮请求前重置：`timelineRef.current = []; setTimeline([])`（放在与现有 `debugText` 重置相邻处）。

- [ ] **Step 3: 类型检查**

Run: `cd web && npx tsc --noEmit`
Expected: 无类型错误

- [ ] **Step 4: 提交**

```bash
cd web
git add src/pages/ChatPage.tsx
git commit -m "feat(web): wire timeline state into ChatPage stream callbacks

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Web — 时间线渲染组件与样式

**Files:**
- Modify: `web/src/pages/ChatPage.tsx`（渲染时间线）
- Modify: `web/src/pages/ChatPage.css`（样式）

- [ ] **Step 1: 渲染时间线（默认展开，节点默认收起 + 分标签）**

在 assistant 消息渲染处（文本上方）插入时间线。新增一个内联组件（同文件内即可）：

```tsx
function TimelineView({ nodes }: { nodes: TimelineNode[] }) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [tab, setTab] = useState<Record<string, 'args' | 'result' | 'meta'>>({})
  if (!nodes.length) return null
  return (
    <div className="tl">
      {nodes.map((n) => {
        const key = n.kind === 'tool' ? `t:${n.id}` : `m:${n.step}`
        const isOpen = open[key]
        const t = tab[key] ?? 'args'
        const dotClass =
          n.kind === 'model' ? 'tl-dot tl-dot-model'
          : (n.kind === 'tool' && (n.phase === 'failed' || !n.allowed)) ? 'tl-dot tl-dot-fail'
          : 'tl-dot'
        const running = (n.kind === 'tool' && n.phase === 'started') || (n.kind === 'model' && n.phase === 'invoked')
        return (
          <div className="tl-item" key={key}>
            <span className={`${dotClass}${running ? ' tl-dot-run' : ''}`} />
            <div className="tl-row" onClick={() => setOpen({ ...open, [key]: !isOpen })}>
              {n.kind === 'model' ? (
                <span className="tl-verb">🧠 模型推理</span>
              ) : (
                <span className="tl-verb">{toolVerb(n.toolName)}</span>
              )}
              <span className="tl-meta">
                {n.kind === 'model'
                  ? `${n.model ?? ''}${n.outputTokens != null ? ` · ${(n.inputTokens ?? 0) + n.outputTokens} tokens` : ''}`
                  : `${running ? '执行中…' : (n.phase === 'failed' || !n.allowed) ? '失败' : `✓ ${n.durationMs ?? 0}ms`}`}
              </span>
            </div>
            {isOpen && (
              <div className="tl-panel">
                {n.kind === 'tool' ? (
                  <>
                    <div className="tl-tabs">
                      <span className={t === 'args' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab({ ...tab, [key]: 'args' })}>入参</span>
                      <span className={t === 'result' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab({ ...tab, [key]: 'result' })}>结果</span>
                      <span className={t === 'meta' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab({ ...tab, [key]: 'meta' })}>元数据</span>
                    </div>
                    {t === 'args' && <pre className="tl-pre">{JSON.stringify(n.arguments, null, 2)}</pre>}
                    {t === 'result' && (
                      <>
                        <pre className="tl-pre">{n.error ? n.error : JSON.stringify(n.result, null, 2)}</pre>
                        {n.truncated && <div className="tl-trunc">结果已截断</div>}
                      </>
                    )}
                    {t === 'meta' && (
                      <pre className="tl-pre">{JSON.stringify({ duration_ms: n.durationMs, allowed: n.allowed, decision: n.decision, step: n.step }, null, 2)}</pre>
                    )}
                  </>
                ) : (
                  <pre className="tl-pre">{JSON.stringify({ mode: n.mode, model: n.model, input_tokens: n.inputTokens, output_tokens: n.outputTokens, message_count: n.messageCount }, null, 2)}</pre>
                )}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
```

在 assistant 消息 JSX 中，文本前渲染：`{msgTimeline.length > 0 && <TimelineView nodes={msgTimeline} />}`（对流式中的消息用 `timeline` state，对历史消息用消息对象上固化的 timeline）。确保 import `toolVerb`。

- [ ] **Step 2: 加样式**

在 `ChatPage.css` 末尾追加：

```css
.tl { border-left: 2px solid #e5e7eb; margin: 8px 0 8px 8px; padding-left: 16px; }
.tl-item { position: relative; padding: 4px 0; }
.tl-dot { position: absolute; left: -21px; top: 9px; width: 9px; height: 9px; border-radius: 50%; background: #6366f1; }
.tl-dot-model { background: #f59e0b; }
.tl-dot-fail { background: #ef4444; }
.tl-dot-run { animation: tl-pulse 1s infinite; }
@keyframes tl-pulse { 0%,100% { opacity: 1; } 50% { opacity: .35; } }
.tl-row { display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: 13px; }
.tl-verb { font-weight: 600; color: #111827; }
.tl-meta { color: #9ca3af; font-size: 11px; }
.tl-panel { margin-top: 6px; background: #f9fafb; border: 1px solid #f0f0f0; border-radius: 6px; padding: 8px 10px; }
.tl-tabs { margin-bottom: 6px; }
.tl-tab { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; margin-right: 4px; color: #9ca3af; cursor: pointer; }
.tl-tab.on { background: #eef2ff; color: #4338ca; font-weight: 600; }
.tl-pre { margin: 0; font-size: 12px; color: #4b5563; white-space: pre-wrap; word-break: break-word; max-height: 300px; overflow: auto; }
.tl-trunc { color: #9ca3af; font-size: 11px; margin-top: 4px; }
```

- [ ] **Step 3: 类型检查 + 构建**

Run: `cd web && npx tsc --noEmit && npm run build 2>&1 | tail -15`
Expected: 类型无错误，构建成功

- [ ] **Step 4: 提交**

```bash
cd web
git add src/pages/ChatPage.tsx src/pages/ChatPage.css
git commit -m "feat(web): render execution timeline with tabbed node details

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10: 端到端手动验证

**Files:** 无（验证任务）

- [ ] **Step 1: 启动 portal 后端**

Run: 按项目惯例启动 portal 后端（参考 `portal/cmd/backend`）。
Expected: 服务起来，SSE 端点 `POST /api/v1/sessions/{id}/messages/stream` 可用。

- [ ] **Step 2: 启动 web 前端**

Run: `cd web && npm run dev`
Expected: 前端可访问。

- [ ] **Step 3: 发一条会触发工具调用的消息**

在 chat 页面向一个已绑定工具（如数据源查询）的 agent 发消息，触发至少一次工具调用。

Expected（逐条核对）：
- 出现执行时间线，时间线本身默认展开。
- 至少一个 🧠 模型推理节点（橙点），responded 后显示 token 数。
- 至少一个工具节点（紫点）：先显示"执行中…"（脉冲），完成后变为"✓ Nms"。
- 点击工具节点展开，看到「入参 / 结果 / 元数据」三个标签，内容正确。
- assistant 最终文本正常显示，未被时间线影响。

- [ ] **Step 4: 验证失败与截断**

触发一个会失败的工具（如无效参数）或返回大结果的工具。
Expected：失败节点红点 + "失败"，结果标签显示 error；大结果场景结果标签底部显示"结果已截断"。

- [ ] **Step 5: 验证向后兼容与 DebugRun**

- 普通 agent（非 DebugRun）也能看到时间线。
- DebugRun agent 仍能看到原始 debug 面板文本，且与时间线并存不冲突。

- [ ] **Step 6: 无需提交**（纯验证）；如发现问题，回到对应 Task 修复并重跑该 Task 的测试。

---

## 完成标准

- Framework：`ModelResponded` 携带 token；`go test ./agent/` 全绿。
- Portal：`SendMessageStream` 走 `RunEvents`，输出 `tool_call`/`model_call` SSE；现有 stream 测试无回归；`go build ./...` 通过。
- Web：时间线渲染（方案 C + 分标签），reducer/verbMap 单测全绿；`tsc --noEmit` 与 `npm run build` 通过。
- 手动 E2E（Task 10）全部核对通过。
