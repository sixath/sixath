package tool

import (
	"context"
	"strings"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/obs"
	"github.com/sixath/framework/tool/lsp"
)

const (
	ErrorTransient = "transient"
	ErrorPermanent = "permanent"
)

const (
	HitStatusHits  = "hits"
	HitStatusEmpty = "empty"
	HitStatusError = "error"
)

type HitStamp struct {
	Status       string
	QueriedIndex string
	Repo         string
	SetRepo      bool            // true：即使 Repo=="" 也写 "repo" 键（grep 0 击）
	Tool         string          // obs
	Ctx          context.Context // nil → Background
}

func HitStatusFromCount(ok bool, n int) string {
	if !ok {
		return HitStatusError
	}
	if n <= 0 {
		return HitStatusEmpty
	}
	return HitStatusHits
}

func StampHitContract(payload map[string]any, s HitStamp) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if s.Status != "" {
		payload["hit_status"] = s.Status
	}
	if s.QueriedIndex != "" {
		payload["queried_index"] = s.QueriedIndex
	}
	if s.SetRepo || s.Repo != "" {
		payload["repo"] = s.Repo
	}
	if s.Status != "" {
		ctx := s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		obs.LogHitContract(ctx, s.Tool, s.Status, s.QueriedIndex, s.Repo)
	}
	return payload
}

func HitContractFromResult(v any) (status, queriedIndex, repo string) {
	switch x := v.(type) {
	case map[string]any:
		return hitStatusString(x["hit_status"]), evidenceStringVal(x["queried_index"]), evidenceStringVal(x["repo"])
	case *executor.QueryResult:
		if x == nil {
			return "", "", ""
		}
		return hitStatusString(x.HitStatus), x.QueriedIndex, ""
	case executor.QueryResult:
		return hitStatusString(x.HitStatus), x.QueriedIndex, ""
	case *QuerySpillStub:
		if x == nil {
			return "", "", ""
		}
		return hitStatusString(x.HitStatus), x.QueriedIndex, ""
	case QuerySpillStub:
		return hitStatusString(x.HitStatus), x.QueriedIndex, ""
	default:
		return "", "", ""
	}
}

func hitStatusString(v any) string {
	s := strings.TrimSpace(evidenceStringVal(v))
	switch s {
	case HitStatusHits, HitStatusEmpty, HitStatusError:
		return s
	default:
		return ""
	}
}

