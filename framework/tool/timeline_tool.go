package tool

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TimelineToolName 是时间线 / 切换点工具的注册名。
const TimelineToolName = "timeline"

const (
	timelineDefaultTool    = "es_log_query"
	timelineDefaultWindow  = "7d"
	timelineMaxSeries      = 6
	timelineMaxEvents      = 6
	timelineMaxBuckets     = 60
	timelineMaxChangePts   = 8
	timelineEventHitsLimit = 30
	timelineEventPerCP     = 5
	timelineQuoteRunes     = 300
)

const (
	timelineRoleFailure = "failure"
	timelineRoleSuccess = "success"
	timelineRoleOther   = "other"
)

const timelineToolDescription = `Find WHEN things changed: first failure, last success, and the change points between them, over a long window (default 7d).
Use it before set_onset: the earliest failure you happened to read is rarely the real onset.
- tool: log search tool to run (default es_log_query). Any read-only tool that returns timestamped text lines (e.g. vm_run_cmd printing a log file) also works: lines starting with a full date-time are bucketed locally.
- base_args: arguments shared by every query (e.g. cluster, index).
- series: [{label, args, role?}] 1-6 queries to track, e.g. the failing message, the success message, a "stuck" state. role: failure | success | other (inferred from the label when omitted: fail/error/stuck → failure, success/ok/healthy → success).
- window: how far back to look (e.g. 24h, 7d, 30d; default 7d). Ignored when base_args carries time_from.
- interval: histogram bucket (e.g. 5m, 1h); chosen from the window when omitted.
- events: optional [{label, args}] queries for change-type logs (deploy, upgrade, config, restart, restore, patch, cert...) — reported only near the change points.
Returns per series: total, first_seen/last_seen with a verbatim quote line, bucket counts; change_points (series starts, stops, or two series swap dominance); suggested_onset / suggested_last_good with quotes you can pass straight to the investigation ledger's set_onset; and event hits near each change point.`

// ChangeSource 为一类变更记录（运维任务、重启/还原、配置变更、发布）的查询；timeline 在每个变化点和 onset 前后自动查询。
type ChangeSource struct {
	Label string         `yaml:"label" json:"label"`
	Tool  string         `yaml:"tool,omitempty" json:"tool,omitempty"`
	Args  map[string]any `yaml:"args" json:"args"`
}

// TimelineOptions timeline 工具的可选配置。
type TimelineOptions struct {
	// ChangeSources 为调用方未传 change_sources 时使用的默认变更源（通常来自工作区 hooks.yaml）。
	ChangeSources []ChangeSource
}

// RegisterTimelineTool 注册 timeline 工具；运行时从 reg 查找日志工具，应在其它工具注册完成后调用。
func RegisterTimelineTool(reg *Registry) error {
	return RegisterTimelineToolWithOptions(reg, TimelineOptions{})
}

