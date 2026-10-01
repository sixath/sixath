package tool

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FsCompareToolName 是远程目录对比工具的注册名。
const FsCompareToolName = "fs_compare"

const (
	fsCompareMinTargets    = 2
	fsCompareMaxTargets    = 4
	fsCompareDefaultLines  = 500
	fsCompareDefaultWindow = "2h"
	fsCompareMaxReport     = 40
)

const fsCompareToolDescription = `Compare the SAME directory on a failing machine and 1-3 healthy ones by file name and modification time (runs op=ls_recent of vm_run_cmd / ssh_exec on each).
Use it to find state left behind by an interrupted process (repair/lock/tmp/partial files), files that stopped updating on the failing side, and what was modified around the onset.
- tool: vm_run_cmd or ssh_exec (default vm_run_cmd).
- targets: [{label, args}] 2-4 items; the FIRST is the failing machine. args select the machine (ip, instance...) and may override path.
- path: directory to list on every target; recursive: walk sub-directories (point at a narrow directory).
- onset: when the problem started, in the machines' local time (e.g. 2026-09-25 08:10). window: how close to onset counts as "near" (default 2h).
Returns only_in (files one side has and the other does not), frozen_on_subject (files the healthy side keeps updating after onset while the failing side stopped before it), modified_near_onset (failing-side files modified within window of onset), plus each listing's parse stats.`

// RegisterFsCompareTool 注册 fs_compare；运行时从 reg 查找远程执行工具，应在其它工具注册完成后调用。
func RegisterFsCompareTool(reg *Registry) error {
	if reg == nil {
		return errors.New("fs_compare: registry is nil")
	}
	return reg.Register(Tool{
		Name:        FsCompareToolName,
		Description: fsCompareToolDescription,
		Effect:      EffectRead,
		Toolset:     ToolsetRCA,
		CheckFn: func(ctx context.Context) error {
			for _, name := range []string{"vm_run_cmd", "ssh_exec"} {
				if _, ok := reg.Get(name); ok {
					return nil
				}
			}
			return errors.New("fs_compare: no remote command tool registered")
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool": map[string]any{"type": "string", "enum": []string{"vm_run_cmd", "ssh_exec"}},
				"targets": map[string]any{
					"type":     "array",
					"minItems": fsCompareMinTargets,
					"maxItems": fsCompareMaxTargets,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label": map[string]any{"type": "string"},
							"args":  map[string]any{"type": "object"},
						},
						"required": []string{"label"},
					},
				},
				"path":      map[string]any{"type": "string"},
				"recursive": map[string]any{"type": "boolean"},
				"onset":     map[string]any{"type": "string", "description": "Problem start in the machines' local time."},
				"window":    map[string]any{"type": "string", "description": "Nearness to onset, e.g. 30m, 2h (default 2h)."},
				"lines":     map[string]any{"type": "integer", "description": "Max listing lines per target (default 500)."},
			},
			"required": []string{"targets"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			out, err := runFsCompare(ctx, reg, params)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return out, nil
		},
	})
}

// fsEntry 为目录列表中的一项；Key 为相对根目录的路径（Windows 下小写）。
type fsEntry struct {
	Key   string
	Path  string
	MTime time.Time
	Size  int64
	Dir   bool
}

type fsListing struct {
	Label   string
	Entries map[string]fsEntry
	Lines   int
	Err     error
}

