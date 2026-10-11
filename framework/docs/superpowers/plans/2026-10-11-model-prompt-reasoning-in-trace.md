# 执行轨迹记录模型提示词与推理实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让执行轨迹（turn_trace 持久化 + 实时时间线）每条模型调用可见提示词（完整 messages，裁剪脱敏）与推理（reasoning_content + 终答）。

**Architecture:** 在 `RunTrace.modelRespondedPayload`（唯一汇聚点，5 个调用点都带 `messages`）内新增 `recordModelCall`——记录 messages/推理/终答并写入 `RunTrace.ModelCalls`，同时丰富 `ModelResponded` 事件载荷（reasoning/answer/finish_reason，不含完整 prompt）。`BuildTurnTrace` 映射 `ModelCalls` 进 `TurnTrace`（随 payload_json 落库，无新列）。portal `modelCallEventFromBus` 读取新事件字段进 `ModelCallPayload`，`ApplyModelCall` 把 reasoning/answer/finish_reason 并入持久化时间线 `TimelineNode`；web 类型 + reducer + 组件展示。

**Tech Stack:** Go（framework harness + portal），TypeScript/React（web）。

---

### Task 1: RunTrace 新增 ModelCallRecord 数据结构（framework）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\framework\harness\trace.go`
- Test: `E:\workspace\github\sixath\sixath\framework\harness\trace_test.go`（新建）

- [ ] **Step 1: 写失败测试**

创建 `E:\workspace\github\sixath\sixath\framework\harness\trace_test.go`：

```go
package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sixath/framework/model"
)

func TestRunTrace_ModelCallRecord_JSON(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	tr.ModelCalls = append(tr.ModelCalls, ModelCallRecord{
		Step:         0,
		Mode:         "tools",
		Messages:     []TracedMessage{{Role: "user", Content: "hi"}},
		Reasoning:    "thinking step",
		Answer:       "final",
		FinishReason: "stop",
		InputTokens:  10,
		OutputTokens: 5,
	})
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	mc, ok := m["model_calls"].([]any)
	if !ok || len(mc) != 1 {
		t.Fatalf("expected model_calls array, got %#v", m["model_calls"])
	}
	first := mc[0].(map[string]any)
	if first["reasoning"] != "thinking step" || first["answer"] != "final" || first["finish_reason"] != "stop" {
		t.Fatalf("unexpected first call: %#v", first)
	}
}

func TestRunTrace_ModelCallRecord_EmptyOmit(t *testing.T) {
	b, err := json.Marshal(&RunTrace{RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "model_calls") {
		t.Fatalf("expected model_calls omitted when empty: %s", b)
	}
}

func TestRunTrace_RecordModelCall_ExtractsReasoningAndAnswer(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	messages := []model.Message{{Role: "user", Content: "hello"}}
	gen := &model.Generation{
		Text:          "ok",
		FinishReason:  "tool_calls",
		TokenUsage:    &model.TokenUsage{InputTokens: 3, OutputTokens: 2},
		Raw:           model.ToolStep{ReasoningContent: "chain", Used: true},
	}
	tr.recordModelCall(messages, gen, "tools", 0)

	if len(tr.ModelCalls) != 1 {
		t.Fatalf("expected 1 model call, got %d", len(tr.ModelCalls))
	}
	rec := tr.ModelCalls[0]
	if rec.Reasoning != "chain" || rec.Answer != "ok" || rec.FinishReason != "tool_calls" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.InputTokens != 3 || rec.OutputTokens != 2 {
		t.Fatalf("unexpected tokens: %+v", rec)
	}
	if rec.Step != 0 || rec.Mode != "tools" {
		t.Fatalf("unexpected step/mode: %+v", rec)
	}
	if len(rec.Messages) != 1 || rec.Messages[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", rec.Messages)
	}
}

func TestRunTrace_RecordModelCall_RedactsCredentials(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	messages := []model.Message{{Role: "tool", Content: `password=supersecret`}}
	gen := &model.Generation{Text: "done", Raw: model.ToolStep{}}
	tr.recordModelCall(messages, gen, "tools", 0)
	if strings.Contains(tr.ModelCalls[0].Messages[0].Content, "supersecret") {
		t.Fatalf("expected credential redacted: %q", tr.ModelCalls[0].Messages[0].Content)
	}
}

func TestRunTrace_RecordModelCall_TruncatesLongContent(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	long := strings.Repeat("x", 10_000)
	messages := []model.Message{{Role: "user", Content: long}}
	gen := &model.Generation{Text: "y"}
	tr.recordModelCall(messages, gen, "tools", 0)
	got := tr.ModelCalls[0].Messages[0].Content
	if len(got) > modelCallRecordContentLimit+8 {
		t.Fatalf("content not truncated: %d", len(got))
	}
}

func TestRunTrace_RecordModelCall_NilGen(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	tr.recordModelCall([]model.Message{{Role: "user", Content: "x"}}, nil, "tools", 1)
	if len(tr.ModelCalls) != 1 {
		t.Fatalf("expected 1 record even with nil gen, got %d", len(tr.ModelCalls))
	}
	if tr.ModelCalls[0].Answer != "" || tr.ModelCalls[0].Reasoning != "" {
		t.Fatalf("expected empty answer/reasoning for nil gen: %+v", tr.ModelCalls[0])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./harness/ -run ModelCall -v`