// RegisterTimelineToolWithOptions 同 RegisterTimelineTool，并接入默认变更源。
func RegisterTimelineToolWithOptions(reg *Registry, opts TimelineOptions) error {
	if reg == nil {
		return errors.New("timeline: registry is nil")
	}
	desc := timelineToolDescription
	if len(opts.ChangeSources) > 0 {
		labels := make([]string, 0, len(opts.ChangeSources))
		for _, s := range opts.ChangeSources {
			labels = append(labels, s.Label)
		}
		desc += "\nConfigured change_sources (queried automatically when you pass none): " + strings.Join(labels, ", ") + "."
	}
	return reg.Register(Tool{
		Name:        TimelineToolName,
		Description: desc,
		Effect:      EffectRead,
		Toolset:     ToolsetRCA,
		CheckFn: func(ctx context.Context) error {
			for _, t := range reg.List() {
				if t.Name == TimelineToolName || compareExcluded[t.Name] {
					continue
				}
				if t.Name == timelineDefaultTool || t.Toolset == ToolsetRCA {
					return nil
				}
			}
			return errors.New("timeline: no log tools registered")
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool":      map[string]any{"type": "string", "description": "Log search tool (default es_log_query)."},
				"base_args": map[string]any{"type": "object", "description": "Arguments shared by every query (cluster, index, ...)."},
				"series": map[string]any{
					"type":     "array",
					"minItems": 1,
					"maxItems": timelineMaxSeries,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label": map[string]any{"type": "string"},
							"args":  map[string]any{"type": "object"},
							"role":  map[string]any{"type": "string", "enum": []string{timelineRoleFailure, timelineRoleSuccess, timelineRoleOther}},
						},
						"required": []string{"label"},
					},
				},
				"window":   map[string]any{"type": "string", "description": "Look-back window, e.g. 24h, 7d (default 7d)."},
				"interval": map[string]any{"type": "string", "description": "Histogram bucket, e.g. 5m, 1h (auto when omitted)."},
				"events": map[string]any{
					"type":     "array",
					"maxItems": timelineMaxEvents,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label": map[string]any{"type": "string"},
							"args":  map[string]any{"type": "object"},
						},
						"required": []string{"label"},
					},
				},
				"time_field": map[string]any{"type": "string", "description": "Time field in hits (default: detected, e.g. @timestamp)."},
				"change_sources": map[string]any{
					"type":        "array",
					"maxItems":    timelineMaxEvents,
					"description": "Change records to check around every change point and the onset: [{label, tool?, args}] (ops tasks, reboot/restore, config change, deploy). tool defaults to the series tool.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label": map[string]any{"type": "string"},
							"tool":  map[string]any{"type": "string"},
							"args":  map[string]any{"type": "object"},
						},
						"required": []string{"label"},
					},
				},
			},
			"required": []string{"series"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			if _, has := params["change_sources"]; !has && len(opts.ChangeSources) > 0 {
				params = cloneTimelineArgs(params, map[string]any{"change_sources": changeSourcesParam(opts.ChangeSources)})
			}
			out, err := runTimeline(ctx, reg, params)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return out, nil
		},
	})
}

type timelineQuery struct {
	Label string
	Role  string
	Args  map[string]any
}

type timelinePoint struct {
	Time  time.Time
	Quote string
}

type timelineBucket struct {
	Time  time.Time
	Key   string
	Count int64
}

type timelineSeriesResult struct {
	Query   timelineQuery
	Total   int
	First   *timelinePoint
	Last    *timelinePoint
	Buckets []timelineBucket
	Err     error
}

type timelineRunner struct {
	ctx       context.Context
	target    Tool
	name      string
	gate      *NestedToolGate
	timeField string
}

