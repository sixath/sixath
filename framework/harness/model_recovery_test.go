package harness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// scriptTurn 为一次模型调用的脚本：err 为调用（setup）错误；deltas/gen 为流式增量与终态
// （gen.Err 非空即流中途失败）。
type scriptTurn struct {
	err    error
	deltas []string
	gen    *model.Generation
}

// scriptedModel 按脚本依次应答 Chat / ChatWithTools；脚本耗尽后重复最后一条。
type scriptedModel struct {
	mu       sync.Mutex
	script   []scriptTurn
	calls    int
	msgLens  []int
	lastMsgs [][]model.Message
}

func (m *scriptedModel) next(msgs []model.Message) scriptTurn {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.msgLens = append(m.msgLens, len(msgs))
	m.lastMsgs = append(m.lastMsgs, append([]model.Message(nil), msgs...))
	if len(m.script) == 0 {
		return scriptTurn{gen: &model.Generation{Text: "", Raw: model.ToolStep{}}}
	}
	turn := m.script[0]
	if len(m.script) > 1 {
		m.script = m.script[1:]
	}
	return turn
}

func (m *scriptedModel) Generate(context.Context, string, ...model.Option) (*model.Generation, error) {
	return &model.Generation{}, nil
}

func (m *scriptedModel) Chat(_ context.Context, msgs []model.Message, _ ...model.Option) (*model.Generation, error) {
	turn := m.next(msgs)
	if turn.err != nil {
		return nil, turn.err
	}
	return turn.gen, nil
}

func (m *scriptedModel) Embed(context.Context, []string, ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func (m *scriptedModel) ChatWithTools(_ context.Context, msgs []model.Message, _ *tool.Registry, _ ...model.Option) (*model.Generation, error) {
	turn := m.next(msgs)
	if turn.err != nil {
		return nil, turn.err
	}
	return turn.gen, nil
}

type scriptedStreamModel struct{ *scriptedModel }

func (m *scriptedStreamModel) ChatWithToolsStream(ctx context.Context, msgs []model.Message, _ *tool.Registry, _ ...model.Option) (<-chan string, <-chan *model.Generation, error) {
	turn := m.next(msgs)
	if turn.err != nil {
		return nil, nil, turn.err
	}
	textCh := make(chan string)
	genCh := make(chan *model.Generation, 1)
	go func() {
		defer close(textCh)
		defer close(genCh)
		for _, d := range turn.deltas {
			select {
			case textCh <- d:
			case <-ctx.Done():
				return
			}
		}
		genCh <- turn.gen
	}()
	return textCh, genCh, nil
}

func toolTurn(id string, args map[string]any) scriptTurn {
	return scriptTurn{gen: &model.Generation{Raw: model.ToolStep{
		Used:      true,
		ToolCalls: []model.ToolCall{{ID: id, Name: "lookup", Arguments: args}},
	}}}
}

func finalTurn(text string) scriptTurn {
	return scriptTurn{gen: &model.Generation{Text: text, Raw: model.ToolStep{}}, deltas: []string{text}}
}

func gateway401() error {
	return &openai.APIError{HTTPStatusCode: 401, Message: "Invalid token (request id: abc)"}
}

// lookupRegistry 注册一个计数工具，用于断言「工具不会被重复执行」。
func lookupRegistry(t *testing.T, executed *int) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(tool.Tool{
		Name:        "lookup",
		Description: "lookup logs",
		Execute: func(context.Context, map[string]any) (any, error) {
			*executed++
			return map[string]any{"hit_status": "hits", "count": 3, "first": "order 42 failed: timeout"}, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *sleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	r.delays = append(r.delays, d)
	r.mu.Unlock()
	return ctx.Err()
}

func recoveryOpts(rec *sleepRecorder, delays ...time.Duration) []ReActOption {
	return []ReActOption{
		WithReActMaxSteps(5),
		WithReActModelRecoveryDelays(delays...),
		WithReActModelRecoverySleep(rec.sleep),
	}
}

func TestModelRecovery_RunRetriesSameStepAfterRetryableError(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "order 42"}),
		{err: gateway401()},
		{err: &model.APIStatusError{StatusCode: 429}},
		finalTurn("订单 42 超时"),
	}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, memory.NewBufferMemory(5), lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond, 2*time.Millisecond, 3*time.Millisecond)...)

	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "order 42?"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Text != "订单 42 超时" {
		t.Fatalf("Text=%q", resp.Text)
	}
	if executed != 1 {
		t.Fatalf("tool executed %d times, want 1", executed)
	}
	if fake.calls != 4 {
		t.Fatalf("model calls=%d want 4", fake.calls)
	}
	if fake.msgLens[1] != fake.msgLens[2] || fake.msgLens[2] != fake.msgLens[3] {
		t.Fatalf("retries must reuse the same messages, lens=%v", fake.msgLens)
	}
	if len(rec.delays) != 2 || rec.delays[0] != time.Millisecond || rec.delays[1] != 2*time.Millisecond {
		t.Fatalf("sleeps=%v want [1ms 2ms]", rec.delays)
	}
	tr := resp.Metadata["trace"].(*RunTrace)
	if tr.ModelRecoveries != 2 || len(tr.ModelRecoveryErrors) != 2 {
		t.Fatalf("recoveries=%d errors=%v", tr.ModelRecoveries, tr.ModelRecoveryErrors)
	}
	if len(tr.Errors) != 0 || tr.ModelUnavailable {
		t.Fatalf("recovered run must not be marked failed: errors=%v unavailable=%v", tr.Errors, tr.ModelUnavailable)
	}
}

