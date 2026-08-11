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

func TestClient_SendsBearerAndOptionalUserHeader(t *testing.T) {
	var gotAuth, gotUser string
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUser = r.Header.Get("X-Sath-User-Id")
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "s1",
			"agent_id":   "a1",
			"user_id":    "u1",
			"created":    true,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "dev-runtime-token")
	out, err := c.ResolveSession(context.Background(), "user-42", ResolveRequest{
		ChannelID: "ch1",
		PeerID:    "peer1",
		AgentID:   "a1",
	})
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if gotAuth != "Bearer dev-runtime-token" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if gotUser != "user-42" {
		t.Fatalf("X-Sath-User-Id=%q", gotUser)
	}
	if gotMethod != http.MethodPost || gotPath != "/runtime/v1/sessions/resolve" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if out.SessionID != "s1" || !out.Created {
		t.Fatalf("out=%+v", out)
	}

	gotUser = "sentinel"
	_, err = c.ResolveSession(context.Background(), "", ResolveRequest{
		ChannelID: "ch1",
		PeerID:    "peer1",
		AgentID:   "a1",
	})
	if err != nil {
		t.Fatalf("ResolveSession without user: %v", err)
	}
	if gotUser != "" {
		t.Fatalf("expected empty X-Sath-User-Id, got %q", gotUser)
	}
}