func runFsCompare(ctx context.Context, reg *Registry, p map[string]any) (map[string]any, error) {
	name, _ := p["tool"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "vm_run_cmd"
	}
	if name != "vm_run_cmd" && name != "ssh_exec" {
		return nil, fmt.Errorf("tool must be vm_run_cmd or ssh_exec")
	}
	target, ok := reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	targets, err := parseCompareTargets(map[string]any{"targets": p["targets"]})
	if err != nil {
		return nil, fmt.Errorf("%v (fs_compare accepts %d-%d)", err, fsCompareMinTargets, fsCompareMaxTargets)
	}
	if len(targets) > fsCompareMaxTargets {
		return nil, fmt.Errorf("targets must contain %d-%d items", fsCompareMinTargets, fsCompareMaxTargets)
	}
	path, _ := p["path"].(string)
	recursive, _ := p["recursive"].(bool)
	lines := intFromParam(p["lines"], fsCompareDefaultLines)
	for i := range targets {
		args := map[string]any{"op": RemoteOpListRecent, "include_dirs": true, "recursive": recursive, "lines": lines}
		if strings.TrimSpace(path) != "" {
			args["path"] = path
		}
		for k, v := range targets[i].Args {
			args[k] = v
		}
		args["op"] = RemoteOpListRecent
		if s, _ := args["path"].(string); strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("target %q: path is required", targets[i].Label)
		}
		if eff := target.EffectFor(args); eff != EffectRead {
			return nil, fmt.Errorf("target %q: listing is not read-only with these args", targets[i].Label)
		}
		targets[i].Args = args
	}
	windowStr, _ := p["window"].(string)
	if strings.TrimSpace(windowStr) == "" {
		windowStr = fsCompareDefaultWindow
	}
	window, err := parseTimelineDuration(strings.TrimSpace(windowStr))
	if err != nil {
		return nil, fmt.Errorf("window: %v", err)
	}
	var onset time.Time
	if s, _ := p["onset"].(string); strings.TrimSpace(s) != "" {
		t, ok := parseFsTime(strings.TrimSpace(s))
		if !ok {
			return nil, fmt.Errorf("onset %q: use a local date-time like 2026-09-25 08:10", s)
		}
		onset = t
	}

	gate := NestedToolGateFrom(ctx)
	listings := make([]fsListing, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t compareTarget) {
			defer wg.Done()
			args := t.Args
			if gate != nil && gate.Before != nil {
				next, err := gate.Before(ctx, name, args)
				if err != nil {
					listings[i] = fsListing{Label: t.Label, Err: fmt.Errorf("blocked: %v", err)}
					return
				}
				if next != nil {
					args = next
				}
			}
			res, err := target.Execute(ctx, args)
			if gate != nil && gate.After != nil {
				res, err = gate.After(ctx, name, res, err)
			}
			if err == nil && resultReportsFailure(res) {
				err = errors.New(failureMessage(res))
			}
			if err != nil {
				listings[i] = fsListing{Label: t.Label, Err: err}
				return
			}
			entries, n := parseFsListing(probeLines(res), time.Now())
			listings[i] = fsListing{Label: t.Label, Entries: entries, Lines: n}
			if len(entries) == 0 {
				listings[i].Err = errors.New("no file entries could be parsed from the listing")
			}
		}(i, t)
	}
	wg.Wait()

	stats := make([]map[string]any, 0, len(listings))
	for _, l := range listings {
		item := map[string]any{"label": l.Label, "ok": l.Err == nil, "entries": len(l.Entries)}
		if l.Err != nil {
			item["error"] = l.Err.Error()
		}
		stats = append(stats, item)
	}
	out := map[string]any{"ok": true, "tool": name, "targets": stats}
	subject := listings[0]
	var healthy []fsListing
	for _, l := range listings[1:] {
		if l.Err == nil {
			healthy = append(healthy, l)
		}
	}
	if subject.Err != nil || len(healthy) == 0 {
		out["note"] = "the failing target and at least one healthy target must be listed successfully; a failed listing is NOT evidence that files are absent"
		return out, nil
	}
	out["only_in"] = fsOnlyIn(subject, healthy)
	if frozen := fsFrozen(subject, healthy, onset, window); len(frozen) > 0 {
		out["frozen_on_subject"] = frozen
	}
	if !onset.IsZero() {
		out["modified_near_onset"] = fsNearOnset(subject, healthy, onset, window)
	}
	if len(healthy) < len(listings)-1 {
		out["warning"] = "some healthy targets failed; differences are computed against the ones that succeeded"
	}
	return out, nil
}

