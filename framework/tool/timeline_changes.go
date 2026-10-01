package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	timelineChangeHitsLimit = 100
	timelineChangesPerPoint = 5
	timelineChangeMinMargin = 30 * time.Minute
)

func changeSourcesParam(srcs []ChangeSource) []any {
	out := make([]any, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, map[string]any{"label": s.Label, "tool": s.Tool, "args": s.Args})
	}
	return out
}

func parseChangeSources(v any, defaultTool string) ([]ChangeSource, error) {
	raw, _ := v.([]any)
	if len(raw) > timelineMaxEvents {
		return nil, fmt.Errorf("at most %d items", timelineMaxEvents)
	}
	out := make([]ChangeSource, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("[%d] must be an object", i)
		}
		s := ChangeSource{}
		s.Label, _ = m["label"].(string)
		s.Tool, _ = m["tool"].(string)
		s.Label, s.Tool = strings.TrimSpace(s.Label), strings.TrimSpace(s.Tool)
		if s.Label == "" {
			s.Label = fmt.Sprintf("change%d", i+1)
		}
		if s.Tool == "" {
			s.Tool = defaultTool
		}
		args, _ := m["args"].(map[string]any)
		s.Args = cloneTimelineArgs(args, nil)
		out = append(out, s)
	}
	return out, nil
}

// changePointsWithOnset 在变化点之外补上 onset（与已有变化点相距不足一个桶时不重复）。
func changePointsWithOnset(cps []timelineChangePoint, onset *timelinePoint, step time.Duration) []timelineChangePoint {
	out := append([]timelineChangePoint(nil), cps...)
	if onset == nil {
		return out
	}
	for _, cp := range cps {
		d := cp.Time.Sub(onset.Time)
		if d < 0 {
			d = -d
		}
		if d < step {
			return out
		}
	}
	out = append(out, timelineChangePoint{Time: onset.Time, Kind: "onset", Detail: "suggested onset"})
	return out
}

// changesNear 对每个变更源查一次覆盖全部时间点的区间，把命中分配到最近的时间点；
// 一条变更归属于 margin 内的所有时间点（同一次转变常产生相邻的 stops/starts/flip）；
// 返回有变更的时间点，以及在所有成功的变更源里都没有找到变更的时间点。
func changesNear(ctx context.Context, reg *Registry, sources []ChangeSource, points []timelineChangePoint, step time.Duration, timeField string) ([]map[string]any, []string) {
	margin := 2 * step
	if margin < timelineChangeMinMargin {
		margin = timelineChangeMinMargin
	}
	from, to := points[0].Time, points[0].Time
	for _, p := range points {
		if p.Time.Before(from) {
			from = p.Time
		}
		if p.Time.After(to) {
			to = p.Time
		}
	}
	from, to = from.Add(-margin), to.Add(margin)
	type change struct{ Source, Quote string }
	near := make([][]change, len(points))
	var errs []map[string]any
	succeeded := 0
	gate := NestedToolGateFrom(ctx)
	for _, src := range sources {
		target, err := changeSourceTool(reg, src)
		if err != nil {
			errs = append(errs, map[string]any{"source": src.Label, "error": err.Error()})
			continue
		}
		r := &timelineRunner{ctx: ctx, target: target, name: src.Tool, gate: gate, timeField: timeField}
		res, err := r.call(cloneTimelineArgs(src.Args, map[string]any{
			"sort": "asc", "limit": timelineChangeHitsLimit,
			"time_from": timelineFormat(from), "time_to": timelineFormat(to),
		}))
		if err != nil {
			errs = append(errs, map[string]any{"source": src.Label, "error": err.Error()})
			continue
		}
		succeeded++
		for _, h := range timelineHits(res) {
			t, ok := timelineHitTime(h, timeField)
			if !ok {
				continue
			}
			q := timelineQuote(h, timeField)
			for i, p := range points {
				d := t.Sub(p.Time)
				if d < 0 {
					d = -d
				}
				if d <= margin && len(near[i]) < timelineChangesPerPoint {
					near[i] = append(near[i], change{Source: src.Label, Quote: q})
				}
			}
		}
	}
	var out []map[string]any
	var missing []string
	for i, p := range points {
		label := fmt.Sprintf("%s (%s)", timelineFormat(p.Time), p.Kind)
		if len(near[i]) == 0 {
			if succeeded > 0 {
				missing = append(missing, label)
			}
			continue
		}
		items := make([]map[string]any, 0, len(near[i]))
		for _, c := range near[i] {
			items = append(items, map[string]any{"source": c.Source, "quote": c.Quote})
		}
		out = append(out, map[string]any{"time": timelineFormat(p.Time), "kind": p.Kind, "changes": items})
	}
	if len(errs) > 0 {
		out = append(out, map[string]any{"source_errors": errs, "note": "a failed change source is NOT evidence that nothing changed"})
	}
	return out, missing
}

func changeSourceTool(reg *Registry, src ChangeSource) (Tool, error) {
	if compareExcluded[src.Tool] {
		return Tool{}, fmt.Errorf("tool %q cannot be used as a change source", src.Tool)
	}
	t, ok := reg.Get(src.Tool)
	if !ok {
		return Tool{}, fmt.Errorf("tool %q is not registered", src.Tool)
	}
	if t.EffectFor(src.Args) != EffectRead {
		return Tool{}, errors.New("change source args are not read-only")
	}
	return t, nil
}