func TestClient_SessionCRUDPaths(t *testing.T) {
	var last callSeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last = callSeen{
			method:   r.Method,
			path:     r.URL.Path,
			auth:     r.Header.Get("Authorization"),
			user:     r.Header.Get("X-Sath-User-Id"),
			rawQuery: r.URL.RawQuery,
			body:     string(body),
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "s-new"})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/agents/ag1/sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/sessions/s1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "s1"})
		case r.Method == http.MethodPut && r.URL.Path == "/runtime/v1/sessions/s1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "s1", "title": "t"})
		case r.Method == http.MethodDelete && r.URL.Path == "/runtime/v1/sessions/s1":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/sessions/s1/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/sessions/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/runtime/v1/sessions/s1/rewind":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	ctx := context.Background()
	uid := "u1"

	if _, err := c.CreateSession(ctx, uid, CreateSessionRequest{AgentID: "ag1", Title: "hi"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/sessions", "Bearer tok", uid)
	if !strings.Contains(last.body, `"agent_id":"ag1"`) {
		t.Fatalf("create body=%s", last.body)
	}

	if _, err := c.ListSessions(ctx, uid, ListSessionsQuery{Page: 1, PageSize: 20}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/sessions", "Bearer tok", uid)
	if !strings.Contains(last.rawQuery, "page=1") || !strings.Contains(last.rawQuery, "page_size=20") {
		t.Fatalf("list query=%s", last.rawQuery)
	}

	if _, err := c.ListSessionsByAgent(ctx, uid, "ag1", ListSessionsQuery{Page: 2, Q: "x"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/agents/ag1/sessions", "Bearer tok", uid)
	if !strings.Contains(last.rawQuery, "page=2") || !strings.Contains(last.rawQuery, "q=x") {
		t.Fatalf("list-by-agent query=%s", last.rawQuery)
	}

	if _, err := c.GetSession(ctx, uid, "s1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/sessions/s1", "Bearer tok", uid)

	if _, err := c.UpdateSession(ctx, uid, "s1", UpdateSessionRequest{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodPut, "/runtime/v1/sessions/s1", "Bearer tok", uid)

	if _, err := c.DeleteSession(ctx, uid, "s1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodDelete, "/runtime/v1/sessions/s1", "Bearer tok", uid)

	if _, err := c.ListMessages(ctx, uid, "s1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/sessions/s1/messages", "Bearer tok", uid)

	if _, err := c.SearchSessions(ctx, uid, SearchSessionsQuery{Query: "hello", AgentID: "ag1", Limit: 5}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/sessions/search", "Bearer tok", uid)
	if !strings.Contains(last.rawQuery, "query=hello") || !strings.Contains(last.rawQuery, "agent_id=ag1") {
		t.Fatalf("search query=%s", last.rawQuery)
	}

	if _, err := c.Rewind(ctx, uid, "s1", RewindRequest{MessageID: "m1"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/sessions/s1/rewind", "Bearer tok", uid)
	if !strings.Contains(last.body, `"message_id":"m1"`) {
		t.Fatalf("rewind body=%s", last.body)
	}
}

func TestClient_HTTPErrorIncludesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"denied"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	_, err := c.GetSession(context.Background(), "u1", "s1")
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("err type=%T (%v)", err, err)
	}
	if httpErr.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode=%d", httpErr.StatusCode)
	}
	if !strings.Contains(string(httpErr.Body), "denied") {
		t.Fatalf("Body=%s", httpErr.Body)
	}
}

func TestClient_ResolveForceNewAndReasonInBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": "s2",
			"agent_id":   "a2",
			"user_id":    "u2",
			"created":    true,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	_, err := c.ResolveSession(context.Background(), "", ResolveRequest{
		ChannelID: "ch1",
		PeerID:    "peer1",
		AgentID:   "a2",
		ForceNew:  true,
		Reason:    "slash_new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"force_new":true`) {
		t.Fatalf("body=%s", gotBody)
	}
	if !strings.Contains(gotBody, `"reason":"slash_new"`) {
		t.Fatalf("body=%s", gotBody)
	}
}

func TestClient_DeleteBindingAndListChannelAgents(t *testing.T) {
	var last callSeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last = callSeen{
			method:   r.Method,
			path:     r.URL.Path,
			auth:     r.Header.Get("Authorization"),
			user:     r.Header.Get("X-Sath-User-Id"),
			rawQuery: r.URL.RawQuery,
			body:     string(body),
		}
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/runtime/v1/sessions/binding":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/channels/ch1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":         "a-def",
				"auto_route_enabled":    true,
				"auto_route_mention":    true,
				"auto_route_classifier": false,
				"agents": []map[string]string{
					{"id": "a-def", "name": "Default", "description": "def desc"},
					{"id": "a2", "name": "Other"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "rt")
	ctx := context.Background()

	if err := c.DeleteBinding(ctx, "ch1", "peer9"); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	assertCall(t, last, http.MethodDelete, "/runtime/v1/sessions/binding", "Bearer rt", "")
	if !strings.Contains(last.rawQuery, "channel_id=ch1") || !strings.Contains(last.rawQuery, "peer_id=peer9") {
		t.Fatalf("query=%s", last.rawQuery)
	}

	out, err := c.ListChannelAgents(ctx, "ch1")
	if err != nil {
		t.Fatalf("ListChannelAgents: %v", err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/channels/ch1/agents", "Bearer rt", "")
	if out.DefaultAgent != "a-def" || len(out.Agents) != 2 {
		t.Fatalf("out=%+v", out)
	}
	if !out.AutoRouteEnabled || !out.AutoRouteMention || out.AutoRouteClassifier {
		t.Fatalf("auto_route flags=%+v", out)
	}
	if out.Agents[0].ID != "a-def" || out.Agents[0].Name != "Default" || out.Agents[0].Description != "def desc" {
		t.Fatalf("agents[0]=%+v", out.Agents[0])
	}
}

func TestClient_TurnsFinalAndStream(t *testing.T) {
	var lastAuth, lastUser, lastPath, lastMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		lastUser = r.Header.Get("X-Sath-User-Id")
		lastPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastMode, _ = body["reply_mode"].(string)
		if lastMode == "stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"type\":\"token\"}\n\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"content": "hi",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "rt")
	ctx := context.Background()

	final, err := c.TurnsFinal(ctx, "u9", TurnRequest{
		SessionID: "s1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("TurnsFinal: %v", err)
	}
	if lastAuth != "Bearer rt" || lastUser != "u9" || lastPath != "/runtime/v1/turns" {
		t.Fatalf("auth=%q user=%q path=%q", lastAuth, lastUser, lastPath)
	}
	if lastMode != "final" {
		t.Fatalf("reply_mode=%q want final", lastMode)
	}
	if final.Status != "ok" || final.Content != "hi" {
		t.Fatalf("final=%+v", final)
	}

	rc, hdr, err := c.TurnsStream(ctx, "u9", TurnRequest{
		SessionID: "s1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("TurnsStream: %v", err)
	}
	defer rc.Close()
	if lastMode != "stream" {
		t.Fatalf("reply_mode=%q want stream", lastMode)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q", ct)
	}
	raw, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "data:") {
		t.Fatalf("stream body=%q", raw)
	}
}

type callSeen struct {
	method, path, auth, user, rawQuery, body string
}

func assertCall(t *testing.T, last callSeen, method, path, auth, user string) {
	t.Helper()
	if last.method != method || last.path != path {
		t.Fatalf("got %s %s want %s %s", last.method, last.path, method, path)
	}
	if last.auth != auth {
		t.Fatalf("Authorization=%q want %q", last.auth, auth)
	}
	if last.user != user {
		t.Fatalf("X-Sath-User-Id=%q want %q", last.user, user)
	}
}
