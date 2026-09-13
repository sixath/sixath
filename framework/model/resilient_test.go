package model

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

// testRetryConfig 关闭 jitter 并把退避压到毫秒级，使断言确定且测试快速。
func testRetryConfig(maxAttempts int) RetryConfig {
	return RetryConfig{
		MaxAttempts:   maxAttempts,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
		DisableJitter: true,
	}
}

type retryFake struct {
	mu        sync.Mutex
	calls     int
	failFirst int
	err       error
	text      string
}

// next 记录一次调用；前 failFirst 次返回配置的错误。
func (f *retryFake) next() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failFirst {
		return f.calls, f.err
	}
	return f.calls, nil
}

func (f *retryFake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// retryFakeModel 仅实现 Model。
type retryFakeModel struct{ f *retryFake }

func (m *retryFakeModel) Generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	return &Generation{Text: m.f.text}, nil
}

func (m *retryFakeModel) Chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	return &Generation{Text: m.f.text}, nil
}

func (m *retryFakeModel) Embed(ctx context.Context, texts []string, opts ...Option) ([]Embedding, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	return []Embedding{{Vector: []float32{1}, Raw: "x"}}, nil
}

// retryFakeStreamingModel 实现 Model + StreamingModel。
type retryFakeStreamingModel struct{ *retryFakeModel }

func (m *retryFakeStreamingModel) ChatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	ch := make(chan string, 1)
	ch <- m.f.text
	close(ch)
	return ch, nil
}

// retryFakeToolModel 实现 Model + ChatWithTools（等价 agent.ToolCallingModel）。
type retryFakeToolModel struct{ *retryFakeModel }

func (m *retryFakeToolModel) ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	return &Generation{Text: m.f.text}, nil
}

// retryFakeToolStreamModel 实现 Model + ChatWithToolsStream。
type retryFakeToolStreamModel struct{ *retryFakeModel }

func (m *retryFakeToolStreamModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, nil, err
	}
	textCh := make(chan string, 1)
	textCh <- m.f.text
	close(textCh)
	genCh := make(chan *Generation, 1)
	genCh <- &Generation{Text: m.f.text}
	close(genCh)
	return textCh, genCh, nil
}

// retryFakeFullModel 实现 Model + Streaming + ToolCalling + ToolCallingStreaming。
type retryFakeFullModel struct{ *retryFakeModel }

func (m *retryFakeFullModel) ChatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	ch := make(chan string, 1)
	ch <- m.f.text
	close(ch)
	return ch, nil
}

func (m *retryFakeFullModel) ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, err
	}
	return &Generation{Text: m.f.text}, nil
}

func (m *retryFakeFullModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	if _, err := m.f.next(); err != nil {
		return nil, nil, err
	}
	textCh := make(chan string, 1)
	textCh <- m.f.text
	close(textCh)
	genCh := make(chan *Generation, 1)
	genCh <- &Generation{Text: m.f.text}
	close(genCh)
	return textCh, genCh, nil
}

// retryFakeMidStreamFailModel setup 永远成功，但流式在产出增量之后失败，
// 用于验证「首 token 之后不重试」。
type retryFakeMidStreamFailModel struct{ *retryFakeModel }

func (m *retryFakeMidStreamFailModel) ChatWithToolsStream(
	ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option,
) (<-chan string, <-chan *Generation, error) {
	m.f.next() // setup 成功
	textCh := make(chan string, 1)
	textCh <- "partial"
	close(textCh)
	genCh := make(chan *Generation, 1)
	genCh <- &Generation{Err: errors.New("connection reset mid-stream")}
	close(genCh)
	return textCh, genCh, nil
}

func TestWrapResilient_RetriesOn429ThenSucceeds(t *testing.T) {
	fake := &retryFake{
		failFirst: 2,
		err:       &APIStatusError{Provider: "openai", StatusCode: 429, Message: "rate limited"},
		text:      "ok",
	}
	m := WrapResilient(&retryFakeModel{f: fake}, testRetryConfig(3))

	gen, err := m.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gen.Text != "ok" {
		t.Fatalf("Text=%q want ok", gen.Text)
	}
	if got := fake.callCount(); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
}

func TestWrapResilient_DoesNotRetryOn400(t *testing.T) {
	fake := &retryFake{
		failFirst: 1,
		err:       &APIStatusError{Provider: "openai", StatusCode: 400, Message: "bad request"},
	}
	m := WrapResilient(&retryFakeModel{f: fake}, testRetryConfig(3))

	if _, err := m.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected error")
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("calls=%d want 1 (400 must not be retried)", got)
	}
}

func TestWrapResilient_ExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	fake := &retryFake{
		failFirst: 99,
		err:       &APIStatusError{Provider: "openai", StatusCode: 503, Message: "unavailable"},
	}
	var retries []int
	cfg := testRetryConfig(3)
	cfg.OnRetry = func(attempt int, err error, delay time.Duration) {
		retries = append(retries, attempt)
	}
	m := WrapResilient(&retryFakeModel{f: fake}, cfg)

	_, err := m.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	var apiErr *APIStatusError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 {
		t.Fatalf("expected APIStatusError 503, got %v", err)
	}
	if got := fake.callCount(); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
	if len(retries) != 2 || retries[0] != 1 || retries[1] != 2 {
		t.Fatalf("OnRetry attempts=%v want [1 2]", retries)
	}
}

