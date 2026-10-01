package tool

import (
	"context"
	"strings"
	"testing"
)

func newCompareTestRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewEmptyRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(reg.Register(Tool{
		Name:   "probe",
		Effect: EffectRead,
		EffectFn: func(args map[string]any) Effect {
			if c, _ := args["cmd"].(string); strings.HasPrefix(c, "rm") {
				return EffectWrite
			}
			return EffectUnknown
		},
		Execute: func(ctx context.Context, p map[string]any) (any, error) {
			host, _ := p["host"].(string)
			switch host {
			case "bad":
				return map[string]any{"stdout": "2026-09-27 10:00:01 boot start\n2026-09-27 10:00:02 disk: timeout\nmax_conn=5"}, nil
			case "down":
				return map[string]any{"ok": false, "error": "host unreachable"}, nil
			default:
				return map[string]any{"stdout": "2026-09-27 09:00:01 boot start\n2026-09-27 09:00:02 boot ok\nmax_conn=50"}, nil
			}
		},
	}))
	must(reg.Register(Tool{
		Name:   "search",
		Effect: EffectRead,
		Execute: func(ctx context.Context, p map[string]any) (any, error) {
			if p["index"] == "bad" {
				return map[string]any{"hits": []map[string]any{{"msg": "a"}, {"msg": "err"}}}, nil
			}
			return map[string]any{"hits": []map[string]any{{"msg": "a"}}}, nil
		},
	}))
	must(RegisterCompareTool(reg))
	return reg
}

func TestCompare_CheckFnRequiresProbeTool(t *testing.T) {
	reg := newCompareTestRegistry(t)
	tl, _ := reg.Get(CompareToolName)
	if tl.CheckFn(context.Background()) == nil {
		t.Fatal("no RCA/execute_read tool registered; compare should be hidden")
	}
	if err := reg.Register(Tool{Name: "es_log_query", Execute: func(context.Context, map[string]any) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	if err := tl.CheckFn(context.Background()); err != nil {
		t.Fatalf("es_log_query registered; compare should be visible: %v", err)
	}
}

func runCompareTool(t *testing.T, reg *Registry, params map[string]any) map[string]any {
	t.Helper()
	tl, _ := reg.Get(CompareToolName)
	out, err := tl.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return out.(map[string]any)
}

func TestCompare_DiffsTargetsWithIgnoreRegex(t *testing.T) {
	reg := newCompareTestRegistry(t)
	out := runCompareTool(t, reg, map[string]any{
		"tool":         "probe",
		"base_args":    map[string]any{"cmd": "tail log"},
		"targets":      []any{map[string]any{"label": "failing", "args": map[string]any{"host": "bad"}}, map[string]any{"label": "healthy", "args": map[string]any{"host": "good"}}},
		"ignore_regex": []any{`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`},
	})
	if out["ok"] != true {
		t.Fatalf("expected ok, got %v", out)
	}
	diff := out["diff"].(map[string]any)
	if diff["identical"] != false {
		t.Fatalf("expected differences: %v", diff)
	}
	if diff["common_lines"] != 1 {
		t.Fatalf("timestamps should be ignored so 'boot start' is common, got %v", diff["common_lines"])
	}
	failing := diff["differing"].(map[string]any)["failing"].(map[string]any)
	joined := strings.Join(failing["lines"].([]string), "|")
	if !strings.Contains(joined, "disk: timeout") || !strings.Contains(joined, "max_conn=5") {
		t.Fatalf("failing diff missing lines: %s", joined)
	}
	if strings.Contains(joined, "boot start") {
		t.Fatalf("common line leaked into diff: %s", joined)
	}
}

func TestCompare_RefusesNonReadEffect(t *testing.T) {
	reg := newCompareTestRegistry(t)
	out := runCompareTool(t, reg, map[string]any{
		"tool":    "probe",
		"targets": []any{map[string]any{"label": "a", "args": map[string]any{"cmd": "rm -rf /tmp/x"}}, map[string]any{"label": "b"}},
	})
	if out["ok"] != false || !strings.Contains(out["error"].(string), "read-only") {
		t.Fatalf("expected refusal, got %v", out)
	}
}

func TestCompare_RefusesExcludedAndUnknownTools(t *testing.T) {
	reg := newCompareTestRegistry(t)
	targets := []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}}
	for _, name := range []string{CompareToolName, "missing"} {
		out := runCompareTool(t, reg, map[string]any{"tool": name, "targets": targets})
		if out["ok"] != false {
			t.Fatalf("%s: expected refusal, got %v", name, out)
		}
	}
	out := runCompareTool(t, reg, map[string]any{"tool": "probe", "targets": []any{map[string]any{"label": "only"}}})
	if out["ok"] != false {
		t.Fatalf("single target should be rejected: %v", out)
	}
}

func TestCompare_FailedTargetReportedAndHitsDiffed(t *testing.T) {
	reg := newCompareTestRegistry(t)
	out := runCompareTool(t, reg, map[string]any{
		"tool":    "probe",
		"targets": []any{map[string]any{"label": "down", "args": map[string]any{"host": "down"}}, map[string]any{"label": "ok", "args": map[string]any{"host": "good"}}},
	})
	results := out["targets"].([]map[string]any)
	if results[0]["ok"] != false || results[0]["error"] != "host unreachable" {
		t.Fatalf("expected failed target, got %v", results[0])
	}
	if _, ok := out["diff"].(map[string]any)["note"]; !ok {
		t.Fatalf("expected note when fewer than two targets succeed: %v", out["diff"])
	}
	if out["failed_targets"] != 1 || out["warning"] == nil {
		t.Fatalf("failed probe must be flagged so it is not read as absence: %v", out)
	}

	out = runCompareTool(t, reg, map[string]any{
		"tool":    "search",
		"targets": []any{map[string]any{"label": "bad", "args": map[string]any{"index": "bad"}}, map[string]any{"label": "good", "args": map[string]any{"index": "good"}}},
	})
	differing := out["diff"].(map[string]any)["differing"].(map[string]any)
	bad := differing["bad"].(map[string]any)["lines"].([]string)
	if len(bad) != 1 || !strings.Contains(bad[0], `"err"`) {
		t.Fatalf("expected only the extra hit, got %v", bad)
	}
	if _, ok := differing["good"]; ok {
		t.Fatalf("good has no extra lines: %v", differing)
	}
}