func runTimeline(ctx context.Context, reg *Registry, p map[string]any) (map[string]any, error) {
	name, _ := p["tool"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = timelineDefaultTool
	}
	if compareExcluded[name] || name == TimelineToolName {
		return nil, fmt.Errorf("tool %q cannot be used inside timeline", name)
	}
	target, ok := reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	if target.CheckFn != nil {
		if err := target.CheckFn(ctx); err != nil {
			return nil, fmt.Errorf("tool %q is unavailable: %v", name, err)
		}
	}
	base, _ := p["base_args"].(map[string]any)
	series, err := parseTimelineQueries(p["series"], base, true)
	if err != nil {
		return nil, fmt.Errorf("series: %v", err)
	}
	if len(series) > timelineMaxSeries {
		return nil, fmt.Errorf("series must contain 1-%d items", timelineMaxSeries)
	}
	events, err := parseTimelineQueries(p["events"], base, false)
	if err != nil {
		return nil, fmt.Errorf("events: %v", err)
	}
	if len(events) > timelineMaxEvents {
		return nil, fmt.Errorf("events must contain at most %d items", timelineMaxEvents)
	}
	for _, q := range append(append([]timelineQuery{}, series...), events...) {
		if eff := target.EffectFor(q.Args); eff != EffectRead {
			return nil, fmt.Errorf("%q: tool %q with these args is not read-only", q.Label, name)
		}
	}

	window, _ := p["window"].(string)
	window = strings.TrimSpace(window)
	if window == "" {
		window = timelineDefaultWindow
	}
	span, err := parseTimelineDuration(window)
	if err != nil {
		return nil, fmt.Errorf("window: %v", err)
	}
	interval, _ := p["interval"].(string)
	interval = strings.TrimSpace(interval)
	if interval == "" {
		interval = timelineAutoInterval(span)
	}
	step, err := parseTimelineDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("interval: %v", err)
	}
	timeFrom := "now-" + window
	for _, q := range series {
		if _, has := q.Args["time_from"]; !has {
			q.Args["time_from"] = timeFrom
		}
	}
	tf, _ := p["time_field"].(string)
	r := &timelineRunner{ctx: ctx, target: target, name: name, gate: NestedToolGateFrom(ctx), timeField: strings.TrimSpace(tf)}

	results := make([]timelineSeriesResult, len(series))
	var wg sync.WaitGroup
	for i, q := range series {
		wg.Add(1)
		go func(i int, q timelineQuery) {
			defer wg.Done()
			results[i] = r.runSeries(q, interval)
		}(i, q)
	}
	wg.Wait()

	okCount := 0
	for _, s := range results {
		if s.Err == nil {
			okCount++
		}
	}
	if okCount == 0 {
		msgs := make([]string, 0, len(results))
		for _, s := range results {
			msgs = append(msgs, fmt.Sprintf("%s: %v", s.Query.Label, s.Err))
		}
		return nil, fmt.Errorf("every series query failed: %s", strings.Join(msgs, "; "))
	}

	changePoints := timelineChangePoints(results, step)
	out := map[string]any{
		"ok":       true,
		"tool":     name,
		"window":   window,
		"interval": interval,
		"series":   timelineSeriesPayload(results),
	}
	if len(changePoints) > 0 {
		cps := make([]map[string]any, 0, len(changePoints))
		for _, cp := range changePoints {
			cps = append(cps, cp.payload())
		}
		out["change_points"] = cps
	}

	onset, onsetSeries := timelineSuggestedOnset(results)
	if onset != nil {
		out["suggested_onset"] = map[string]any{"time": timelineFormat(onset.Time), "quote": onset.Quote, "series": onsetSeries}
		if lg, lgSeries := r.lastGoodBefore(results, onset.Time); lg != nil {
			out["suggested_last_good"] = map[string]any{"time": timelineFormat(lg.Time), "quote": lg.Quote, "series": lgSeries}
		} else if timelineHasRole(results, timelineRoleSuccess) {
			out["last_good_note"] = "no success record before the onset inside the window; widen window to find the last good point"
		}
		if onset.Time.Sub(timelineWindowStart(results)) < step*2 {
			out["onset_note"] = "the first failure is at the very start of the window, so the real onset may be earlier; widen window"
		}
	}
	if len(events) > 0 {
		out["events"] = r.eventsNear(events, changePoints, step)
	}
	sources, err := parseChangeSources(p["change_sources"], name)
	if err != nil {
		return nil, fmt.Errorf("change_sources: %v", err)
	}
	points := changePointsWithOnset(changePoints, onset, step)
	if len(sources) > 0 && len(points) > 0 {
		near, missing := changesNear(ctx, reg, sources, points, step, r.timeField)
		if len(near) > 0 {
			out["changes_near"] = near
		}
		if len(missing) > 0 {
			out["changes_warning"] = "no recorded change found near " + strings.Join(missing, "; ") +
				" — something still changed there: check config/parameter values with field_history, or other change records"
		}
	} else if len(changePoints) > 1 {
		out["changes_hint"] = "several change points: for each one ask what changed at that moment (pass change_sources for ops/config/deploy records, or use field_history to track a parameter's value)"
	}
	if okCount < len(results) {
		out["warning"] = "some series failed (see series[].error); a failed query is NOT evidence that the pattern is absent"
	}
	return out, nil
}

func parseTimelineQueries(v any, base map[string]any, required bool) ([]timelineQuery, error) {
	raw, _ := v.([]any)
	if len(raw) == 0 {
		if required {
			return nil, errors.New("at least one item is required")
		}
		return nil, nil
	}
	seen := map[string]bool{}
	out := make([]timelineQuery, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("[%d] must be an object", i)
		}
		label, _ := m["label"].(string)
		label = strings.TrimSpace(label)
		if label == "" {
			label = fmt.Sprintf("q%d", i+1)
		}
		if seen[label] {
			return nil, fmt.Errorf("duplicate label %q", label)
		}
		seen[label] = true
		args := make(map[string]any, len(base)+4)
		for k, val := range base {
			args[k] = val
		}
		if own, ok := m["args"].(map[string]any); ok {
			for k, val := range own {
				args[k] = val
			}
		}
		role, _ := m["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		switch role {
		case timelineRoleFailure, timelineRoleSuccess, timelineRoleOther:
		case "":
			role = inferTimelineRole(label)
		default:
			return nil, fmt.Errorf("[%d] role must be failure, success or other", i)
		}
		out = append(out, timelineQuery{Label: label, Role: role, Args: args})
	}
	return out, nil
}

