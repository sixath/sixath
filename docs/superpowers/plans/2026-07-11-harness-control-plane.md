# Harness 控制面脊柱（Phase 0）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 Phase 0 控制面：可 block 的 `ToolHook`、Permission 固定顺序、`HookBlocked` 事件，以及 `on_chat_session_end` 钩子表面（Portal `DeleteSession` 触发）；空 Hook 时行为与今日一致。

**Architecture:** 在 `executeOneToolCall` 内按规格插入 `Before → Permission → Execute → After`；Hook block 以 **tool 结果回写模型**（`return record, nil` + `blocked=true`），不走 `ErrToolPermissionDenied` 中断路径。ChatSession 钩子放在 `framework/agent` 轻量 registry，由 Portal `DeleteSession` 调用；Growth 迁入该钩子属 Phase 1（G2），本 Phase 只留接口与调用点。

**Tech Stack:** Go；`framework/agent`、`framework/events`；Portal `internal/service/chat.go` + `internal/biz`（若 DeleteSession 在 usecase）。

**Spec:** `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md`（C1–C3、A1–A4）

> **Git 说明**：若工作区已初始化 git 则按步提交；否则跳过 Commit 步。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| Create `framework/agent/tool_hook.go` | `ToolHook` 接口、`runToolHooksBefore` / `runToolHooksAfter`、`ErrToolHookBlocked` |
| Create `framework/agent/tool_hook_test.go` | Hook 顺序、block、args 变换单测（纯函数级） |
| Create `framework/agent/chat_session_hook.go` | `ChatSessionHook`、`ChatSessionHookRegistry`、`OnChatSessionEnd` |
| Create `framework/agent/chat_session_hook_test.go` | 注册顺序、单 hook 失败不阻断后续、空 registry |
| Modify `framework/events/event.go` | 新增 `HookBlocked Kind` |
| Modify `framework/events/bus_test.go`（或小测） | Kind 常量存在 |
| Modify `framework/agent/trace.go` | `ToolCallRecord.Blocked`；`StreamEventHookBlocked` |
| Modify `framework/agent/react_agent.go` | `ToolHooks`、`executeOneToolCall`、`toolMessageContent`；**stream 分支优先判 `record.Blocked`** |
| Modify `framework/agent/react_agent_test.go` | block 不 Execute；`blocked:true`；Bus `HookBlocked`；空 hooks 回归 |
| Modify `portal/internal/service/chat.go` | `DeleteSession` 调 registry；**SSE switch 处理 `StreamEventHookBlocked`** |
| Create `portal/internal/service/chat_session_hook_wiring_test.go` | DeleteSession 触发 hook |
| Modify Portal wire（`cmd`/`wire.go`/`wire_gen.go` 以仓库实际为准） | 注入 `*agent.ChatSessionHookRegistry` |
| Modify `framework/docs/design-agent-runtime-hermes-inspired.md` §6.3 | After 同序；block→tool 消息 + HookBlocked |

**不在本计划：** EvidenceGate、CDP、G1/G2 Growth 迁入、并行 tool、C4/C5。

---

### Task 1: 事件 Kind + ToolCallRecord.Blocked

**Files:**
- Modify: `framework/events/event.go`
- Modify: `framework/agent/trace.go`
- Test: `framework/events/bus_test.go`（或新建 `framework/events/kind_test.go`）

- [x] **Step 1: 写失败测试（Kind 常量可读）**

在 `framework/events/bus_test.go` 追加：

```go
func TestHookBlockedKind(t *testing.T) {
	if HookBlocked != "agent.hook.blocked" {
		t.Fatalf("HookBlocked=%q", HookBlocked)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./events/ -run TestHookBlockedKind -v`  
Expected: FAIL undefined `HookBlocked`

- [ ] **Step 3: 实现 Kind + Record 字段**

`framework/events/event.go` 在 `PermissionDenied` 旁增加：

```go
HookBlocked Kind = "agent.hook.blocked"
```

`framework/agent/trace.go` 的 `ToolCallRecord` 增加：

```go
Blocked bool `json:"blocked,omitempty"`
```

`StreamEventType` 增加：

```go
StreamEventHookBlocked StreamEventType = "hook_blocked"
```

- [ ] **Step 4: 跑通测试**

Run: `cd framework && go test ./events/ -run TestHookBlockedKind -v`  
Expected: PASS

- [ ] **Step 5: Commit（若有 git）**

