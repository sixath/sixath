package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// CompareToolName 是对照探测工具的注册名。
const CompareToolName = "compare"

const (
	compareMinTargets        = 2
	compareMaxTargets        = 6
	compareDefaultMaxLines   = 40
	compareMaxLinesCap       = 200
	compareOutputPreviewRune = 3000
)

// compareExcluded 为不允许经 compare 间接调用的工具：自身、会话态工具、重型子 agent。
var compareExcluded = map[string]bool{
	CompareToolName:       true,
	TimelineToolName:      true,
	FsCompareToolName:     true,
	FieldHistoryToolName:  true,
	CaseLibraryToolName:   true,
	InvestigationToolName: true,
	"todo":                true,
	"deep_investigate":    true,
	"ask_user":            true,
}

const compareToolDescription = `Run the SAME read-only probe (one tool) against 2-6 targets and diff the results.
Use it for contrast checks: failing object vs a healthy peer, bad time window vs a normal one, broken config vs a working one.
- tool: name of a read-only tool (e.g. es_log_query, vm_run_cmd with a read-only command, execute_read, http_request GET).
- base_args: arguments shared by all targets; each target's args are merged over base_args.
- targets: [{label, args}] — label names the target in the diff (e.g. "vm-225781", "healthy-peer").
- ignore_regex: patterns stripped before comparing (timestamps, request ids, pids) so only meaningful differences remain.
Returns each target's output preview plus, per target, the lines that are not present in every other target.
Write/exec calls are refused; run them directly with the original tool instead.`

// RegisterCompareTool 注册 compare 工具；运行时从 reg 查找被探测的工具，因此应在其它工具注册完成后调用。
func RegisterCompareTool(reg *Registry) error {
	if reg == nil {
		return errors.New("compare: registry is nil")
	}
	return reg.Register(Tool{
		Name:        CompareToolName,
		Description: compareToolDescription,
		Effect:      EffectRead,
		Toolset:     ToolsetRCA,
		CheckFn: func(ctx context.Context) error {
			for _, t := range reg.List() {
				if t.Name == CompareToolName || compareExcluded[t.Name] {
					continue
				}
				if t.Toolset == ToolsetRCA || t.Name == "execute_read" {
					return nil
				}
			}
			return errors.New("compare: no probe tools registered")
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool":      map[string]any{"type": "string", "description": "Read-only tool to run for every target."},
				"base_args": map[string]any{"type": "object", "description": "Arguments shared by all targets."},
				"targets": map[string]any{
					"type":     "array",
					"minItems": compareMinTargets,
					"maxItems": compareMaxTargets,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label": map[string]any{"type": "string"},
							"args":  map[string]any{"type": "object"},
						},
						"required": []string{"label"},
					},
				},
				"ignore_regex": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Regex patterns removed from every line before diffing.",
				},
				"max_lines": map[string]any{"type": "integer", "description": "Max differing lines reported per target (default 40)."},
			},
			"required": []string{"tool", "targets"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			out, err := runCompare(ctx, reg, params)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return out, nil
		},
	})
}

type compareTarget struct {
	Label string
	Args  map[string]any
}

type compareRun struct {
	Label  string
	Result any
	Err    error
	Lines  []string
}

