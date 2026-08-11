package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sixath/gateway/internal/runtimeclient"
)

func TestPrepareAutoRoute_MentionHit(t *testing.T) {
	var routeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":         "a1",
				"auto_route_enabled":    true,
				"auto_route_mention":    true,
				"auto_route_classifier": true,
				"agents": []map[string]string{
					{"id": "a1", "name": "ops"},
					{"id": "a2", "name": "ops-bot"},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/route"):
			atomic.AddInt32(&routeCalls, 1)
			http.Error(w, "should not call", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	plan := prepareAutoRoute(context.Background(), runtimeclient.New(srv.URL, "rt"), "ch1", "p1", "@ops-bot hello")
	if plan.Source != "mention" || plan.AgentID != "a2" || plan.TurnText != "hello" || plan.Reason != "auto_mention" {
		t.Fatalf("plan=%+v", plan)
	}
	if atomic.LoadInt32(&routeCalls) != 0 {
		t.Fatalf("routeCalls=%d", routeCalls)
	}
}

func TestPrepareAutoRoute_UnknownMentionFallsThrough(t *testing.T) {
	var routeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":         "a1",
				"auto_route_enabled":    true,
				"auto_route_mention":    true,
				"auto_route_classifier": true,
				"agents":                []map[string]string{{"id": "a1", "name": "ops"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/route"):
			atomic.AddInt32(&routeCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent_id": "a1", "confidence": "high", "source": "classifier", "reason": "classifier_high",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	plan := prepareAutoRoute(context.Background(), runtimeclient.New(srv.URL, "rt"), "ch1", "p1", "@foo hello")
	if plan.Source != "classifier" || plan.AgentID != "a1" {
		t.Fatalf("plan=%+v", plan)
	}
	if atomic.LoadInt32(&routeCalls) != 1 {
		t.Fatalf("routeCalls=%d", routeCalls)
	}
}

func TestPrepareAutoRoute_FlagsOff(t *testing.T) {
	var routeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":         "a1",
				"auto_route_enabled":    false,
				"auto_route_mention":    true,
				"auto_route_classifier": true,
				"agents":                []map[string]string{{"id": "a1", "name": "ops"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/route"):
			atomic.AddInt32(&routeCalls, 1)
			w.WriteHeader(500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	plan := prepareAutoRoute(context.Background(), runtimeclient.New(srv.URL, "rt"), "ch1", "p1", "@ops hello")
	if plan.AgentID != "" || plan.Source != "none" || plan.TurnText != "@ops hello" {
		t.Fatalf("plan=%+v", plan)
	}
	if atomic.LoadInt32(&routeCalls) != 0 {
		t.Fatalf("routeCalls=%d", routeCalls)
	}
}

func TestPrepareAutoRoute_ListFailureStillRoutes(t *testing.T) {
	var routeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			http.Error(w, `{"reason":"CHANNEL_NOT_FOUND"}`, 404)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/route"):
			atomic.AddInt32(&routeCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent_id": "x", "confidence": "high", "source": "classifier", "reason": "ok",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	plan := prepareAutoRoute(context.Background(), runtimeclient.New(srv.URL, "rt"), "ch1", "p1", "hello")
	if plan.AgentID != "x" || plan.Source != "classifier" {
		t.Fatalf("plan=%+v", plan)
	}
	if atomic.LoadInt32(&routeCalls) != 1 {
		t.Fatalf("routeCalls=%d", routeCalls)
	}
}

func TestPrepareAutoRoute_Route5xxFailOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":         "a1",
				"auto_route_enabled":    true,
				"auto_route_mention":    true,
				"auto_route_classifier": true,
				"agents":                []map[string]string{{"id": "a1", "name": "a"}, {"id": "a2", "name": "b"}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/route"):
			http.Error(w, "boom", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	plan := prepareAutoRoute(context.Background(), runtimeclient.New(srv.URL, "rt"), "ch1", "p1", "hello")
	if plan.AgentID != "" || plan.Source != "none" {
		t.Fatalf("plan=%+v", plan)
	}
}