```bash
git add framework/events/event.go framework/events/bus_test.go framework/agent/trace.go
git commit -m "feat(agent): add HookBlocked event kind and ToolCallRecord.Blocked"
```

---

### Task 2: ToolHook 接口与 Before/After 辅助函数

**Files:**
- Create: `framework/agent/tool_hook.go`
- Create: `framework/agent/tool_hook_test.go`

- [ ] **Step 1: 写失败测试**

```go
package agent

import (
	"context"
	"errors"
	"testing"
)

type recordingHook struct {
	name   string
	order  *[]string
	before func(ctx context.Context, toolName string, args map[string]any) (map[string]any, error)
	after  func(ctx context.Context, toolName string, result any, err error) (any, error)
}

func (h *recordingHook) Before(ctx context.Context, toolName string, args map[string]any) (map[string]any, error) {
	*h.order = append(*h.order, h.name+":before")
	if h.before != nil {
		return h.before(ctx, toolName, args)
	}
	return args, nil
}

func (h *recordingHook) After(ctx context.Context, toolName string, result any, err error) (any, error) {
	*h.order = append(*h.order, h.name+":after")
	if h.after != nil {
		return h.after(ctx, toolName, result, err)
	}
	return result, err
}

func TestRunToolHooksBefore_orderAndArgsChain(t *testing.T) {
	var order []string
	h1 := &recordingHook{name: "h1", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		out := map[string]any{"x": 1}
		for k, v := range args {
			out[k] = v
		}
		out["from"] = "h1"
		return out, nil
	}}
	h2 := &recordingHook{name: "h2", order: &order, before: func(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
		if args["from"] != "h1" {
			t.Fatalf("expected h1 chain, got %#v", args)
		}
		args["from"] = "h2"
		return args, nil
	}}
	out, err := runToolHooksBefore(context.Background(), []ToolHook{h1, h2}, "demo", map[string]any{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if out["from"] != "h2" {
		t.Fatalf("got %#v", out)
	}
	want := []string{"h1:before", "h2:before"}
	if len(order) != 2 || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order=%v", order)
	}
}

func TestRunToolHooksBefore_block(t *testing.T) {
	var denyOrder []string
	h := &recordingHook{name: "deny", order: &denyOrder, before: func(context.Context, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("not allowed by policy hook")
	}}
	_, err := runToolHooksBefore(context.Background(), []ToolHook{h}, "demo", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunToolHooksAfter_sameOrderAsBefore(t *testing.T) {
	var order []string
	h1 := &recordingHook{name: "h1", order: &order}
	h2 := &recordingHook{name: "h2", order: &order}
	_, err := runToolHooksAfter(context.Background(), []ToolHook{h1, h2}, "demo", "ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "h1:after" || order[1] != "h2:after" {
		t.Fatalf("After must be same order as Before, got %v", order)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./agent/ -run 'TestRunToolHooks' -v`  
Expected: FAIL undefined `ToolHook` / `runToolHooksBefore`

- [ ] **Step 3: 最小实现**

`framework/agent/tool_hook.go`：

```go
package agent

import (
	"context"
	"errors"
	"fmt"
)

// ErrToolHookBlocked Before 拒绝执行工具（与 PermissionDenied 区分：回写模型 tool 消息，不中断整步为 RunError）。
var ErrToolHookBlocked = errors.New("tool hook blocked")

// ToolHook 工具生命周期钩子（harness 控制面；设计 §3.2 / runtime §6.3）。
// Middleware 负责 HTTP/Agent 外围；本接口只服务单次 tool 调用。
type ToolHook interface {
	Before(ctx context.Context, name string, params map[string]any) (map[string]any, error)
	After(ctx context.Context, name string, result any, err error) (any, error)
}

func runToolHooksBefore(ctx context.Context, hooks []ToolHook, name string, params map[string]any) (map[string]any, error) {
	out := params
	if out == nil {
		out = map[string]any{}
	}
	for _, h := range hooks {
		if h == nil {
			continue
		}
		next, err := h.Before(ctx, name, out)
		if err != nil {
			return out, fmt.Errorf("%w: %v", ErrToolHookBlocked, err)
		}
		if next != nil {
			out = next
		}
	}
	return out, nil
}

// runToolHooksAfter After 与 Before **同序**（规格写死）。
func runToolHooksAfter(ctx context.Context, hooks []ToolHook, name string, result any, execErr error) (any, error) {
	out := result
	err := execErr
	for _, h := range hooks {
		if h == nil {
			continue
		}
		var nextErr error
		out, nextErr = h.After(ctx, name, out, err)
		err = nextErr
	}
	return out, err
}
```

