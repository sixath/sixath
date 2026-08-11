# wecom_bot 企微卡片进度反馈 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `wecom_bot` 长 Turn 的企微 stream 卡从静态「处理中…」改为每 5 秒刷新的进度文案（耗时/阶段/工具/步数），结束后整卡换成最终答案或失败卡。

**Architecture:** Gateway 在 `HandleWecomMsgCallback` 的业务 Turn 路径改用 `Runtime.TurnsStream`；本地 `ProgressState` 消费 SSE；`time.Ticker(5s)`（测试可注入更短间隔）节流 `RespondStream(finish=false)`；终态停 ticker 后 `finish=true` 终卡。Portal/Web 不改。

**Tech Stack:** Go、`gateway/internal/adapter`、`runtimeclient.TurnsStream`、Portal SSE（`tool_call`/`model_call`/`chunk`/`error`/`done`/`input_required`/`confirm_required`）

**Spec:** `docs/superpowers/specs/2026-08-11-wecom-bot-progress-card-design.md`

---

## File map

| File | Responsibility |
|------|----------------|
| `gateway/internal/adapter/wecom_progress.go` | `ProgressState`、`ApplySSEEvent`、`FormatProgressText`、SSE 扫描、`consumeWecomTurnStream` |
| `gateway/internal/adapter/wecom_progress_test.go` | **`package adapter`**（白盒）：文案、映射、去重、consume 节流/HITL/空正文/ctx 取消 |
| `gateway/internal/adapter/wecom_bot.go` | `WecomBotDeps.ProgressTick`；长 Turn 改走 stream+progress；保留回调开头「处理中…」 |
| `gateway/internal/adapter/wecom_bot_test.go` | **`package adapter_test`**：业务 Turn mock 改为 SSE；断言进度/终卡；HITL E2E；快路径 |
| `gateway/internal/adapter/wecom_bot_test.go` helper | `writeRuntimeSSEOK` / `writeRuntimeSSEError` 等 |

**不改：** Portal、Web、其它 adapter；不新增 progress API。

---

### Task 1: ProgressState + FormatProgressText（TDD）

**Files:**
- Create: `gateway/internal/adapter/wecom_progress.go`
- Create: `gateway/internal/adapter/wecom_progress_test.go`

- [ ] **Step 1: 写失败测试**

```go
package adapter // same package as wecom_progress.go (white-box)

import (
	"strings"
	"testing"
	"time"
)

func TestFormatProgressText_Skeleton(t *testing.T) {
	st := NewProgressState(time.Unix(0, 0))
	got := FormatProgressText(st, time.Unix(0, 0))
	for _, want := range []string{"处理中…", "耗时 00:00", "阶段 思考中", "工具 —", "已完成 0 步"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestFormatProgressText_ElapsedAndFields(t *testing.T) {
	st := NewProgressState(time.Unix(100, 0))
	st.Stage = "调用工具"
	st.ToolName = "kubectl_logs"
	st.StepsDone = 2
	got := FormatProgressText(st, time.Unix(142, 0))
	if !strings.Contains(got, "耗时 00:42") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "工具 kubectl_logs") || !strings.Contains(got, "已完成 2 步") {
		t.Fatalf("got %q", got)
	}
}
```

> **测试包约定：** `wecom_progress_test.go` 一律 `package adapter`，以便直接测未导出的 `consumeWecomTurnStream`。`wecom_bot_test.go` 继续 `package adapter_test`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/adapter/ -run TestFormatProgressText -count=1`

Working directory: `gateway`

Expected: FAIL（undefined / not found）

- [ ] **Step 3: 最小实现**

在 `wecom_progress.go`（package `adapter`）：

```go
const (
	progressStageThinking = "思考中"
	progressStageTool     = "调用工具"
	progressStageReply    = "生成回复"
)

type ProgressState struct {
	StartedAt time.Time
	Stage     string
	ToolName  string
	StepsDone int
	Failed    bool
	HITL      bool
	ErrMsg    string

	doneToolIDs    map[string]struct{}
	doneModelSteps map[int]struct{}
	toolInFlight   bool
}

func NewProgressState(startedAt time.Time) *ProgressState {
	return &ProgressState{
		StartedAt:      startedAt,
		Stage:          progressStageThinking,
		doneToolIDs:    map[string]struct{}{},
		doneModelSteps: map[int]struct{}{},
	}
}

