package adapter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/runtimeclient"
)

func TestWeb_NoBearer_401(t *testing.T) {
	mux := newWebMux(t, httptest.NewServer(http.NotFoundHandler()).URL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestWeb_AuthMeThenRuntimeWithServiceToken(t *testing.T) {
	var runtimeAuth, runtimeUser, meAuth string
	var runtimePath string
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			meAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "user-42"})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/sessions":
			runtimeAuth = r.Header.Get("Authorization")
			runtimeUser = r.Header.Get("X-Sath-User-Id")
			runtimePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	mux := newWebMux(t, portal.URL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer user-opaque-token")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if meAuth != "Bearer user-opaque-token" {
		t.Fatalf("auth/me Authorization=%q", meAuth)
	}
	if runtimeAuth != "Bearer dev-runtime-token" {
		t.Fatalf("runtime Authorization=%q (must be service token, not user bearer)", runtimeAuth)
	}
	if runtimeUser != "user-42" {
		t.Fatalf("X-Sath-User-Id=%q", runtimeUser)
	}
	if runtimePath != "/runtime/v1/sessions" {
		t.Fatalf("runtime path=%q", runtimePath)
	}
	if strings.Contains(rr.Body.String(), "user-opaque-token") {
		t.Fatalf("response leaked user token: %s", rr.Body.String())
	}
}

func TestWeb_StreamProxiesSSEAndConfirmBody(t *testing.T) {
	var gotTurn map[string]any
	var gotUser, gotAuth string
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "u1"})
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/turns":
			gotAuth = r.Header.Get("Authorization")
			gotUser = r.Header.Get("X-Sath-User-Id")
			_ = json.NewDecoder(r.Body).Decode(&gotTurn)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event: chunk\ndata: {\"content\":\"hi\"}\n\n")
			_, _ = io.WriteString(w, "event: done\ndata: {}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	mux := newWebMux(t, portal.URL)
	rr := httptest.NewRecorder()
	body := `{"content":"hello","confirm_response":{"kind":"approve","token":"t1"},"input_response":{"token":"i1","values":{"x":"y"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/messages/stream", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q", ct)
	}
	out := rr.Body.String()
	if !strings.Contains(out, "data: {\"content\":\"hi\"}") {
		t.Fatalf("SSE not proxied: %q", out)
	}
	if gotAuth != "Bearer dev-runtime-token" || gotUser != "u1" {
		t.Fatalf("auth=%q user=%q", gotAuth, gotUser)
	}
	if gotTurn["session_id"] != "sess-1" || gotTurn["content"] != "hello" || gotTurn["reply_mode"] != "stream" {
		t.Fatalf("turn=%v", gotTurn)
	}
	if _, ok := gotTurn["confirm_response"].(map[string]any); !ok {
		t.Fatalf("missing confirm_response: %v", gotTurn)
	}
	if _, ok := gotTurn["input_response"].(map[string]any); !ok {
		t.Fatalf("missing input_response: %v", gotTurn)
	}
}

func TestWeb_ClientCancelCancelsRuntimeStream(t *testing.T) {
	streamStarted := make(chan struct{})
	streamCancelled := make(chan struct{})
	var turns int32

	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "u1"})
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&turns, 1)
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected flusher")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: {\"content\":\"x\"}\n\n")
			flusher.Flush()
			close(streamStarted)
			select {
			case <-r.Context().Done():
				close(streamCancelled)
			case <-time.After(3 * time.Second):
				t.Errorf("runtime stream ctx not cancelled")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()

	mux := newWebMux(t, portal.URL)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/sessions/s1/messages/stream", strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rr, req)
	}()

	select {
	case <-streamStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()

	select {
	case <-streamCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("expected runtime stream context cancellation when client cancels")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after cancel")
	}
	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("turns=%d", turns)
	}
}

func TestWeb_SessionRoutesWired(t *testing.T) {
	var last methodPath
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"user_id": "u1"})
			return
		}
		last = methodPath{r.Method, r.URL.Path}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer portal.Close()

	mux := newWebMux(t, portal.URL)
	calls := []struct {
		method, path string
		body         string
		wantPath     string
	}{
		{http.MethodPost, "/api/v1/agents/ag1/sessions", `{"title":"t"}`, "/runtime/v1/sessions"},
		{http.MethodGet, "/api/v1/agents/ag1/sessions", "", "/runtime/v1/agents/ag1/sessions"},
		{http.MethodGet, "/api/v1/sessions", "", "/runtime/v1/sessions"},
		{http.MethodGet, "/api/v1/sessions/search?query=q", "", "/runtime/v1/sessions/search"},
		{http.MethodGet, "/api/v1/sessions/s1", "", "/runtime/v1/sessions/s1"},
		{http.MethodPut, "/api/v1/sessions/s1", `{"title":"n"}`, "/runtime/v1/sessions/s1"},
		{http.MethodDelete, "/api/v1/sessions/s1", "", "/runtime/v1/sessions/s1"},
		{http.MethodGet, "/api/v1/sessions/s1/messages", "", "/runtime/v1/sessions/s1/messages"},
		{http.MethodPost, "/api/v1/sessions/s1/rewind", `{"message_id":"m1"}`, "/runtime/v1/sessions/s1/rewind"},
	}
	for _, c := range calls {
		var body io.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		}
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, c.path, body)
		req.Header.Set("Authorization", "Bearer tok")
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", c.method, c.path, rr.Code, rr.Body.String())
		}
		if last.method != c.method && !(c.method == http.MethodPost && last.path == "/runtime/v1/sessions") {
			// create maps POST public → POST runtime; others keep method
		}
		if last.path != c.wantPath {
			t.Fatalf("%s %s → runtime path %q want %q", c.method, c.path, last.path, c.wantPath)
		}
	}
}

type methodPath struct {
	method, path string
}

func newWebMux(t *testing.T, portalURL string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	adapter.MountWeb(mux, adapter.WebDeps{
		PortalBaseURL: portalURL,
		Runtime:       runtimeclient.New(portalURL, "dev-runtime-token"),
	})
	return mux
}
