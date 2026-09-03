package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	spillRowThreshold   = 50
	spillByteThreshold  = 8192
	spillSampleRows     = 5
	spillSampleRowBytes = 512
	spillTTL            = 24 * time.Hour
)

var (
	spillFileMaxBytes int64 = 32 << 20
	spillSeq          uint64
)

type QuerySpillStub struct {
	Spilled         bool             `json:"spilled"`
	Path            string           `json:"path"`
	Count           int              `json:"count"`
	OK              bool             `json:"ok"`
	HitStatus       string           `json:"hit_status,omitempty"`
	Cluster         string           `json:"cluster,omitempty"`
	QueriedIndex    string           `json:"queried_index,omitempty"`
	HasMore         bool             `json:"has_more,omitempty"`
	ContinueFrom    int              `json:"continue_from,omitempty"`
	NextFrom        int              `json:"next_from,omitempty"`
	From            int              `json:"from,omitempty"`
	Returned        int              `json:"returned,omitempty"`
	Truncated       bool             `json:"truncated,omitempty"`
	Total           int              `json:"total,omitempty"`
	Columns         []string         `json:"columns,omitempty"`
	ExtractedIDs    []string         `json:"extracted_ids,omitempty"`
	EvidenceRefs    []EvidenceRef    `json:"evidence_refs,omitempty"`
	SourcePath      string           `json:"source_path,omitempty"`
	UniqueCount     int              `json:"unique_count,omitempty"`
	ExitCode        *int            `json:"exit_code,omitempty"`
	TimedOut        bool            `json:"timed_out,omitempty"`
	GroupsTruncated bool             `json:"groups_truncated,omitempty"`
	FileTruncated   bool             `json:"file_truncated,omitempty"`
	Sample          []map[string]any `json:"sample"`
	UnknownFields   any              `json:"unknown_fields,omitempty"`
	SimilarFields   any              `json:"similar_fields,omitempty"`
	MappingError    string           `json:"mapping_error,omitempty"`
	QueryRewritten  bool             `json:"query_rewritten,omitempty"`
	FieldHints      any              `json:"field_hints,omitempty"`
	TraceID         string           `json:"trace_id,omitempty"`
	SpillError      string           `json:"spill_error,omitempty"`
	SkippedBadLines int              `json:"skipped_bad_lines,omitempty"`
}

type SpillView struct {
	Spilled      bool
	HasMore      bool
	Truncated    bool
	ContinueFrom int
	NextFrom     int
	HitStatus    string
	Cluster      string
	QueriedIndex string
}

func MaybeSpill(ctx context.Context, toolName string, rows []map[string]any, payload map[string]any, refs []EvidenceRef) (*QuerySpillStub, map[string]any) {
	return spillRowSet(ctx, toolName, rows, payload, payload, refs)
}

func spillRowSet(ctx context.Context, toolName string, rows []map[string]any, marshalTarget any, payload map[string]any, refs []EvidenceRef) (*QuerySpillStub, map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if len(rows) == 0 {
		return nil, payload
	}
	if !exceedsSpillThreshold(len(rows), marshalTarget) {
		return nil, payload
	}
	ws, _ := ctx.Value(ContextKeyWorkspaceRoot).(string)
	if strings.TrimSpace(ws) == "" {
		payload["spill_error"] = "workspace_root_missing"
		return nil, payload
	}
	sess, _ := ctx.Value(ContextKeySessionID).(string)
	rel, full, err := newSpillFilePath(ws, sess, toolName)
	if err != nil {
		payload["spill_error"] = "path_rejected"
		return nil, payload
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		payload["spill_error"] = "mkdir_failed"
		return nil, payload
	}
	n, fileTrunc, sample, err := writeJSONL(full, rows)
	if err != nil {
		payload["spill_error"] = "write_failed"
		return nil, payload
	}
	expireSessionResults(filepath.Dir(full), time.Now())
	stub := stubFromPayload(payload, rel, n, sample, refs)
	stub.FileTruncated = fileTrunc
	stub.OK = true
	stub.Spilled = true
	if len(stub.Columns) == 0 {
		stub.Columns = columnsFromRows(rows)
	}
	return stub, payload
}

