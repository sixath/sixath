package tool

import (
	"strings"
	"time"
)

// FormChange 为 timeline 在问题开始之后发现的失败形式变化（新的失败系列出现或主导系列翻转）。
type FormChange struct {
	Time   time.Time
	Kind   string
	Detail string
	// Margin 为判断"附近"的时间范围（与 timeline 查 change_sources 相同）。
	Margin time.Duration
	// Changes 为 timeline 在该点附近找到的变更记录原文。
	Changes []string
}

// FieldTransition 为 field_history 发现的一次取值变化。
type FieldTransition struct {
	Time   time.Time
	Quotes []string
}

// TimelineFormChanges 从一次 timeline 结果中取出 suggested_onset 之后（超过 Margin）的 starts/flip 变化点；
// 以同一结果内的 onset 为锚点，避免与台账中不同时区的时间比较。
func TimelineFormChanges(result any) []FormChange {
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	step, _ := parseTimelineDuration(stringOf(m["interval"]))
	margin := 2 * step
	if margin < timelineChangeMinMargin {
		margin = timelineChangeMinMargin
	}
	var anchor time.Time
	if o, ok := m["suggested_onset"].(map[string]any); ok {
		anchor, _ = parseTimelineTime(o["time"])
	}
	near := map[string][]string{}
	for _, n := range mapList(m["changes_near"]) {
		t := stringOf(n["time"])
		for _, c := range mapList(n["changes"]) {
			if q := stringOf(c["quote"]); q != "" {
				near[t] = append(near[t], q)
			}
		}
	}
	var out []FormChange
	for _, cp := range mapList(m["change_points"]) {
		kind := stringOf(cp["kind"])
		if kind != "starts" && kind != "flip" {
			continue
		}
		t, ok := parseTimelineTime(cp["time"])
		if !ok {
			continue
		}
		if anchor.IsZero() || !t.After(anchor.Add(margin)) {
			continue
		}
		out = append(out, FormChange{Time: t, Kind: kind, Detail: stringOf(cp["detail"]), Margin: margin, Changes: near[stringOf(cp["time"])]})
	}
	return out
}

// FieldHistoryTransitions 从一次 field_history 结果中取出取值变化。
func FieldHistoryTransitions(result any) []FieldTransition {
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	var out []FieldTransition
	for _, tr := range mapList(m["transitions"]) {
		t, ok := parseTimelineTime(tr["time"])
		if !ok {
			continue
		}
		var qs []string
		for _, k := range []string{"quote", "last_before"} {
			if q := stringOf(tr[k]); q != "" {
				qs = append(qs, q)
			}
		}
		out = append(out, FieldTransition{Time: t, Quotes: qs})
	}
	return out
}

func stringOf(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func mapList(v any) []map[string]any {
	switch l := v.(type) {
	case []map[string]any:
		return l
	case []any:
		out := make([]map[string]any, 0, len(l))
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
