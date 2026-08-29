package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

const resultStatsKeyCap = 10000

// RegisterResultStatsTool registers result_stats for aggregating spilled query jsonl.
func RegisterResultStatsTool(reg *Registry) error {
	if reg == nil {
		return errors.New("result_stats: registry is nil")
	}
	return reg.Register(Tool{
		Name:        "result_stats",
		Description: "Aggregate a spilled query jsonl under tmp/results/. Do not read_file the whole file.",
		Toolset:     ToolsetFile,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative jsonl path under tmp/results/.",
				},
				"group_by": map[string]any{
					"type":        "string",
					"description": "Dotted field path to group by (mutually exclusive with unique).",
				},
				"unique": map[string]any{
					"type":        "string",
					"description": "Dotted field path for unique values (mutually exclusive with group_by).",
				},
			},
			"required": []string{"path"},
		},
		Execute: executeResultStats,
	})
}

func executeResultStats(ctx context.Context, params map[string]any) (any, error) {
	groupBy, _ := params["group_by"].(string)
	unique, _ := params["unique"].(string)
	groupBy = strings.TrimSpace(groupBy)
	unique = strings.TrimSpace(unique)
	if groupBy != "" && unique != "" {
		return map[string]any{"error": "group_by and unique are mutually exclusive"}, nil
	}

	ws, err := workspaceRootFromCtx(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	rel, _ := params["path"].(string)
	if strings.TrimSpace(rel) == "" {
		return map[string]any{"error": "path is required"}, nil
	}
	full, relOut, err := resolveResultsPath(ws, rel)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"error": "file not found; re-query"}, nil
		}
		return map[string]any{"error": err.Error()}, nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if st.IsDir() {
		return map[string]any{"error": "file not found; re-query"}, nil
	}

	if groupBy == "" && unique == "" {
		n, err := countJSONLLines(f)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		return map[string]any{"path": relOut, "count": n}, nil
	}

	field := groupBy
	if unique != "" {
		field = unique
	}
	agg, err := scanResultStats(f, field)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if agg.good == 0 && agg.skipped > 0 {
		return map[string]any{"error": "no valid jsonl lines; re-query"}, nil
	}

	var rows []map[string]any
	inline := map[string]any{
		"path":  relOut,
		"count": agg.good,
	}
	if unique != "" {
		inline["unique_count"] = len(agg.order)
		inline["unique_values"] = append([]string(nil), agg.order...)
		rows = uniqueStatRows(agg.order)
	} else {
		groups := groupStatRows(agg.counts, agg.order)
		inline["groups"] = groups
		rows = groups
	}
	if agg.skipped > 0 {
		inline["skipped_bad_lines"] = agg.skipped
	}
	if agg.truncated {
		inline["groups_truncated"] = true
	}

	stub, out := MaybeSpill(ctx, "result_stats", rows, inline, nil)
	if stub == nil {
		return out, nil
	}
	stub.SourcePath = relOut
	if unique != "" {
		stub.UniqueCount = len(agg.order)
	}
	stub.GroupsTruncated = agg.truncated
	stub.SkippedBadLines = agg.skipped
	return stub, nil
}

type resultStatsAgg struct {
	counts    map[string]int
	order     []string
	truncated bool
	skipped   int
	good      int
}

func countJSONLLines(f *os.File) (int, error) {
	sc := newResultStatsScanner(f)
	n := 0
	for sc.Scan() {
		n++
	}
	return n, sc.Err()
}

func scanResultStats(f *os.File, field string) (*resultStatsAgg, error) {
	agg := &resultStatsAgg{counts: map[string]int{}}
	sc := newResultStatsScanner(f)
scan:
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil || row == nil {
			agg.skipped++
			continue
		}
		agg.good++
		for _, k := range resultStatsKeysAt(row, field) {
			if _, ok := agg.counts[k]; ok {
				agg.counts[k]++
				continue
			}
			if len(agg.counts) >= resultStatsKeyCap {
				agg.truncated = true
				break scan
			}
			agg.counts[k] = 1
			agg.order = append(agg.order, k)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return agg, nil
}

func newResultStatsScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, int(spillFileMaxBytes))
	return sc
}

func resultStatsKeysAt(row map[string]any, dotted string) []string {
	dotted = strings.TrimSpace(dotted)
	if dotted == "" || row == nil {
		return nil
	}
	parts := strings.Split(dotted, ".")
	var cur any = row
	for i, part := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		next, ok := m[part]
		if !ok {
			return nil
		}
		if i == len(parts)-1 {
			return resultStatsScalarKeys(next)
		}
		cur = next
	}
	return nil
}

func resultStatsScalarKeys(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return nil
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if item == nil {
				continue
			}
			if _, isObj := item.(map[string]any); isObj {
				continue
			}
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(x)}
	}
}

func groupStatRows(counts map[string]int, order []string) []map[string]any {
	keys := append([]string(nil), order...)
	sort.Slice(keys, func(i, j int) bool {
		ci, cj := counts[keys[i]], counts[keys[j]]
		if ci != cj {
			return ci > cj
		}
		return keys[i] < keys[j]
	})
	out := make([]map[string]any, len(keys))
	for i, k := range keys {
		out[i] = map[string]any{"value": k, "count": counts[k]}
	}
	return out
}

func uniqueStatRows(order []string) []map[string]any {
	out := make([]map[string]any, len(order))
	for i, k := range order {
		out[i] = map[string]any{"value": k}
	}
	return out
}
