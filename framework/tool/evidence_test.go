package tool

import (
	"errors"
	"testing"

	"github.com/sixath/framework/executor"
)

func TestNormalizeEvidenceResult_okWithRefs(t *testing.T) {
	in := map[string]any{
		"trace_id": "abc",
		"spans":    []any{},
	}
	out := NormalizeRCAResult(in, EvidenceMeta{Tool: "jaeger_trace", OK: true})
	if out["ok"] != true {
		t.Fatalf("%#v", out)
	}
	if out["trace_id"] != "abc" {
		t.Fatalf("expected trace_id preserved, got %#v", out)
	}
	refs, _ := out["evidence_refs"].([]EvidenceRef)
	if len(refs) == 0 || refs[0].Kind != "jaeger_trace" {
		t.Fatalf("refs=%#v", refs)
	}
	if refs[0].TraceID != "abc" {
		t.Fatalf("refs[0].TraceID=%q, want abc", refs[0].TraceID)
	}
}

func TestNormalizeEvidenceResult_transientError(t *testing.T) {
	out := NormalizeRCAResult(
		map[string]any{"error": "timeout"},
		EvidenceMeta{Tool: "es_log_query", OK: false, ErrorCode: ErrorTransient},
	)
	if out["ok"] != false || out["error_code"] != ErrorTransient {
		t.Fatalf("%#v", out)
	}
	if out["error"] != "timeout" {
		t.Fatalf("expected original error preserved, got %#v", out)
	}
	if _, has := out["evidence_refs"]; has {
		t.Fatalf("unexpected evidence_refs on error: %#v", out)
	}
}

func TestNormalizeEvidenceResult_explicitRefs(t *testing.T) {
	refs := []EvidenceRef{{Kind: "rca_grep", Repo: "svc-a", Path: "main.go", Line: 10}}
	out := NormalizeRCAResult(
		map[string]any{"matches": []any{}},
		EvidenceMeta{Tool: "rca_grep", OK: true, Refs: refs},
	)
	got, _ := out["evidence_refs"].([]EvidenceRef)
	if len(got) != 1 || got[0].Repo != "svc-a" {
		t.Fatalf("refs=%#v", got)
	}
}