func runCompare(ctx context.Context, reg *Registry, p map[string]any) (map[string]any, error) {
	name, _ := p["tool"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tool is required")
	}
	if compareExcluded[name] {
		return nil, fmt.Errorf("tool %q cannot be used inside compare", name)
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
	targets, err := parseCompareTargets(p)
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if eff := target.EffectFor(t.Args); eff != EffectRead {
			if eff == EffectUnknown {
				eff = "unknown"
			}
			return nil, fmt.Errorf("target %q: tool %q with these args has effect %q; compare only runs read-only probes", t.Label, name, eff)
		}
	}
	ignores, err := parseCompareIgnores(p["ignore_regex"])
	if err != nil {
		return nil, err
	}
	maxLines := compareDefaultMaxLines
	if v, ok := p["max_lines"].(float64); ok && v > 0 {
		maxLines = int(v)
	} else if v, ok := p["max_lines"].(int); ok && v > 0 {
		maxLines = v
	}
	if maxLines > compareMaxLinesCap {
		maxLines = compareMaxLinesCap
	}

	gate := NestedToolGateFrom(ctx)
	runs := make([]compareRun, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t compareTarget) {
			defer wg.Done()
			args := t.Args
			if gate != nil && gate.Before != nil {
				next, err := gate.Before(ctx, name, args)
				if err != nil {
					runs[i] = compareRun{Label: t.Label, Err: fmt.Errorf("blocked: %v", err)}
					return
				}
				if next != nil {
					args = next
				}
				if target.EffectFor(args) != EffectRead {
					runs[i] = compareRun{Label: t.Label, Err: errors.New("blocked: args rewritten by hook are no longer read-only")}
					return
				}
			}
			res, err := target.Execute(ctx, args)
			if gate != nil && gate.After != nil {
				res, err = gate.After(ctx, name, res, err)
			}
			if err == nil && resultReportsFailure(res) {
				err = errors.New(failureMessage(res))
			}
			runs[i] = compareRun{Label: t.Label, Result: res, Err: err}
			if err == nil {
				runs[i].Lines = normalizeCompareLines(probeLines(res), ignores)
			}
		}(i, t)
	}
	wg.Wait()

	results := make([]map[string]any, 0, len(runs))
	var okRuns []compareRun
	for _, r := range runs {
		item := map[string]any{"label": r.Label, "ok": r.Err == nil}
		if r.Err != nil {
			item["error"] = r.Err.Error()
		} else {
			item["line_count"] = len(r.Lines)
			item["output"] = truncateRunes(ObservationText(r.Result), compareOutputPreviewRune)
			okRuns = append(okRuns, r)
		}
		results = append(results, item)
	}
	out := map[string]any{"ok": true, "tool": name, "targets": results}
	if failed := len(runs) - len(okRuns); failed > 0 {
		out["failed_targets"] = failed
		out["warning"] = "some targets failed (see targets[].error); a failed or timed-out probe is NOT evidence that the other side lacks those lines — rerun it narrower before drawing conclusions"
	}
	if len(okRuns) < compareMinTargets {
		out["diff"] = map[string]any{"note": "fewer than two targets succeeded; nothing to compare"}
		return out, nil
	}
	out["diff"] = diffCompareRuns(okRuns, maxLines)
	return out, nil
}

func parseCompareTargets(p map[string]any) ([]compareTarget, error) {
	base, _ := p["base_args"].(map[string]any)
	raw, _ := p["targets"].([]any)
	if len(raw) < compareMinTargets || len(raw) > compareMaxTargets {
		return nil, fmt.Errorf("targets must contain %d-%d items", compareMinTargets, compareMaxTargets)
	}
	seen := map[string]bool{}
	out := make([]compareTarget, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("targets[%d] must be an object", i)
		}
		label, _ := m["label"].(string)
		label = strings.TrimSpace(label)
		if label == "" {
			label = fmt.Sprintf("target%d", i+1)
		}
		if seen[label] {
			return nil, fmt.Errorf("duplicate target label %q", label)
		}
		seen[label] = true
		args := make(map[string]any, len(base))
		for k, v := range base {
			args[k] = v
		}
		if own, ok := m["args"].(map[string]any); ok {
			for k, v := range own {
				args[k] = v
			}
		}
		out = append(out, compareTarget{Label: label, Args: args})
	}
	return out, nil
}