Expected: 编译失败（`ModelCallRecord`、`TracedMessage`、`recordModelCall`、`modelCallRecordContentLimit` 未定义）。

- [ ] **Step 3: 实现数据结构与记录方法**

在 `E:\workspace\github\sixath\sixath\framework\harness\trace.go` 顶部的 import 增加 `"github.com/sixath/framework/redact"` 与 `"github.com/sixath/framework/model"`（model 已导入）。在文件末尾追加：

```go
const (
	// modelCallRecordContentLimit 单条提示词消息内容 / 推理 / 终答的字节上限（UTF-8 安全截断）。
	modelCallRecordContentLimit = 2048
	// maxModelCallRecords 单次 Run 内保留的模型调用记录上限；超限时优先保留靠后调用。
	maxModelCallRecords = 20
)

// ModelCallRecord 记录一次模型调用：发送的提示词（裁剪+脱敏）+ 推理与终答。
type ModelCallRecord struct {
	Step         int              `json:"step"`
	Mode         string           `json:"mode,omitempty"`
	Messages     []TracedMessage  `json:"messages,omitempty"`
	Reasoning    string           `json:"reasoning,omitempty"`
	Answer       string           `json:"answer,omitempty"`
	FinishReason string           `json:"finish_reason,omitempty"`
	InputTokens  int              `json:"input_tokens,omitempty"`
	OutputTokens int              `json:"output_tokens,omitempty"`
}

// TracedMessage 序列化的一条提示词消息（裁剪+脱敏后）。
type TracedMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []TracedToolCall `json:"tool_calls,omitempty"`
}

// TracedToolCall 序列化的一个模型 tool_calls 提议（裁剪+脱敏后）。
type TracedToolCall struct {
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// recordModelCall 记录一次模型调用。messages 为发送的完整 prompt（裁剪+脱敏后保存）；
// gen 提供推理（ToolStep.ReasoningContent）、终答、finish_reason 与 token。gen 可为 nil。
func (t *RunTrace) recordModelCall(messages []model.Message, gen *model.Generation, mode string, step int) {
	if t == nil {
		return
	}
	rec := ModelCallRecord{Step: step, Mode: mode, Messages: traceMessages(messages)}
	if gen != nil {
		rec.FinishReason = gen.FinishReason
		rec.Answer = safeTruncate(redact.String(gen.Text), modelCallRecordContentLimit)
		if st, ok := gen.Raw.(model.ToolStep); ok && st.ReasoningContent != "" {
			rec.Reasoning = safeTruncate(redact.String(st.ReasoningContent), modelCallRecordContentLimit)
		}
		if gen.TokenUsage != nil {
			rec.InputTokens = gen.TokenUsage.InputTokens
			rec.OutputTokens = gen.TokenUsage.OutputTokens
		}
	}
	t.ModelCalls = append(t.ModelCalls, rec)
	if len(t.ModelCalls) > maxModelCallRecords {
		// 超限时保留靠后调用，丢最早。
		t.ModelCalls = t.ModelCalls[len(t.ModelCalls)-maxModelCallRecords:]
	}
}

func traceMessages(messages []model.Message) []TracedMessage {
	out := make([]TracedMessage, 0, len(messages))
	for _, m := range messages {
		tm := TracedMessage{Role: m.Role, Content: safeTruncate(redact.String(m.Content), modelCallRecordContentLimit)}
		if tc, ok := m.Metadata["tool_call_id"].(string); ok {
			tm.ToolCallID = tc
		}
		out = append(out, tm)
	}
	return out
}

// safeTruncate 按 UTF-8 边界截断到 maxBytes；不追加省略号。
func safeTruncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
```