func FormatProgressText(st *ProgressState, now time.Time) string {
	tool := st.ToolName
	if tool == "" {
		tool = "—"
	}
	d := now.Sub(st.StartedAt)
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	mm, ss := sec/60, sec%60
	return fmt.Sprintf("处理中…\n耗时 %02d:%02d\n阶段 %s\n工具 %s\n已完成 %d 步",
		mm, ss, st.Stage, tool, st.StepsDone)
}
```

> 正文聚合放在 `consumeWecomTurnStream` 局部 `strings.Builder`，不要把 `chunk` 文本塞进进度文案。

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/adapter/ -run TestFormatProgressText -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/adapter/wecom_progress.go gateway/internal/adapter/wecom_progress_test.go
git commit -m "feat(gateway): add wecom progress card text formatter"
```

---

### Task 2: ApplySSEEvent 映射与去重（TDD）

**Files:**
- Modify: `gateway/internal/adapter/wecom_progress.go`
- Modify: `gateway/internal/adapter/wecom_progress_test.go`

- [ ] **Step 1: 写失败测试（严格按 spec §3.2）**

```go
func TestProgressState_ApplySSE_ToolAndModel(t *testing.T) {
	st := NewProgressState(time.Now())
	st.ApplySSEEvent("model_call", []byte(`{"model_call":{"step":0,"phase":"invoked","model":"m"}}`))
	if st.Stage != "思考中" {
		t.Fatalf("stage=%q", st.Stage)
	}
	st.ApplySSEEvent("tool_call", []byte(`{"tool_call":{"id":"c1","step":1,"phase":"started","tool_name":"kubectl_logs"}}`))
	if st.Stage != "调用工具" || st.ToolName != "kubectl_logs" {
		t.Fatalf("%+v", st)
	}
	st.ApplySSEEvent("model_call", []byte(`{"model_call":{"step":2,"phase":"invoked"}}`))
	if st.Stage != "调用工具" {
		t.Fatalf("stage=%q want 调用工具", st.Stage)
	}
	st.ApplySSEEvent("tool_call", []byte(`{"tool_call":{"id":"c1","step":1,"phase":"completed","tool_name":"kubectl_logs"}}`))
	if st.StepsDone != 1 || st.ToolName != "kubectl_logs" {
		t.Fatalf("steps=%d tool=%q", st.StepsDone, st.ToolName)
	}
	st.ApplySSEEvent("tool_call", []byte(`{"tool_call":{"id":"c1","step":1,"phase":"completed","tool_name":"kubectl_logs"}}`))
	if st.StepsDone != 1 {
		t.Fatalf("duplicate completed counted: %d", st.StepsDone)
	}
	st.ApplySSEEvent("model_call", []byte(`{"model_call":{"step":0,"phase":"responded"}}`))
	if st.StepsDone != 2 {
		t.Fatalf("steps=%d", st.StepsDone)
	}
	if st.Stage != "思考中" {
		t.Fatalf("after responded stage=%q", st.Stage)
	}
	st.ApplySSEEvent("chunk", []byte(`{"content":"你好"}`))
	if st.Stage != "生成回复" {
		t.Fatalf("stage=%q", st.Stage)
	}
}

const hitlNoSurfaceMsg = "hitl required but reply_mode=final has no interactive surface"

func TestProgressState_ApplySSE_HITLAndError(t *testing.T) {
	st := NewProgressState(time.Now())
	st.ApplySSEEvent("confirm_required", []byte(`{"confirmation":{}}`))
	if !st.Failed || !st.HITL || st.ErrMsg != hitlNoSurfaceMsg {
		t.Fatalf("%+v", st)
	}
	st2 := NewProgressState(time.Now())
	st2.ApplySSEEvent("error", []byte(`{"error":"boom"}`))
	if !st2.Failed || st2.ErrMsg != "boom" {
		t.Fatalf("%+v", st2)
	}
}
```