func exceedsSpillThreshold(n int, marshalTarget any) bool {
	if n > spillRowThreshold {
		return true
	}
	b, err := json.Marshal(marshalTarget)
	return err == nil && len(b) > spillByteThreshold
}

func newSpillFilePath(ws, sessionID, toolName string) (rel string, full string, err error) {
	return newSpillNamedFile(ws, sessionID, toolName, ".jsonl")
}

func newSpillNamedFile(ws, sessionID, toolName, ext string) (rel string, full string, err error) {
	if ext == "" {
		ext = ".jsonl"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	sess := sanitizeSessionID(sessionID)
	seq := atomic.AddUint64(&spillSeq, 1)
	name := fmt.Sprintf("%d_%s_%d%s", time.Now().UnixMilli(), toolName, seq, ext)
	rel = filepath.ToSlash(filepath.Join("tmp", "results", sess, name))
	full, rel, err = resolveResultsPath(ws, rel)
	if err != nil {
		return "", "", err
	}
	return rel, full, nil
}

func sanitizeSessionID(sessionID string) string {
	var b strings.Builder
	for _, r := range sessionID {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	s := b.String()
	if strings.TrimSpace(s) == "" {
		return "_nosession"
	}
	return s
}

func resolveResultsPath(ws, rel string) (string, string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || !strings.HasPrefix(rel, "tmp/results/") {
		return "", "", fmt.Errorf("query_spill: path must be under tmp/results/")
	}
	full, err := ResolveWorkspacePath(ws, rel)
	if err != nil {
		return "", "", err
	}
	rootAbs, err := filepath.Abs(ws)
	if err != nil {
		return "", "", err
	}
	rootClean := filepath.Clean(rootAbs)
	relOut, err := filepath.Rel(rootClean, full)
	if err != nil {
		return "", "", err
	}
	relOut = filepath.ToSlash(relOut)
	if !strings.HasPrefix(relOut, "tmp/results/") {
		return "", "", fmt.Errorf("query_spill: resolved path outside tmp/results/")
	}
	return full, relOut, nil
}

func writeJSONL(full string, rows []map[string]any) (n int, fileTruncated bool, sample []map[string]any, err error) {
	f, err := os.Create(full)
	if err != nil {
		return 0, false, nil, err
	}
	remove := true
	defer func() {
		_ = f.Close()
		if remove {
			_ = os.Remove(full)
		}
	}()

	w := bufio.NewWriter(f)
	var written int64
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return 0, false, nil, err
		}
		if int64(len(line))+1+written > spillFileMaxBytes {
			fileTruncated = true
			break
		}
		if _, err := w.Write(line); err != nil {
			return 0, false, nil, err
		}
		if err := w.WriteByte('\n'); err != nil {
			return 0, false, nil, err
		}
		written += int64(len(line) + 1)
		n++
		if len(sample) < spillSampleRows {
			sample = append(sample, sampleRow(row, line))
		}
	}
	if n == 0 && fileTruncated {
		return 0, false, nil, fmt.Errorf("query_spill: first row exceeds file cap")
	}
	if err := w.Flush(); err != nil {
		return 0, false, nil, err
	}
	remove = false
	return n, fileTruncated, sample, nil
}

func sampleRow(row map[string]any, marshaled []byte) map[string]any {
	if len(marshaled) <= spillSampleRowBytes {
		return row
	}
	preview := string(marshaled)
	if len(preview) > 120 {
		preview = preview[:120]
	}
	return map[string]any{
		"_truncated": true,
		"preview":    preview,
	}
}

