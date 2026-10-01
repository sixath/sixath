package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FieldHistoryToolName 是参数取值历史工具的注册名。
const FieldHistoryToolName = "field_history"

const (
	fieldHistoryDefaultWindow = "3d"
	fieldHistoryPageSize      = 500
	fieldHistoryDefaultPages  = 4
	fieldHistoryMaxPages      = 10
	fieldHistoryContextRunes  = 80
	fieldHistoryMaxSegments   = 30
)

const fieldHistoryToolDescription = `Track how a value in the logs changed over time: a config/parameter (timeout, retry count, version, flag), an argument sent in requests, a state field.
Use it when the failure changes form inside the window (different error, different timeout, stuck instead of failing): something was changed again, and this finds when and to what.
- tool: log search tool (default es_log_query); text tools that print timestamped lines also work.
- base_args: the query selecting the lines that carry the value (e.g. {query: "startup_wait_seconds AND gid[32745]"}).
- pattern: regex with ONE capture group for the value, e.g. "startup_wait_seconds\\D{0,3}(\\d+)".
- window: look-back (default 3d; ignored when base_args has time_from). max_pages: pages of 500 hits to scan (default 4, max 10).
Returns segments [{value, first, last, count, quote}] in time order and transitions [{time, from, to, quote}] — each quote is a verbatim line you can cite in the investigation ledger.`

// RegisterFieldHistoryTool 注册 field_history；运行时从 reg 查找日志工具，应在其它工具注册完成后调用。
func RegisterFieldHistoryTool(reg *Registry) error {
	if reg == nil {
		return errors.New("field_history: registry is nil")
	}
	return reg.Register(Tool{
		Name:        FieldHistoryToolName,
		Description: fieldHistoryToolDescription,
		Effect:      EffectRead,
		Toolset:     ToolsetRCA,
		CheckFn: func(ctx context.Context) error {
			for _, t := range reg.List() {
				if t.Name == FieldHistoryToolName || compareExcluded[t.Name] {
					continue
				}
				if t.Name == timelineDefaultTool || t.Toolset == ToolsetRCA {
					return nil
				}
			}
			return errors.New("field_history: no log tools registered")
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool":       map[string]any{"type": "string", "description": "Log search tool (default es_log_query)."},
				"base_args":  map[string]any{"type": "object", "description": "Query selecting the lines that carry the value."},
				"pattern":    map[string]any{"type": "string", "description": "Regex with one capture group for the value."},
				"window":     map[string]any{"type": "string", "description": "Look-back window (default 3d)."},
				"max_pages":  map[string]any{"type": "integer"},
				"time_field": map[string]any{"type": "string"},
			},
			"required": []string{"base_args", "pattern"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			out, err := runFieldHistory(ctx, reg, params)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return out, nil
		},
	})
}

type fieldPoint struct {
	Time  time.Time
	Value string
	Quote string
}

