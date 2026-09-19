package metadata

import "testing"

func TestGroupIndicesByPattern_DateSuffixAndLonely(t *testing.T) {
	got := GroupIndicesByPattern([]string{"app-2026.01.02", "app-2026.01.03", "lonely"})
	if !containsStr(got, "app-*") {
		t.Fatalf("want app-* from date-suffixed indices, got %v", got)
	}
	if !containsStr(got, "lonely") {
		t.Fatalf("ungroupable index must stay itself, got %v", got)
	}
	if containsStr(got, "app-2026.01.02") {
		t.Fatalf("physical dated names must not appear as patterns: %v", got)
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