func fsOnlyIn(subject fsListing, healthy []fsListing) map[string]any {
	out := map[string]any{}
	var subjOnly []string
	for _, k := range sortedFsKeys(subject.Entries) {
		inAny := false
		for _, h := range healthy {
			if _, ok := h.Entries[k]; ok {
				inAny = true
				break
			}
		}
		if !inAny {
			subjOnly = append(subjOnly, fsDescribe(subject.Entries[k]))
		}
	}
	out[subject.Label] = fsCap(subjOnly)
	for _, h := range healthy {
		var only []string
		for _, k := range sortedFsKeys(h.Entries) {
			if _, ok := subject.Entries[k]; !ok {
				only = append(only, fsDescribe(h.Entries[k]))
			}
		}
		out[h.Label] = fsCap(only)
	}
	return out
}

// fsFrozen 找出健康机仍在更新、故障机却停止更新的文件：有 onset 时以 onset 为界，否则以相差超过 window 判定。
func fsFrozen(subject fsListing, healthy []fsListing, onset time.Time, window time.Duration) []map[string]any {
	var out []map[string]any
	for _, k := range sortedFsKeys(subject.Entries) {
		s := subject.Entries[k]
		if s.Dir {
			continue
		}
		others := map[string]string{}
		for _, h := range healthy {
			he, ok := h.Entries[k]
			if !ok {
				continue
			}
			frozen := false
			if !onset.IsZero() {
				frozen = s.MTime.Before(onset) && !he.MTime.Before(onset)
			} else {
				frozen = he.MTime.Sub(s.MTime) > window
			}
			if frozen {
				others[h.Label] = fsFormat(he.MTime)
			}
		}
		if len(others) > 0 {
			out = append(out, map[string]any{"path": s.Path, "subject_mtime": fsFormat(s.MTime), "healthy_mtime": others})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["subject_mtime"].(string) > out[j]["subject_mtime"].(string) })
	if len(out) > fsCompareMaxReport {
		out = out[:fsCompareMaxReport]
	}
	return out
}

func fsNearOnset(subject fsListing, healthy []fsListing, onset time.Time, window time.Duration) []map[string]any {
	var out []map[string]any
	for _, k := range sortedFsKeys(subject.Entries) {
		e := subject.Entries[k]
		d := e.MTime.Sub(onset)
		if d < -window || d > window {
			continue
		}
		item := map[string]any{"path": e.Path, "mtime": fsFormat(e.MTime)}
		if e.Dir {
			item["dir"] = true
		}
		inHealthy := false
		for _, h := range healthy {
			if _, ok := h.Entries[k]; ok {
				inHealthy = true
			}
		}
		if !inHealthy {
			item["subject_only"] = true
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["mtime"].(string) < out[j]["mtime"].(string) })
	if len(out) > fsCompareMaxReport {
		out = out[:fsCompareMaxReport]
	}
	return out
}