func TestModelRecovery_EmitsRecoveringEvent(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{{err: gateway401()}, finalTurn("ok")}}
	bus := events.NewBus()
	var mu sync.Mutex
	var got []events.Event
	bus.Subscribe(false, func(_ context.Context, e events.Event) {
		if e.Kind == events.ModelRecovering {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
	})
	rec := &sleepRecorder{}
	opts := append(recoveryOpts(rec, 5*time.Millisecond), WithReActEventBus(bus))
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), opts...)
	if _, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("ModelRecovering events=%d want 1", len(got))
	}
	if got[0].Payload["delay_ms"] != int64(5) || got[0].Payload["attempt"] != 1 {
		t.Fatalf("payload=%v", got[0].Payload)
	}
}

func TestModelRecovery_NonRetryableErrorWithoutToolsReturnsError(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{{err: &model.APIStatusError{StatusCode: 400, Message: "bad"}}}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond)...)
	_, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("want RunError, got %v", err)
	}
	if len(rec.delays) != 0 || fake.calls != 1 {
		t.Fatalf("400 must not be recovered: sleeps=%v calls=%d", rec.delays, fake.calls)
	}
	if runErr.Trace.ModelUnavailable {
		t.Fatal("no tool progress: must not degrade")
	}
}

func TestModelRecovery_ExhaustedWithoutToolsReturnsError(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{{err: gateway401()}}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond, time.Millisecond)...)
	_, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "hi"}}})
	var runErr *RunError
	if !errors.As(err, &runErr) {
		t.Fatalf("want RunError, got %v", err)
	}
	if fake.calls != 3 || runErr.Trace.ModelRecoveries != 2 {
		t.Fatalf("calls=%d recoveries=%d", fake.calls, runErr.Trace.ModelRecoveries)
	}
}

func TestModelRecovery_RunExhaustedDeliversDegradedAnswer(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	mem := memory.NewBufferMemory(5)
	fake := &scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "order 42", "password": "hunter2"}),
		{err: gateway401()},
	}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, mem, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond, time.Millisecond)...)

	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "order 42?"}}})
	if err != nil {
		t.Fatalf("degraded run must not return error: %v", err)
	}
	for _, want := range []string{ModelUnavailableAnswerPrefix, "HTTP 401", "未经模型总结", "lookup", "hits", "order 42 failed", "稍后重试"} {
		if !strings.Contains(resp.Text, want) {
			t.Fatalf("degraded answer missing %q:\n%s", want, resp.Text)
		}
	}
	if !strings.HasPrefix(resp.Text, ModelUnavailableAnswerPrefix) {
		t.Fatalf("answer must start with prefix:\n%s", resp.Text)
	}
	if strings.Contains(resp.Text, "hunter2") {
		t.Fatalf("secret args must be redacted:\n%s", resp.Text)
	}
	tr := resp.Metadata["trace"].(*RunTrace)
	if !tr.ModelUnavailable || len(tr.Errors) != 1 || tr.ModelRecoveries != 2 {
		t.Fatalf("trace unavailable=%v errors=%v recoveries=%d", tr.ModelUnavailable, tr.Errors, tr.ModelRecoveries)
	}
	if resp.Metadata["model_unavailable"] != true {
		t.Fatalf("metadata=%v", resp.Metadata)
	}
	recent, _ := mem.GetRecent(context.Background(), 5)
	if len(recent) == 0 || recent[len(recent)-1].Message.Content != resp.Text {
		t.Fatalf("degraded answer must be stored as assistant message: %#v", recent)
	}
	if executed != 1 {
		t.Fatalf("tool executed %d times", executed)
	}
}

