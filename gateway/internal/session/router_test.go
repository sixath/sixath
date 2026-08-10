package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/runtimeclient"
)

func TestRouter_CacheThenInvalidate(t *testing.T) {
	var resolveCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/sessions/resolve" {
			resolveCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "s1",
				"agent_id":   "a1",
				"user_id":    "u1",
				"created":    false,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := runtimeclient.New(srv.URL, "tok")
	router := NewRouter(client, time.Minute)
	ctx := context.Background()
	req := runtimeclient.ResolveRequest{ChannelID: "ch1", PeerID: "p1"}

	if _, err := router.Resolve(ctx, "", req); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if _, err := router.Resolve(ctx, "", req); err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
	if got := resolveCalls.Load(); got != 1 {
		t.Fatalf("resolve calls=%d want 1 before invalidate", got)
	}

	router.Invalidate("ch1", "p1")

	if _, err := router.Resolve(ctx, "", req); err != nil {
		t.Fatalf("after invalidate: %v", err)
	}
	if got := resolveCalls.Load(); got != 2 {
		t.Fatalf("resolve calls=%d want 2 after invalidate", got)
	}
}
