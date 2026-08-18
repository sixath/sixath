package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

func registerFakeRCARead(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "rca_read",
		Description: "read source",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			return map[string]any{
				"ok":      true,
				"file":    "helper.go",
				"content": helperReadContent,
			}, nil
		},
	}); err != nil {
		t.Fatalf("register rca_read: %v", err)
	}
	return reg
}

func TestReActCodeClaimGate_machineVetoSoftInject(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:     true,
			ToolName: "rca_read",
			Arguments: map[string]any{
				"repo": "migu",
				"file": "helper.go",
			},
		}},
		finalReply: "" +
			"会写入本地映射\n```go\n" +
			"info.State = 1\n" +
			"InsertUnionUserAreaInfo(info, flowID)\n" +
			"```\n",
	}
	reg := registerFakeRCARead(t)
	stub := &stubClaimModel{reply: `{"verdict":"pass","issues":[]}`}
	bus := events.NewBus()
	var mismatchN int
	var mu sync.Mutex
	bus.Subscribe(false, func(ctx context.Context, e events.Event) {
		if e.Kind == events.CodeClaimMismatch {
			mu.Lock()
			mismatchN++
			mu.Unlock()
		}
	})

	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(4),
		WithReActEventBus(bus),
		WithReActCodeClaimGate(CodeClaimGateConfig{Enabled: true, Auditor: stub}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "区域已有用户时会怎样"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil || tr.CodeClaimNudges != 1 {
		t.Fatalf("CodeClaimNudges want 1, got %#v", tr)
	}
	if resp.Metadata["code_claim_mismatch"] != true {
		t.Fatalf("expected code_claim_mismatch after second fail, got %#v", resp.Metadata)
	}
	if stub.last != nil {
		t.Fatal("machine veto must skip LLM auditor")
	}
	mu.Lock()
	n := mismatchN
	mu.Unlock()
	if n != 1 {
		t.Fatalf("CodeClaimMismatch events=%d want 1", n)
	}
}

func TestReActCodeClaimGate_proseFailInjectsLLM(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{
		toolSteps: []model.ToolStep{{
			Used:      true,
			ToolName:  "rca_read",
			Arguments: map[string]any{"file": "helper.go"},
		}},
		finalReply: "区域已有用户时，union-access 会把 UID 写入本地 DBUnionUserAreaInfo 映射表。",
	}
	reg := registerFakeRCARead(t)
	stub := &stubClaimModel{reply: `{"verdict":"fail","issues":[{"kind":"dropped_guard","path":"helper.go","symbol":"InsertUnionUserAreaInfo","guard":"errcode == 0","claim":"会写入本地映射"}]}`}
	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(4),
		WithReActCodeClaimGate(CodeClaimGateConfig{Enabled: true, Auditor: stub}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "区域已有用户时会怎样"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stub.last == nil {
		t.Fatal("prose claim must reach LLM auditor")
	}
	tr, _ := resp.Metadata["trace"].(*RunTrace)
	if tr == nil || tr.CodeClaimNudges != 1 {
		t.Fatalf("CodeClaimNudges want 1, got %#v", tr)
	}
	found := false
	for _, m := range fake.lastToolMessages {
		if m.Role == "user" && strings.Contains(m.Content, "InsertUnionUserAreaInfo") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("inject prompt should mention the failed symbol")
	}
}

func TestReActCodeClaimGate_noRcaReadSkipsAuditor(t *testing.T) {
	mem := memory.NewBufferMemory(5)
	fake := &fakeOpenAIClient{finalReply: "hello"}
	reg := tool.NewRegistry()
	_ = tool.RegisterCalculatorTool(reg)
	stub := &stubClaimModel{reply: `{"verdict":"fail","issues":[{"kind":"x"}]}`}
	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(3),
		WithReActCodeClaimGate(CodeClaimGateConfig{Enabled: true, Auditor: stub}),
	)
	resp, err := react.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stub.last != nil {
		t.Fatal("auditor must not run without rca_read")
	}
	if fake.toolCalls != 1 {
		t.Fatalf("toolCalls=%d want 1 (no inject)", fake.toolCalls)
	}
	if resp.Metadata["code_claim_mismatch"] == true {
		t.Fatal("must not mark mismatch without sources")
	}
}

func TestCollectCodeQuoteSources_readAndGrep(t *testing.T) {
	got := CollectCodeQuoteSources([]ToolCallRecord{
		{ToolName: "rca_read", Result: map[string]any{"file": "a.go", "content": "1|foo"}},
		{ToolName: "rca_grep", Result: map[string]any{
			"matches": []any{map[string]any{"file": "b.go", "snippet": "InsertX()"}},
		}},
		{ToolName: "calculator_add", Result: map[string]any{"sum": 1}},
	})
	if len(got) != 2 {
		t.Fatalf("sources=%d want 2: %#v", len(got), got)
	}
	if got[0].Path != "a.go" || got[0].Content != "1|foo" {
		t.Fatalf("read source %#v", got[0])
	}
	if got[1].Path != "b.go" || got[1].Content != "InsertX()" {
		t.Fatalf("grep source %#v", got[1])
	}
}

func TestReActCodeClaimGate_streamBuffersUntilGate(t *testing.T) {
	bad := "```go\ninfo.State = 1\nInsertUnionUserAreaInfo(info, flowID)\n```\n"
	mem := memory.NewBufferMemory(5)
	reg := registerFakeRCARead(t)
	stub := &stubClaimModel{reply: `{"verdict":"pass","issues":[]}`}
	fake := &fakeSequencedStreamingToolClient{
		gens: []*model.Generation{
			{Raw: model.ToolStep{Used: true, ToolName: "rca_read", Arguments: map[string]any{"file": "helper.go"}}},
			{Text: bad, Raw: model.ToolStep{Used: false}},
			{Text: bad, Raw: model.ToolStep{Used: false}},
		},
		texts: []string{"", bad, bad},
	}
	react := NewReActAgent(fake, mem, reg,
		WithReActMaxSteps(4),
		WithReActCodeClaimGate(CodeClaimGateConfig{Enabled: true, Auditor: stub}),
	)
	ch, err := react.RunEvents(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "区域已有用户时会怎样"}},
	})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	var joined strings.Builder
	for ev := range ch {
		if ev.Type == StreamEventDelta {
			joined.WriteString(ev.Text)
		}
	}
	n := strings.Count(joined.String(), "InsertUnionUserAreaInfo")
	if n != 1 {
		t.Fatalf("first mismatched stream must be held then dropped; want 1 flush, got %d in %q", n, joined.String())
	}
	if stub.last != nil {
		t.Fatal("machine veto must skip LLM")
	}
}