func TestModelRecovery_NonRetryableWithToolsDegrades(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "x"}),
		{err: &model.APIStatusError{StatusCode: 400, Message: "context too long"}},
	}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond)...)
	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.delays) != 0 {
		t.Fatalf("400 must not sleep: %v", rec.delays)
	}
	if !strings.Contains(resp.Text, "HTTP 400") {
		t.Fatalf("text=%s", resp.Text)
	}
}

func TestModelRecovery_ForcedSummaryFailureDegrades(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "x"}),
		{err: &model.APIStatusError{StatusCode: 503}},
	}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed),
		WithReActMaxSteps(1), WithReActModelRecoveryDelays(time.Millisecond), WithReActModelRecoverySleep(rec.sleep))
	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasPrefix(resp.Text, ModelUnavailableAnswerPrefix) || len(rec.delays) != 1 {
		t.Fatalf("text=%q sleeps=%v", resp.Text, rec.delays)
	}
}

func TestModelRecovery_CanceledDuringCooldownStops(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{toolTurn("c1", nil), {err: gateway401()}}}
	ctx, cancel := context.WithCancel(context.Background())
	sleep := func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed),
		WithReActMaxSteps(5), WithReActModelRecoveryDelays(time.Hour), WithReActModelRecoverySleep(sleep))
	resp, err := a.Run(ctx, &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatalf("canceled run must not deliver a degraded answer: %#v", resp)
	}
	if fake.calls != 2 {
		t.Fatalf("calls=%d want 2", fake.calls)
	}
}

func collectStream(t *testing.T, ch <-chan StreamEvent) (deltas string, done *StreamEvent, errs []string) {
	t.Helper()
	var b strings.Builder
	for ev := range ch {
		switch ev.Type {
		case StreamEventDelta:
			b.WriteString(ev.Text)
		case StreamEventDone:
			e := ev
			done = &e
		case StreamEventError:
			errs = append(errs, ev.Error)
		}
	}
	return b.String(), done, errs
}

func TestModelRecovery_StreamMidStreamFailureRetriesStep(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedStreamModel{&scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "x"}),
		{deltas: []string{"部分结论"}, gen: &model.Generation{Err: gateway401()}},
		finalTurn("完整结论"),
	}}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond)...)
	ch, err := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("RunEvents: %v", err)
	}
	deltas, done, errs := collectStream(t, ch)
	if len(errs) != 0 || done == nil {
		t.Fatalf("errs=%v done=%v", errs, done)
	}
	if !strings.Contains(deltas, "部分结论"+ModelRetryNotice+"完整结论") {
		t.Fatalf("deltas=%q", deltas)
	}
	if done.Text != "完整结论" {
		t.Fatalf("done text=%q", done.Text)
	}
	if executed != 1 || fake.calls != 3 {
		t.Fatalf("executed=%d calls=%d", executed, fake.calls)
	}
	if fake.msgLens[1] != fake.msgLens[2] {
		t.Fatalf("retry must reuse messages: %v", fake.msgLens)
	}
}

func TestModelRecovery_StreamSetupFailureNoNotice(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedStreamModel{&scriptedModel{script: []scriptTurn{{err: gateway401()}, finalTurn("ok")}}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond)...)
	ch, _ := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	deltas, done, errs := collectStream(t, ch)
	if len(errs) != 0 || done == nil || deltas != "ok" {
		t.Fatalf("deltas=%q errs=%v done=%v", deltas, errs, done)
	}
}