func TestWrapResilient_StreamRetriesSetupFailure(t *testing.T) {
	fake := &retryFake{
		failFirst: 2,
		err:       &APIStatusError{Provider: "openai", StatusCode: 500, Message: "boom"},
		text:      "hello",
	}
	m := WrapResilient(&retryFakeFullModel{retryFakeModel: &retryFakeModel{f: fake}}, testRetryConfig(3))

	sm, ok := m.(StreamingModel)
	if !ok {
		t.Fatalf("wrapper %T lost StreamingModel", m)
	}
	ch, err := sm.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got := drain(ch); got != "hello" {
		t.Fatalf("stream=%q want hello", got)
	}
	if got := fake.callCount(); got != 3 {
		t.Fatalf("calls=%d want 3", got)
	}
}

func TestWrapResilient_ToolCallingStreamRetriesSetupFailure(t *testing.T) {
	fake := &retryFake{
		failFirst: 1,
		err:       &APIStatusError{Provider: "openai", StatusCode: 502, Message: "bad gateway"},
		text:      "answer",
	}
	m := WrapResilient(&retryFakeFullModel{retryFakeModel: &retryFakeModel{f: fake}}, testRetryConfig(2))

	tsm, ok := m.(ToolCallingStreamingModel)
	if !ok {
		t.Fatalf("wrapper %T lost ToolCallingStreamingModel", m)
	}
	textCh, genCh, err := tsm.ChatWithToolsStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatWithToolsStream: %v", err)
	}
	if got := drain(textCh); got != "answer" {
		t.Fatalf("stream=%q want answer", got)
	}
	gen := <-genCh
	if gen == nil || gen.Text != "answer" {
		t.Fatalf("final gen=%+v", gen)
	}
	if got := fake.callCount(); got != 2 {
		t.Fatalf("calls=%d want 2", got)
	}
}

func TestWrapResilient_DoesNotRetryAfterFirstToken(t *testing.T) {
	fake := &retryFake{text: "partial"}
	m := WrapResilient(&retryFakeMidStreamFailModel{retryFakeModel: &retryFakeModel{f: fake}}, testRetryConfig(3))

	tsm := m.(ToolCallingStreamingModel)
	textCh, genCh, err := tsm.ChatWithToolsStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("setup should succeed: %v", err)
	}
	if got := drain(textCh); got != "partial" {
		t.Fatalf("stream=%q want partial", got)
	}
	gen := <-genCh
	if gen == nil || gen.Err == nil {
		t.Fatal("mid-stream error must be surfaced, not swallowed")
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("calls=%d want 1 (no retry after deltas)", got)
	}
}

func TestWrapResilient_ParentContextCancelStopsRetry(t *testing.T) {
	fake := &retryFake{failFirst: 99, err: &APIStatusError{StatusCode: 500}}
	m := WrapResilient(&retryFakeModel{f: fake}, testRetryConfig(5))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Chat(ctx, []Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
	if got := fake.callCount(); got != 0 {
		t.Fatalf("calls=%d want 0 (canceled before first attempt)", got)
	}
}

func TestWrapResilient_DeadlineDuringBackoffStopsRetry(t *testing.T) {
	fake := &retryFake{failFirst: 99, err: &APIStatusError{StatusCode: 500}}
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 200 * time.Millisecond, MaxDelay: 200 * time.Millisecond, DisableJitter: true}
	m := WrapResilient(&retryFakeModel{f: fake}, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := m.Chat(ctx, []Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v want context.DeadlineExceeded", err)
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("calls=%d want 1 (must not keep hammering after deadline)", got)
	}
}

func TestWrapResilient_PreservesCapabilities(t *testing.T) {
	tests := []struct {
		name         string
		inner        Model
		wantStream   bool
		wantTools    bool
		wantToolsStr bool
	}{
		{"model only", &retryFakeModel{f: &retryFake{}}, false, false, false},
		{"streaming", &retryFakeStreamingModel{retryFakeModel: &retryFakeModel{f: &retryFake{}}}, true, false, false},
		{"tool calling", &retryFakeToolModel{retryFakeModel: &retryFakeModel{f: &retryFake{}}}, false, true, false},
		{"tool streaming", &retryFakeToolStreamModel{retryFakeModel: &retryFakeModel{f: &retryFake{}}}, false, false, true},
		{"full", &retryFakeFullModel{retryFakeModel: &retryFakeModel{f: &retryFake{}}}, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := WrapResilient(tt.inner, testRetryConfig(1))
			if _, ok := w.(StreamingModel); ok != tt.wantStream {
				t.Fatalf("StreamingModel=%v want %v (wrapper %T)", ok, tt.wantStream, w)
			}
			if _, ok := w.(toolCallingModel); ok != tt.wantTools {
				t.Fatalf("ToolCallingModel=%v want %v (wrapper %T)", ok, tt.wantTools, w)
			}
			if _, ok := w.(ToolCallingStreamingModel); ok != tt.wantToolsStr {
				t.Fatalf("ToolCallingStreamingModel=%v want %v (wrapper %T)", ok, tt.wantToolsStr, w)
			}
			if got := UnwrapModel(w); got != tt.inner {
				t.Fatalf("UnwrapModel=%T want %T", got, tt.inner)
			}
		})
	}
}