JSON 外壳必须与 Portal `WriteEvent` 一致：`tool_call` 事件的 data 为 `{"tool_call":{...}}`，`model_call` 同理；`chunk` 为 `{"content":"..."}`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/adapter/ -run TestProgressState_ApplySSE -count=1`

Expected: FAIL

- [ ] **Step 3: 实现 `ApplySSEEvent(event string, data []byte)`**

规则摘要：

| event | 行为 |
|-------|------|
| `model_call` invoked | 若 `toolInFlight` 则保持「调用工具」，否则「思考中」 |
| `tool_call` started | `toolInFlight=true`；阶段「调用工具」；设 `ToolName` |
| `tool_call` completed | 按 `id` 去重 `StepsDone++`；`toolInFlight=false`；保留 `ToolName` |
| `model_call` responded | 按 `step` 去重 `StepsDone++`；若阶段不是「生成回复」则「思考中」 |
| `chunk` 非空 content | 阶段「生成回复」（正文聚合放在 consume 层） |
| `input_required` / `confirm_required` | `Failed=HITL=true`；`ErrMsg` 固定为 `hitl required but reply_mode=final has no interactive surface`（与 Portal `AggregateFinal` 一致） |
| `error` | `Failed=true`；解析 `error` 字段 |
| `done` / 其它 | 忽略（consume 层看 `done` 结束读循环） |

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/adapter/ -run TestProgressState_ApplySSE -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/adapter/wecom_progress.go gateway/internal/adapter/wecom_progress_test.go
git commit -m "feat(gateway): map Portal SSE events into wecom ProgressState"
```

---

### Task 3: SSE 扫描 + consume + 5s 节流推卡（TDD）

**Files:**
- Modify: `gateway/internal/adapter/wecom_progress.go`
- Modify: `gateway/internal/adapter/wecom_progress_test.go`
- Modify: `gateway/internal/adapter/wecom_bot.go`（仅加 `ProgressTick` 字段到 `WecomBotDeps`，若 consume 需要）

- [ ] **Step 1: 写失败测试 — 节流、空正文、HITL、ctx 取消**

`wecom_progress_test.go` 内复用与 `wecom_bot_test.go` 同结构的轻量 fake（同 package 可直接定义）：

```go
type progressFakeConn struct {
	mu    sync.Mutex
	calls []respondCall
}

func (f *progressFakeConn) RespondStream(_ context.Context, reqID, streamID, content string, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, respondCall{reqID: reqID, streamID: streamID, content: content, finish: finish})
	return nil
}

func (f *progressFakeConn) snapshot() []respondCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]respondCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestConsumeWecomTurnStream_ThrottlesProgress(t *testing.T) {
	pr, pw := io.Pipe()
	conn := &progressFakeConn{}
	started := time.Now()
	done := make(chan wecomStreamTurnResult, 1)
	go func() {
		done <- consumeWecomTurnStream(context.Background(), conn, "req", "sid", pr, started, 20*time.Millisecond)
	}()

	// Keep stream open long enough for skeleton + >=1 tick, then finish.
	time.Sleep(55 * time.Millisecond)
	_, _ = io.WriteString(pw, "event: chunk\ndata: {\"content\":\"晴\"}\n\n")
	_, _ = io.WriteString(pw, "event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n")
	_ = pw.Close()

	res := <-done
	if res.Failed || res.Content != "晴" {
		t.Fatalf("res=%+v", res)
	}
	calls := conn.snapshot()
	progress := 0
	for _, c := range calls {
		if c.finish {
			t.Fatalf("consume must not finish card: %+v", c)
		}
		if strings.Contains(c.content, "耗时") {
			progress++
		}
	}
	if progress < 2 {
		t.Fatalf("progress pushes=%d want >=2 (skeleton+tick): %+v", progress, calls)
	}
}

func TestConsumeWecomTurnStream_EmptyContentFails(t *testing.T) {
	body := strings.NewReader("event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n")
	conn := &progressFakeConn{}
	res := consumeWecomTurnStream(context.Background(), conn, "req", "sid", body, time.Now(), time.Hour)
	if !res.Failed || res.ErrMsg == "" {
		t.Fatalf("want failure, got %+v", res)
	}
}

func TestConsumeWecomTurnStream_HITLFails(t *testing.T) {
	body := strings.NewReader(
		"event: confirm_required\ndata: {\"confirmation\":{\"token\":\"t\"}}\n\n" +
			"event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n",
	)
	conn := &progressFakeConn{}
	res := consumeWecomTurnStream(context.Background(), conn, "req", "sid", body, time.Now(), time.Hour)
	if !res.Failed || res.ErrMsg != hitlNoSurfaceMsg {
		t.Fatalf("%+v", res)
	}
}

func TestConsumeWecomTurnStream_ContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	conn := &progressFakeConn{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan wecomStreamTurnResult, 1)
	go func() {
		done <- consumeWecomTurnStream(ctx, conn, "req", "sid", pr, time.Now(), time.Hour)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	res := <-done
	if !res.Failed {
		t.Fatalf("want failed on cancel, got %+v", res)
	}
}
```

