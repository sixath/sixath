package adapter_test

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/sixath/gateway/internal/pendingswitch"
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

func (f *fakeWecomConn) RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

func writeRuntimeSSEOK(w http.ResponseWriter, answer string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	payload, _ := json.Marshal(map[string]any{"content": answer})
	_, _ = io.WriteString(w, "event: chunk\ndata: "+string(payload)+"\n\n")
	_, _ = io.WriteString(w, "event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n")
}

func writeRuntimeSSEHITL(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event: confirm_required\ndata: {\"confirmation\":{\"token\":\"t\"}}\n\n")
	_, _ = io.WriteString(w, "event: done\ndata: {\"done\":true,\"content\":\"\"}\n\n")
}

// hitlNoSurfaceMsg matches portal AggregateFinal / wecom_progress (unexported there).
const hitlNoSurfaceMsg = "hitl required but reply_mode=final has no interactive surface"

func assertFastPathNoProgressTicker(t *testing.T, calls []respondCall) {
	t.Helper()
	for _, c := range calls {
		if c.finish {
			continue
		}
		if c.content != "处理中…" && strings.Contains(c.content, "耗时") {
			t.Fatalf("fast-path finish=false must be 处理中… or lack 耗时: %+v", c)
		}
	}
}

func assertFinishedCardOnly(t *testing.T, calls []respondCall, substr string) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("respond calls=%d want 1: %+v", len(calls), calls)
	}
	if !calls[0].finish {
		t.Fatalf("expected finish=true: %+v", calls[0])
	}
	if calls[0].content == "处理中…" {
		t.Fatalf("must not leave 处理中…: %+v", calls[0])
	}
	if substr != "" && !strings.Contains(calls[0].content, substr) {
		t.Fatalf("missing %q in %q", substr, calls[0].content)
	}
	assertFastPathNoProgressTicker(t, calls)
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
			writeRuntimeSSEOK(w, "晴")
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
	if len(calls) < 3 {
		t.Fatalf("respond calls=%d want >=3: %+v", len(calls), calls)
	}
	if calls[0].finish || calls[0].content != "处理中…" {
		t.Fatalf("first call=%+v", calls[0])
	}
	last := calls[len(calls)-1]
	if !last.finish {
		t.Fatalf("last finish=false")
	}
	if calls[0].streamID == "" || calls[0].streamID != last.streamID {
		t.Fatalf("streamID mismatch: %q vs %q", calls[0].streamID, last.streamID)
	}
	wantCard := wecom.FormatReplyCard("alice", "今天天气如何", "晴")
	if last.content != wantCard {
		t.Fatalf("reply card=%q want %q", last.content, wantCard)
	}
	if strings.Contains(last.content, "耗时") || strings.Contains(last.content, "阶段") {
		t.Fatalf("final card should not contain progress fields: %q", last.content)
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
			writeRuntimeSSEOK(w, "once")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-dup","aibotid":"BOT","chatid":"C1","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"hi"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-1", ch, n, deps)
	firstCalls := len(conn.snapshot())
	if firstCalls < 3 {
		t.Fatalf("first call responds=%d want >=3: %+v", firstCalls, conn.snapshot())
	}
	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-2", ch, n, deps)

	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", turns)
	}
	calls := conn.snapshot()
	if len(calls) != firstCalls {
		t.Fatalf("expected no extra responds on duplicate msgid, got %d after %d: %+v", len(calls), firstCalls, calls)
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
			writeRuntimeSSEOK(w, "ok")
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

func TestWecomBot_TurnStreamTimeout_MapsUserError(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-timeout","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"slow"}}`)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	adapter.HandleWecomMsgCallback(ctx, conn, "req-timeout", ch, n, deps)

	calls := conn.snapshot()
	if len(calls) < 2 {
		t.Fatalf("respond calls=%d want >=2: %+v", len(calls), calls)
	}
	last := calls[len(calls)-1]
	if !last.finish {
		t.Fatal("expected finish=true on timeout")
	}
	if strings.Contains(last.content, "context deadline exceeded") {
		t.Fatalf("raw timeout leaked to user: %q", last.content)
	}
	if !strings.Contains(last.content, "操作失败，请稍后重试") {
		t.Fatalf("failure card=%q", last.content)
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

func TestWecomBot_AgentListCommand_NoTurn(t *testing.T) {
	var turns int32
	var resolveCalls int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent": "agent-1",
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			atomic.AddInt32(&resolveCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			writeRuntimeSSEOK(w, "nope")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-agents","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/agents"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-agents", ch, n, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("expected 0 turns, got %d", turns)
	}
	if atomic.LoadInt32(&resolveCalls) != 0 {
		t.Fatalf("list command should not resolve, got %d", resolveCalls)
	}
	calls := conn.snapshot()
	assertFinishedCardOnly(t, calls, "可用 Agent")
	if !strings.Contains(calls[0].content, "Ops") {
		t.Fatalf("list reply=%q", calls[0].content)
	}
}

func TestWecomBot_AgentSwitchCommand_ForceNew_NoTurn(t *testing.T) {
	var turns int32
	var gotResolve map[string]any
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent": "agent-1",
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewDecoder(r.Body).Decode(&gotResolve)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-new",
				"agent_id":   "agent-2",
				"user_id":    "u1",
				"created":    true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			writeRuntimeSSEOK(w, "nope")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-switch","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"@小天才 /agent agent-2"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-switch", ch, n, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("expected 0 turns, got %d", turns)
	}
	if gotResolve["force_new"] != true || gotResolve["agent_id"] != "agent-2" {
		t.Fatalf("resolve=%v", gotResolve)
	}
	calls := conn.snapshot()
	assertFinishedCardOnly(t, calls, "已切换到")
}

func TestWecomBot_HITL_FailureCard(t *testing.T) {
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
			writeRuntimeSSEHITL(w)
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-hitl","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"需要确认的操作"}}`)

	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-hitl", ch, n, deps)

	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("turns=%d want 1", turns)
	}
	calls := conn.snapshot()
	if len(calls) < 2 {
		t.Fatalf("respond calls=%d want >=2: %+v", len(calls), calls)
	}
	last := calls[len(calls)-1]
	if !last.finish {
		t.Fatal("expected finish=true on HITL failure")
	}
	wantCard := wecom.FormatFailureCard("alice", "需要确认的操作", hitlNoSurfaceMsg)
	if last.content != wantCard {
		t.Fatalf("failure card=%q want %q", last.content, wantCard)
	}
}