func inferTimelineRole(label string) string {
	l := strings.ToLower(label)
	for _, k := range []string{"fail", "error", "err", "stuck", "bad", "timeout", "exception", "失败", "异常", "错误", "卡"} {
		if strings.Contains(l, k) {
			return timelineRoleFailure
		}
	}
	for _, k := range []string{"success", "succeed", "ok", "good", "healthy", "normal", "成功", "正常"} {
		if strings.Contains(l, k) {
			return timelineRoleSuccess
		}
	}
	return timelineRoleOther
}

func parseTimelineDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, errors.New("empty duration")
	}
	unit := s[len(s)-1]
	mult := time.Duration(0)
	switch unit {
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	}
	if mult > 0 {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(n) * mult, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30m, 6h, 7d)", s)
	}
	return d, nil
}

// timelineAutoInterval 让桶数落在约 30-60 个，既能看出切换点又不撑爆输出。
func timelineAutoInterval(span time.Duration) string {
	switch {
	case span <= 2*time.Hour:
		return "2m"
	case span <= 6*time.Hour:
		return "10m"
	case span <= 24*time.Hour:
		return "30m"
	case span <= 3*24*time.Hour:
		return "1h"
	case span <= 7*24*time.Hour:
		return "3h"
	case span <= 30*24*time.Hour:
		return "12h"
	default:
		return "1d"
	}
}

func cloneTimelineArgs(src map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(src)+len(extra))
	for k, v := range src {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func (r *timelineRunner) call(args map[string]any) (map[string]any, error) {
	if r.gate != nil && r.gate.Before != nil {
		next, err := r.gate.Before(r.ctx, r.name, args)
		if err != nil {
			return nil, fmt.Errorf("blocked: %v", err)
		}
		if next != nil {
			args = next
		}
		if r.target.EffectFor(args) != EffectRead {
			return nil, errors.New("blocked: args rewritten by hook are no longer read-only")
		}
	}
	res, err := r.target.Execute(r.ctx, args)
	if r.gate != nil && r.gate.After != nil {
		res, err = r.gate.After(r.ctx, r.name, res, err)
	}
	if err != nil {
		return nil, err
	}
	if resultReportsFailure(res) {
		return nil, errors.New(failureMessage(res))
	}
	if m, ok := res.(map[string]any); ok {
		if _, hasHits := m["hits"]; hasHits {
			return m, nil
		}
	}
	text := timelineResultText(res)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("tool %q returned neither hits nor text", r.name)
	}
	return timelineLocalize(text, args), nil
}

