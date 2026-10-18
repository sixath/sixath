package runtimeclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ListGatewayChannelsMapsIlinkFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"channels": []map[string]any{{
				"id":              "wx-1",
				"type":            "weixin_ilink",
				"enabled":         true,
				"bot_token":       "bt",
				"base_url":        "https://ilinkai.weixin.qq.com",
				"generation":      3,
				"session_expired": true,
				"allowed_peers":   []string{"a@im.wechat", "b@im.wechat"},
			}},
		})
	}))
	defer srv.Close()

	chs, err := New(srv.URL, "rt").ListGatewayChannels(context.Background())
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	ch := chs[0]
	if ch.BotToken != "bt" || ch.BaseURL != "https://ilinkai.weixin.qq.com" || ch.Generation != 3 || !ch.SessionExpired {
		t.Fatalf("ch=%+v", ch)
	}
	if len(ch.AllowedPeers) != 2 || ch.AllowedPeers[1] != "b@im.wechat" {
		t.Fatalf("allowed_peers=%v", ch.AllowedPeers)
	}
}

func TestClient_IlinkRuntimeEndpoints(t *testing.T) {
	var last callSeen
	conflict := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last = callSeen{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(body)}
		if conflict {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":409,"reason":"ILINK_GENERATION_MISMATCH"}`))
			return
		}
		switch {
		case r.URL.Path == "/runtime/v1/gateway/channels/wx-1/ilink/peers":
			_, _ = w.Write([]byte(`{"allowed":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/gateway/channels/wx-1/ilink/cursor":
			_, _ = w.Write([]byte(`{"generation":2,"get_updates_buf":"buf-1"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "rt")
	ctx := context.Background()

	allowed, err := c.ReportIlinkPeer(ctx, "wx-1", IlinkPeerReport{
		Generation: 2, PeerID: "a@im.wechat", ContextToken: "ctx", Preview: "hi", At: "2026-10-18T09:00:00Z",
	})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", "Bearer rt", "")
	for _, want := range []string{`"generation":2`, `"peer_id":"a@im.wechat"`, `"context_token":"ctx"`, `"preview":"hi"`, `"at":"2026-10-18T09:00:00Z"`} {
		if !strings.Contains(last.body, want) {
			t.Fatalf("peer body %s missing %s", last.body, want)
		}
	}

	cur, err := c.GetIlinkCursor(ctx, "wx-1")
	if err != nil || cur.Generation != 2 || cur.GetUpdatesBuf != "buf-1" {
		t.Fatalf("cur=%+v err=%v", cur, err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "Bearer rt", "")

	if err := c.PutIlinkCursor(ctx, "wx-1", IlinkCursor{Generation: 2, GetUpdatesBuf: "buf-2"}); err != nil {
		t.Fatalf("PutIlinkCursor: %v", err)
	}
	assertCall(t, last, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "Bearer rt", "")
	if last.body != `{"generation":2,"get_updates_buf":"buf-2"}` {
		t.Fatalf("cursor body=%s", last.body)
	}

	if err := c.ReportIlinkSessionExpired(ctx, "wx-1", 2, "-14"); err != nil {
		t.Fatalf("ReportIlinkSessionExpired: %v", err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/session-expired", "Bearer rt", "")
	if last.body != `{"generation":2,"reason":"-14"}` {
		t.Fatalf("session-expired body=%s", last.body)
	}

	conflict = true
	err = c.PutIlinkCursor(ctx, "wx-1", IlinkCursor{Generation: 1})
	if !IsIlinkGenerationMismatch(err) {
		t.Fatalf("err=%v want generation mismatch", err)
	}
	if _, err := c.ReportIlinkPeer(ctx, "wx-1", IlinkPeerReport{Generation: 1, PeerID: "a"}); !IsIlinkGenerationMismatch(err) {
		t.Fatalf("peer err=%v want generation mismatch", err)
	}
	if IsIlinkGenerationMismatch(nil) {
		t.Fatal("nil is not a mismatch")
	}
}
