package adapter // same package as wecom_progress.go (white-box)

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type progressRespondCall struct {
	reqID, streamID, content string
	finish                   bool
}

type progressFakeConn struct {
	mu    sync.Mutex
	calls []progressRespondCall
}

func (f *progressFakeConn) RespondStream(_ context.Context, reqID, streamID, content string, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, progressRespondCall{reqID: reqID, streamID: streamID, content: content, finish: finish})
	return nil
}

func (f *progressFakeConn) snapshot() []progressRespondCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]progressRespondCall, len(f.calls))
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

	// Wait until skeleton + at least one tick (tick=20ms); tolerate scheduler jitter.
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		progress := 0
		for _, c := range conn.snapshot() {
			if strings.Contains(c.content, "耗时") {
				progress++
			}
		}
		if progress >= 2 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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

func TestFormatProgressText_Skeleton(t *testing.T) {
	st := NewProgressState(time.Unix(0, 0))
	got := FormatProgressText(st, time.Unix(0, 0))
	for _, want := range []string{"处理中…", "耗时 00:00", "阶段 思考中", "工具 —", "已完成 0 步"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

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

func TestProgressState_ApplySSE_FailedIgnoresLaterEvents(t *testing.T) {
	st := NewProgressState(time.Now())
	st.ApplySSEEvent("confirm_required", []byte(`{"confirmation":{}}`))
	st.ApplySSEEvent("chunk", []byte(`{"content":"你好"}`))
	if st.Stage != progressStageThinking {
		t.Fatalf("stage=%q want %q after failed+chunk", st.Stage, progressStageThinking)
	}
}

func TestProgressState_ApplySSE_DuplicateModelResponded(t *testing.T) {
	st := NewProgressState(time.Now())
	st.ApplySSEEvent("model_call", []byte(`{"model_call":{"step":0,"phase":"responded"}}`))
	if st.StepsDone != 1 {
		t.Fatalf("steps=%d want 1", st.StepsDone)
	}
	st.ApplySSEEvent("model_call", []byte(`{"model_call":{"step":0,"phase":"responded"}}`))
	if st.StepsDone != 1 {
		t.Fatalf("duplicate responded counted: %d", st.StepsDone)
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