func timelineResultText(res any) string {
	switch v := res.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case map[string]any:
		for _, k := range []string{"stdout", "output", "content", "text"} {
			if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// timelineLocalize 让返回纯文本日志的工具也能用：带完整日期时间的行视为命中，
// 并在本地执行 time_from/time_to（仅绝对时间）、sort、limit 与 agg_interval。
func timelineLocalize(text string, args map[string]any) map[string]any {
	var hits []map[string]any
	var times []time.Time
	from, hasFrom := timelineAbsArg(args["time_from"])
	to, hasTo := timelineAbsArg(args["time_to"])
	q, _ := args["query"].(string)
	keep := timelineTextFilter(q)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		loc := timelineLineTimeRe.FindStringIndex(line)
		if loc == nil || loc[0] > 2 || !keep(line) {
			continue
		}
		t, ok := parseTimelineTime(strings.Replace(line[loc[0]:loc[1]], ",", ".", 1))
		if !ok || (hasFrom && t.Before(from)) || (hasTo && t.After(to)) {
			continue
		}
		hits = append(hits, map[string]any{"@timestamp": timelineFormat(t), "message": strings.TrimSpace(line[loc[1]:])})
		times = append(times, t)
	}
	idx := make([]int, len(hits))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return times[idx[a]].Before(times[idx[b]]) })
	if s, _ := args["sort"].(string); strings.HasSuffix(strings.ToLower(s), "desc") {
		for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
			idx[i], idx[j] = idx[j], idx[i]
		}
	}
	out := map[string]any{"ok": true, "total": len(hits), "local": true}
	if iv, _ := args["agg_interval"].(string); iv != "" {
		if step, err := parseTimelineDuration(iv); err == nil {
			counts := map[int64]int{}
			for _, t := range times {
				counts[t.Truncate(step).UnixMilli()]++
			}
			buckets := make([]any, 0, len(counts))
			for k, c := range counts {
				buckets = append(buckets, map[string]any{"key": float64(k), "count": c})
			}
			out["aggregations"] = map[string]any{esLogAggTimeline: map[string]any{"buckets": buckets}}
		}
	}
	limit := intFromParam(args["limit"], len(idx))
	sorted := make([]map[string]any, 0, len(idx))
	for i, k := range idx {
		if i >= limit {
			break
		}
		sorted = append(sorted, hits[k])
	}
	out["hits"] = sorted
	return out
}

// timelineTextFilter 对文本结果按 query 的普通词做行过滤（OR 查询任一词命中即可），
// 使不理解 query 的文本源也能分出不同 series；含正则/通配语法的 query 不过滤。
func timelineTextFilter(query string) func(string) bool {
	query = strings.TrimSpace(query)
	if query == "" || strings.ContainsAny(query, `*?[]{}\^$~/()`) {
		return func(string) bool { return true }
	}
	anyTerm := strings.Contains(query, " OR ") || strings.Contains(query, "||")
	var terms []string
	for _, f := range strings.Fields(strings.NewReplacer(`"`, " ", "||", " ", "&&", " ").Replace(query)) {
		switch f {
		case "AND", "OR", "NOT":
			continue
		}
		if strings.HasPrefix(f, "-") || strings.HasPrefix(f, "!") {
			continue
		}
		f = strings.TrimPrefix(f, "+")
		if i := strings.Index(f, ":"); i > 0 && luceneFieldNameRe.MatchString(f[:i]) {
			f = f[i+1:]
		}
		if f != "" {
			terms = append(terms, strings.ToLower(f))
		}
	}
	if len(terms) == 0 {
		return func(string) bool { return true }
	}
	return func(line string) bool {
		l := strings.ToLower(line)
		for _, t := range terms {
			hit := strings.Contains(l, t)
			if anyTerm && hit {
				return true
			}
			if !anyTerm && !hit {
				return false
			}
		}
		return !anyTerm
	}
}

var timelineLineTimeRe = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)

func timelineAbsArg(v any) (time.Time, bool) {
	s, _ := v.(string)
	if strings.HasPrefix(strings.TrimSpace(s), "now") {
		return time.Time{}, false
	}
	return parseTimelineTime(s)
}

func (r *timelineRunner) runSeries(q timelineQuery, interval string) timelineSeriesResult {
	out := timelineSeriesResult{Query: q}
	first, err := r.call(cloneTimelineArgs(q.Args, map[string]any{"sort": "asc", "limit": 1, "agg_interval": interval}))
	if err != nil {
		out.Err = err
		return out
	}
	out.Total = intFromParam(first["total"], 0)
	out.First = r.firstPoint(first)
	out.Buckets = timelineBuckets(first)
	if out.First == nil && len(out.Buckets) == 0 {
		return out
	}
	last, err := r.call(cloneTimelineArgs(q.Args, map[string]any{"sort": "desc", "limit": 1}))
	if err == nil {
		out.Last = r.firstPoint(last)
	}
	return out
}

func (r *timelineRunner) firstPoint(res map[string]any) *timelinePoint {
	hits := timelineHits(res)
	if len(hits) == 0 {
		return nil
	}
	h := hits[0]
	t, ok := timelineHitTime(h, r.timeField)
	if !ok {
		return nil
	}
	return &timelinePoint{Time: t, Quote: timelineQuote(h, r.timeField)}
}

