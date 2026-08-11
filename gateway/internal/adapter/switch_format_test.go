package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/runtimeclient"
)

func TestFormatSwitchPrompt_Bound(t *testing.T) {
	agents := []pendingswitch.Agent{
		{ID: "a1", Name: "Alpha"},
		{ID: "a2", Name: "Ops Bot"},
		{ID: "a3", Name: "RCA"},
	}
	out := formatSwitchPrompt(agents, "a2", "bound")
	if !strings.Contains(out, "当前：Ops Bot") {
		t.Fatalf("missing bound header: %q", out)
	}
	if !strings.Contains(out, "2. Ops Bot  ← 当前") {
		t.Fatalf("missing current marker: %q", out)
	}
	if !strings.Contains(out, "2 分钟") {
		t.Fatalf("missing TTL hint: %q", out)
	}
	if !strings.Contains(out, "/switch") {
		t.Fatalf("missing /switch hint: %q", out)
	}
}

func TestFormatSwitchPrompt_Unbound(t *testing.T) {
	agents := []pendingswitch.Agent{{ID: "a1", Name: "Alpha"}}
	out := formatSwitchPrompt(agents, "", "unbound")
	if !strings.Contains(out, "当前：未绑定（下一条将使用 default）") {
		t.Fatalf("missing unbound header: %q", out)
	}
	if strings.Contains(out, "← 当前") {
		t.Fatalf("unexpected current marker: %q", out)
	}
}

func TestFormatSwitchPrompt_Unknown(t *testing.T) {
	agents := []pendingswitch.Agent{{ID: "a1", Name: "Alpha"}}
	out := formatSwitchPrompt(agents, "", "unknown")
	if !strings.Contains(out, "当前：未知") {
		t.Fatalf("missing unknown header: %q", out)
	}
	if strings.Contains(out, "← 当前") {
		t.Fatalf("unexpected current marker: %q", out)
	}
}

func TestStartSwitch_PutsPendingAndFormats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/channels/ch1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent": "a1",
				"agents": []map[string]string{
					{"id": "a1", "name": "Alpha"},
					{"id": "a2", "name": "Ops Bot"},
				},
			})
		case "/runtime/v1/sessions/binding":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"channel_id": "ch1",
				"peer_id":    "peer1",
				"session_id": "sess-1",
				"agent_id":   "a2",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rt := runtimeclient.New(srv.URL, "token")
	store := pendingswitch.New()
	msg, err := startSwitch(context.Background(), rt, store, "ch1", "peer1")
	if err != nil {
		t.Fatalf("startSwitch: %v", err)
	}
	if !strings.Contains(msg, "当前：Ops Bot") {
		t.Fatalf("prompt: %q", msg)
	}
	if !strings.Contains(msg, "2. Ops Bot  ← 当前") {
		t.Fatalf("prompt: %q", msg)
	}

	ent, ok := store.Get("ch1", "peer1", time.Now())
	if !ok {
		t.Fatal("expected pending entry")
	}
	if len(ent.Agents) != 2 {
		t.Fatalf("agents=%d want 2", len(ent.Agents))
	}
	if ent.ExpiresAt.Before(time.Now().Add(119 * time.Second)) {
		t.Fatalf("expires too soon: %v", ent.ExpiresAt)
	}
}

func TestStartSwitch_UnboundBinding404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/v1/channels/ch1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]string{{"id": "a1", "name": "Alpha"}},
			})
		case "/runtime/v1/sessions/binding":
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rt := runtimeclient.New(srv.URL, "token")
	store := pendingswitch.New()
	msg, err := startSwitch(context.Background(), rt, store, "ch1", "peer1")
	if err != nil {
		t.Fatalf("startSwitch: %v", err)
	}
	if !strings.Contains(msg, "当前：未绑定（下一条将使用 default）") {
		t.Fatalf("prompt: %q", msg)
	}
	if _, ok := store.Get("ch1", "peer1", time.Now()); !ok {
		t.Fatal("expected pending entry")
	}
}

func TestStartSwitch_EmptyAgentsNoPut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/runtime/v1/channels/ch1/agents" {
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	rt := runtimeclient.New(srv.URL, "token")
	store := pendingswitch.New()
	_, err := startSwitch(context.Background(), rt, store, "ch1", "peer1")
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
	if _, ok := store.Get("ch1", "peer1", time.Now()); ok {
		t.Fatal("expected no pending entry")
	}
}
