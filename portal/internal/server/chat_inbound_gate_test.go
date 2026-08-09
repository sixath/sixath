package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func TestPublicChatInboundGate_DisabledRejectsInbound(t *testing.T) {
	ConfigurePublicInbound(false)
	t.Cleanup(func() { ConfigurePublicInbound(true) })

	srv := newGateTestServer()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"send_message", http.MethodPost, "/api/v1/sessions/sess-1/messages", `{"content":"hi"}`},
		{"send_message_stream", http.MethodPost, "/api/v1/sessions/sess-1/messages/stream", `{"content":"hi"}`},
		{"create_session", http.MethodPost, "/api/v1/agents/agent-1/sessions", `{"title":"t"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Ret struct {
					Reason string `json:"reason"`
				} `json:"ret"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v body=%s", err, rec.Body.String())
			}
			if body.Ret.Reason != "FORBIDDEN" {
				t.Fatalf("reason = %q, want FORBIDDEN", body.Ret.Reason)
			}
		})
	}
}

func TestPublicChatInboundGate_DisabledAllowsRuntimeAndChannel(t *testing.T) {
	ConfigurePublicInbound(false)
	t.Cleanup(func() { ConfigurePublicInbound(true) })

	srv := newGateTestServer()

	for _, path := range []string{
		"/runtime/v1/sessions",
		"/api/v1/channels",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != `{"ok":true}` {
				t.Fatalf("body = %q, want ok", got)
			}
		})
	}
}

func TestPublicChatInboundGate_EnabledAllowsInbound(t *testing.T) {
	ConfigurePublicInbound(true)
	t.Cleanup(func() { ConfigurePublicInbound(true) })

	srv := newGateTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/sess-1/messages", strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func newGateTestServer() *khttp.Server {
	ok := func(ctx khttp.Context) error {
		ctx.Response().Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(ctx.Response(), `{"ok":true}`)
		return nil
	}
	srv := khttp.NewServer(khttp.Filter(PublicChatInboundFilter()))
	r := srv.Route("/")
	r.POST("/api/v1/sessions/{session_id}/messages", ok)
	r.POST("/api/v1/sessions/{session_id}/messages/stream", ok)
	r.POST("/api/v1/agents/{agent_id}/sessions", ok)
	r.GET("/api/v1/channels", ok)
	r.GET("/runtime/v1/sessions", ok)
	return srv
}