func switchAgentsPortalHandler(t *testing.T, w http.ResponseWriter, r *http.Request, turns *int32, resolveCalls *int32, gotResolve *map[string]any) bool {
	t.Helper()
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"default_agent": "agent-1",
			"agents": []map[string]any{
				{"id": "agent-1", "name": "Default"},
				{"id": "agent-2", "name": "Ops Bot"},
				{"id": "agent-3", "name": "RCA"},
			},
		})
		return true
	case r.URL.Path == "/runtime/v1/sessions/binding":
		_ = json.NewEncoder(w).Encode(map[string]string{
			"channel_id": "xiaotiancai",
			"peer_id":    "chat:C1",
			"session_id": "sess-bound",
			"agent_id":   "agent-2",
		})
		return true
	case r.URL.Path == "/runtime/v1/sessions/resolve":
		atomic.AddInt32(resolveCalls, 1)
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*gotResolve = body
		agentID, _ := body["agent_id"].(string)
		if agentID == "" {
			agentID = "agent-1"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "sess-new",
			"agent_id":   agentID,
			"user_id":    "u1",
			"created":    true,
		})
		return true
	case r.URL.Path == "/runtime/v1/turns":
		atomic.AddInt32(turns, 1)
		writeRuntimeSSEOK(w, "biz")
		return true
	case r.Method == http.MethodDelete && r.URL.Path == "/runtime/v1/sessions/binding":
		w.WriteHeader(http.StatusNoContent)
		return true
	default:
		return false
	}
}