若 `respondCall` 仅在 `wecom_bot_test.go`（`adapter_test`）里，则在 `wecom_progress_test.go` 内再定义同名字段的本地 struct，或把 `respondCall`/`fakeWecomConn` 挪到 `wecom_fakes_test.go`（`package adapter`）供两边用——**推荐**：Task 3 在 progress 测试文件内自包含 `progressFakeConn`，不跨包依赖。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/adapter/ -run TestConsumeWecomTurnStream -count=1`

Expected: FAIL

- [ ] **Step 3: 实现 scan + consume**

建议 API（未导出，同包测试）：

```go
type wecomStreamTurnResult struct {
	Content string
	Failed  bool
	ErrMsg  string
}

func consumeWecomTurnStream(
	ctx context.Context,
	conn WecomConn,
	reqID, streamID string,
	body io.Reader,
	startedAt time.Time,
	tick time.Duration,
) wecomStreamTurnResult
```

行为：

1. `tick<=0` → `5*time.Second`
2. 立刻 `RespondStream(..., FormatProgressText(...), false)`（骨架）
3. 启动 ticker；SSE 读循环在 goroutine，事件进 channel；主循环 `select`：事件 / tick / ctx.Done
4. SSE 只 `ApplySSEEvent`；`chunk` 额外追加到局部 `full strings.Builder`
5. tick：`RespondStream` 进度文案；失败只 `log.Printf`，不中断
6. 读到 `done` 或 reader EOF / ctx 取消 → stop ticker
7. 若 `Failed` 或 `strings.TrimSpace(full)==""` → `Failed=true` 与 `ErrMsg`
8. **本函数只推进度卡（finish=false），不推终卡**；终卡留在 `HandleWecomMsgCallback`

SSE 解析：按行 `event:` / `data:`，空行派发；忽略未知事件。

注意：

- `bufio.Scanner` 放 goroutine，避免阻塞 tick
- `ctx` 取消 → `Failed=true`，`ErrMsg` 来自 `ctx.Err().Error()`
- 中间进度推送失败：日志，继续
- 勿把 `tool_call`/`model_call`/`debug` 拼进 `Content`

- [ ] **Step 4: 测试通过**

Run: `go test ./internal/adapter/ -run TestConsumeWecomTurnStream -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/adapter/wecom_progress.go gateway/internal/adapter/wecom_progress_test.go gateway/internal/adapter/wecom_bot.go
git commit -m "feat(gateway): throttle wecom progress card updates from SSE stream"
```

---

### Task 4: 接线 `HandleWecomMsgCallback` + 更新现有 Turn mock

**Files:**
- Modify: `gateway/internal/adapter/wecom_bot.go`
- Modify: `gateway/internal/adapter/wecom_bot_test.go`

- [ ] **Step 1: 在 deps 增加可选 tick**

```go
type WecomBotDeps struct {
	// ...
	ProgressTick time.Duration // 0 => 5s；测试可设更短
}
```

- [ ] **Step 2: 替换 `TurnsFinal` 块**

将 Resolve 成功后的 `TurnsFinal` 改为：

```go
rc, _, err := deps.Runtime.TurnsStream(ctx, resolved.UserID, runtimeclient.TurnRequest{
	SessionID:      resolved.SessionID,
	Content:        n.RuntimeContent,
	ChannelID:      ch.ID,
	PeerID:         n.PeerID,
	CorrelationID:  corr,
	IdempotencyKey: n.MsgID,
})
if err != nil {
	failMsg := mapRuntimeUserError(err)
	_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
	deps.Idempotency.Complete(n.MsgID, failMsg)
	return
}
defer rc.Close()