- [ ] **Step 4: 跑通测试**

Run: `cd framework && go test ./agent/ -run 'TestRunToolHooks' -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add framework/agent/tool_hook.go framework/agent/tool_hook_test.go
git commit -m "feat(agent): add ToolHook Before/After helpers with same-order After"
```

---

### Task 3: ReAct 接入 ToolHooks（C1 + C3 + A1/A4）

**Files:**
- Modify: `framework/agent/react_agent.go`（config、`WithReActToolHooks`、`executeOneToolCall`、`toolMessageContent`、stream 两处 eventType）
- Modify: `framework/agent/react_agent_test.go`
- Modify: `portal/internal/service/chat.go`（SSE `switch` 增加 `StreamEventHookBlocked`）

**顺序写死（对照 spec §3.2）：**

1. 解析 / hypertool 展开（现有）  
2. `tools.Get`  
3. **`runToolHooksBefore`** → 失败则：`record.Blocked=true`、`record.Error=...`、`record.Allowed=false`、emit `HookBlocked`、**`return record, nil`**（模型可见）  
4. **`permissionPolicy().AllowTool`**（现有；仍可返回 `ErrToolPermissionDenied`）  
5. `Execute`  
6. **`runToolHooksAfter`**（用执行后的 result/err）  
7. emit Completed/Failed；成功则现有 `ToolSuccessHook`

- [ ] **Step 1: 写失败集成测试（必须覆盖完成定义）**

```go
func TestReActAgent_ToolHookBeforeBlock_DoesNotExecute(t *testing.T) {
	executed := false
	reg := tool.NewRegistry()
	_ = reg.Register(tool.Tool{
		Name:        "spy_tool",
		Description: "spy",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			executed = true
			return "should not run", nil
		},
	})
	// fake：第一步 tool_call spy_tool，第二步纯文本（复制 ToolSuccessHook 测试的 fake 模式）
	var blockOrder []string
	hook := &recordingHook{name: "block", order: &blockOrder, before: func(context.Context, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("blocked by test")
	}}
	bus := events.NewBus()
	var sawHookBlocked bool
	bus.Subscribe(false, func(ctx context.Context, ev events.Event) {
		if ev.Kind == events.HookBlocked {
			sawHookBlocked = true
		}
	})
	react := NewReActAgent(fake, nil, reg,
		WithReActToolHooks(hook),
		WithReActEventBus(bus),
		WithReActMaxSteps(3),
	)
	resp, err := react.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if executed {
		t.Fatal("Execute must not run when Before blocks")
	}
	if !sawHookBlocked {
		t.Fatal("expected events.HookBlocked on Bus")
	}
	// 从 Metadata["trace"].(*RunTrace) 取 ToolCalls（Response 无 Trace 字段；对照现有测试）
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil {
		t.Fatal("expected RunTrace in Metadata")
	}
	found := false
	for _, rec := range tr.ToolCalls {
		if rec.ToolName != "spy_tool" {
			continue
		}
		found = true
		if !rec.Blocked {
			t.Fatal("expected Blocked=true")
		}
		content := toolMessageContent(rec)
		if !strings.Contains(content, `"blocked":true`) && !strings.Contains(content, `"blocked": true`) {
			t.Fatalf("tool message must include blocked:true, got %s", content)
		}
	}
	if !found {
		t.Fatal("expected spy_tool in Trace.ToolCalls")
	}
}

func TestReActAgent_RunEvents_HookBlockedNotPermissionDenied(t *testing.T) {
	// RunEvents 路径：收到 StreamEventHookBlocked，且不得仅出现 PermissionDenied 冒充 hook block
	// 构造同上 spy + blocking hook；消费 channel，断言 types 含 StreamEventHookBlocked
}

func TestReActAgent_EmptyToolHooks_SameAsBaseline(t *testing.T) {
	// 同一 fake + registry：无 WithReActToolHooks vs WithReActToolHooks() 零参数
	// 两者均成功执行计算器类工具；Trace 中该工具 Error=="" 且 Blocked==false
}
```

**Stream 分支改法（必须写进 Step 3）：** 在 `runToolEventsSync` / `runToolEvents` 里，把现有：

```go
if !record.Allowed {
    eventType = StreamEventPermissionDenied
} else if record.Error != "" {
```

