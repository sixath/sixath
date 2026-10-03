package tool

import (
	"encoding/json"
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

func TestFlattenMappingFieldNames_RuntimeAndOpenObjects(t *testing.T) {
	raw := []byte(`{
	  "idx-1": {
	    "mappings": {
	      "runtime": {"day_of_week": {"type": "keyword"}},
	      "properties": {
	        "labels": {"type": "flattened"},
	        "payload": {"type": "object", "enabled": false},
	        "meta": {"properties": {"host": {"type": "keyword"}}}
	      }
	    }
	  },
	  "idx-legacy": {
	    "mappings": {"_doc": {"runtime": {"legacy_rt": {"type": "long"}}, "properties": {"x": {"type": "keyword"}}}}
	  }
	}`)
	catalog := flattenMappingFieldNames(raw)
	for _, want := range []string{"day_of_week", "legacy_rt", "labels", "labels.*", "payload.*", "meta.host"} {
		if !containsStr(catalog, want) {
			t.Fatalf("catalog %v missing %s", catalog, want)
		}
	}
	if containsStr(catalog, "meta.*") {
		t.Fatalf("regular object must not be open: %v", catalog)
	}
	got := unknownQueryFields([]string{"day_of_week", "labels.app.tier", "payload.a", "meta.host", "meta.nope", "labelsx.a"}, catalog)
	if len(got) != 2 || got[0] != "meta.nope" || got[1] != "labelsx.a" {
		t.Fatalf("unknown %v", got)
	}
	if sim := suggestSimilarMappedFields("lables", catalog); containsStr(sim, "labels.*") {
		t.Fatalf("open-prefix markers must not be suggested: %v", sim)
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