在 trace.go 顶部 import 中追加 `"unicode/utf8"`。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd framework && go test ./harness/ -run ModelCall -v`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
cd framework
git add harness/trace.go harness/trace_test.go
git commit -m "feat(trace): record model prompt & reasoning into RunTrace"
```

---

### Task 2: 在 modelRespondedPayload 汇聚采集 + 丰富 ModelResponded 事件（framework）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\framework\harness\react_agent.go`
- Test: `E:\workspace\github\sixath\sixath\framework\harness\react_agent_events_token_test.go`（现有，追加用例）

- [ ] **Step 1: 写失败测试**

在 `E:\workspace\github\sixath\sixath\framework\harness\react_agent_events_token_test.go` 末尾追加：

```go
func TestModelRespondedPayload_IncludesReasoningAndAnswer(t *testing.T) {
	tr := &RunTrace{RequestID: "r1"}
	gen := newGeneration(model.ToolStep{Used: true, ReasoningContent: "because x"})
	gen.Text = "do the thing"
	p := tr.modelRespondedPayload(gen, 0, "tools", []model.Message{{Role: "user", Content: "go"}})
	if p["reasoning"] != "because x" {
		t.Fatalf("expected reasoning, got %#v", p)
	}
	if p["answer"] != "do the thing" {
		t.Fatalf("expected answer, got %#v", p)
	}
	if p["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason, got %#v", p)
	}
	if len(tr.ModelCalls) == 0 {
		t.Fatal("expected model call recorded")
	}
}

func newGeneration(step model.ToolStep) model.Generation {
	return model.Generation{Raw: step, FinishReason: "tool_calls", TokenUsage: &model.TokenUsage{InputTokens: 1, OutputTokens: 1}}
}

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./harness/ -run TestModelRespondedPayload_IncludesReasoningAndAnswer -v`
Expected: 编译失败（`modelRespondedPayload` 签名不匹配）。

- [ ] **Step 3: 改 modelRespondedPayload 签名并采集 + 丰富载荷**

在 `E:\workspace\github\sixath\sixath\framework\harness\react_agent.go` 把 `modelRespondedPayload` 改为接收 `messages`，内部记录模型调用并丰富载荷：

```go
// modelRespondedPayload 记录本次模型调用（含提示词与推理）并构造 ModelResponded 事件 payload。
// 挂在 RunTrace 上，使「聚合用量 / ModelCalls / 事件 payload」必然同源，不会漏记。
func (t *RunTrace) modelRespondedPayload(gen model.Generation, step int, mode string, messages []model.Message) map[string]any {
	t.recordModelUsage(&gen)
	t.recordModelCall(messages, &gen, mode, step)
	p := map[string]any{"text_length": len(gen.Text), "step": step}
	if st, ok := gen.Raw.(model.ToolStep); ok && st.ReasoningContent != "" {
		p["reasoning"] = safeTruncate(redact.String(st.ReasoningContent), modelCallRecordContentLimit)
	}
	if gen.FinishReason != "" {
		p["finish_reason"] = gen.FinishReason
	}
	if gen.Text != "" {
		p["answer"] = safeTruncate(redact.String(gen.Text), modelCallRecordContentLimit)
	}
	if gen.TokenUsage != nil {
		p["input_tokens"] = gen.TokenUsage.InputTokens
		p["output_tokens"] = gen.TokenUsage.OutputTokens
	}
	return p
}
```