type EvidenceRef struct {
	Kind    string `json:"kind"`
	TraceID string `json:"trace_id,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type EvidenceMeta struct {
	Tool      string
	OK        bool
	ErrorCode string
	Refs      []EvidenceRef // if empty, derive minimal ref from Tool+payload when OK
}

// NormalizeRCAResult merges contract fields into payload without deleting existing keys.
func NormalizeRCAResult(payload map[string]any, meta EvidenceMeta) map[string]any {
	out := make(map[string]any, len(payload)+3)
	for k, v := range payload {
		out[k] = v
	}

	out["ok"] = meta.OK
	if !meta.OK && meta.ErrorCode != "" {
		out["error_code"] = meta.ErrorCode
	}

	refs := meta.Refs
	if meta.OK && len(refs) == 0 {
		refs = deriveEvidenceRefs(meta.Tool, payload)
	}
	if len(refs) > 0 {
		out["evidence_refs"] = refs
	}
	return out
}

// CollectEvidenceRefs extracts evidence_refs from one or more tool result values (map or nested).
func CollectEvidenceRefs(results ...any) []EvidenceRef {
	var out []EvidenceRef
	for _, r := range results {
		collectEvidenceRefsFromValue(r, &out)
	}
	return out
}

func collectEvidenceRefsFromValue(v any, out *[]EvidenceRef) {
	switch x := v.(type) {
	case map[string]any:
		if refs, ok := x["evidence_refs"]; ok {
			*out = append(*out, parseEvidenceRefs(refs)...)
		}
		for _, val := range x {
			collectEvidenceRefsFromValue(val, out)
		}
	case []any:
		for _, item := range x {
			collectEvidenceRefsFromValue(item, out)
		}
	case []map[string]any:
		for _, item := range x {
			collectEvidenceRefsFromValue(item, out)
		}
	case *QuerySpillStub:
		if x != nil {
			*out = append(*out, x.EvidenceRefs...)
		}
	case QuerySpillStub:
		*out = append(*out, x.EvidenceRefs...)
	}
}

func parseEvidenceRefs(v any) []EvidenceRef {
	switch x := v.(type) {
	case []EvidenceRef:
		return append([]EvidenceRef(nil), x...)
	case []any:
		refs := make([]EvidenceRef, 0, len(x))
		for _, item := range x {
			if ref, ok := evidenceRefFromMap(item); ok {
				refs = append(refs, ref)
			}
		}
		return refs
	default:
		if ref, ok := evidenceRefFromMap(v); ok {
			return []EvidenceRef{ref}
		}
	}
	return nil
}

func evidenceRefFromMap(v any) (EvidenceRef, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		if ref, ok := v.(EvidenceRef); ok {
			return ref, true
		}
		return EvidenceRef{}, false
	}
	ref := EvidenceRef{
		Kind:    evidenceStringVal(m["kind"]),
		TraceID: evidenceStringVal(m["trace_id"]),
		Repo:    evidenceStringVal(m["repo"]),
		Path:    evidenceStringVal(m["path"]),
		Summary: evidenceStringVal(m["summary"]),
		Line:    intFromParam(m["line"], 0),
	}
	if ref.Kind == "" && ref.TraceID == "" && ref.Repo == "" && ref.Path == "" && ref.Line == 0 && ref.Summary == "" {
		return EvidenceRef{}, false
	}
	return ref, true
}

func deriveEvidenceRefs(tool string, payload map[string]any) []EvidenceRef {
	if payload == nil {
		return nil
	}
	switch tool {
	case "jaeger_trace":
		return deriveJaegerRefs(payload)
	case "es_log_query":
		return deriveESLogRefs(payload)
	case "rca_grep", "rca_glob":
		return deriveRCAMatchRefs(tool, payload)
	case "rca_read":
		return deriveRCAReadRefs(tool, payload)
	case "rca_symbol":
		return deriveRCASymbolRefs(tool, payload)
	default:
		return nil
	}
}

func deriveJaegerRefs(payload map[string]any) []EvidenceRef {
	traceID := evidenceStringVal(payload["trace_id"])
	if traceID == "" {
		traceID = jaegerTraceIDFromData(payload["data"])
	}
	ref := EvidenceRef{Kind: "jaeger_trace"}
	if traceID != "" {
		ref.TraceID = traceID
	}
	return []EvidenceRef{ref}
}

func jaegerTraceIDFromData(data any) string {
	switch x := data.(type) {
	case []any:
		if len(x) == 0 {
			return ""
		}
		if m, ok := x[0].(map[string]any); ok {
			if tid := evidenceStringVal(m["traceID"]); tid != "" {
				return tid
			}
			return evidenceStringVal(m["trace_id"])
		}
	case []map[string]any:
		if len(x) == 0 {
			return ""
		}
		if tid := evidenceStringVal(x[0]["traceID"]); tid != "" {
			return tid
		}
		return evidenceStringVal(x[0]["trace_id"])
	}
	return ""
}

func deriveESLogRefs(payload map[string]any) []EvidenceRef {
	ref := EvidenceRef{Kind: "es_log_query"}
	if tid := evidenceStringVal(payload["trace_id"]); tid != "" {
		ref.TraceID = tid
	}
	if !hasResultHits(payload["hits"]) {
		ref.Summary = "no hits"
	}
	return []EvidenceRef{ref}
}

func deriveRCAMatchRefs(tool string, payload map[string]any) []EvidenceRef {
	matches := payload["matches"]
	if !hasResultHits(matches) {
		return []EvidenceRef{{Kind: tool, Summary: "no matches"}}
	}
	refs := make([]EvidenceRef, 0)
	forEachMatch(matches, func(m map[string]any) {
		ref := EvidenceRef{Kind: tool}
		if repo := evidenceStringVal(m["repo"]); repo != "" {
			ref.Repo = repo
		}
		if path := evidenceStringVal(m["file"]); path != "" {
			ref.Path = path
		}
		if line := intFromParam(m["line"], 0); line > 0 {
			ref.Line = line
		}
		refs = append(refs, ref)
	})
	if len(refs) == 0 {
		return []EvidenceRef{{Kind: tool, Summary: "no matches"}}
	}
	return refs
}

func deriveRCASymbolRefs(tool string, payload map[string]any) []EvidenceRef {
	locations := payload["locations"]
	if !hasRCASymbolLocations(locations) {
		if evidenceStringVal(payload["action"]) == "references" {
			ref := EvidenceRef{Kind: tool, Summary: "no inbound callers"}
			if repo := evidenceStringVal(payload["repo"]); repo != "" {
				ref.Repo = repo
			}
			return []EvidenceRef{ref}
		}
		return nil
	}
	action := evidenceStringVal(payload["action"])
	refs := make([]EvidenceRef, 0)
	forEachRCASymbolLocation(locations, func(repo, file string, line int, name string) {
		ref := EvidenceRef{Kind: tool}
		if repo != "" {
			ref.Repo = repo
		}
		if file != "" {
			ref.Path = file
		}
		if line > 0 {
			ref.Line = line
		}
		if summary := rcaSymbolSummary(action, name); summary != "" {
			ref.Summary = summary
		}
		refs = append(refs, ref)
	})
	if len(refs) == 0 {
		return nil
	}
	return refs
}

func hasRCASymbolLocations(v any) bool {
	switch x := v.(type) {
	case []lsp.Location:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case []map[string]any:
		return len(x) > 0
	default:
		return false
	}
}

func forEachRCASymbolLocation(v any, fn func(repo, file string, line int, name string)) {
	switch x := v.(type) {
	case []lsp.Location:
		for _, loc := range x {
			fn(loc.Repo, loc.File, loc.Line, loc.Name)
		}
	case []any:
		for _, item := range x {
			switch loc := item.(type) {
			case lsp.Location:
				fn(loc.Repo, loc.File, loc.Line, loc.Name)
			case map[string]any:
				fn(
					evidenceStringVal(loc["repo"]),
					evidenceStringVal(loc["file"]),
					intFromParam(loc["line"], 0),
					evidenceStringVal(loc["name"]),
				)
			}
		}
	case []map[string]any:
		for _, loc := range x {
			fn(
				evidenceStringVal(loc["repo"]),
				evidenceStringVal(loc["file"]),
				intFromParam(loc["line"], 0),
				evidenceStringVal(loc["name"]),
			)
		}
	}
}

func rcaSymbolSummary(action, name string) string {
	if action == "" && name == "" {
		return ""
	}
	if name == "" {
		return action
	}
	if action == "" {
		return name
	}
	return action + " " + name
}

func deriveRCAReadRefs(tool string, payload map[string]any) []EvidenceRef {
	if _, hasErr := payload["error"]; hasErr {
		return []EvidenceRef{{Kind: tool, Summary: "no hit"}}
	}
	repo := evidenceStringVal(payload["repo"])
	path := evidenceStringVal(payload["file"])
	if repo == "" && path == "" {
		return []EvidenceRef{{Kind: tool, Summary: "no hit"}}
	}
	return []EvidenceRef{{Kind: tool, Repo: repo, Path: path}}
}

func hasResultHits(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) > 0
	case []map[string]any:
		return len(x) > 0
	default:
		return false
	}
}

func forEachMatch(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				fn(m)
			}
		}
	case []map[string]any:
		for _, m := range x {
			fn(m)
		}
	}
}

func evidenceStringVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// classifyRCAError maps tool/runtime errors to ErrorTransient or ErrorPermanent (E3).
func classifyRCAError(err error) string {
	if err == nil {
		return ErrorPermanent
	}
	msg := strings.ToLower(err.Error())

	if code, ok := httpStatusFromErrorMessage(msg); ok {
		if code >= 500 {
			return ErrorTransient
		}
		if code >= 400 {
			return ErrorPermanent
		}
	}

	transientHints := []string{
		"timeout",
		"deadline exceeded",
		"connection refused",
		"connection reset",
		"connection timed out",
		"i/o timeout",
		"temporary failure",
		"no such host",
		"network is unreachable",
		"broken pipe",
		"eof",
		"tls handshake timeout",
	}
	for _, h := range transientHints {
		if strings.Contains(msg, h) {
			return ErrorTransient
		}
	}
	return ErrorPermanent
}

func httpStatusFromErrorMessage(msg string) (int, bool) {
	// Matches messages like "jaeger returned 502: ..." or "status 404".
	for _, prefix := range []string{"returned ", "status "} {
		if i := strings.Index(msg, prefix); i >= 0 {
			rest := msg[i+len(prefix):]
			n := 0
			digits := 0
			for _, r := range rest {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
				digits++
				if digits == 3 {
					return n, true
				}
			}
		}
	}
	return 0, false
}

func rcaOK(tool string, payload map[string]any) map[string]any {
	return NormalizeRCAResult(payload, EvidenceMeta{Tool: tool, OK: true})
}

func rcaErr(tool string, errMsg string, code string) map[string]any {
	return NormalizeRCAResult(map[string]any{"error": errMsg}, EvidenceMeta{
		Tool: tool, OK: false, ErrorCode: code,
	})
}

func rcaErrFrom(tool string, err error) map[string]any {
	return rcaErr(tool, err.Error(), classifyRCAError(err))
}
