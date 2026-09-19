package tool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRewriteEmptyHit_TermOnTextOnlyBecomesMatchPhrase(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"term": map[string]any{"operation": "DiscardUserArchive"}}}
	got, changed, hints := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{
		"operation": {Type: "text"},
	})
	if !changed {
		t.Fatal("expected rewrite")
	}
	q := got["query"].(map[string]any)
	if _, stillTerm := q["term"]; stillTerm {
		t.Fatalf("text-only term must not stay term: %#v", got)
	}
	mp, _ := q["match_phrase"].(map[string]any)
	if mp["operation"] != "DiscardUserArchive" {
		t.Fatalf("want match_phrase on operation, got %#v", got)
	}
	if len(hints) != 1 || hints[0].Field != "operation" || hints[0].Type != "text" {
		t.Fatalf("hints=%#v", hints)
	}
}

func TestRewriteEmptyHit_TermOnTextWithKeywordUsesKeywordSubfield(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"bool": map[string]any{
		"must": []any{map[string]any{"term": map[string]any{"operation": "/cgarchive.ArchiveService/DiscardUserArchive"}}},
	}}}
	got, changed, hints := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{
		"operation": {Type: "text", KeywordSubfield: true},
	})
	if !changed {
		t.Fatal("expected rewrite to operation.keyword")
	}
	must := got["query"].(map[string]any)["bool"].(map[string]any)["must"].([]any)
	term := must[0].(map[string]any)["term"].(map[string]any)
	if _, ok := term["operation.keyword"]; !ok {
		t.Fatalf("want term on operation.keyword, got %#v", must[0])
	}
	if hints[0].Prefer[0] != "term on operation.keyword" {
		t.Fatalf("prefer=%v", hints[0].Prefer)
	}
}

func TestRewriteEmptyHit_MatchOnKeywordBecomesTerm(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"match": map[string]any{"status": "ok"}}}
	got, changed, _ := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{
		"status": {Type: "keyword"},
	})
	if !changed {
		t.Fatal("expected rewrite")
	}
	term := got["query"].(map[string]any)["term"].(map[string]any)
	if term["status"] != "ok" {
		t.Fatalf("got %#v", got)
	}
}

func TestRewriteEmptyHit_AlreadyCorrectNoChange(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"term": map[string]any{"status": "ok"}}}
	_, changed, hints := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{
		"status": {Type: "keyword"},
	})
	if changed {
		t.Fatal("keyword+term is already correct")
	}
	if len(hints) != 1 || hints[0].Type != "keyword" {
		t.Fatalf("still want mapping hint, got %#v", hints)
	}
}

func TestSuggestedQueriesForMapping(t *testing.T) {
	textKw := ESFieldMapping{Type: "text", KeywordSubfield: true}
	got := textKw.SuggestedQueries("operation")
	if !containsStr(got, "term on operation.keyword") || !containsStr(got, "match on operation") {
		t.Fatalf("suggested=%v", got)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestParseESFieldMapping_TextWithKeyword(t *testing.T) {
	raw := []byte(`{
	  "idx-1": {
	    "mappings": {
	      "operation": {
	        "full_name": "operation",
	        "mapping": {
	          "operation": {
	            "type": "text",
	            "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}
	          }
	        }
	      }
	    }
	  }
	}`)
	m, ok := parseESFieldMappingJSON(raw, "operation")
	if !ok || m.Type != "text" || !m.KeywordSubfield {
		t.Fatalf("got %#v ok=%v", m, ok)
	}
}

func TestParseLuceneQueryFields_FlowID(t *testing.T) {
	got := parseLuceneQueryFields("flow_id: 4103_j0qjifnv99pq")
	if len(got) != 1 || got[0] != "flow_id" {
		t.Fatalf("got %v", got)
	}
}

func TestParseLuceneQueryFields_SkipsQuotedColons(t *testing.T) {
	got := parseLuceneQueryFields(`message:"a:b:c" AND level:ERROR`)
	if !containsStr(got, "message") || !containsStr(got, "level") {
		t.Fatalf("got %v", got)
	}
	if containsStr(got, "a") || containsStr(got, "b") {
		t.Fatalf("quoted colons must not become fields: %v", got)
	}
}

func TestCollectQueryFieldNames_QueryString(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "flow_id: 4103_j0qjifnv99pq"}}}
	got := collectQueryFieldNames(dsl)
	if len(got) != 1 || got[0] != "flow_id" {
		t.Fatalf("got %v", got)
	}
}

func TestCollectQueryFieldNames_TermsQuery(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"terms": map[string]any{"flowId": []any{"4103_a", "4103_b"}}}}
	got := collectQueryFieldNames(dsl)
	if !containsStr(got, "flowId") {
		t.Fatalf("terms query must expose flowId for mapping check, got %v", got)
	}
}

func TestCollectQueryFieldNames_AggsAndExists(t *testing.T) {
	dsl := map[string]any{
		"query": map[string]any{"exists": map[string]any{"field": "flowId"}},
		"aggs":  map[string]any{"by_gid": map[string]any{"terms": map[string]any{"field": "gid", "size": 50}}},
	}
	got := collectQueryFieldNames(dsl)
	if !containsStr(got, "flowId") || !containsStr(got, "gid") {
		t.Fatalf("exists + aggs fields must be checked against mapping, got %v", got)
	}
	if containsStr(got, "field") || containsStr(got, "size") {
		t.Fatalf("meta keys must not look like mapped fields: %v", got)
	}
}