// lastGoodBefore 对每个 success series 精确查一次 onset 之前最近的一条记录。
func (r *timelineRunner) lastGoodBefore(results []timelineSeriesResult, onset time.Time) (*timelinePoint, string) {
	var best *timelinePoint
	bestLabel := ""
	for _, s := range results {
		if s.Err != nil || s.Query.Role != timelineRoleSuccess || s.First == nil || !s.First.Time.Before(onset) {
			continue
		}
		res, err := r.call(cloneTimelineArgs(s.Query.Args, map[string]any{
			"sort": "desc", "limit": 1, "time_to": timelineFormat(onset.Add(-time.Millisecond)),
		}))
		if err != nil {
			continue
		}
		pt := r.firstPoint(res)
		if pt == nil || !pt.Time.Before(onset) {
			continue
		}
		if best == nil || pt.Time.After(best.Time) {
			best, bestLabel = pt, s.Query.Label
		}
	}
	return best, bestLabel
}

func timelineHits(res map[string]any) []map[string]any {
	switch v := res["hits"].(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, x := range v {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

var timelineTimeKeys = []string{"@timestamp", "timestamp", "time", "ts", "log_time", "logtime", "datetime", "date", "created_at"}

func timelineHitTime(h map[string]any, field string) (time.Time, bool) {
	if field != "" {
		if t, ok := parseTimelineTime(h[field]); ok {
			return t, true
		}
	}
	for _, k := range timelineTimeKeys {
		if t, ok := parseTimelineTime(h[k]); ok {
			return t, true
		}
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "time") || strings.Contains(lk, "date") {
			if t, ok := parseTimelineTime(h[k]); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

var timelineLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000Z0700",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05.000",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
}

func parseTimelineTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return t, !t.IsZero()
	case float64:
		return timelineEpoch(t)
	case int64:
		return timelineEpoch(float64(t))
	case int:
		return timelineEpoch(float64(t))
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}, false
		}
		for _, l := range timelineLayouts {
			if ts, err := time.Parse(l, s); err == nil {
				return ts, true
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return timelineEpoch(f)
		}
	}
	return time.Time{}, false
}

// timelineEpoch 按量级区分秒与毫秒时间戳。
func timelineEpoch(f float64) (time.Time, bool) {
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return time.Time{}, false
	}
	if f > 1e11 {
		return time.UnixMilli(int64(f)).UTC(), true
	}
	return time.Unix(int64(f), 0).UTC(), true
}

var timelineMessageKeys = []string{"message", "msg", "log", "body", "content", "text", "event", "error"}

// timelineQuote 生成一行「时间 + 正文」的原文，直接出现在本工具输出里，可作为台账引用。
func timelineQuote(h map[string]any, field string) string {
	var parts []string
	if t, ok := timelineHitTime(h, field); ok {
		parts = append(parts, timelineFormat(t))
	}
	for _, k := range timelineMessageKeys {
		if s, ok := h[k].(string); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, strings.Join(strings.Fields(s), " "))
			break
		}
	}
	if len(parts) < 2 {
		keys := make([]string, 0, len(h))
		for k := range h {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := h[k].(string); ok && strings.TrimSpace(s) != "" && len(s) > 8 {
				if _, isTime := parseTimelineTime(s); isTime {
					continue
				}
				parts = append(parts, strings.Join(strings.Fields(s), " "))
				break
			}
		}
	}
	return truncateRunes(strings.Join(parts, " "), timelineQuoteRunes)
}