同步更新全部 5 个调用点，各带 `mode` 与 `messages`：
- L456 主循环 tools → `trace.modelRespondedPayload(*gen, step, "tools", messages)`
- L756 runPlainStream 非流式 → `trace.modelRespondedPayload(*gen, -1, "plain", messages)`
- L818 runToolEventsSync → `trace.modelRespondedPayload(*gen, step, "tools", messages)`
- L941 流式 tools → `trace.modelRespondedPayload(*gen, step, "tools", messages)`
- L1322 runPlain → `trace.modelRespondedPayload(*gen, -1, "plain", messages)`

- [ ] **Step 4: 处理流式 plain 路径（L786）的 inline 记录**

在 `runPlainStream` 的流式分支（`sm.ChatStream`，无 `*Generation`）末尾，L786 处把 inline 的 `map[string]any` 换成也记录一次模型调用。由于该分支无 reasoning/toolstep，仅记录应答文本：

```go
genForTrace := &model.Generation{Text: text, FinishReason: "eof"}
t := trace
t.recordModelCall(messages, genForTrace, "plain_stream", -1)
emit(events.ModelResponded, map[string]any{
	"text_length":    len(text),
	"step":           -1,
	"answer":         safeTruncate(redact.String(text), modelCallRecordContentLimit),
	"finish_reason":  "eof",
})
```

（此处 `t := trace` 仅为让新增记录紧邻调用；`trace` 直接可用，去掉 `t := trace`。）

- [ ] **Step 5: 运行测试确认通过**

Run: `cd framework && go test ./harness/ -run ModelRespondedPayload -v`
Expected: PASS。再跑 `go build ./...` 确认编译通过。

- [ ] **Step 6: 提交**

```bash
cd framework
git add harness/react_agent.go harness/react_agent_events_token_test.go
git commit -m "feat(react): surface model reasoning/answer in events & record calls"
```

---

### Task 3: TurnTrace 映射 ModelCalls（framework）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\framework\harness\turn_trace.go`
- Test: `E:\workspace\github\sixath\sixath\framework\harness\turn_trace_test.go`

- [ ] **Step 1: 写失败测试**

在 `turn_trace_test.go` 追加：

```go
func TestBuildTurnTrace_MapsModelCalls(t *testing.T) {
	tr := &RunTrace{
		RequestID: "r1",
		ModelCalls: []ModelCallRecord{{
			Step: 0, Mode: "tools",
			Messages:  []TracedMessage{{Role: "user", Content: "ask"}},
			Reasoning: "chain", Answer: "answer", FinishReason: "stop",
			InputTokens: 4, OutputTokens: 3,
		}},
	}
	out := BuildTurnTrace(TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "r1"}, tr)
	if len(out.ModelCalls) != 1 {
		t.Fatalf("expected 1 model call in turn trace, got %d", len(out.ModelCalls))
	}
	m := out.ModelCalls[0]
	if m.Reasoning != "chain" || m.Answer != "answer" || m.FinishReason != "stop" {
		t.Fatalf("unexpected model call: %+v", m)
	}
	if len(m.Messages) != 1 || m.Messages[0].Content != "ask" {
		t.Fatalf("unexpected messages: %+v", m.Messages)
	}
}

func TestBuildTurnTrace_ModelCallsNilSafe(t *testing.T) {
	tr := &RunTrace{RequestID: "r1", ModelCalls: nil}
	if out := BuildTurnTrace(TurnTraceMeta{}, tr); out.ModelCalls != nil {
		t.Fatalf("expected nil model calls, got %#v", out.ModelCalls)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd framework && go test ./harness/ -run TestBuildTurnTrace_MapsModelCalls -v`
Expected: 编译失败（`TurnTrace.ModelCalls` 未定义）。

- [ ] **Step 3: 给 TurnTrace 加字段并映射**

在 `turn_trace.go` 的 `TurnTrace` 结构体追加字段：

```go
ModelCalls []ModelCallRecord `json:"model_calls,omitempty"`
```

在 `BuildTurnTrace` 的 `out := &TurnTrace{...}` 的字段列表追加：