func TestModelRecovery_StreamExhaustedDeliversDegradedDone(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	mem := memory.NewBufferMemory(5)
	fake := &scriptedStreamModel{&scriptedModel{script: []scriptTurn{
		toolTurn("c1", map[string]any{"q": "x"}),
		{err: gateway401()},
	}}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, mem, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond, time.Millisecond, time.Millisecond)...)
	ch, _ := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	deltas, done, errs := collectStream(t, ch)
	if len(errs) != 0 {
		t.Fatalf("degraded stream must not emit error: %v", errs)
	}
	if done == nil || !strings.HasPrefix(done.Text, ModelUnavailableAnswerPrefix) {
		t.Fatalf("done=%+v", done)
	}
	if !strings.Contains(deltas, ModelUnavailableAnswerPrefix) {
		t.Fatalf("degraded answer must be streamed as delta: %q", deltas)
	}
	if done.Metadata["model_unavailable"] != true || done.Trace == nil || !done.Trace.ModelUnavailable {
		t.Fatalf("done metadata=%v trace=%+v", done.Metadata, done.Trace)
	}
	if done.Trace.ModelRecoveries != 3 || len(rec.delays) != 3 {
		t.Fatalf("recoveries=%d sleeps=%v", done.Trace.ModelRecoveries, rec.delays)
	}
	recent, _ := mem.GetRecent(context.Background(), 5)
	if len(recent) == 0 || recent[len(recent)-1].Message.Content != done.Text {
		t.Fatal("degraded answer must be stored as assistant message")
	}
}

func TestModelRecovery_SyncEventsPathRecovers(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	executed := 0
	fake := &scriptedModel{script: []scriptTurn{toolTurn("c1", nil), {err: gateway401()}, finalTurn("done")}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, lookupRegistry(t, &executed), recoveryOpts(rec, time.Millisecond)...)
	ch, _ := a.RunEvents(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	deltas, done, errs := collectStream(t, ch)
	if len(errs) != 0 || done == nil || deltas != "done" || executed != 1 {
		t.Fatalf("deltas=%q errs=%v executed=%d", deltas, errs, executed)
	}
}

func TestModelRecovery_PlainRunRecovers(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	fake := &scriptedModel{script: []scriptTurn{{err: gateway401()}, finalTurn("hi")}}
	rec := &sleepRecorder{}
	a := NewReActAgent(fake, nil, nil, recoveryOpts(rec, time.Millisecond)...)
	resp, err := a.Run(context.Background(), &Request{Messages: []model.Message{{Role: "user", Content: "x"}}})
	if err != nil || resp.Text != "hi" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
}

func TestModelRecoveryDelays_EnvAndDefaults(t *testing.T) {
	t.Setenv(EnvModelRecoveryDelays, "")
	a := NewReActAgent(&scriptedModel{}, nil, nil)
	got := a.modelRecoveryDelays()
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("default delays=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default delays=%v", got)
		}
	}

	t.Setenv(EnvModelRecoveryDelays, "100, 200")
	if got := a.modelRecoveryDelays(); len(got) != 2 || got[0] != 100*time.Millisecond || got[1] != 200*time.Millisecond {
		t.Fatalf("env delays=%v", got)
	}
	for _, off := range []string{"0", "off", "OFF"} {
		t.Setenv(EnvModelRecoveryDelays, off)
		if got := a.modelRecoveryDelays(); len(got) != 0 {
			t.Fatalf("%q must disable recovery, got %v", off, got)
		}
	}
	t.Setenv(EnvModelRecoveryDelays, "garbage")
	if got := a.modelRecoveryDelays(); len(got) != 3 {
		t.Fatalf("invalid env must fall back to defaults, got %v", got)
	}

	t.Setenv(EnvModelRecoveryDelays, "")
	disabled := NewReActAgent(&scriptedModel{}, nil, nil, WithReActModelRecoveryDelays())
	if got := disabled.modelRecoveryDelays(); len(got) != 0 {
		t.Fatalf("empty option must disable, got %v", got)
	}
}
