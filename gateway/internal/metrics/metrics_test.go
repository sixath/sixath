package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStatusClass(t *testing.T) {
	cases := map[int]string{
		200: "ok", 301: "ok", 399: "ok",
		400: "client_error", 404: "client_error", 499: "client_error",
		500: "server_error", 502: "server_error",
	}
	for code, want := range cases {
		if got := StatusClass(code); got != want {
			t.Fatalf("StatusClass(%d)=%q want %q", code, got, want)
		}
	}
}

func TestRender_CounterAndGauge(t *testing.T) {
	const ch = "metrics-test-counter"
	ObserveIdempotency(ch, "duplicate")
	SetWecomConnections(ch, 3)

	out := Render()
	for _, want := range []string{
		`# HELP gateway_idempotency_events_total`,
		`# TYPE gateway_idempotency_events_total counter`,
		`gateway_idempotency_events_total{channel="metrics-test-counter",outcome="duplicate"} `,
		`gateway_wecom_active_connections{channel="metrics-test-counter"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_HistogramBuckets(t *testing.T) {
	const ch = "metrics-test-hist"
	ObserveTurn(ch, "ok", 750*time.Millisecond)

	out := Render()
	for _, want := range []string{
		`gateway_turn_duration_seconds_bucket{channel="metrics-test-hist",le="0.5"} 0`,
		`gateway_turn_duration_seconds_bucket{channel="metrics-test-hist",le="1"} 1`,
		`gateway_turn_duration_seconds_bucket{channel="metrics-test-hist",le="+Inf"} 1`,
		`gateway_turn_duration_seconds_sum{channel="metrics-test-hist"} 0.75`,
		`gateway_turn_duration_seconds_count{channel="metrics-test-hist"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_LabelEscaping(t *testing.T) {
	const ch = `quote"chan`
	ObserveIdempotency(ch, "new")

	out := Render()
	if !strings.Contains(out, `channel="quote\"chan"`) {
		t.Fatalf("label not escaped properly:\n%s", out)
	}
}

func TestRender_DeterministicOrder(t *testing.T) {
	a := Render()
	b := Render()
	if a != b {
		t.Fatalf("Render is non-deterministic between calls")
	}
}

func TestConcurrentObserveAndRender(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ObservePortalRequest("turns", 200, nil, time.Millisecond)
				ObserveReply("concurrent-test", "reply_url", nil)
			}
		}(i)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			_ = Render()
		}
	}()
	wg.Wait()
	<-done
	// 能跑完且不 panic 即为通过；数据一致性由上面的确定性用例保障。
}

func TestEmptyLabelsFallbackToUnknown(t *testing.T) {
	ObserveIdempotency("", "new")
	out := Render()
	if !strings.Contains(out, `channel="unknown",outcome="new"`) {
		t.Fatalf("empty channel should fall back to unknown:\n%s", out)
	}
}