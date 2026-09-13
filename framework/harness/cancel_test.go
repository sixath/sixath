package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// blockingStreamModel 先产出若干增量，然后挂住直到 ctx 取消——模拟"模型还在生成时用户点了停止"。
type blockingStreamModel struct{ deltas []string }

func (m *blockingStreamModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: strings.Join(m.deltas, "")}, nil
}
func (m *blockingStreamModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: strings.Join(m.deltas, "")}, nil
}
func (m *blockingStreamModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (m *blockingStreamModel) ChatStream(ctx context.Context, _ []model.Message, _ ...model.Option) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		for _, d := range m.deltas {
			select {
			case ch <- d:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
	}()
	return ch, nil
}

// cancelAfterToolModel 第一轮发起一次 tool_call，第二轮挂住直到取消。
type cancelAfterToolModel struct{ toolName string }

func (m *cancelAfterToolModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "unused"}, nil
}
func (m *cancelAfterToolModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "unused"}, nil
}
func (m *cancelAfterToolModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (m *cancelAfterToolModel) ChatWithTools(ctx context.Context, _ []model.Message, _ *tool.Registry, _ ...model.Option) (*model.Generation, error) {
	if m.toolName != "" {
		name := m.toolName
		m.toolName = "" // 仅第一轮发起工具调用
		return &model.Generation{
			Raw: model.ToolStep{
				Used:       true,
				ToolCallID: "call-1",
				ToolName:   name,
				Arguments:  map[string]any{"q": "x"},
			},
		}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type streamSummary struct {
	types         []StreamEventType
	text          strings.Builder
	cancelledEv   *StreamEvent
	sawError      bool
	sawDone       bool
	cancelIs      func() error
	cancelOnDelta bool
}

func drainEvents(t *testing.T, ch <-chan StreamEvent, cancelFn func() error, onDelta bool) streamSummary {
	t.Helper()
	var sum streamSummary
	done := make(chan struct{})
	go func() {
		defer close(done)
		fired := false
		for ev := range ch {
			sum.types = append(sum.types, ev.Type)
			switch ev.Type {
			case StreamEventDelta:
				sum.text.WriteString(ev.Text)
				if onDelta && !fired {
					fired = true
					if err := cancelFn(); err != nil {
						t.Errorf("cancel returned error: %v", err)
					}
				}
			case StreamEventCancelled:
				cp := ev
				sum.cancelledEv = &cp
			case StreamEventError:
				sum.sawError = true
			case StreamEventDone:
				sum.sawDone = true
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not terminate (possible goroutine leak / missing cancel)")
	}
	return sum
}

func containsType(types []StreamEventType, want StreamEventType) bool {
	for _, tp := range types {
		if tp == want {
			return true
		}
	}
	return false
}

func TestReActAgent_Cancel_MidStreamEmitsCancelled(t *testing.T) {
	m := &blockingStreamModel{deltas: []string{"思考中", "…"}}
	a := NewReActAgent(m, nil, nil, WithReActMaxSteps(2))

	ch, err := a.RunEvents(context.Background(), &Request{
		RequestID: "run-cancel-1",
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}

	sum := drainEvents(t, ch, func() error { return a.Cancel("run-cancel-1") }, true)

	if sum.cancelledEv == nil {
		t.Fatalf("expected a cancelled event, got %v", sum.types)
	}
	if sum.sawError {
		t.Fatalf("cancel must not be reported as an error: %v", sum.types)
	}
	if sum.sawDone {
		t.Fatalf("cancel must not be followed by done: %v", sum.types)
	}
	if sum.text.String() != "思考中…" {
		t.Fatalf("partial text lost: %q", sum.text.String())
	}
	tr := sum.cancelledEv.Trace
	if tr == nil || !tr.Canceled || !tr.CanceledByRequest {
		t.Fatalf("trace must record cancellation: %+v", tr)
	}
	if by, ok := sum.cancelledEv.Metadata["by_request"].(bool); !ok || !by {
		t.Fatalf("cancelled metadata by_request=%v", sum.cancelledEv.Metadata["by_request"])
	}
	if got := a.InflightRuns(); got != 0 {
		t.Fatalf("inflight runs leaked: %d", got)
	}
}

func TestReActAgent_ParentContextCancel_IsReportedAsCancelled(t *testing.T) {
	m := &blockingStreamModel{deltas: []string{"partial"}}
	a := NewReActAgent(m, nil, nil, WithReActMaxSteps(2))

	parent, cancelParent := context.WithCancel(context.Background())
	ch, err := a.RunEvents(parent, &Request{
		RequestID: "run-cancel-2",
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}

	sum := drainEvents(t, ch, func() error {
		cancelParent()
		return nil
	}, true)

	if sum.cancelledEv == nil {
		t.Fatalf("client disconnect must also end as cancelled, got %v", sum.types)
	}
	if sum.sawError {
		t.Fatalf("parent cancel must not surface as error: %v", sum.types)
	}
	tr := sum.cancelledEv.Trace
	if tr == nil || !tr.Canceled {
		t.Fatalf("trace must record cancellation: %+v", tr)
	}
	if tr.CanceledByRequest {
		t.Fatal("parent ctx cancel must not be attributed to the Cancel API")
	}
	if got := a.InflightRuns(); got != 0 {
		t.Fatalf("inflight runs leaked: %d", got)
	}
}

func TestReActAgent_Cancel_PreservesCompletedToolResults(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name: "echo",
		Execute: func(context.Context, map[string]any) (any, error) {
			return "tool-ok", nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	a := NewReActAgent(&cancelAfterToolModel{toolName: "echo"}, nil, reg, WithReActMaxSteps(3))

	ch, err := a.RunEvents(context.Background(), &Request{
		RequestID: "run-cancel-3",
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}

	// 工具完成后第二轮模型调用会挂住：这里不依赖 delta，改用短延时后取消。
	time.AfterFunc(150*time.Millisecond, func() { _ = a.Cancel("run-cancel-3") })
	sum := drainEvents(t, ch, func() error { return nil }, false)

	if sum.cancelledEv == nil {
		t.Fatalf("expected cancelled event, got %v", sum.types)
	}
	tr := sum.cancelledEv.Trace
	if tr == nil {
		t.Fatal("cancelled event must carry the trace")
	}
	if len(tr.ToolCalls) != 1 {
		t.Fatalf("tool results must be preserved on cancel, got %+v", tr.ToolCalls)
	}
	if tr.ToolCalls[0].ToolName != "echo" || tr.ToolCalls[0].Error != "" {
		t.Fatalf("unexpected tool record: %+v", tr.ToolCalls[0])
	}
}

func TestReActAgent_Cancel_UnknownRun(t *testing.T) {
	a := NewReActAgent(&blockingStreamModel{}, nil, nil)
	if err := a.Cancel("nope"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("err=%v want ErrRunNotFound", err)
	}
}

func TestReActAgent_Cancel_AfterCompletion(t *testing.T) {
	m := &blockingStreamModel{deltas: []string{"done"}}
	a := NewReActAgent(m, nil, nil, WithReActMaxSteps(1))
	// 非流式 Run：正常结束后句柄必须已注销。
	if _, err := a.Run(context.Background(), &Request{
		RequestID: "run-done-1",
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.InflightRuns(); got != 0 {
		t.Fatalf("inflight runs after completion: %d", got)
	}
	if err := a.Cancel("run-done-1"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("cancel after completion err=%v want ErrRunNotFound", err)
	}
}

func TestReActAgent_RunWithoutRequestIDIsNotCancelable(t *testing.T) {
	m := &blockingStreamModel{deltas: []string{"x"}}
	a := NewReActAgent(m, nil, nil, WithReActMaxSteps(1))
	// 无 RequestID：不注册句柄，Run 正常结束即可（不应 panic / 泄漏）。
	resp, err := a.Run(context.Background(), &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Text != "x" {
		t.Fatalf("resp=%+v", resp)
	}
	if got := a.InflightRuns(); got != 0 {
		t.Fatalf("inflight runs leaked: %d", got)
	}
}

// finiteStreamModel 产出增量后正常结束（用于确认没有把正常完成误判为取消）。
type finiteStreamModel struct{ deltas []string }

func (m *finiteStreamModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: strings.Join(m.deltas, "")}, nil
}
func (m *finiteStreamModel) Chat(context.Context, []model.Message, ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: strings.Join(m.deltas, "")}, nil
}
func (m *finiteStreamModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
func (m *finiteStreamModel) ChatStream(_ context.Context, _ []model.Message, _ ...model.Option) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		for _, d := range m.deltas {
			ch <- d
		}
	}()
	return ch, nil
}

func TestReActAgent_NormalStreamEmitsDoneNotCancelled(t *testing.T) {
	a := NewReActAgent(&finiteStreamModel{deltas: []string{"a", "b"}}, nil, nil, WithReActMaxSteps(1))
	ch, err := a.RunEvents(context.Background(), &Request{
		RequestID: "run-normal",
		Messages:  []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	sum := drainEvents(t, ch, func() error { return nil }, false)

	if !sum.sawDone {
		t.Fatalf("normal completion must emit done: %v", sum.types)
	}
	if containsType(sum.types, StreamEventCancelled) {
		t.Fatalf("normal completion must not emit cancelled: %v", sum.types)
	}
	if sum.text.String() != "ab" {
		t.Fatalf("text=%q", sum.text.String())
	}
	if got := a.InflightRuns(); got != 0 {
		t.Fatalf("inflight runs leaked: %d", got)
	}
}
