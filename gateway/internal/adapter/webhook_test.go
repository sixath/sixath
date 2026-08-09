package adapter_test

import (
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
	"github.com/sixath/gateway/internal/reply"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

func TestWebhook_BadSecret_401(t *testing.T) {
	h, _, cleanup := newWebhookFixture(t, channelYAML(true, nil))
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "wrong-secret", "127.0.0.1:1", map[string]any{
		"content": "hi",
		"peer_id": "p1",
	})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_DisabledChannel_410(t *testing.T) {
	h, _, cleanup := newWebhookFixture(t, channelYAML(false, nil))
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content": "hi",
		"peer_id": "p1",
	})
	if rr.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_IPNotWhitelisted_403(t *testing.T) {
	h, _, cleanup := newWebhookFixture(t, channelYAML(true, []string{"10.0.0.1"}))
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "192.168.1.9:55", map[string]any{
		"content": "hi",
		"peer_id": "p1",
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_ReplyURL_LoopbackRejected_400(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "0")
	h, _, cleanup := newWebhookFixture(t, channelYAML(true, nil))
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content":   "hi",
		"peer_id":   "p1",
		"reply_url": "http://127.0.0.1:9999/cb",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_ReplyURL_PrivateRejected_400(t *testing.T) {
	h, _, cleanup := newWebhookFixture(t, channelYAML(true, nil))
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content":   "hi",
		"peer_id":   "p1",
		"reply_url": "http://10.0.0.8/cb",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_Async_202_PostsReplyURL(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "1")
	var turns int32
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "user-from-resolve",
				"created":    true,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			if got := r.Header.Get("X-Sath-User-Id"); got != "user-from-resolve" {
				t.Errorf("turns X-Sath-User-Id=%q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"correlation_id": "c-portal",
				"status":         "ok",
				"content":        "hello back",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	replyCh := make(chan map[string]any, 1)
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		replyCh <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer replySrv.Close()

	h, _, cleanup := newWebhookFixtureWithPortal(t, channelYAML(true, nil), portal.URL)
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content":    "hi",
		"peer_id":    "p1",
		"reply_url":  replySrv.URL,
		"reply_mode": "async",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var accepted map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	corr, _ := accepted["correlation_id"].(string)
	if corr == "" {
		t.Fatalf("missing correlation_id: %v", accepted)
	}

	select {
	case body := <-replyCh:
		if body["status"] != "ok" || body["content"] != "hello back" {
			t.Fatalf("reply body=%v", body)
		}
		if body["correlation_id"] != corr {
			t.Fatalf("reply correlation_id=%v want %s", body["correlation_id"], corr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for reply_url POST")
	}
	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("turns=%d", turns)
	}
}

func TestWebhook_IdempotencyKey_NoSecondTurn(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "1")
	var turns int32
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    false,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"correlation_id": "ignored",
				"status":         "ok",
				"content":        "once",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	var replyMu sync.Mutex
	var replies int
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyMu.Lock()
		replies++
		replyMu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer replySrv.Close()

	h, _, cleanup := newWebhookFixtureWithPortal(t, channelYAML(true, nil), portal.URL)
	defer cleanup()

	body := map[string]any{
		"content":          "hi",
		"peer_id":          "p1",
		"reply_url":        replySrv.URL,
		"idempotency_key":  "idem-1",
		"reply_mode":       "async",
	}
	rr1 := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", body)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", rr1.Code, rr1.Body.String())
	}
	var a1 map[string]any
	_ = json.Unmarshal(rr1.Body.Bytes(), &a1)
	corr1, _ := a1["correlation_id"].(string)

	// Wait for first turn to complete so store holds a finished entry.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&turns) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	rr2 := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", body)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", rr2.Code, rr2.Body.String())
	}
	var a2 map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &a2)
	corr2, _ := a2["correlation_id"].(string)
	if corr1 == "" || corr1 != corr2 {
		t.Fatalf("correlation mismatch: %q vs %q", corr1, corr2)
	}
	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", turns)
	}
}

func TestWebhook_Sync_200(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"correlation_id": "c1",
				"status":         "ok",
				"content":        "sync-ok",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	h, _, cleanup := newWebhookFixtureWithPortal(t, channelYAML(true, nil), portal.URL)
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content":    "hi",
		"peer_id":    "p1",
		"reply_mode": "sync",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["content"] != "sync-ok" {
		t.Fatalf("body=%v", body)
	}
	if body["correlation_id"] == "" {
		t.Fatalf("missing correlation_id: %v", body)
	}
}

func TestWebhook_PortalFailed_ReplyURLFailed(t *testing.T) {
	t.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "1")
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1",
				"agent_id":   "agent-1",
				"user_id":    "u1",
				"created":    true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	replyCh := make(chan map[string]any, 1)
	replySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		replyCh <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer replySrv.Close()

	h, _, cleanup := newWebhookFixtureWithPortal(t, channelYAML(true, nil), portal.URL)
	defer cleanup()

	rr := postHook(t, h, "demo-webhook", "dev-webhook-secret", "127.0.0.1:1", map[string]any{
		"content":   "hi",
		"peer_id":   "p1",
		"reply_url": replySrv.URL,
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case body := <-replyCh:
		if body["status"] != "failed" {
			t.Fatalf("reply body=%v", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for failed reply_url")
	}
}

func channelYAML(enabled bool, whitelist []string) string {
	wl := "[]"
	if len(whitelist) > 0 {
		parts := make([]string, len(whitelist))
		for i, ip := range whitelist {
			parts[i] = `"` + ip + `"`
		}
		wl = "[" + strings.Join(parts, ", ") + "]"
	}
	en := "true"
	if !enabled {
		en = "false"
	}
	return `
channels:
  - id: demo-webhook
    type: webhook
    default_agent: "agent-1"
    webhook_secret: "dev-webhook-secret"
    ip_whitelist: ` + wl + `
    enabled: ` + en + `
    default_reply_mode: async
`
}

func newWebhookFixture(t *testing.T, yaml string) (http.Handler, string, func()) {
	t.Helper()
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "sess-1",
			"agent_id":   "agent-1",
			"user_id":    "u1",
			"created":    true,
		})
	}))
	h, cleanupInner := buildHandler(t, yaml, portal.URL)
	return h, portal.URL, func() {
		cleanupInner()
		portal.Close()
	}
}

func newWebhookFixtureWithPortal(t *testing.T, yaml, portalURL string) (http.Handler, string, func()) {
	t.Helper()
	h, cleanup := buildHandler(t, yaml, portalURL)
	return h, portalURL, cleanup
}

func buildHandler(t *testing.T, yaml, portalURL string) (http.Handler, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := channel.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rt := runtimeclient.New(portalURL, "dev-runtime-token")
	h := adapter.NewWebhookHandler(adapter.WebhookDeps{
		Registry:     reg,
		Runtime:      rt,
		Sessions:     session.NewRouter(rt, 30*time.Second),
		Idempotency:  idempotency.NewStore(10 * time.Minute),
		Reply:        reply.NewDispatcher(nil),
		TurnTimeout:  5 * time.Second,
	})
	mux := http.NewServeMux()
	mux.Handle("POST /hooks/{channel_id}", h)
	return mux, func() {}
}

func postHook(t *testing.T, h http.Handler, channelID, secret, remoteAddr string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/hooks/"+channelID, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Secret", secret)
	req.RemoteAddr = remoteAddr
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
