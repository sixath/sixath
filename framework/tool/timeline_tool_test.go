package tool

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var timelineT0 = time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

type fakeLogDoc struct {
	At  time.Time
	Msg string
}

func timelineFixtureDocs() []fakeLogDoc {
	var docs []fakeLogDoc
	for h := 0; h <= 10; h++ {
		docs = append(docs, fakeLogDoc{timelineT0.Add(time.Duration(h) * time.Hour), "job ok vm=1"})
	}
	for m := 630; m <= 1200; m += 30 {
		docs = append(docs, fakeLogDoc{timelineT0.Add(time.Duration(m) * time.Minute), "job failed: counter exceeded vm=1"})
	}
	docs = append(docs,
		fakeLogDoc{timelineT0.Add(10*time.Hour + 10*time.Minute), "patch applied repair_mode=on"},
		fakeLogDoc{timelineT0.Add(2 * time.Hour), "patch applied unrelated host"},
	)
	return docs
}

// fakeLogSearch 实现 timeline 依赖的 es_log_query 子集：query 子串匹配、ISO 时间范围、排序、limit、agg_interval。
func fakeLogSearch(docs []fakeLogDoc, calls *[]map[string]any, mu *sync.Mutex) func(ctx context.Context, p map[string]any) (any, error) {
	return func(ctx context.Context, p map[string]any) (any, error) {
		mu.Lock()
		*calls = append(*calls, p)
		mu.Unlock()
		q, _ := p["query"].(string)
		if q == "boom" {
			return map[string]any{"ok": false, "error": "time out"}, nil
		}
		var from, to time.Time
		if s, _ := p["time_from"].(string); !strings.HasPrefix(s, "now") {
			from, _ = parseTimelineTime(s)
		}
		if s, _ := p["time_to"].(string); s != "" {
			to, _ = parseTimelineTime(s)
		}
		var matched []fakeLogDoc
		for _, d := range docs {
			if !strings.Contains(d.Msg, q) {
				continue
			}
			if !from.IsZero() && d.At.Before(from) {
				continue
			}
			if !to.IsZero() && d.At.After(to) {
				continue
			}
			matched = append(matched, d)
		}
		sort.Slice(matched, func(i, j int) bool { return matched[i].At.Before(matched[j].At) })
		if p["sort"] == "desc" {
			for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
		out := map[string]any{"ok": true, "total": len(matched)}
		if iv, _ := p["agg_interval"].(string); iv != "" {
			step, _ := parseTimelineDuration(iv)
			counts := map[time.Time]int{}
			for _, d := range matched {
				counts[d.At.Truncate(step)]++
			}
			var buckets []any
			for k, c := range counts {
				buckets = append(buckets, map[string]any{"key": k.Format(time.RFC3339), "count": c})
			}
			out["aggregations"] = map[string]any{esLogAggTimeline: map[string]any{"buckets": buckets}}
		}
		limit := intFromParam(p["limit"], 50)
		var hits []map[string]any
		for i, d := range matched {
			if i >= limit {
				break
			}
			hits = append(hits, map[string]any{"@timestamp": d.At.Format(time.RFC3339), "message": d.Msg})
		}
		out["hits"] = hits
		return out, nil
	}
}

func newTimelineRegistry(t *testing.T) (*Registry, *[]map[string]any) {
	t.Helper()
	reg := NewRegistry()
	var calls []map[string]any
	var mu sync.Mutex
	if err := reg.Register(Tool{
		Name:       "es_log_query",
		Effect:     EffectRead,
		Toolset:    ToolsetRCA,
		Parameters: map[string]any{"type": "object"},
		Execute:    fakeLogSearch(timelineFixtureDocs(), &calls, &mu),
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterTimelineTool(reg); err != nil {
		t.Fatal(err)
	}
	return reg, &calls
}

func runTimelineTool(t *testing.T, reg *Registry, ctx context.Context, params map[string]any) map[string]any {
	t.Helper()
	tl, ok := reg.Get(TimelineToolName)
	if !ok {
		t.Fatal("timeline not registered")
	}
	res, err := tl.Execute(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	return res.(map[string]any)
}

func timelineParams() map[string]any {
	return map[string]any{
		"base_args": map[string]any{"cluster": "c1"},
		"series": []any{
			map[string]any{"label": "success", "args": map[string]any{"query": "job ok"}},
			map[string]any{"label": "failure", "args": map[string]any{"query": "job failed"}},
		},
		"window":   "1d",
		"interval": "1h",
		"events":   []any{map[string]any{"label": "patch", "args": map[string]any{"query": "patch applied"}}},
	}
}

func TestTimeline_ChangeSourcesNearChangePoints(t *testing.T) {
	reg := NewRegistry()
	var calls []map[string]any
	var mu sync.Mutex
	_ = reg.Register(Tool{Name: "es_log_query", Effect: EffectRead, Toolset: ToolsetRCA, Execute: fakeLogSearch(timelineFixtureDocs(), &calls, &mu)})
	if err := RegisterTimelineToolWithOptions(reg, TimelineOptions{ChangeSources: []ChangeSource{{Label: "ops", Args: map[string]any{"query": "patch applied"}}}}); err != nil {
		t.Fatal(err)
	}
	p := timelineParams()
	delete(p, "events")
	out := runTimelineTool(t, reg, context.Background(), p)
	near, _ := out["changes_near"].([]map[string]any)
	if len(near) == 0 || out["changes_warning"] != nil {
		t.Fatalf("configured change source must be queried: %v", out)
	}
	found := false
	for _, n := range near {
		for _, c := range n["changes"].([]map[string]any) {
			found = found || strings.Contains(c["quote"].(string), "repair_mode=on")
		}
	}
	if !found {
		t.Fatalf("change near onset missing: %v", near)
	}

	p["change_sources"] = []any{map[string]any{"label": "config", "args": map[string]any{"query": "no such change"}}}
	out = runTimelineTool(t, reg, context.Background(), p)
	if w, _ := out["changes_warning"].(string); !strings.Contains(w, "field_history") {
		t.Fatalf("points without any recorded change must be flagged: %v", out)
	}
}

func TestTimeline_FindsOnsetLastGoodChangePointsAndEvents(t *testing.T) {
	reg, calls := newTimelineRegistry(t)
	out := runTimelineTool(t, reg, context.Background(), timelineParams())
	if out["ok"] != true {
		t.Fatalf("ok=false: %v", out)
	}
	onset, _ := out["suggested_onset"].(map[string]any)
	if onset["time"] != "2026-09-20T10:30:00.000Z" || !strings.Contains(onset["quote"].(string), "job failed") || onset["series"] != "failure" {
		t.Fatalf("suggested_onset=%v", onset)
	}
	lg, _ := out["suggested_last_good"].(map[string]any)
	if lg["time"] != "2026-09-20T10:00:00.000Z" || !strings.Contains(lg["quote"].(string), "job ok") {
		t.Fatalf("suggested_last_good=%v", lg)
	}

	cps, _ := out["change_points"].([]map[string]any)
	kinds := map[string]bool{}
	for _, cp := range cps {
		kinds[cp["kind"].(string)] = true
	}
	for _, k := range []string{"starts", "stops", "flip"} {
		if !kinds[k] {
			t.Fatalf("missing change point %q: %v", k, cps)
		}
	}

	evs, _ := out["events"].([]map[string]any)
	if len(evs) != 1 {
		t.Fatalf("events=%v", evs)
	}
	text := ObservationText(evs[0])
	if !strings.Contains(text, "repair_mode=on") || strings.Contains(text, "unrelated host") {
		t.Fatalf("events must keep only hits near change points: %s", text)
	}

	obs := ObservationText(out)
	for _, q := range []string{onset["quote"].(string), lg["quote"].(string)} {
		if !strings.Contains(normalizeQuote(obs), normalizeQuote(q)) {
			t.Fatalf("quote %q must be verifiable against the tool output", q)
		}
	}
	for _, c := range *calls {
		if c["cluster"] != "c1" {
			t.Fatalf("base_args must be merged into every call: %v", c)
		}
	}
}

func TestTimeline_PartialAndTotalFailure(t *testing.T) {
	reg, _ := newTimelineRegistry(t)
	p := timelineParams()
	p["series"] = []any{
		map[string]any{"label": "failure", "args": map[string]any{"query": "job failed"}},
		map[string]any{"label": "stuck", "args": map[string]any{"query": "boom"}},
	}
	out := runTimelineTool(t, reg, context.Background(), p)
	if out["ok"] != true || out["warning"] == nil {
		t.Fatalf("partial failure must still answer with a warning: %v", out)
	}
	series := out["series"].([]map[string]any)
	if series[1]["ok"] != false || series[1]["error"] != "time out" {
		t.Fatalf("failed series must carry its error: %v", series[1])
	}

	p["series"] = []any{map[string]any{"label": "stuck", "args": map[string]any{"query": "boom"}}}
	out = runTimelineTool(t, reg, context.Background(), p)
	if out["ok"] != false {
		t.Fatalf("all series failing must be ok=false: %v", out)
	}
}

func TestTimeline_NestedGateAppliesToEveryCall(t *testing.T) {
	reg, calls := newTimelineRegistry(t)
	var mu sync.Mutex
	seen := 0
	gate := &NestedToolGate{Before: func(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
		mu.Lock()
		defer mu.Unlock()
		seen++
		if q, _ := args["query"].(string); q == "patch applied" {
			return nil, errors.New("denied")
		}
		return args, nil
	}}
	out := runTimelineTool(t, reg, WithNestedToolGate(context.Background(), gate), timelineParams())
	if seen != len(*calls)+1 {
		t.Fatalf("gate saw %d calls, tool ran %d (+1 blocked)", seen, len(*calls))
	}
	evs := out["events"].([]map[string]any)
	if evs[0]["ok"] != false || !strings.Contains(evs[0]["error"].(string), "blocked") {
		t.Fatalf("blocked event must be reported: %v", evs[0])
	}
}

func TestTimeline_OnsetAtWindowStartIsFlagged(t *testing.T) {
	reg, _ := newTimelineRegistry(t)
	p := timelineParams()
	p["series"] = []any{map[string]any{"label": "failure", "args": map[string]any{"query": "job failed"}}}
	delete(p, "events")
	out := runTimelineTool(t, reg, context.Background(), p)
	if out["onset_note"] == nil {
		t.Fatalf("onset at the first data point must warn to widen the window: %v", out)
	}
	if _, ok := out["suggested_last_good"]; ok {
		t.Fatalf("no success series, no last_good: %v", out)
	}
}

func TestTimeline_TextLogToolIsBucketedLocally(t *testing.T) {
	reg := NewRegistry()
	logText := strings.Join([]string{
		"2026-09-19 08:00:01 INFO prestart ok vm=7",
		"2026-09-19 20:00:05 INFO prestart ok vm=7",
		"2026-09-20 09:58:40 WARN repair aborted: bin/engine.dll locked",
		"2026-09-20 10:01:30 ERROR prestart failed vm=7",
		"2026-09-20 11:01:30 ERROR prestart failed vm=7",
		"noise line without time",
	}, "\n")
	if err := reg.Register(Tool{
		Name: "vm_run_cmd", Effect: EffectRead, Parameters: map[string]any{"type": "object"},
		Execute: func(ctx context.Context, p map[string]any) (any, error) {
			q, _ := p["cmd"].(string)
			var keep []string
			for _, l := range strings.Split(logText, "\n") {
				if strings.Contains(l, q) {
					keep = append(keep, l)
				}
			}
			return map[string]any{"ok": true, "stdout": strings.Join(keep, "\n")}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterTimelineTool(reg); err != nil {
		t.Fatal(err)
	}
	out := runTimelineTool(t, reg, context.Background(), map[string]any{
		"tool": "vm_run_cmd",
		"series": []any{
			map[string]any{"label": "ok", "args": map[string]any{"cmd": "prestart ok"}},
			map[string]any{"label": "failed", "args": map[string]any{"cmd": "prestart failed"}},
		},
		"window": "3d",
		"events": []any{map[string]any{"label": "repair", "args": map[string]any{"cmd": "repair"}}},
	})
	onset, _ := out["suggested_onset"].(map[string]any)
	lg, _ := out["suggested_last_good"].(map[string]any)
	if onset["time"] != "2026-09-20T10:01:30.000Z" || lg["time"] != "2026-09-19T20:00:05.000Z" {
		t.Fatalf("onset=%v last_good=%v", onset, lg)
	}
	if !strings.Contains(ObservationText(out["events"]), "repair aborted") {
		t.Fatalf("event near the change point expected: %v", out["events"])
	}
}

func TestTimelineTextFilter(t *testing.T) {
	line := "2026-09-20 22:41:10 ERROR vm=300112 prestart game launch failed"
	for q, want := range map[string]bool{
		"":                            true,
		"vm=300112 failed":            true,
		"msg:failed AND vm=300112":    true,
		"vm=300112 ok":                false,
		"ok OR failed":                true,
		"ok OR success":               false,
		`"launch failed" -ok`:         true,
		"fail*":                       true,
		"(prestart AND nothingthere)": true,
	} {
		if got := timelineTextFilter(q)(line); got != want {
			t.Errorf("query %q: got %v want %v", q, got, want)
		}
	}
}

func TestTimelineHelpers(t *testing.T) {
	if d, err := parseTimelineDuration("7d"); err != nil || d != 7*24*time.Hour {
		t.Fatalf("7d=%v %v", d, err)
	}
	if _, err := parseTimelineDuration("x"); err == nil {
		t.Fatal("invalid duration must fail")
	}
	if timelineAutoInterval(7*24*time.Hour) != "3h" || timelineAutoInterval(time.Hour) != "2m" {
		t.Fatal("auto interval")
	}
	if inferTimelineRole("Stuck VMs") != timelineRoleFailure || inferTimelineRole("healthy peers") != timelineRoleSuccess || inferTimelineRole("deploys") != timelineRoleOther {
		t.Fatal("role inference")
	}
	if ts, ok := parseTimelineTime(float64(timelineT0.UnixMilli())); !ok || !ts.Equal(timelineT0) {
		t.Fatalf("epoch millis: %v", ts)
	}
	if ts, ok := parseTimelineTime("2026-09-20 00:00:00"); !ok || !ts.Equal(timelineT0) {
		t.Fatalf("space layout: %v", ts)
	}
}