func TestWrapResilient_NilModel(t *testing.T) {
	if got := WrapResilient(nil, RetryConfig{}); got != nil {
		t.Fatalf("WrapResilient(nil)=%v want nil", got)
	}
}

func TestUnwrapModel_ReturnsInputWhenNotDecorated(t *testing.T) {
	inner := &retryFakeModel{f: &retryFake{}}
	if got := UnwrapModel(inner); got != Model(inner) {
		t.Fatalf("UnwrapModel=%T want same instance", got)
	}
}

func TestRetryConfig_BackoffSequence(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 250 * time.Millisecond, DisableJitter: true}.normalized()
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
	}
	for i, w := range want {
		if got := cfg.backoff(i+1, 0); got != w {
			t.Fatalf("backoff(attempt=%d)=%v want %v", i+1, got, w)
		}
	}
}

func TestRetryConfig_BackoffHonoursRetryAfterAndCap(t *testing.T) {
	cfg := RetryConfig{BaseDelay: time.Second, MaxDelay: 2 * time.Second, DisableJitter: true}.normalized()
	if got := cfg.backoff(1, 1500*time.Millisecond); got != 1500*time.Millisecond {
		t.Fatalf("Retry-After honoured: got %v", got)
	}
	if got := cfg.backoff(1, time.Hour); got != 2*time.Second {
		t.Fatalf("Retry-After capped by MaxDelay: got %v", got)
	}
}

func TestRetryConfig_JitterStaysWithinBound(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 50 * time.Millisecond, MaxDelay: 50 * time.Millisecond}.normalized()
	for i := 0; i < 50; i++ {
		got := cfg.backoff(1, 0)
		if got <= 0 || got > 50*time.Millisecond {
			t.Fatalf("jittered delay %v out of (0, 50ms]", got)
		}
	}
}

// netErrStub 用于验证 net.Error 分类（超时/临时错误值得重试）。
type netErrStub struct{ timeout bool }

func (e netErrStub) Error() string   { return "net stub" }
func (e netErrStub) Timeout() bool   { return e.timeout }
func (e netErrStub) Temporary() bool { return true }

var _ net.Error = netErrStub{}

func TestRetryableModelError_Classification(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil", nil, false},
		{"429", &APIStatusError{StatusCode: 429}, true},
		{"408", &APIStatusError{StatusCode: 408}, true},
		{"500", &APIStatusError{StatusCode: 500}, true},
		{"503", &APIStatusError{StatusCode: 503}, true},
		{"400", &APIStatusError{StatusCode: 400}, false},
		{"401", &APIStatusError{StatusCode: 401}, false},
		{"404", &APIStatusError{StatusCode: 404}, false},
		{"openai 429", &openai.APIError{HTTPStatusCode: 429}, true},
		{"openai 403", &openai.APIError{HTTPStatusCode: 403}, false},
		{"openai request transport", &openai.RequestError{}, true},
		// SDK 在响应体非 JSON 时构造的形态：外层带真实状态码、内层状态码为 0。
		{
			"openai request wrapping APIError",
			&openai.RequestError{HTTPStatusCode: 502, Err: &openai.APIError{}},
			true,
		},
		{
			"openai request wrapping APIError 400",
			&openai.RequestError{HTTPStatusCode: 400, Err: &openai.APIError{}},
			false,
		},
		{"net error", netErrStub{timeout: true}, true},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"canceled", context.Canceled, false},
		{"generic", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := retryableModelError(tt.err)
			if got != tt.retryable {
				t.Fatalf("retryableModelError(%v)=%v want %v", tt.err, got, tt.retryable)
			}
		})
	}
}

func TestRetryableModelError_PropagatesRetryAfter(t *testing.T) {
	err := &APIStatusError{StatusCode: 429, RetryAfter: 3 * time.Second}
	retryable, retryAfter := retryableModelError(err)
	if !retryable || retryAfter != 3*time.Second {
		t.Fatalf("retryable=%v retryAfter=%v", retryable, retryAfter)
	}
}

func TestParseRetryAfterHeader(t *testing.T) {
	if got := parseRetryAfterHeader("2"); got != 2*time.Second {
		t.Fatalf("seconds: got %v", got)
	}
	if got := parseRetryAfterHeader(""); got != 0 {
		t.Fatalf("empty: got %v", got)
	}
	if got := parseRetryAfterHeader("nonsense"); got != 0 {
		t.Fatalf("nonsense: got %v", got)
	}
	if got := parseRetryAfterHeader("-5"); got != 0 {
		t.Fatalf("negative: got %v", got)
	}
}

func drain(ch <-chan string) string {
	out := ""
	for s := range ch {
		out += s
	}
	return out
}
