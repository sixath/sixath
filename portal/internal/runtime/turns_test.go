package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"
	"backend/internal/service"
)

type fakeTurns struct {
	mu sync.Mutex

	finalContent string
	finalErr     error

	streamEvents []service.ChatStreamEvent
	streamErr    error

	// Cancel observation for TestTurns_CancelContext
	started    chan struct{}
	sawCancel  bool
	blockUntil <-chan struct{} // never closed → wait on ctx.Done

	// emitEventsThenBlock: send streamEvents first, then wait on ctx (for final timeout+partial).
	emitEventsThenBlock       bool
	emitDeadlineErrorOnCancel bool
	savedOK                   bool
}

func (f *fakeTurns) SendMessage(_ context.Context, req *chatv1.SendMessageRequest) (*chatv1.MessageReply, error) {
	if f.finalErr != nil {
		return nil, f.finalErr
	}
	content := f.finalContent
	if content == "" {
		content = "assistant:" + req.GetContent()
	}
	return &chatv1.MessageReply{
		Id:        "msg-1",
		SessionId: req.GetSessionId(),
		Role:      "assistant",
		Content:   content,
	}, nil
}

func (f *fakeTurns) SendMessageStream(ctx context.Context, req *chatv1.SendMessageRequest) (<-chan service.ChatStreamEvent, string, error) {
	f.mu.Lock()
	started := f.started
	f.started = nil
	block := f.blockUntil
	events := append([]service.ChatStreamEvent(nil), f.streamEvents...)
	streamErr := f.streamErr
	f.mu.Unlock()

	if started != nil {
		close(started)
	}
	if streamErr != nil {
		return nil, req.GetSessionId(), streamErr
	}

	f.mu.Lock()
	emitThenBlock := f.emitEventsThenBlock
	emitDeadline := f.emitDeadlineErrorOnCancel
	f.mu.Unlock()

	ch := make(chan service.ChatStreamEvent, 8)
	go func() {
		defer close(ch)
		sendEvents := func() {
			for _, ev := range events {
				select {
				case <-ctx.Done():
					return
				case ch <- ev:
				}
			}
		}
		waitBlock := func() {
			if block == nil {
				return
			}
			select {
			case <-ctx.Done():
				f.mu.Lock()
				f.sawCancel = true
				f.mu.Unlock()
				if emitDeadline {
					ch <- service.ChatStreamEvent{Type: service.ChatStreamEventError, Error: context.DeadlineExceeded.Error()}
				}
			case <-block:
			}
		}
		if emitThenBlock {
			sendEvents()
			waitBlock()
			return
		}
		if block != nil {
			waitBlock()
			if ctx.Err() != nil {
				return
			}
		}
		sendEvents()
		if len(events) == 0 && block == nil {
			ch <- service.ChatStreamEvent{Type: service.ChatStreamEventChunk, Content: "stream-hi"}
		}
	}()
	return ch, req.GetSessionId(), nil
}

func (f *fakeTurns) SaveAssistantMessage(_ context.Context, _ string, content string, _ map[string]any) (*chatv1.MessageReply, error) {
	f.mu.Lock()
	f.savedOK = true
	f.mu.Unlock()
	return &chatv1.MessageReply{Id: "asst-1", Content: content}, nil
}

func (f *fakeTurns) canceled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sawCancel
}

func turnTestService(chat *fakeChat, turns *fakeTurns) *Service {
	if chat == nil {
		chat = newFakeChat()
	}
	svc := newTestService(chat, nil, &fakeSessions{byID: chat.sessions})
	svc.turns = turns
	return svc
}

func TestTurns_FinalReturnsJSON(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-t", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	// final aggregates assistant text from the stream path (HITL-aware).
	turns := &fakeTurns{streamEvents: []service.ChatStreamEvent{
		{Type: service.ChatStreamEventChunk, Content: "final-reply"},
	}}
	srv := testRuntimeServer(t, turnTestService(chat, turns))

	body := `{"session_id":"` + sess.ID + `","content":"hi","reply_mode":"final","correlation_id":"corr-1"}`
	req := runtimeReq(http.MethodPost, "/runtime/v1/turns", body, "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		CorrelationID string `json:"correlation_id"`
		Status        string `json:"status"`
		Content       string `json:"content"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if out.CorrelationID != "corr-1" || out.Status != "ok" || out.Content != "final-reply" || out.Error != "" {
		t.Fatalf("unexpected final body: %+v", out)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestTurns_StreamSetsSSEHeaders(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-t", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	turns := &fakeTurns{streamEvents: []service.ChatStreamEvent{
		{Type: service.ChatStreamEventChunk, Content: "hello"},
	}}
	srv := testRuntimeServer(t, turnTestService(chat, turns))

	body := `{"session_id":"` + sess.ID + `","content":"hi","reply_mode":"stream"}`
	req := runtimeReq(http.MethodPost, "/runtime/v1/turns", body, "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), "event: chunk") {
		t.Fatalf("body missing chunk event: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("body missing done event: %s", rec.Body.String())
	}
}

func TestTurns_OwnsSessionACL(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "owner"), "agent-t", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := testRuntimeServer(t, turnTestService(chat, &fakeTurns{finalContent: "x"}))

	body := `{"session_id":"` + sess.ID + `","content":"hi","reply_mode":"final"}`
	req := runtimeReq(http.MethodPost, "/runtime/v1/turns", body, "intruder", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestTurns_CancelContext(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-t", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	turns := &fakeTurns{
		started:    started,
		blockUntil: make(chan struct{}), // never closed → wait on ctx
	}
	srv := testRuntimeServer(t, turnTestService(chat, turns))

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"session_id":"` + sess.ID + `","content":"hi","reply_mode":"stream"}`
	req := runtimeReq(http.MethodPost, "/runtime/v1/turns", body, "user-1", true).WithContext(ctx)

	done := make(chan struct{})
	rec := httptest.NewRecorder()
	go func() {
		defer close(done)
		srv.ServeHTTP(rec, req)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn runner did not start")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after cancel")
	}
	if !turns.canceled() {
		t.Fatal("expected turn runner to observe ctx.Done")
	}
}

func TestTurns_FinalTimeoutFails(t *testing.T) {
	old := turnFinalTimeout
	turnFinalTimeout = 40 * time.Millisecond
	t.Cleanup(func() { turnFinalTimeout = old })

	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-t", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	turns := &fakeTurns{
		streamEvents: []service.ChatStreamEvent{
			{Type: service.ChatStreamEventChunk, Content: "partial-ok"},
		},
		emitEventsThenBlock:       true,
		blockUntil:                make(chan struct{}), // never closed
		emitDeadlineErrorOnCancel: true,
	}
	srv := testRuntimeServer(t, turnTestService(chat, turns))

	body := `{"session_id":"` + sess.ID + `","content":"hi","reply_mode":"final","correlation_id":"corr-to"}`
	req := runtimeReq(http.MethodPost, "/runtime/v1/turns", body, "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		CorrelationID string `json:"correlation_id"`
		Status        string `json:"status"`
		Content       string `json:"content"`
		Error         string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if out.Status != "failed" {
		t.Fatalf("status = %q, want failed; body=%+v", out.Status, out)
	}
	if out.Error == "" {
		t.Fatal("expected timeout/deadline error")
	}
	if out.Content != "partial-ok" {
		t.Fatalf("content = %q, want scrubbed/partial partial-ok", out.Content)
	}
	if turns.savedOK {
		t.Fatal("must not persist assistant message on timeout")
	}
}