```go
ModelCalls: tr.ModelCalls,
```

`ModelCallRecord` 已在 `recordModelCall` 内完成裁剪+脱敏，此处直接浅拷贝（`[]ModelCallRecord(tr.ModelCalls)` 即可）避免 `ModelCalls` 属性别名问题：

```go
out.ModelCalls = append([]ModelCallRecord(nil), tr.ModelCalls...)
```

（放在 `out := &TurnTrace{...}` 之后统一赋值，或用构造字面量内 `ModelCalls: append([]ModelCallRecord(nil), tr.ModelCalls...)`。）

- [ ] **Step 4: 运行测试确认通过**

Run: `cd framework && go test ./harness/ -run TestBuildTurnTrace_MapsModelCalls -v`
Expected: PASS。再跑 `go test ./turntrace/...`。

- [ ] **Step 5: 提交**

```bash
cd framework
git add harness/turn_trace.go harness/turn_trace_test.go
git commit -m "feat(turn_trace): persist model prompt & reasoning records"
```

---

### Task 4: portal ModelCallPayload 与事件映射（portal）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\portal\internal\service\chat_stream.go`
- Test: `E:\workspace\github\sixath\sixath\portal\internal\service\chat_stream_test.go`（若无则新建；先确认现有测试文件名）

- [ ] **Step 1: 定位现有测试文件 & 写失败测试**

先列出 portal service 现有测试命名：

Run: `cd portal && ls internal/service/chat_stream*` 

在现有或新建 `chat_stream_test.go` 追加：

```go
func TestModelCallEventFromBus_RespondedCarriesReasoning(t *testing.T) {
	p := modelCallEventFromBus(events.Event{
		Kind: events.ModelResponded,
		Payload: map[string]any{
			"step": 2, "mode": "tools",
			"reasoning":     "because y",
			"answer":        "final answer",
			"finish_reason": "tool_calls",
			"input_tokens":  7,
			"output_tokens": 5,
		},
	}, "gpt-4o")
	if p == nil {
		t.Fatal("expected payload")
	}
	if p.Reasoning != "because y" || p.Answer != "final answer" || p.FinishReason != "tool_calls" {
		t.Fatalf("unexpected: %+v", p)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd portal && go test ./internal/service/ -run TestModelCallEventFromBus_RespondedCarriesReasoning -v`
Expected: 编译失败（`ModelCallPayload.Reasoning` 等未定义）或断言失败。

- [ ] **Step 3: ModelCallPayload 加字段**

`E:\workspace\github\sixath\sixath\portal\internal\service\chat_stream.go` 的 `ModelCallPayload` 追加：

```go
// reasoning 为 thinking 模型的推理链；answer 为终答；finish_reason 为停止原因。
// 实时 SSE 不携带完整 prompts（见 spec §4），仅带摘要；完整提示词走 turn_trace 回查。
Reasoning    string `json:"reasoning,omitempty"`
Answer       string `json:"answer,omitempty"`
FinishReason string `json:"finish_reason,omitempty"`
```

- [ ] **Step 4: modelCallEventFromBus 读取新字段**

在 `modelCallEventFromBus`（chat_stream.go:~189）`ModelResponded` 分支（或 switch 之后）追加：

```go
if v, ok := e.Payload["reasoning"].(string); ok {
	p.Reasoning = v
}
if v, ok := e.Payload["answer"].(string); ok {
	p.Answer = v
}
if v, ok := e.Payload["finish_reason"].(string); ok {
	p.FinishReason = v
}
```

**边界**：`ModelInvoked` 分支不读这三个字段（它们只在 responded 出现），但放 switch 后统一赋值也安全——`ModelInvoked` 载荷里无这些 key，读出来为空串。放在 switch 后即可。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd portal && go test ./internal/service/ -run ModelCallEventFromBus -v`
Expected: PASS。再 `go build ./...`。

- [ ] **Step 6: 提交**

```bash
cd portal
git add internal/service/chat_stream.go internal/service/chat_stream_test.go
git commit -m "feat(portal): carry model reasoning/answer in model_call SSE payload"
```

---

### Task 5: portal TimelineNode 持久化 reasoning/answer（portal）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\portal\internal\service\timeline_persist.go`
- Test: `E:\workspace\github\sixath\sixath\portal\internal\service\timeline_persist_test.go`（现有）