res := consumeWecomTurnStream(ctx, conn, reqID, streamID, rc, time.Now(), deps.ProgressTick)
if res.Failed || strings.TrimSpace(res.Content) == "" {
	failMsg := res.ErrMsg
	if failMsg == "" {
		failMsg = "turn failed"
	}
	_ = conn.RespondStream(ctx, reqID, streamID, wecom.FormatFailureCard(n.AskerName, n.QuestionText, failMsg), true)
	deps.Idempotency.Complete(n.MsgID, failMsg)
	return
}
card := wecom.FormatReplyCard(n.AskerName, n.QuestionText, res.Content)
_ = conn.RespondStream(ctx, reqID, streamID, card, true)
deps.Idempotency.Complete(n.MsgID, card)
```

保留回调最前面的 `RespondStream(..., wecomProcessingContent, false)`（快路径不变）。

- [ ] **Step 3: 测试 helper — Portal SSE 响应**

```go
func writeRuntimeSSEOK(w http.ResponseWriter, answer string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(map[string]any{"content": answer})
	_, _ = io.WriteString(w, "event: chunk\ndata: "+string(payload)+"\n\n")
	_, _ = io.WriteString(w, "event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n")
}
```

所有走业务 Turn 的 mock（`TestWecomBot_TextTurn_ReplyCard`、`IdempotentMsgID`、`GroupPeer`、`TurnFailure`、`ExpiredPending_BusinessTurn` 等）在 `/runtime/v1/turns` 改为 SSE，不再 JSON `status/content`。

**特别注意：** `IdempotentMsgID` 等旧断言 `len(calls)==2` 必须改为成功路径 `len(calls)>=3`（处理中… + 进度骨架 + 终卡）；第二次同 msgid 仍为 0 次额外 turns / 无新 respond。

`TurnFailure`：返回 `event: error` + `done`，或非 2xx（保持 `TurnsStream` 错误路径）。

- [ ] **Step 4: 更新 `TextTurn` 断言**

长 Turn 成功路径响应序列至少：

1. `finish=false` 且 content 为「处理中…」（回调开头）
2. 一个或多个 `finish=false` 进度文案（含「耗时」）
3. `finish=true` 且 `FormatReplyCard(..., answer)`，**不含**「耗时」/「阶段」

```go
calls := conn.snapshot()
if len(calls) < 3 {
	t.Fatalf("respond calls=%d want >=3: %+v", len(calls), calls)
}
if calls[0].content != "处理中…" || calls[0].finish {
	t.Fatalf("first=%+v", calls[0])
}
last := calls[len(calls)-1]
if !last.finish || last.content != wantCard {
	t.Fatalf("last=%+v want %q", last, wantCard)
}
```

- [ ] **Step 5: 跑 adapter 全量测试并修到绿**

Run: `go test ./internal/adapter/ -count=1`

Working directory: `gateway`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add gateway/internal/adapter/wecom_bot.go gateway/internal/adapter/wecom_bot_test.go gateway/internal/adapter/wecom_progress.go
git commit -m "feat(gateway): stream wecom turns with progress card updates"
```

---

### Task 5: 回归 — 快路径无 ticker、HITL 失败卡

**Files:**
- Modify: `gateway/internal/adapter/wecom_bot_test.go`

- [ ] **Step 1: 快路径断言**

`TestWecomBot_AgentListCommand_NoTurn` 等：`turns==0`；仍可有「处理中…」+ 终卡；所有 `finish=false` 的 content 应等于「处理中…」或不含「耗时」（未走 stream progress）。

- [ ] **Step 2: 新增 HITL 端到端**

Portal `/runtime/v1/turns` 写：

```text
event: confirm_required
data: {"confirmation":{"token":"t"}}

event: done
data: {"done":true,"content":""}
```

断言终卡为 `FormatFailureCard`，且 turns 调用 1 次。

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/adapter/ -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add gateway/internal/adapter/
git commit -m "test(gateway): cover wecom progress HITL and slash-command paths"
```

---

### Task 6: 手工验证清单（不阻塞合并）

- [ ] 本地重启 Gateway（加载新二进制）
- [ ] 企微发一条会触发多工具的长问题
- [ ] 观察同一条卡片约每 5 秒更新耗时/阶段/工具/步数
- [ ] 结束后整卡为最终答案，无进度区块
- [ ] （可选）在 PR 描述勾选结果；无需改代码

---

## Self-review（作者核对）

| Spec 项 | Plan 覆盖 |
|---------|-----------|
| 仅 wecom_bot | ✅ 只改 gateway adapter |
| 耗时/阶段/工具/步数 | ✅ Task 1–2 |
| 固定 5s + 首帧骨架 | ✅ Task 3（`ProgressTick` 仅测试） |
| Gateway 聚合 SSE | ✅ Task 3–4 |
| 终卡替换进度 | ✅ Task 4 |
| HITL/空正文失败 | ✅ Task 3、5 |
| 快路径保持 processing | ✅ Task 4–5 |
| 非目标未引入 | ✅ 无 Portal/Web/可配置间隔 |

---

## Execution notes

- 工作目录跑测：`gateway/` 下 `go test ./internal/adapter/ -count=1`
- `TurnsStream` 已使用无 `Client.Timeout` 的 stream client；Turn 上限继续靠现有 `TurnTimeout` ctx
- 勿把 `tool_call`/`model_call`/`debug` 文本拼进终卡正文（只聚合 `chunk`）