func TestNormalizeEvidenceResult_deriveRCAGrep(t *testing.T) {
	out := NormalizeRCAResult(
		map[string]any{
			"matches": []any{
				map[string]any{"repo": "service-a", "file": "a.go", "line": 12},
			},
		},
		EvidenceMeta{Tool: "rca_grep", OK: true},
	)
	refs, _ := out["evidence_refs"].([]EvidenceRef)
	if len(refs) != 1 {
		t.Fatalf("refs=%#v", refs)
	}
	if refs[0].Kind != "rca_grep" || refs[0].Repo != "service-a" || refs[0].Path != "a.go" || refs[0].Line != 12 {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestNormalizeEvidenceResult_deriveESLogQuery(t *testing.T) {
	out := NormalizeRCAResult(
		map[string]any{
			"trace_id": "tid-1",
			"hits": []any{
				map[string]any{"service": "svc"},
			},
		},
		EvidenceMeta{Tool: "es_log_query", OK: true},
	)
	refs, _ := out["evidence_refs"].([]EvidenceRef)
	if len(refs) != 1 || refs[0].Kind != "es_log_query" || refs[0].TraceID != "tid-1" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestCollectEvidenceRefsFromToolResults(t *testing.T) {
	r1 := map[string]any{
		"ok": true,
		"evidence_refs": []EvidenceRef{
			{Kind: "jaeger_trace", TraceID: "t1"},
		},
	}
	r2 := map[string]any{
		"ok": true,
		"evidence_refs": []any{
			map[string]any{"kind": "rca_grep", "repo": "svc-b", "path": "b.go", "line": 3},
		},
	}
	nested := map[string]any{
		"steps": []any{
			map[string]any{
				"evidence_refs": []EvidenceRef{{Kind: "es_log_query", TraceID: "t2"}},
			},
		},
	}

	refs := CollectEvidenceRefs(r1, r2, nested)
	if len(refs) != 3 {
		t.Fatalf("len=%d refs=%#v", len(refs), refs)
	}
	if refs[0].Kind != "jaeger_trace" || refs[0].TraceID != "t1" {
		t.Fatalf("refs[0]=%#v", refs[0])
	}
	if refs[1].Kind != "rca_grep" || refs[1].Repo != "svc-b" || refs[1].Path != "b.go" || refs[1].Line != 3 {
		t.Fatalf("refs[1]=%#v", refs[1])
	}
	if refs[2].Kind != "es_log_query" || refs[2].TraceID != "t2" {
		t.Fatalf("refs[2]=%#v", refs[2])
	}
}

func assertRCAEvidenceOK(t *testing.T, m map[string]any, kind string) {
	t.Helper()
	if m["ok"] != true {
		t.Fatalf("want ok=true, got %#v", m)
	}
	if _, has := m["error_code"]; has {
		t.Fatalf("unexpected error_code on success: %#v", m)
	}
	refs, ok := m["evidence_refs"].([]EvidenceRef)
	if !ok || len(refs) == 0 {
		t.Fatalf("want evidence_refs, got %#v", m["evidence_refs"])
	}
	if refs[0].Kind != kind {
		t.Fatalf("refs[0].Kind=%q, want %q", refs[0].Kind, kind)
	}
}

func assertRCAEvidenceError(t *testing.T, m map[string]any, code string) {
	t.Helper()
	if m["ok"] != false {
		t.Fatalf("want ok=false, got %#v", m)
	}
	if m["error_code"] != code {
		t.Fatalf("error_code=%v, want %q; full=%#v", m["error_code"], code, m)
	}
	if _, has := m["error"]; !has {
		t.Fatalf("expected error field, got %#v", m)
	}
}

func TestDeriveEvidenceRefs_RCASymbol(t *testing.T) {
	payload := map[string]any{
		"locations": []any{
			map[string]any{"repo": "svc", "file": "a.go", "line": 9, "name": "Foo"},
		},
	}
	refs := deriveEvidenceRefs("rca_symbol", payload)
	if len(refs) != 1 {
		t.Fatalf("refs=%#v", refs)
	}
	if refs[0].Kind != "rca_symbol" || refs[0].Repo != "svc" || refs[0].Path != "a.go" || refs[0].Line != 9 {
		t.Fatalf("refs[0]=%#v", refs[0])
	}
}

func TestDeriveEvidenceRefs_RCASymbol_emptyLocations(t *testing.T) {
	payload := map[string]any{"locations": []any{}}
	if refs := deriveEvidenceRefs("rca_symbol", payload); len(refs) != 0 {
		t.Fatalf("refs=%#v, want empty", refs)
	}
}

func TestDeriveEvidenceRefs_RCASymbol_emptyReferencesSummary(t *testing.T) {
	payload := map[string]any{"action": "references", "repo": "svc", "locations": []any{}}
	refs := deriveEvidenceRefs("rca_symbol", payload)
	if len(refs) != 1 || refs[0].Summary != "no inbound callers" || refs[0].Repo != "svc" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestClassifyRCAError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("context deadline exceeded"), ErrorTransient},
		{errors.New("i/o timeout connecting"), ErrorTransient},
		{errors.New("connection refused"), ErrorTransient},
		{errors.New("jaeger returned 502: bad gateway"), ErrorTransient},
		{errors.New("jaeger returned 404: not found"), ErrorPermanent},
		{errors.New("pattern is required"), ErrorPermanent},
		{errors.New("path escapes root"), ErrorPermanent},
		{errors.New("decode jaeger response: invalid"), ErrorPermanent},
		{nil, ErrorPermanent},
	}
	for _, tc := range cases {
		got := classifyRCAError(tc.err)
		if got != tc.want {
			t.Fatalf("classifyRCAError(%v)=%q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestStampHitContract_empty(t *testing.T) {
	out := StampHitContract(map[string]any{"hits": []any{}}, HitStamp{Status: HitStatusEmpty, QueriedIndex: "vm-manager-*"})
	if out["hit_status"] != HitStatusEmpty || out["queried_index"] != "vm-manager-*" {
		t.Fatalf("%#v", out)
	}
}

func TestHitContractFromResult_missingNotHits(t *testing.T) {
	st, _, _ := HitContractFromResult(map[string]any{"hits": []any{}})
	if st != "" {
		t.Fatalf("missing hit_status must not be hits, got %q", st)
	}
	st, idx, repo := HitContractFromResult(map[string]any{
		"hit_status": HitStatusEmpty, "queried_index": "vm-manager-*", "repo": "svc",
	})
	if st != HitStatusEmpty || idx != "vm-manager-*" || repo != "svc" {
		t.Fatalf("%q %q %q", st, idx, repo)
	}

	qr := &executor.QueryResult{HitStatus: HitStatusEmpty}
	st, idx, repo = HitContractFromResult(qr)
	if st != HitStatusEmpty || idx != "" || repo != "" {
		t.Fatalf("QueryResult ptr %q %q %q", st, idx, repo)
	}
	st, idx, repo = HitContractFromResult(*qr)
	if st != HitStatusEmpty || idx != "" || repo != "" {
		t.Fatalf("QueryResult val %q %q %q", st, idx, repo)
	}
}

func TestHitStatusFromCount(t *testing.T) {
	if HitStatusFromCount(true, 0) != HitStatusEmpty || HitStatusFromCount(true, 2) != HitStatusHits || HitStatusFromCount(false, 0) != HitStatusError {
		t.Fatal("HitStatusFromCount")
	}
}

func TestHitContractFromResult_SpillStub(t *testing.T) {
	st, idx, _ := HitContractFromResult(&QuerySpillStub{
		HitStatus: HitStatusHits, QueriedIndex: "backend-cgsession-*",
	})
	if st != HitStatusHits || idx != "backend-cgsession-*" {
		t.Fatalf("%q %q", st, idx)
	}
}

func TestCollectEvidenceRefs_SpillStub(t *testing.T) {
	stub := &QuerySpillStub{
		Spilled: true, Path: "tmp/results/s/1.jsonl", Count: 51, OK: true,
		HitStatus: HitStatusHits,
		EvidenceRefs: []EvidenceRef{{Kind: "es_log_query", TraceID: "t1"}},
		Sample: []map[string]any{{"i": 1}},
	}
	refs := CollectEvidenceRefs(stub)
	if len(refs) != 1 || refs[0].Kind != "es_log_query" || refs[0].Summary == "no hits" {
		t.Fatalf("%#v", refs)
	}
}