- [ ] **Step 1: 写失败测试**

在 `timeline_persist_test.go` 追加（先确认该文件 import 与 helper；若无则新建结构）：

```go
func TestApplyModelCall_StoresReasoningAndAnswer(t *testing.T) {
	a := &TimelineAccumulator{}
	a.ApplyModelCall(&ModelCallPayload{
		Step: 1, Phase: "invoked", Mode: "tools",
	})
	a.ApplyModelCall(&ModelCallPayload{
		Step: 1, Phase: "responded",
		Reasoning: "chain", Answer: "answer", FinishReason: "stop",
		InputTokens: 3, OutputTokens: 2,
	})
	nodes := a.Finalize()
	if len(nodes) != 1 || nodes[0].Kind != "model" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
	n := nodes[0]
	if n.Reasoning == nil || *n.Reasoning != "chain" {
		t.Fatalf("expected reasoning on node: %#v", n)
	}
	if n.Answer == nil || *n.Answer != "answer" {
		t.Fatalf("expected answer on node: %#v", n)
	}
	if n.FinishReason == nil || *n.FinishReason != "stop" {
		t.Fatalf("expected finish_reason on node: %#v", n)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd portal && go test ./internal/service/ -run TestApplyModelCall_StoresReasoningAndAnswer -v`
Expected: FAIL（`*TimelineNode.Reasoning` 为 nil，无该字段编译失败）。

- [ ] **Step 3: TimelineNode 加字段 + ApplyModelCall 存 + timelineNodeToMap 序列化**

在 `TimelineNode` 追加：

```go
Reasoning    *string `json:"reasoning,omitempty"`
Answer       *string `json:"answer,omitempty"`
FinishReason *string `json:"finishReason,omitempty"`
```

`ApplyModelCall` 的 upsert 分支（`n.Phase = p.Phase` 后端）追加：

```go
if p.Reasoning != "" {
	n.Reasoning = &p.Reasoning
}
if p.Answer != "" {
	n.Answer = &p.Answer
}
if p.FinishReason != "" {
	n.FinishReason = &p.FinishReason
}
```

`ApplyModelCall` 的新建分支（node 字面量后）追加：

```go
if p.Reasoning != "" {
	v := p.Reasoning
	node.Reasoning = &v
}
if p.Answer != "" {
	v := p.Answer
	node.Answer = &v
}
if p.FinishReason != "" {
	v := p.FinishReason
	node.FinishReason = &v
}
```

`timelineNodeToMap` 追加：

```go
if n.Reasoning != nil {
	m["reasoning"] = *n.Reasoning
}
if n.Answer != nil {
	m["answer"] = *n.Answer
}
if n.FinishReason != nil {
	m["finishReason"] = *n.FinishReason
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd portal && go test ./internal/service/ -run ApplyModelCall -v`
Expected: PASS。再跑关键回归：`go test ./internal/service/ -run Timeline`.

- [ ] **Step 5: 提交**

```bash
cd portal
git add internal/service/timeline_persist.go internal/service/timeline_persist_test.go
git commit -m "feat(portal): persist model reasoning/answer in timeline nodes"
```

---

