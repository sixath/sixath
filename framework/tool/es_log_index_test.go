package tool

import (
	"context"
	"testing"
)

func TestIndexUnresolvedWhenCatEmpty(t *testing.T) {
	st := resolveIndexPhysical("cgschedule-*", []string{})
	if st.Resolved {
		t.Fatal("empty cat must be unresolved")
	}
	got := suggestIndexPatterns("cgschedule-*", []string{"app-logs-*", "svc-logs-*"}, "app-logs-*")
	if len(got) == 0 {
		t.Fatal("suggestions must not be empty")
	}
	if got[0] != "app-logs-*" {
		t.Fatalf("default_index must be first, got %v", got)
	}
	if !containsStr(got, "svc-logs-*") {
		t.Fatalf("must include catalog patterns without requiring backend- prefix: %v", got)
	}
	for _, p := range got {
		if len(p) >= 9 && p[:9] == "backend-" {
			t.Fatalf("must not inject hardcoded backend- patterns: %v", got)
		}
	}
}

func TestIndexResolvedStar(t *testing.T) {
	if !indexPatternAlwaysResolved("*") || !indexPatternAlwaysResolved("_all") {
		t.Fatal("* and _all must be treated as resolved without cat")
	}
	st := resolveIndexPhysical("*", nil)
	if !st.Resolved {
		t.Fatal("star must resolve even with nil catalog")
	}
}

func TestSuggestPrefersDefaultIndex(t *testing.T) {
	got := suggestIndexPatterns("cgschedule-*", []string{"app-logs-*", "other-*"}, "app-logs-*")
	if len(got) == 0 || got[0] != "app-logs-*" {
		t.Fatalf("first suggestion want default app-logs-*, got %v", got)
	}
}

func TestSuggestIndexPriorityAndLimit(t *testing.T) {
	catalog := []string{"aaa-*", "bbb-*", "prod-vm-*", "prod-game-*", "zzz-*"}
	got := suggestIndexPatternsWith("unknown-*", catalog, ESLogCluster{
		DefaultIndex:  "def-*",
		IndexPriority: []string{"prod-game-*", "prod-*"},
	})
	if len(got) < 4 || got[0] != "def-*" || got[1] != "prod-game-*" || got[2] != "prod-vm-*" {
		t.Fatalf("default first, then priority patterns in configured order: %v", got)
	}

	many := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		many = append(many, "idx"+string(rune('a'+i%26))+string(rune('a'+i/26))+"-*")
	}
	if n := len(suggestIndexPatternsWith("", many, ESLogCluster{})); n != esIndexSuggestLimit {
		t.Fatalf("default limit=%d got %d", esIndexSuggestLimit, n)
	}
	if n := len(suggestIndexPatternsWith("", many, ESLogCluster{IndexSuggestLimit: 50})); n != 50 {
		t.Fatalf("configured limit 50, got %d", n)
	}
}

func TestIndexResolvedWhenCatHits(t *testing.T) {
	st := resolveIndexPhysical("app-logs-*", []string{"app-logs-2026.01.02"})
	if !st.Resolved {
		t.Fatal("matching physical index must resolve")
	}
}

type memIndexCatalog struct {
	byPattern map[string][]string
	calls     []string
}

func (m *memIndexCatalog) ListIndexNames(_ context.Context, _, indexPattern string) ([]string, error) {
	m.calls = append(m.calls, indexPattern)
	if m.byPattern == nil {
		return nil, nil
	}
	return m.byPattern[indexPattern], nil
}