func TestRewriteEmptyHit_TermsOnTextWithKeyword(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"terms": map[string]any{"flowId": []any{"4103_a"}}}}
	got, changed, _ := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{
		"flowId": {Type: "text", KeywordSubfield: true},
	})
	if !changed {
		t.Fatal("terms on text+.keyword should rewrite to flowId.keyword")
	}
	terms := got["query"].(map[string]any)["terms"].(map[string]any)
	if _, ok := terms["flowId.keyword"]; !ok {
		t.Fatalf("want terms on flowId.keyword, got %#v", got)
	}
}

func TestSuggestSimilarMappedFields_NormalizedName(t *testing.T) {
	got := suggestSimilarMappedFields("flow_id", []string{"trace_id", "flowId", "operation", "message"})
	if !containsStr(got, "flowId") {
		t.Fatalf("flow_id should match flowId, got %v", got)
	}
	if containsStr(got, "trace_id") {
		t.Fatalf("trace_id is not similar enough to flow_id: %v", got)
	}
}

func TestFlattenMappingFieldNames(t *testing.T) {
	raw := []byte(`{
	  "idx-1": {
	    "mappings": {
	      "properties": {
	        "trace_id": {"type": "keyword"},
	        "message": {"type": "text", "fields": {"keyword": {"type": "keyword"}}}
	      }
	    }
	  }
	}`)
	got := flattenMappingFieldNames(raw)
	if !containsStr(got, "trace_id") || !containsStr(got, "message") {
		t.Fatalf("got %v", got)
	}
}

func TestRewriteEmptyHit_RoundTripJSON(t *testing.T) {
	orig := `{"query":{"term":{"operation":"x"}}}`
	var dsl map[string]any
	if err := json.Unmarshal([]byte(orig), &dsl); err != nil {
		t.Fatal(err)
	}
	got, changed, _ := rewriteEmptyHitQuery(dsl, map[string]ESFieldMapping{"operation": {Type: "text"}})
	if !changed {
		t.Fatal("expected change")
	}
	b, _ := json.Marshal(got)
	if string(b) == orig {
		t.Fatal("dsl should differ")
	}
}

func TestRewriteUnknownLuceneFields_SimilarField(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "vmid:199306"}}}
	got, changed, reason := rewriteUnknownQueryFields(dsl, []string{"vm_id", "M", "L"}, "")
	if !changed || reason != "similar_field" {
		t.Fatalf("changed=%v reason=%q", changed, reason)
	}
	qs := got["query"].(map[string]any)["query_string"].(map[string]any)
	if qs["query"] != "vm_id:199306" {
		t.Fatalf("want vm_id rewrite, got %#v", qs)
	}
}

func TestRewriteUnknownLuceneFields_BodyCandidateMessage(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "foo:bar"}}}
	got, changed, reason := rewriteUnknownQueryFields(dsl, []string{"message"}, "")
	if !changed || reason != "body_field" {
		t.Fatalf("changed=%v reason=%q", changed, reason)
	}
	qs := got["query"].(map[string]any)["query_string"].(map[string]any)
	if qs["default_field"] != "message" {
		t.Fatalf("want default_field message, got %#v", qs)
	}
	if !strings.Contains(fmt.Sprint(qs["query"]), "bar") {
		t.Fatalf("want value bar kept, got %#v", qs)
	}
}

func TestRewriteUnknownLuceneFields_BodyCandidateM(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "foo:bar"}}}
	got, changed, reason := rewriteUnknownQueryFields(dsl, []string{"M", "L"}, "")
	if !changed || reason != "body_field" {
		t.Fatalf("changed=%v reason=%q", changed, reason)
	}
	qs := got["query"].(map[string]any)["query_string"].(map[string]any)
	if qs["default_field"] != "M" {
		t.Fatalf("want default_field M from candidate list, got %#v", qs)
	}
}

func TestRewriteUnknownLuceneFields_Unfielded(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "foo:bar"}}}
	got, changed, reason := rewriteUnknownQueryFields(dsl, []string{"L"}, "")
	if !changed || reason != "unfielded" {
		t.Fatalf("changed=%v reason=%q", changed, reason)
	}
	qs := got["query"].(map[string]any)["query_string"].(map[string]any)
	if _, ok := qs["default_field"]; ok {
		t.Fatalf("unfielded must not set default_field: %#v", qs)
	}
	if !strings.Contains(fmt.Sprint(qs["query"]), "bar") {
		t.Fatalf("want value bar kept, got %#v", qs)
	}
}

func TestRewriteUnknownLuceneFields_MappedUnchanged(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "vm_id:199306"}}}
	_, changed, reason := rewriteUnknownQueryFields(dsl, []string{"vm_id", "M"}, "")
	if changed || reason != "" {
		t.Fatalf("mapped field must not rewrite, changed=%v reason=%q", changed, reason)
	}
}

func TestRewriteUnknownLuceneFields_ConfiguredBodyFieldWins(t *testing.T) {
	dsl := map[string]any{"query": map[string]any{"query_string": map[string]any{"query": "foo:bar"}}}
	got, changed, reason := rewriteUnknownQueryFields(dsl, []string{"text", "message"}, "text")
	if !changed || reason != "body_field" {
		t.Fatalf("changed=%v reason=%q", changed, reason)
	}
	qs := got["query"].(map[string]any)["query_string"].(map[string]any)
	if qs["default_field"] != "text" {
		t.Fatalf("configured body_field must win over message candidate, got %#v", qs)
	}
}