func TestWecomBot_SwitchThenDigit_ForceNew_NoTurn(t *testing.T) {
	var turns int32
	var resolveCalls int32
	gotResolve := map[string]any{}
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		if !switchAgentsPortalHandler(t, w, r, &turns, &resolveCalls, &gotResolve) {
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}

	nSwitch := mustNormalize(t, `{"msgid":"M-sw1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-sw1", ch, nSwitch, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("after /switch turns=%d want 0", turns)
	}
	if atomic.LoadInt32(&resolveCalls) != 0 {
		t.Fatalf("after /switch resolve=%d want 0", resolveCalls)
	}
	calls := conn.snapshot()
	assertFinishedCardOnly(t, calls, "Ops Bot  ← 当前")
	if _, ok := deps.PendingSwitch.Get(ch.ID, nSwitch.PeerID, time.Now()); !ok {
		t.Fatal("expected pending after /switch")
	}

	conn2 := &fakeWecomConn{}
	nDigit := mustNormalize(t, `{"msgid":"M-sw2","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"2"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), conn2, "req-sw2", ch, nDigit, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("after digit turns=%d want 0", turns)
	}
	if atomic.LoadInt32(&resolveCalls) != 1 {
		t.Fatalf("resolve calls=%d want 1", resolveCalls)
	}
	if gotResolve["force_new"] != true || gotResolve["agent_id"] != "agent-2" {
		t.Fatalf("resolve=%v", gotResolve)
	}
	digitCalls := conn2.snapshot()
	assertFinishedCardOnly(t, digitCalls, "已切换到")
	if _, ok := deps.PendingSwitch.Get(ch.ID, nDigit.PeerID, time.Now()); ok {
		t.Fatal("pending should be cleared after digit bind")
	}
}

func TestWecomBot_SwitchThenInvalidThenDigit(t *testing.T) {
	var turns int32
	var resolveCalls int32
	gotResolve := map[string]any{}
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		if !switchAgentsPortalHandler(t, w, r, &turns, &resolveCalls, &gotResolve) {
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	peer := "chat:C1"

	nSwitch := mustNormalize(t, `{"msgid":"M-sw-inv1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), &fakeWecomConn{}, "req-inv1", ch, nSwitch, deps)

	nHello := mustNormalize(t, `{"msgid":"M-sw-inv2","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"hello"}}`)
	connHello := &fakeWecomConn{}
	adapter.HandleWecomMsgCallback(context.Background(), connHello, "req-inv2", ch, nHello, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("after hello turns=%d want 0", turns)
	}
	helloCalls := connHello.snapshot()
	assertFinishedCardOnly(t, helloCalls, "没有发给 Agent")
	if !strings.Contains(helloCalls[0].content, "请回复 1–3") {
		t.Fatalf("hello prompt=%+v", helloCalls)
	}
	if _, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now()); !ok {
		t.Fatal("pending should remain after invalid input")
	}

	connDigit := &fakeWecomConn{}
	nDigit := mustNormalize(t, `{"msgid":"M-sw-inv3","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"2"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), connDigit, "req-inv3", ch, nDigit, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("after digit turns=%d want 0", turns)
	}
	if gotResolve["agent_id"] != "agent-2" {
		t.Fatalf("resolve=%v", gotResolve)
	}
}

func TestWecomBot_ExpiredPending_BusinessTurn(t *testing.T) {
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
			writeRuntimeSSEOK(w, "ok")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	peer := "chat:C1"
	deps.PendingSwitch.Put(ch.ID, peer, pendingswitch.Entry{
		Agents: []pendingswitch.Agent{
			{ID: "agent-1", Name: "Default"},
			{ID: "agent-2", Name: "Ops Bot"},
		},
		ExpiresAt: time.Now().Add(-time.Minute),
	})

	n := mustNormalize(t, `{"msgid":"M-exp","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"今天天气如何"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), &fakeWecomConn{}, "req-exp", ch, n, deps)

	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("turns=%d want 1 after expired pending", turns)
	}
}

func TestWecomBot_SwitchShowsCurrentBinding(t *testing.T) {
	var bindingCalls int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent": "agent-1",
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/binding":
			atomic.AddInt32(&bindingCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"channel_id": "xiaotiancai",
				"peer_id":    "user:alice",
				"session_id": "sess-bound",
				"agent_id":   "agent-2",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-cur","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-cur", ch, n, deps)

	if atomic.LoadInt32(&bindingCalls) != 1 {
		t.Fatalf("binding calls=%d want 1", bindingCalls)
	}
	calls := conn.snapshot()
	assertFinishedCardOnly(t, calls, "当前：Ops Bot")
	if !strings.Contains(calls[0].content, "2. Ops Bot  ← 当前") {
		t.Fatalf("missing current marker: %q", calls[0].content)
	}
}

func TestWecomBot_PendingUnbind_ClearsAndUnbinds(t *testing.T) {
	var deleteCalls int32
	var turns int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/binding" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]string{
				"channel_id": "xiaotiancai",
				"peer_id":    "user:alice",
				"session_id": "sess-bound",
				"agent_id":   "agent-2",
			})
		case r.URL.Path == "/runtime/v1/sessions/binding" && r.Method == http.MethodDelete:
			atomic.AddInt32(&deleteCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			writeRuntimeSSEOK(w, "nope")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	peer := "user:alice"

	connSwitch := &fakeWecomConn{}
	nSwitch := mustNormalize(t, `{"msgid":"M-un-sw","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), connSwitch, "req-un-sw", ch, nSwitch, deps)
	if _, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now()); !ok {
		t.Fatal("expected pending after /switch")
	}

	connUnbind := &fakeWecomConn{}
	nUnbind := mustNormalize(t, `{"msgid":"M-unbind","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/unbind"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), connUnbind, "req-unbind", ch, nUnbind, deps)

	if atomic.LoadInt32(&deleteCalls) != 1 {
		t.Fatalf("delete binding calls=%d want 1", deleteCalls)
	}
	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("turns=%d want 0", turns)
	}
	if _, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now()); ok {
		t.Fatal("pending should be cleared after /unbind")
	}
	unbindCalls := connUnbind.snapshot()
	assertFinishedCardOnly(t, unbindCalls, "已解除绑定")
}

func TestWecomBot_TwoSwitch_RefreshesPending(t *testing.T) {
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Alpha"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/binding":
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	peer := "user:alice"

	n1 := mustNormalize(t, `{"msgid":"M-sw-a","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), &fakeWecomConn{}, "req-sw-a", ch, n1, deps)
	ent1, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now())
	if !ok {
		t.Fatal("expected pending after first /switch")
	}
	exp1 := ent1.ExpiresAt

	time.Sleep(5 * time.Millisecond)

	n2 := mustNormalize(t, `{"msgid":"M-sw-b","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), &fakeWecomConn{}, "req-sw-b", ch, n2, deps)
	ent2, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now())
	if !ok {
		t.Fatal("expected pending after second /switch")
	}
	if !ent2.ExpiresAt.After(exp1) {
		t.Fatalf("expires not refreshed: %v vs %v", ent2.ExpiresAt, exp1)
	}
}

func TestWecomBot_Who_ShowsBindingNoPending(t *testing.T) {
	var turns int32
	var resolveCalls int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent": "agent-1",
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/binding":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"channel_id": "xiaotiancai",
				"peer_id":    "user:alice",
				"session_id": "sess-bound",
				"agent_id":   "agent-2",
			})
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			atomic.AddInt32(&resolveCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			writeRuntimeSSEOK(w, "nope")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	conn := &fakeWecomConn{}
	n := mustNormalize(t, `{"msgid":"M-who","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/who"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), conn, "req-who", ch, n, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("turns=%d want 0", turns)
	}
	if atomic.LoadInt32(&resolveCalls) != 0 {
		t.Fatalf("resolve=%d want 0", resolveCalls)
	}
	if _, ok := deps.PendingSwitch.Get(ch.ID, "user:alice", time.Now()); ok {
		t.Fatal("/who must not put pending")
	}
	calls := conn.snapshot()
	assertFinishedCardOnly(t, calls, "当前绑定：Ops Bot")
}

func TestWecomBot_Who_KeepsPending(t *testing.T) {
	var turns int32
	portal := newWecomPortal(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops Bot"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/binding":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"channel_id": "xiaotiancai",
				"peer_id":    "user:alice",
				"session_id": "sess-bound",
				"agent_id":   "agent-2",
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			writeRuntimeSSEOK(w, "nope")
		default:
			http.NotFound(w, r)
		}
	})
	defer portal.Close()

	deps, ch := newWecomBotFixture(t, portal.URL)
	peer := "user:alice"

	nSwitch := mustNormalize(t, `{"msgid":"M-who-sw","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/switch"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), &fakeWecomConn{}, "req-who-sw", ch, nSwitch, deps)
	if _, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now()); !ok {
		t.Fatal("expected pending after /switch")
	}

	connWho := &fakeWecomConn{}
	nWho := mustNormalize(t, `{"msgid":"M-who-keep","aibotid":"BOT","chatid":"","chattype":"single","from":{"userid":"alice"},"msgtype":"text","text":{"content":"/who"}}`)
	adapter.HandleWecomMsgCallback(context.Background(), connWho, "req-who-keep", ch, nWho, deps)

	if atomic.LoadInt32(&turns) != 0 {
		t.Fatalf("turns=%d want 0", turns)
	}
	if _, ok := deps.PendingSwitch.Get(ch.ID, peer, time.Now()); !ok {
		t.Fatal("pending should remain after /who")
	}
	calls := connWho.snapshot()
	assertFinishedCardOnly(t, calls, "当前绑定：Ops Bot")
	if !strings.Contains(calls[0].content, "不影响选号") {
		t.Fatalf("missing pending hint: %q", calls[0].content)
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
			writeRuntimeSSEOK(w, "ok")
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
		Registry:      reg,
		Runtime:       rt,
		Sessions:      session.NewRouter(rt, 30*time.Second),
		Idempotency:   idempotency.NewStore(10 * time.Minute),
		PendingSwitch: pendingswitch.New(),
		TurnTimeout:   5 * time.Second,
	}
	return deps, ch
}