func timelineFormat(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func timelineBuckets(res map[string]any) []timelineBucket {
	aggs, _ := res["aggregations"].(map[string]any)
	entry, _ := aggs[esLogAggTimeline].(map[string]any)
	var out []timelineBucket
	appendBucket := func(key any, count int64) {
		t, ok := parseTimelineTime(key)
		if !ok || count <= 0 {
			return
		}
		out = append(out, timelineBucket{Time: t, Key: timelineFormat(t), Count: count})
	}
	switch bs := entry["buckets"].(type) {
	case []map[string]any:
		for _, b := range bs {
			appendBucket(b["key"], int64(intFromParam(b["count"], 0)))
		}
	case []any:
		for _, x := range bs {
			if b, ok := x.(map[string]any); ok {
				appendBucket(b["key"], int64(intFromParam(b["count"], 0)))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func timelineSeriesPayload(results []timelineSeriesResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, s := range results {
		item := map[string]any{"label": s.Query.Label, "role": s.Query.Role, "ok": s.Err == nil}
		if s.Err != nil {
			item["error"] = s.Err.Error()
			out = append(out, item)
			continue
		}
		item["total"] = s.Total
		if s.First != nil {
			item["first_seen"] = timelineFormat(s.First.Time)
			item["first_quote"] = s.First.Quote
		}
		if s.Last != nil {
			item["last_seen"] = timelineFormat(s.Last.Time)
			item["last_quote"] = s.Last.Quote
		}
		buckets := s.Buckets
		if len(buckets) > timelineMaxBuckets {
			item["buckets_truncated"] = len(buckets) - timelineMaxBuckets
			buckets = buckets[len(buckets)-timelineMaxBuckets:]
		}
		bs := make([]map[string]any, 0, len(buckets))
		for _, b := range buckets {
			bs = append(bs, map[string]any{"key": b.Key, "count": b.Count})
		}
		item["buckets"] = bs
		if s.First == nil && len(s.Buckets) == 0 {
			item["note"] = "no matches in the window"
		}
		out = append(out, item)
	}
	return out
}

type timelineChangePoint struct {
	Time   time.Time
	Kind   string
	Series []string
	Detail string
}

func (cp timelineChangePoint) payload() map[string]any {
	return map[string]any{"time": timelineFormat(cp.Time), "kind": cp.Kind, "series": cp.Series, "detail": cp.Detail}
}

// timelineChangePoints 找三类切换：series 开始出现、停止出现、两个 series 的桶内计数优势互换。
func timelineChangePoints(results []timelineSeriesResult, step time.Duration) []timelineChangePoint {
	var start, end time.Time
	for _, s := range results {
		for _, b := range s.Buckets {
			if start.IsZero() || b.Time.Before(start) {
				start = b.Time
			}
			if b.Time.After(end) {
				end = b.Time
			}
		}
	}
	if start.IsZero() {
		return nil
	}
	var cps []timelineChangePoint
	for _, s := range results {
		if s.Err != nil || len(s.Buckets) == 0 {
			continue
		}
		first, last := s.Buckets[0], s.Buckets[len(s.Buckets)-1]
		if first.Time.After(start) {
			at := first.Time
			if s.First != nil && s.First.Time.After(at.Add(-step)) {
				at = s.First.Time
			}
			cps = append(cps, timelineChangePoint{Time: at, Kind: "starts", Series: []string{s.Query.Label},
				Detail: fmt.Sprintf("%s (%s) first appears; absent in the window before", s.Query.Label, s.Query.Role)})
		}
		if last.Time.Before(end) {
			at := last.Time
			if s.Last != nil && s.Last.Time.After(at) {
				at = s.Last.Time
			}
			cps = append(cps, timelineChangePoint{Time: at, Kind: "stops", Series: []string{s.Query.Label},
				Detail: fmt.Sprintf("%s (%s) last appears; absent afterwards while other series continue", s.Query.Label, s.Query.Role)})
		}
	}
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if cp, ok := timelineFlip(results[i], results[j]); ok {
				cps = append(cps, cp)
			}
		}
	}
	sort.SliceStable(cps, func(a, b int) bool { return cps[a].Time.Before(cps[b].Time) })
	if len(cps) > timelineMaxChangePts {
		cps = cps[:timelineMaxChangePts]
	}
	return cps
}

// timelineFlip 找两个 series 第一次由 A 占优变为 B 占优的桶（缺失的桶按 0 计）。
func timelineFlip(a, b timelineSeriesResult) (timelineChangePoint, bool) {
	if a.Err != nil || b.Err != nil || len(a.Buckets) == 0 || len(b.Buckets) == 0 {
		return timelineChangePoint{}, false
	}
	counts := map[int64][2]int64{}
	for _, bk := range a.Buckets {
		c := counts[bk.Time.UnixMilli()]
		c[0] = bk.Count
		counts[bk.Time.UnixMilli()] = c
	}
	for _, bk := range b.Buckets {
		c := counts[bk.Time.UnixMilli()]
		c[1] = bk.Count
		counts[bk.Time.UnixMilli()] = c
	}
	keys := make([]int64, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	lead := 0
	for _, k := range keys {
		c := counts[k]
		cur := 0
		switch {
		case c[0] > c[1]:
			cur = 1
		case c[1] > c[0]:
			cur = 2
		}
		if cur == 0 {
			continue
		}
		if lead != 0 && cur != lead {
			from, to := a.Query.Label, b.Query.Label
			if lead == 2 {
				from, to = to, from
			}
			return timelineChangePoint{Time: time.UnixMilli(k).UTC(), Kind: "flip", Series: []string{from, to},
				Detail: fmt.Sprintf("%s outnumbered %s before this bucket; %s dominates from here", from, to, to)}, true
		}
		lead = cur
	}
	return timelineChangePoint{}, false
}

func timelineSuggestedOnset(results []timelineSeriesResult) (*timelinePoint, string) {
	var best *timelinePoint
	label := ""
	for _, s := range results {
		if s.Err != nil || s.Query.Role != timelineRoleFailure || s.First == nil {
			continue
		}
		if best == nil || s.First.Time.Before(best.Time) {
			best, label = s.First, s.Query.Label
		}
	}
	return best, label
}

func timelineHasRole(results []timelineSeriesResult, role string) bool {
	for _, s := range results {
		if s.Err == nil && s.Query.Role == role {
			return true
		}
	}
	return false
}

func timelineWindowStart(results []timelineSeriesResult) time.Time {
	var start time.Time
	for _, s := range results {
		if s.Err != nil {
			continue
		}
		for _, t := range []*timelinePoint{s.First} {
			if t != nil && (start.IsZero() || t.Time.Before(start)) {
				start = t.Time
			}
		}
		if len(s.Buckets) > 0 && (start.IsZero() || s.Buckets[0].Time.Before(start)) {
			start = s.Buckets[0].Time
		}
	}
	return start
}

// eventsNear 每个 event 查一次覆盖全部切换点的时间段，再把命中分配到距离最近的切换点。
func (r *timelineRunner) eventsNear(events []timelineQuery, cps []timelineChangePoint, step time.Duration) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	if len(cps) == 0 {
		for _, e := range events {
			out = append(out, map[string]any{"label": e.Label, "note": "no change points found, so events were not queried"})
		}
		return out
	}
	margin := 2 * step
	from := cps[0].Time.Add(-margin)
	to := cps[len(cps)-1].Time.Add(margin)
	for _, e := range events {
		item := map[string]any{"label": e.Label}
		res, err := r.call(cloneTimelineArgs(e.Args, map[string]any{
			"sort": "asc", "limit": timelineEventHitsLimit,
			"time_from": timelineFormat(from), "time_to": timelineFormat(to),
		}))
		if err != nil {
			item["ok"] = false
			item["error"] = err.Error()
			out = append(out, item)
			continue
		}
		item["ok"] = true
		item["total"] = intFromParam(res["total"], 0)
		near := map[int][]string{}
		for _, h := range timelineHits(res) {
			t, ok := timelineHitTime(h, r.timeField)
			if !ok {
				continue
			}
			idx, best := -1, time.Duration(math.MaxInt64)
			for i, cp := range cps {
				d := t.Sub(cp.Time)
				if d < 0 {
					d = -d
				}
				if d <= margin && d < best {
					idx, best = i, d
				}
			}
			if idx >= 0 && len(near[idx]) < timelineEventPerCP {
				near[idx] = append(near[idx], timelineQuote(h, r.timeField))
			}
		}
		var groups []map[string]any
		for i, cp := range cps {
			if len(near[i]) == 0 {
				continue
			}
			groups = append(groups, map[string]any{"change_point": timelineFormat(cp.Time), "kind": cp.Kind, "hits": near[i]})
		}
		if len(groups) == 0 {
			item["note"] = fmt.Sprintf("no hits within %s of any change point", margin)
		} else {
			item["near"] = groups
		}
		out = append(out, item)
	}
	return out
}