func parseCompareIgnores(v any) ([]*regexp.Regexp, error) {
	var pats []string
	switch t := v.(type) {
	case nil:
	case string:
		if strings.TrimSpace(t) != "" {
			pats = []string{t}
		}
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				pats = append(pats, s)
			}
		}
	case []string:
		pats = t
	}
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, s := range pats {
		re, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("ignore_regex %q: %v", s, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// resultReportsFailure 识别工具以 {"ok":false,...} 形式返回的失败。
func resultReportsFailure(res any) bool {
	m, ok := res.(map[string]any)
	if !ok {
		return false
	}
	v, ok := m["ok"].(bool)
	return ok && !v
}

func failureMessage(res any) string {
	if m, ok := res.(map[string]any); ok {
		if s, ok := m["error"].(string); ok && s != "" {
			return s
		}
	}
	return "tool reported ok=false"
}

// probeLines 把工具结果拆成可比较的行：优先文本输出，其次逐条命中，最后退化为缩进 JSON。
func probeLines(res any) []string {
	switch v := res.(type) {
	case nil:
		return nil
	case string:
		return strings.Split(v, "\n")
	case []byte:
		return strings.Split(string(v), "\n")
	case map[string]any:
		for _, k := range []string{"stdout", "output", "content", "text"} {
			if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
				lines := strings.Split(s, "\n")
				if se, ok := v["stderr"].(string); ok && strings.TrimSpace(se) != "" {
					for _, l := range strings.Split(se, "\n") {
						lines = append(lines, "stderr: "+l)
					}
				}
				return lines
			}
		}
		for _, k := range []string{"hits", "rows", "items", "results"} {
			if lines, ok := itemLines(v[k]); ok {
				if aggs, ok := v["aggregations"]; ok {
					lines = append(lines, jsonLines(aggs)...)
				}
				return lines
			}
		}
	}
	return jsonLines(res)
}

func itemLines(v any) ([]string, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, string(it))
	}
	return out, true
}

func jsonLines(v any) []string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []string{fmt.Sprint(v)}
	}
	return strings.Split(string(b), "\n")
}

var compareSpaceRE = regexp.MustCompile(`\s+`)

func normalizeCompareLines(lines []string, ignores []*regexp.Regexp) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		for _, re := range ignores {
			l = re.ReplaceAllString(l, "…")
		}
		l = strings.TrimSpace(compareSpaceRE.ReplaceAllString(l, " "))
		l = strings.TrimRight(l, ",")
		if l == "" || l == "{" || l == "}" || l == "[" || l == "]" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// diffCompareRuns 对每个目标列出「并非所有其它目标都出现」的行，保持原顺序、去重。
func diffCompareRuns(runs []compareRun, maxLines int) map[string]any {
	sets := make([]map[string]bool, len(runs))
	for i, r := range runs {
		sets[i] = make(map[string]bool, len(r.Lines))
		for _, l := range r.Lines {
			sets[i][l] = true
		}
	}
	common := 0
	for l := range sets[0] {
		inAll := true
		for j := 1; j < len(sets); j++ {
			if !sets[j][l] {
				inAll = false
				break
			}
		}
		if inAll {
			common++
		}
	}
	differing := map[string]any{}
	identical := true
	for i, r := range runs {
		var lines []string
		emitted := map[string]bool{}
		total := 0
		for _, l := range r.Lines {
			if emitted[l] {
				continue
			}
			missing := false
			for j := range runs {
				if j != i && !sets[j][l] {
					missing = true
					break
				}
			}
			if !missing {
				continue
			}
			emitted[l] = true
			total++
			if len(lines) < maxLines {
				lines = append(lines, truncateRunes(l, 400))
			}
		}
		if total > 0 {
			identical = false
			entry := map[string]any{"lines": lines, "count": total}
			if total > len(lines) {
				entry["truncated"] = total - len(lines)
			}
			differing[r.Label] = entry
		}
	}
	labels := make([]string, 0, len(runs))
	for _, r := range runs {
		labels = append(labels, r.Label)
	}
	sort.Strings(labels)
	return map[string]any{
		"identical":    identical,
		"common_lines": common,
		"differing":    differing,
		"compared":     labels,
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
