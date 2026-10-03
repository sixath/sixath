package tool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sixath/framework/executor"
)

func emptyResult() map[string]any { return map[string]any{"ok": true, "hit_status": HitStatusEmpty} }

func TestRunEmptyProbe_MarksSuspectWhenRelaxedHasData(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant {
			return []ProbeVariant{{Label: "time_window_only"}, {Label: "without:service:foo"}}
		},
		Count: func(_ context.Context, v ProbeVariant) (int64, error) {
			if v.Label == "time_window_only" {
				return 12034, nil
			}
			return 57, nil
		},
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if out["hit_status"] != HitStatusSuspect {
		t.Fatalf("status %v", out["hit_status"])
	}
	d := out["diagnosis"].(*executor.Diagnosis)
	if len(d.Probes) != 2 || d.Hint == "" || d.Truncated {
		t.Fatalf("diag %+v", d)
	}
}

func TestRunEmptyProbe_KeepsEmptyWhenAllZero(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "time_window_only"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { return 0, nil },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if out["hit_status"] != HitStatusEmpty || out["diagnosis"] == nil {
		t.Fatalf("got %+v", out)
	}
}

func TestRunEmptyProbe_SkipsNonEmpty(t *testing.T) {
	called := false
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "x"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { called = true; return 1, nil },
	}
	runEmptyProbe(context.Background(), p, nil, map[string]any{"hit_status": HitStatusHits})
	if called {
		t.Fatal("must not probe non-empty results")
	}
}

func TestRunEmptyProbe_BudgetMaxThreeAndTruncated(t *testing.T) {
	n := 0
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant {
			return []ProbeVariant{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}}
		},
		Count: func(context.Context, ProbeVariant) (int64, error) { n++; return 0, nil },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	if n != 3 || !out["diagnosis"].(*executor.Diagnosis).Truncated {
		t.Fatalf("n=%d diag=%+v", n, out["diagnosis"])
	}
}

func TestRunEmptyProbe_RespectsToolDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "slow"}, {Label: "slow2"}} },
		Count: func(c context.Context, _ ProbeVariant) (int64, error) {
			<-c.Done()
			return 0, c.Err()
		},
	}
	start := time.Now()
	out := runEmptyProbe(ctx, p, nil, emptyResult()).(map[string]any)
	if time.Since(start) > time.Second {
		t.Fatal("probe must stop at tool deadline")
	}
	if out["hit_status"] != HitStatusEmpty {
		t.Fatalf("original result must survive: %+v", out)
	}
}

func TestRunEmptyProbe_CountErrorRecorded(t *testing.T) {
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { return 0, errors.New("boom") },
	}
	out := runEmptyProbe(context.Background(), p, nil, emptyResult()).(map[string]any)
	d := out["diagnosis"].(*executor.Diagnosis)
	if len(d.Errors) != 1 || out["hit_status"] != HitStatusEmpty {
		t.Fatalf("got %+v", d)
	}
}

func TestRunEmptyProbe_DisabledByEnv(t *testing.T) {
	t.Setenv(EnvToolEmptyProbe, "off")
	called := false
	p := &EmptyProbe{
		Relax: func(map[string]any) []ProbeVariant { return []ProbeVariant{{Label: "a"}} },
		Count: func(context.Context, ProbeVariant) (int64, error) { called = true; return 1, nil },
	}
	runEmptyProbe(context.Background(), p, nil, emptyResult())
	if called {
		t.Fatal("env off must disable probing")
	}
}