### Task 6: web 类型与 reducer 透传字段（web）

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\web\src\api\chatStream.ts`
- Modify: `E:\workspace\github\sixath\sixath\web\src\pages\timelineReducer.ts`

- [ ] **Step 1: 扩展 ModelCallPayload 类型与 reducer ModelNode**

`chatStream.ts` 的 `ModelCallPayload` 接口追加：

```ts
reasoning?: string
answer?: string
finish_reason?: string
```

`timelineReducer.ts` 的 `ModelNode` 类型追加：

```ts
reasoning?: string
answer?: string
finishReason?: string
```

`normalizeTimeline` 的 model 分支（`out.push({...})` 内）追加读取：

```ts
reasoning: typeof row.reasoning === 'string' ? row.reasoning : undefined,
answer: typeof row.answer === 'string' ? row.answer : undefined,
finishReason: typeof row.finishReason === 'string' ? row.finishReason : typeof row.finish_reason === 'string' ? row.finish_reason : undefined,
```

- [ ] **Step 2: 类型校验**

Run: `cd web && npx tsc --noEmit`
Expected: 无类型错误。

- [ ] **Step 3: 提交**

```bash
cd web
git add src/api/chatStream.ts src/pages/timelineReducer.ts
git commit -m "feat(web): thread model reasoning/answer through timeline types"
```

---

### Task 7: web 时间线模型节点渲染推理/终答

**Files:**
- Modify: `E:\workspace\github\sixath\sixath\web\src\pages\ChatPage.tsx:842-844`（模型节点面板）

模型节点在 `ChatPage.tsx` 的 timeline 渲染函数里，当前面板（`tl-panel`）内 model 分支只输出一段 JSON 摘要（L843）。改为在 JSON 摘要之上加两段可折叠展示推理与终答。

**定位（已确认）**：L796 `key = n.kind === 'tool' ? ... : m:${n.step}`；L842-844 是 model 分支（`else`） `<pre className="tl-pre">{JSON.stringify({mode,model,input_tokens,...})}</pre>`。

- [ ] **Step 1: 改模型节点面板渲染（TDD 用类型与构建校验）**

把 L842-844 的 `else` 分支替换为：

```tsx
) : (
  <>
    {n.reasoning && (
      <details className="tl-detail">
        <summary>推理</summary>
        <pre className="tl-pre">{n.reasoning}</pre>
      </details>
    )}
    {n.answer && (
      <details className="tl-detail">
        <summary>终答 · {n.finishReason ?? ''}</summary>
        <pre className="tl-pre">{n.answer}</pre>
      </details>
    )}
    <pre className="tl-pre">{JSON.stringify({ mode: n.mode, model: n.model, input_tokens: n.inputTokens, output_tokens: n.outputTokens, message_count: n.messageCount }, null, 2)}</pre>
  </>
)
```

在 `ChatPage.tsx` 的 `<style>` 或对应 CSS 文件增加 `.tl-detail` 样式（等宽、可折叠、限高滚动）：

```css
.tl-detail summary { cursor: pointer; opacity: .8; }
.tl-detail pre { max-height: 240px; overflow: auto; white-space: pre-wrap; word-break: break-word; }
```

- [ ] **Step 2: 类型与构建校验**

Run: `cd web && npx tsc --noEmit`
Expected: 无错误（`reasoning`/`answer`/`finishReason` 已在 Task 6 加入 `ModelNode` 类型；确认 `ChatPage.tsx` 里 `n` 的联合类型能访问到——若 `n` 在 model 分支被 TS 收窄仍报字段不存在，则需把 `n` as `ModelNode` 断言，见 Task 6 类型）。

- [ ] **Step 3: 浏览器验证**

启动 dev server: `cd web && npm run dev`。用浏览器预览发一条触发模型调用的对话，确认：模型节点展开后出现「推理」「终答」可折叠区块；thinking 模型有 reasoning，非 thinking 隐藏推理区；终答与 token 摘要正常。

- [ ] **Step 4: 提交**

```bash
cd web
git add src/pages/ChatPage.tsx
git commit -m "feat(web): render model reasoning & answer in run timeline"
```

---

### Task 8: 端到端回归验证

**Files:**
- 无源码改动，仅验证。

- [ ] **Step 1: framework 全量测试**

Run: `cd framework && go test ./...`
Expected: 全绿（含既有 React/trace/turntrace 测试）。

- [ ] **Step 2: portal 相关测试**

Run: `cd portal && go test ./internal/service/...`
Expected: 全绿。

- [ ] **Step 3: web 构建**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: 构建成功。

- [ ] **Step 4: 提交收尾（如 CI 分离则分开提交）**

有多种验证其 end-to-end 行为是否一致——确认 turn_trace 回查含 `model_calls`、实时 SSE `model_call` 含 reasoning/answer、时间线 metadata 含 reasoning/answer。人工 smoke 一条真实对话后记录在 commit message。