func stubFromPayload(payload map[string]any, rel string, count int, sample []map[string]any, refs []EvidenceRef) *QuerySpillStub {
	stub := &QuerySpillStub{
		Path:   rel,
		Count:  count,
		Sample: sample,
	}
	if payload == nil {
		return stub
	}
	stub.HitStatus = evidenceStringVal(payload["hit_status"])
	stub.Cluster = evidenceStringVal(payload["cluster"])
	stub.QueriedIndex = evidenceStringVal(payload["queried_index"])
	stub.HasMore = spillTruthy(payload["has_more"])
	stub.ContinueFrom = intFromParam(payload["continue_from"], 0)
	stub.NextFrom = intFromParam(payload["next_from"], 0)
	stub.From = intFromParam(payload["from"], 0)
	stub.Returned = intFromParam(payload["returned"], 0)
	stub.Truncated = spillTruthy(payload["truncated"])
	stub.Total = intFromParam(payload["total"], 0)
	stub.Columns = spillStringSliceFromAny(payload["columns"])
	stub.ExtractedIDs = spillStringSliceFromAny(payload["extracted_ids"])
	stub.UnknownFields = payload["unknown_fields"]
	stub.SimilarFields = payload["similar_fields"]
	stub.MappingError = evidenceStringVal(payload["mapping_error"])
	stub.QueryRewritten = spillTruthy(payload["query_rewritten"])
	stub.FieldHints = payload["field_hints"]
	stub.TraceID = evidenceStringVal(payload["trace_id"])
	if len(refs) > 0 {
		stub.EvidenceRefs = refs
	} else if v, ok := payload["evidence_refs"]; ok {
		stub.EvidenceRefs = evidenceRefsFromAny(v)
	}
	return stub
}

func columnsFromRows(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	var cols []string
	for _, row := range rows {
		for _, k := range keysInRowOrder(row) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			cols = append(cols, k)
		}
	}
	return cols
}

func keysInRowOrder(row map[string]any) []string {
	if row == nil {
		return nil
	}
	b, err := json.Marshal(row)
	if err != nil {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		if k, ok := t.(string); ok {
			keys = append(keys, k)
		}
		var skip json.RawMessage
		_ = dec.Decode(&skip)
	}
	return keys
}

func expireSessionResults(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-spillTTL)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, ent.Name()))
		}
	}
}

func SpillFields(v any) SpillView {
	switch x := v.(type) {
	case *QuerySpillStub:
		if x == nil {
			return SpillView{}
		}
		return spillViewFromStub(*x)
	case QuerySpillStub:
		return spillViewFromStub(x)
	case map[string]any:
		return spillViewFromMap(x)
	default:
		return SpillView{}
	}
}

func spillViewFromStub(s QuerySpillStub) SpillView {
	return SpillView{
		Spilled:      s.Spilled,
		HasMore:      s.HasMore,
		Truncated:    s.Truncated,
		ContinueFrom: s.ContinueFrom,
		NextFrom:     s.NextFrom,
		HitStatus:    s.HitStatus,
		Cluster:      s.Cluster,
		QueriedIndex: s.QueriedIndex,
	}
}

func spillViewFromMap(m map[string]any) SpillView {
	if m == nil {
		return SpillView{}
	}
	return SpillView{
		Spilled:      spillTruthy(m["spilled"]),
		HasMore:      spillTruthy(m["has_more"]),
		Truncated:    spillTruthy(m["truncated"]),
		ContinueFrom: spillIntFromAny(m["continue_from"]),
		NextFrom:     spillIntFromAny(m["next_from"]),
		HitStatus:    evidenceStringVal(m["hit_status"]),
		Cluster:      evidenceStringVal(m["cluster"]),
		QueriedIndex: evidenceStringVal(m["queried_index"]),
	}
}

func spillTruthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "true" || s == "1"
	default:
		return false
	}
}

func spillIntFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func spillStringSliceFromAny(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := evidenceStringVal(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func evidenceRefsFromAny(v any) []EvidenceRef {
	switch x := v.(type) {
	case []EvidenceRef:
		return x
	case []any:
		out := make([]EvidenceRef, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				ref := EvidenceRef{Kind: evidenceStringVal(m["kind"])}
				ref.TraceID = evidenceStringVal(m["trace_id"])
				ref.Repo = evidenceStringVal(m["repo"])
				ref.Path = evidenceStringVal(m["path"])
				ref.Line = intFromParam(m["line"], 0)
				ref.Summary = evidenceStringVal(m["summary"])
				out = append(out, ref)
			}
		}
		return out
	default:
		return nil
	}
}
