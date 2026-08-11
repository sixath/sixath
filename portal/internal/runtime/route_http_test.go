package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/biz"
)

type fakeRouter struct {
	result *biz.AgentRouteResult
	err    error
	calls  int
	last   biz.AgentRouteInput
}

func (f *fakeRouter) Route(_ context.Context, in biz.AgentRouteInput) (*biz.AgentRouteResult, error) {
	f.calls++
	f.last = in
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestRuntimeRoute_SingleCandidate(t *testing.T) {
	router := &fakeRouter{result: &biz.AgentRouteResult{
		AgentID:    "a1",
		Confidence: biz.RouteConfidenceHigh,
		Source:     biz.RouteSourceDefault,
		Reason:     "single_candidate",
	}}
	svc := newTestService(nil, nil, nil)
	svc.router = router
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodPost, "/runtime/v1/channels/ch1/route",
		`{"text":"hello","peer_id":"p1"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body routeReply
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.AgentID != "a1" || body.Confidence != "high" || body.Source != "default" || body.Reason != "single_candidate" {
		t.Fatalf("body=%+v", body)
	}
	if router.calls != 1 || router.last.ChannelID != "ch1" || router.last.PeerID != "p1" || router.last.Text != "hello" {
		t.Fatalf("router last=%+v calls=%d", router.last, router.calls)
	}
}

func TestRuntimeRoute_ClassifierHigh(t *testing.T) {
	router := &fakeRouter{result: &biz.AgentRouteResult{
		AgentID:    "b",
		Confidence: biz.RouteConfidenceHigh,
		Source:     biz.RouteSourceClassifier,
		Reason:     "classifier_high",
	}}
	svc := newTestService(nil, nil, nil)
	svc.router = router
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodPost, "/runtime/v1/channels/ch-multi/route",
		`{"text":"need ops"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body routeReply
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Source != "classifier" || body.AgentID != "b" {
		t.Fatalf("body=%+v", body)
	}
}

func TestRuntimeRoute_ChannelMissing(t *testing.T) {
	router := &fakeRouter{err: biz.ErrChannelNotFound}
	svc := newTestService(nil, nil, nil)
	svc.router = router
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodPost, "/runtime/v1/channels/missing/route",
		`{"text":"x"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CHANNEL_NOT_FOUND") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