改为：

```go
if record.Blocked {
    eventType = StreamEventHookBlocked
} else if !record.Allowed {
    eventType = StreamEventPermissionDenied
} else if record.Error != "" {
```

**Portal SSE（本 Task 一并改，避免死常量）：** `portal/internal/service/chat.go` 约 640 行：

```go
case agent.StreamEventToolFailed, agent.StreamEventPermissionDenied:
```

改为同时包含 `agent.StreamEventHookBlocked`（与 ToolFailed 同等映射到前端 tool 失败/提示；payload 可带 `blocked: true`）。

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./agent/ -run 'TestReActAgent_ToolHookBeforeBlock|TestReActAgent_RunEvents_HookBlocked|TestReActAgent_EmptyToolHooks' -v`  
Expected: FAIL（API 未实现或断言失败）

- [ ] **Step 3: 改 react_agent.go + Portal switch**

在 `reActConfig` 增加 `ToolHooks []ToolHook` 与 `WithReActToolHooks`。

`executeOneToolCall`：`Get` 成功后、Permission **之前**调用 `runToolHooksBefore`；block 时：

```go
	record.Blocked = true
	record.Allowed = false
	record.Error = hookErr.Error()
	record.Decision = "hook_blocked"
	emit(events.HookBlocked, map[string]any{...})
	return record, nil
```

`Execute` 后调用 `runToolHooksAfter`；`toolMessageContent` 在 `Blocked` 时写 `payload["blocked"]=true`。

两处 stream 分支按上文优先判 `record.Blocked`。

Portal `chat.go` SSE switch 加入 `StreamEventHookBlocked`。

- [ ] **Step 4: 跑 agent + 编译 portal**

```bash
cd framework && go test ./agent/ -count=1
cd portal && go test ./internal/service/ -count=1
```

Expected: PASS（至少 agent 全绿；portal 若 DeleteSession 未改仍可通过编译）

- [ ] **Step 5: Commit**

```bash
git add framework/agent/react_agent.go framework/agent/react_agent_test.go portal/internal/service/chat.go
git commit -m "feat(agent): wire ToolHooks; emit HookBlocked distinct from PermissionDenied"
```

---

### Task 4: ChatSessionHook registry（C2 表面）

**Files:**
- Create: `framework/agent/chat_session_hook.go`
- Create: `framework/agent/chat_session_hook_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestChatSessionHookRegistry_OnEndOrder(t *testing.T) {
	var order []string
	r := NewChatSessionHookRegistry()
	r.Register(ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		order = append(order, "a:"+sessionID)
		return nil
	}))
	r.Register(ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		order = append(order, "b:"+sessionID)
		return nil
	}))
	if err := r.OnChatSessionEnd(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "a:s1" || order[1] != "b:s1" {
		t.Fatalf("%v", order)
	}
}

