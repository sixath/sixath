package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestTraceIDFromTraceparent(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got := traceIDFromTraceparent(valid); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceID=%q", got)
	}
	for _, bad := range []string{
		"",
		"invalid",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01", // 全零 trace-id
		"00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-00f067aa0ba902b7-01", // 非 hex
		"00-4bf92f3577b34da6a3ce929d0e0e4736-zzzz-01",             // span 非 hex
	} {
		if got := traceIDFromTraceparent(bad); got != "" {
			t.Fatalf("traceIDFromTraceparent(%q)=%q want empty", bad, got)
		}
	}
}

func TestOutgoingTraceparent_ReusesTraceID(t *testing.T) {
	incoming := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	out := OutgoingTraceparent(incoming)
	if !strings.HasPrefix(out, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("should reuse trace-id, got %q", out)
	}
	if out == incoming {
		t.Fatalf("should use a fresh span-id, got identical %q", out)
	}
	assertValidTraceparent(t, out)
}

func TestOutgoingTraceparent_GeneratesWhenAbsent(t *testing.T) {
	for _, in := range []string{"", "garbage"} {
		out := OutgoingTraceparent(in)
		assertValidTraceparent(t, out)
	}
}

var traceparentRe = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

func assertValidTraceparent(t *testing.T, tp string) {
	t.Helper()
	if !traceparentRe.MatchString(tp) {
		t.Fatalf("invalid traceparent %q", tp)
	}
	if strings.Trim(tp[3:35], "0") == "" {
		t.Fatalf("trace-id must not be all zeros: %q", tp)
	}
}

func TestMiddleware_InjectsIDsAndPropagatesToContext(t *testing.T) {
	var gotRequestID, gotTraceparent string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = RequestIDFrom(r.Context())
		gotTraceparent = TraceparentFrom(r.Context())
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(Middleware(inner))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/hooks/abc", nil)
	req.Header.Set(HeaderRequestID, "req-123")
	req.Header.Set(HeaderTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if gotRequestID != "req-123" {
		t.Fatalf("request id = %q", gotRequestID)
	}
	if !strings.HasPrefix(gotTraceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("traceparent should keep trace-id, got %q", gotTraceparent)
	}
	if resp.Header.Get(HeaderRequestID) != "req-123" {
		t.Fatalf("response request id = %q", resp.Header.Get(HeaderRequestID))
	}
	assertValidTraceparent(t, resp.Header.Get(HeaderTraceparent))
}

func TestMiddleware_GeneratesRequestIDWhenAbsent(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) == "" {
			t.Error("request id should be generated")
		}
	})
	srv := httptest.NewServer(Middleware(inner))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get(HeaderRequestID) == "" {
		t.Fatal("response should echo request id")
	}
}

func TestResponseRecorder_Status(t *testing.T) {
	rec := NewResponseRecorder(httptest.NewRecorder())
	if rec.Status() != http.StatusOK {
		t.Fatalf("default status = %d", rec.Status())
	}
	rec.WriteHeader(http.StatusBadGateway)
	if rec.Status() != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Status())
	}
}

func TestDetach_PreservesIDsButNotCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	parent = WithRequestID(parent, "rid-1")
	parent = WithTraceparent(parent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	cancel()

	detached := Detach(parent)
	if detached.Err() != nil {
		t.Fatalf("detached ctx should not inherit cancellation, got %v", detached.Err())
	}
	if RequestIDFrom(detached) != "rid-1" {
		t.Fatal("request id lost during detach")
	}
	if !strings.HasPrefix(TraceparentFrom(detached), "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatal("traceparent lost during detach")
	}
}

func TestWithRequestID_RoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc")
	if RequestIDFrom(ctx) != "abc" {
		t.Fatal("request id round trip failed")
	}
	if RequestIDFrom(context.Background()) != "" {
		t.Fatal("empty context should yield empty id")
	}
}