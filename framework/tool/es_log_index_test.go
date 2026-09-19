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