func TestChatSessionHookRegistry_HookErrorDoesNotStopOthers(t *testing.T) {
	var saw bool
	r := NewChatSessionHookRegistry()
	r.Register(ChatSessionHookFunc(func(context.Context, string) error {
		return errors.New("boom")
	}))
	r.Register(ChatSessionHookFunc(func(context.Context, string) error {
		saw = true
		return nil
	}))
	err := r.OnChatSessionEnd(context.Background(), "s")
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !saw {
		t.Fatal("later hooks must still run")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd framework && go test ./agent/ -run TestChatSessionHookRegistry -v`  
Expected: FAIL undefined

- [ ] **Step 3: 实现**

```go
package agent

import (
	"context"
	"errors"
	"sync"
)

// ChatSessionHook ChatSession 结束时回调（规格 §3.1；非 AgentRun、非 BrowserSession）。
type ChatSessionHook interface {
	OnChatSessionEnd(ctx context.Context, sessionID string) error
}

type ChatSessionHookFunc func(ctx context.Context, sessionID string) error

func (f ChatSessionHookFunc) OnChatSessionEnd(ctx context.Context, sessionID string) error {
	return f(ctx, sessionID)
}

type ChatSessionHookRegistry struct {
	mu    sync.Mutex
	hooks []ChatSessionHook
}

func NewChatSessionHookRegistry() *ChatSessionHookRegistry {
	return &ChatSessionHookRegistry{}
}

func (r *ChatSessionHookRegistry) Register(h ChatSessionHook) {
	if r == nil || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, h)
}

// OnChatSessionEnd 依次调用；单 hook 失败不阻止后续；返回 errors.Join。
func (r *ChatSessionHookRegistry) OnChatSessionEnd(ctx context.Context, sessionID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	hooks := append([]ChatSessionHook(nil), r.hooks...)
	r.mu.Unlock()
	var errs []error
	for _, h := range hooks {
		if h == nil {
			continue
		}
		if err := h.OnChatSessionEnd(ctx, sessionID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 4: 跑通**

Run: `cd framework && go test ./agent/ -run TestChatSessionHookRegistry -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add framework/agent/chat_session_hook.go framework/agent/chat_session_hook_test.go
git commit -m "feat(agent): add ChatSessionHookRegistry for on_chat_session_end"
```

---

### Task 5: Portal DeleteSession 触发 ChatSession 钩子（C2 接线）

**Files:**
- Modify: `portal/internal/service/chat.go`（字段 + `DeleteSession`；SSE 若 Task 3 未改则本 Task 补齐）
- Modify: `NewChatService` / wire Provider（搜索实际构造点；改签名后执行 `wire` 或手改 `wire_gen.go`）
- Test: `portal/internal/service/chat_session_hook_wiring_test.go`

> A2（CheckFn/catalog）与 A3（confirm kind）**本 Phase 不改代码**——仅保证不破坏；标题勿与 C2 混淆。

- [ ] **Step 1: 读现有 DeleteSession 与 ChatService 构造 / wire**

确认 `DeleteSession`（约 `chat.go:159`）、`NewChatService`、是否存在 `wire.go`。若有 wire，计划改 Provider 并重新生成。

- [ ] **Step 2: 写失败测试（spy）**

```go
func TestDeleteSession_InvokesChatSessionEndHooks(t *testing.T) {
	// 最小 ChatService：chatUC mock 删除成功；注入 registry + spy
	// DeleteSession 后 spy 收到 session id
}
```

- [ ] **Step 3: 实现接线**

```go
// ChatService 增加：
sessionHooks *agent.ChatSessionHookRegistry

// DeleteSession：chatUC.DeleteSession 成功之后：
if s.sessionHooks != nil {
	if herr := s.sessionHooks.OnChatSessionEnd(ctx, req.GetId()); herr != nil {
		s.log.Warnf("chat session end hooks: session_id=%s err=%v", req.GetId(), herr)
	}
}
```

钩子失败**不**导致 DeleteSession API 失败。

- [ ] **Step 4: 跑 portal 测试**

Run: `cd portal && go test ./internal/service/ -run TestDeleteSession_InvokesChatSessionEndHooks -v`  
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/service/chat.go portal/internal/service/chat_session_hook_wiring_test.go
# 含 NewChatService / wire_gen.go
git commit -m "feat(portal): invoke ChatSessionHookRegistry on DeleteSession"
```

---

### Task 6: 文档交叉引用 + 全量回归

**Files:**
- Modify: `framework/docs/design-agent-runtime-hermes-inspired.md` §6.3（After 同序；block→tool 消息；指向 harness gap spec）
- Modify: `docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md` 状态行可标「Phase 0 plan ready」

- [ ] **Step 1: 更新 runtime 设计 §6.3 两句**

写明：After 与 Before **同序**；Before error → `blocked=true` tool 消息 + `events.HookBlocked`；Permission 仍在 Before 之后。

- [ ] **Step 2: 全量相关测试**

```bash
cd framework && go test ./agent/ ./events/ -count=1
cd portal && go test ./internal/service/ -count=1
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add framework/docs/design-agent-runtime-hermes-inspired.md docs/superpowers/specs/2026-07-11-harness-engineering-gap-design.md
git commit -m "docs: align runtime hook order with harness control-plane Phase 0"
```

---

## 完成定义（对照 spec Phase 0）

| ID | 验收 |
|----|------|
| C1 | Before block → Execute 未调用；模型 tool 消息含 `blocked: true` |
| C2 | `ChatSessionHookRegistry` 存在；`DeleteSession` 成功后调用 |
| C3 | 代码顺序 Before → Permission → Execute → After |
| A1 | Permission / Guardrails / confirm 行为不变（回归绿） |
| A4 | `HookBlocked` 事件可被 Bus 订阅 |
| 空 Hook | 与改造前一致 |

**下一步（不在本计划）：** Phase 1 EvidenceGate + G2 把 Growth session-end 注册进 registry；Phase 2 CDP。
