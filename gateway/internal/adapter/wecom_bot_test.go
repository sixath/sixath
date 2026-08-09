package adapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
	"github.com/sixath/gateway/internal/wecom"
)

type fakeWecomConn struct {
	mu    sync.Mutex
	calls []respondCall
}

type respondCall struct {
	reqID    string
	streamID string
	content  string
	finish   bool
}

func (f *fakeWecomConn) RespondStream(_ context.Context, reqID, streamID, content string, finish bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, respondCall{reqID: reqID, streamID: streamID, content: content, finish: finish})
	return nil
}

func (f *fakeWecomConn) snapshot() []respondCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]respondCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestWecomBot_TextTurn_ReplyCard(t *testing.T) {
	var turns int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body["content"].(string), "今天天气如何") {
				t.Errorf("turn content=%v", body["content"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"correlation_id": "c-portal",
				"status":         "ok",
				"content":        "晴",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"@小天才 今天天气如何"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-abc", ch, n, deps)

	calls := conn.snapshot()
	if len(calls) != 2 {
		t.Fatalf("respond calls=%d want 2: %+v", len(calls), calls)
	}
	if calls[0].finish || calls[0].content != "处理中…" {
		t.Fatalf("first call=%+v", calls[0])
	}
	if !calls[1].finish {
		t.Fatalf("second finish=false")
	}
	if calls[0].streamID == "" || calls[0].streamID != calls[1].streamID {
		t.Fatalf("streamID mismatch: %q vs %q", calls[0].streamID, calls[1].streamID)
	}
	wantCard := wecom.FormatReplyCard("alice", "今天天气如何", "晴")
	if calls[1].content != wantCard {
		t.Fatalf("reply card=%q want %q", calls[1].content, wantCard)
	}
	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("turns=%d", turns)
	}
}

func TestWecomBot_IdempotentMsgID(t *testing.T) {
	var turns int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    false,
			})
		case "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"content": "once",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-dup","aibotid":"BOT","chatid":"C1","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"hi"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-1", ch, n, deps)
	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-2", ch, n, deps)

	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", turns)
	}
	calls := conn.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 responds (ack+final), got %d: %+v", len(calls), calls)
	}
}

func TestWecomBot_GroupPeer(t *testing.T) {
	var gotPeer string
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/sessions/resolve":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotPeer, _ = body["peer_id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-g",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case "/runtime/v1/turns":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "content": "ok"})
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-g","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"@小天才 hi"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-g", ch, n, deps)

	if gotPeer != "chat:C1" {
		t.Fatalf("peer_id=%q want chat:C1", gotPeer)
	}
}

func TestWecomBot_TurnFailure_FailureCard(t *testing.T) {
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case "/runtime/v1/turns":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-fail","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"今天天气如何"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-fail", ch, n, deps)

	calls := conn.snapshot()
	if len(calls) != 2 {
		t.Fatalf("respond calls=%d want 2: %+v", len(calls), calls)
	}
	if !calls[1].finish {
		t.Fatal("expected finish=true on failure")
	}
	if !strings.Contains(calls[1].content, "发起人：alice") || !strings.Contains(calls[1].content, "问题：今天天气如何") {
		t.Fatalf("failure card=%q", calls[1].content)
	}
	errBody := strings.TrimSpace(strings.TrimPrefix(calls[1].content, "发起人：alice\n问题：今天天气如何\n\n"))
	if errBody == "" {
		t.Fatalf("missing error body in %q", calls[1].content)
	}
	wantPrefix := wecom.FormatFailureCard("alice", "今天天气如何", errBody)
	if calls[1].content != wantPrefix {
		t.Fatalf("failure card=%q want %q", calls[1].content, wantPrefix)
	}
}

func TestWecomBot_ReqIDPassthrough(t *testing.T) {
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case "/runtime/v1/turns":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "content": "ok"})
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-req","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"bob"},"msgtype":"text","text":{"content":"ping"}}`)

	const reqID = "headers-req-id-xyz"
	adapter.HandleWecomMsgCallback(context.Background(), conn, reqID, ch, n, deps)

	for i, c := range conn.snapshot() {
		if c.reqID != reqID {
			t.Fatalf("call[%d].reqID=%q want %q", i, c.reqID, reqID)
		}
	}
}

func mustNormalize(t *testing.T, raw string) wecom.Normalized {
	t.Helper()
	n, err := wecom.NormalizeMsgBody([]byte(raw), wecom.NormalizeOpts{BotNames: []string{"小天才"}, BotID: "BOT"})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func newWecomPortal(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(h)
}

func newWecomBotFixture(t *testing.T, portalURL string) (adapter.WecomBotDeps, channel.Channel) {
	t.Helper()
	yaml := `
channels:
  - id: xiaotiancai
    type: wecom_bot
    default_agent: "agent-1"
    enabled: true
    bot_id: "BOT"
    secret: "SECRET"
    bot_names: ["小天才"]
    ws_url: "wss://example.test/ws"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := channel.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := reg.Get("xiaotiancai")
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeclient.New(portalURL, "dev-runtime-token")
	deps := adapter.WecomBotDeps{
		Registry:    reg,
		Runtime:     rt,
		Sessions:    session.NewRouter(rt, 30*time.Second),
		Idempotency: idempotency.NewStore(10 * time.Minute),
		TurnTimeout: 5 * time.Second,
	}
	return deps, ch
}