func sortedFsKeys(m map[string]fsEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fsDescribe(e fsEntry) string {
	if e.Dir {
		return fmt.Sprintf("%s/ (%s)", e.Path, fsFormat(e.MTime))
	}
	return fmt.Sprintf("%s (%s, %d bytes)", e.Path, fsFormat(e.MTime), e.Size)
}

func fsCap(list []string) any {
	if len(list) <= fsCompareMaxReport {
		if list == nil {
			return []string{}
		}
		return list
	}
	return map[string]any{"items": list[:fsCompareMaxReport], "truncated": len(list) - fsCompareMaxReport}
}

func fsFormat(t time.Time) string { return t.Format("2006-01-02 15:04") }

var (
	fsWinLineRe   = regexp.MustCompile(`^(\d{4}[/-]\d{1,2}[/-]\d{1,2}|\d{1,2}/\d{1,2}/\d{4})\s+(\d{1,2}:\d{2})(?:\s*([AaPp][Mm]))?\s+(<DIR>|<JUNCTION>|<SYMLINKD?>|[\d,.]+)\s+(.+?)\s*$`)
	fsWinHeaderRe = regexp.MustCompile(`^\s*(?:Directory of\s+(.+?)|(.+?)\s*的目录)\s*$`)
	fsPosixLineRe = regexp.MustCompile(`^([-dlbcps][rwxsStT-]{9}\S*)\s+\d+\s+\S+\s+\S+\s+(\d+)\s+(\w{3}\s+\d{1,2}\s+(?:\d{1,2}:\d{2}|\d{4})|\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:\s+[+-]\d{4})?)\s+(.+?)\s*$`)
	fsPosixHdrRe  = regexp.MustCompile(`^(\S.*):$`)
)

// parseFsListing 解析 Windows dir 或 POSIX ls -l(R) 的输出；返回按相对路径索引的条目与解析到的行数。
func parseFsListing(lines []string, now time.Time) (map[string]fsEntry, int) {
	out := map[string]fsEntry{}
	root, dir := "", ""
	windows := false
	parsed := 0
	rel := func(name string) string {
		d := strings.TrimPrefix(dir, root)
		d = strings.Trim(strings.ReplaceAll(d, `\`, "/"), "/")
		if d == "" {
			return name
		}
		return d + "/" + name
	}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if m := fsWinHeaderRe.FindStringSubmatch(line); m != nil {
			windows = true
			dir = strings.TrimSpace(m[1] + m[2])
			if root == "" {
				root = dir
			}
			continue
		}
		if m := fsWinLineRe.FindStringSubmatch(line); m != nil {
			windows = true
			name := m[5]
			if name == "." || name == ".." {
				continue
			}
			t, ok := parseFsTime(m[1] + " " + m[2] + strings.ToUpper(strings.TrimSpace(" "+m[3])))
			if !ok {
				continue
			}
			isDir := strings.HasPrefix(m[4], "<")
			var size int64
			if !isDir {
				size, _ = strconv.ParseInt(strings.NewReplacer(",", "", ".", "").Replace(m[4]), 10, 64)
			}
			p := rel(name)
			out[strings.ToLower(p)] = fsEntry{Key: strings.ToLower(p), Path: p, MTime: t, Size: size, Dir: isDir}
			parsed++
			continue
		}
		if m := fsPosixLineRe.FindStringSubmatch(line); m != nil {
			name := m[4]
			if i := strings.Index(name, " -> "); i > 0 && strings.HasPrefix(m[1], "l") {
				name = name[:i]
			}
			if name == "." || name == ".." {
				continue
			}
			t, ok := parsePosixLsTime(m[3], now)
			if !ok {
				continue
			}
			size, _ := strconv.ParseInt(m[2], 10, 64)
			p := rel(name)
			out[p] = fsEntry{Key: p, Path: p, MTime: t, Size: size, Dir: strings.HasPrefix(m[1], "d")}
			parsed++
			continue
		}
		if !windows {
			if m := fsPosixHdrRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil && !strings.HasPrefix(line, "total ") {
				dir = m[1]
				if root == "" {
					root = dir
				}
			}
		}
	}
	return out, parsed
}

var fsTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04",
	"2006/01/02 15:04:05", "2006/01/02 15:04", "2006/1/2 15:04",
	"01/02/2006 03:04PM", "01/02/2006 3:04PM", "01/02/2006 15:04", "1/2/2006 3:04PM",
}

// parseFsTime 解析机器本地时间（不带时区，按 UTC 挂钟比较）。
func parseFsTime(s string) (time.Time, bool) {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Replace(strings.Replace(s, " AM", "AM", 1), " PM", "PM", 1)
	for _, layout := range fsTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parsePosixLsTime(s string, now time.Time) (time.Time, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if t, ok := parseFsTime(s); ok {
		return t, true
	}
	if len(s) >= 19 {
		if t, err := time.Parse("2006-01-02 15:04:05", s[:19]); err == nil {
			return t, true
		}
	}
	if t, err := time.Parse("Jan 2 2006", s); err == nil {
		return t, true
	}
	if t, err := time.Parse("Jan 2 15:04", s); err == nil {
		t = t.AddDate(now.Year(), 0, 0)
		if t.After(now.Add(24 * time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
		return t, true
	}
	return time.Time{}, false
}