func runFieldHistory(ctx context.Context, reg *Registry, p map[string]any) (map[string]any, error) {
	name, _ := p["tool"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = timelineDefaultTool
	}
	if compareExcluded[name] {
		return nil, fmt.Errorf("tool %q cannot be used inside field_history", name)
	}
	target, ok := reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	pat, _ := p["pattern"].(string)
	re, err := regexp.Compile(strings.TrimSpace(pat))
	if err != nil || strings.TrimSpace(pat) == "" {
		return nil, fmt.Errorf("pattern: invalid regex %q", pat)
	}
	if re.NumSubexp() < 1 {
		return nil, errors.New("pattern needs one capture group for the value, e.g. timeout\\D{0,3}(\\d+)")
	}
	base, _ := p["base_args"].(map[string]any)
	args := cloneTimelineArgs(base, nil)
	window, _ := p["window"].(string)
	if strings.TrimSpace(window) == "" {
		window = fieldHistoryDefaultWindow
	}
	if _, err := parseTimelineDuration(strings.TrimSpace(window)); err != nil {
		return nil, fmt.Errorf("window: %v", err)
	}
	if _, has := args["time_from"]; !has {
		args["time_from"] = "now-" + strings.TrimSpace(window)
	}
	if eff := target.EffectFor(args); eff != EffectRead {
		return nil, fmt.Errorf("tool %q with these args is not read-only", name)
	}
	pages := intFromParam(p["max_pages"], fieldHistoryDefaultPages)
	if pages <= 0 {
		pages = fieldHistoryDefaultPages
	}
	if pages > fieldHistoryMaxPages {
		pages = fieldHistoryMaxPages
	}
	tf, _ := p["time_field"].(string)
	r := &timelineRunner{ctx: ctx, target: target, name: name, gate: NestedToolGateFrom(ctx), timeField: strings.TrimSpace(tf)}

	var points []fieldPoint
	scanned, matchedLines := 0, 0
	truncated := false
	for page := 0; page < pages; page++ {
		res, err := r.call(cloneTimelineArgs(args, map[string]any{"sort": "asc", "limit": fieldHistoryPageSize, "from": page * fieldHistoryPageSize}))
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		hits := timelineHits(res)
		scanned += len(hits)
		for _, h := range hits {
			t, ok := timelineHitTime(h, r.timeField)
			if !ok {
				continue
			}
			if pt, ok := fieldPointFromHit(h, re, t); ok {
				points = append(points, pt)
				matchedLines++
			}
		}
		local, _ := res["local"].(bool)
		if local || len(hits) < fieldHistoryPageSize {
			break
		}
		if page == pages-1 {
			truncated = true
		}
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })

	out := map[string]any{"ok": true, "tool": name, "scanned": scanned, "matched": matchedLines}
	if len(points) == 0 {
		out["note"] = "no line in the result matched pattern; check the capture group against a sample line (a failed or empty query is NOT evidence the value never changed)"
		return out, nil
	}
	segs, trans := fieldSegments(points)
	if len(segs) > fieldHistoryMaxSegments {
		out["segments_truncated"] = len(segs) - fieldHistoryMaxSegments
		segs = segs[len(segs)-fieldHistoryMaxSegments:]
	}
	out["segments"] = segs
	if len(trans) > 0 {
		out["transitions"] = trans
	} else {
		out["note"] = "the value did not change in the scanned range"
	}
	if truncated {
		out["warning"] = "more hits than scanned (max_pages reached); later changes may be missing — narrow base_args or the window"
	}
	return out, nil
}

// fieldPointFromHit 先在消息正文里找值，再退回整条记录的 JSON；quote 为时间加命中附近的原文。
func fieldPointFromHit(h map[string]any, re *regexp.Regexp, t time.Time) (fieldPoint, bool) {
	texts := make([]string, 0, 2)
	for _, k := range timelineMessageKeys {
		if s, ok := h[k].(string); ok && s != "" {
			texts = append(texts, s)
			break
		}
	}
	if raw, err := json.Marshal(h); err == nil {
		texts = append(texts, string(raw))
	}
	for _, s := range texts {
		loc := re.FindStringSubmatchIndex(s)
		if loc == nil || loc[2] < 0 {
			continue
		}
		value := s[loc[2]:loc[3]]
		return fieldPoint{Time: t, Value: value, Quote: timelineFormat(t) + " " + fieldContext(s, loc[0], loc[1])}, true
	}
	return fieldPoint{}, false
}

func fieldContext(s string, start, end int) string {
	r := []rune(s)
	startR := len([]rune(s[:start]))
	endR := len([]rune(s[:end]))
	lo, hi := startR-fieldHistoryContextRunes, endR+fieldHistoryContextRunes
	if lo < 0 {
		lo = 0
	}
	if hi > len(r) {
		hi = len(r)
	}
	return strings.Join(strings.Fields(string(r[lo:hi])), " ")
}

func fieldSegments(points []fieldPoint) ([]map[string]any, []map[string]any) {
	type seg struct {
		value       string
		first, last fieldPoint
		count       int
	}
	var segs []seg
	for _, pt := range points {
		if n := len(segs); n > 0 && segs[n-1].value == pt.Value {
			segs[n-1].last = pt
			segs[n-1].count++
			continue
		}
		segs = append(segs, seg{value: pt.Value, first: pt, last: pt, count: 1})
	}
	outSegs := make([]map[string]any, 0, len(segs))
	var trans []map[string]any
	for i, s := range segs {
		outSegs = append(outSegs, map[string]any{
			"value": s.value, "first": timelineFormat(s.first.Time), "last": timelineFormat(s.last.Time),
			"count": s.count, "quote": s.first.Quote,
		})
		if i > 0 {
			prev := segs[i-1]
			trans = append(trans, map[string]any{
				"time": timelineFormat(s.first.Time), "from": prev.value, "to": s.value,
				"last_before": prev.last.Quote, "quote": s.first.Quote,
			})
		}
	}
	return outSegs, trans
}
