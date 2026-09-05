package memory

import "testing"

func TestKindMatchesFilter(t *testing.T) {
	if !KindMatchesFilter(KindFact, "") {
		t.Fatal("fact should pass default filter")
	}
	if KindMatchesFilter(KindProcedural, "") {
		t.Fatal("procedural should not pass default filter")
	}
	if !KindMatchesFilter(KindProcedural, KindProcedural) {
		t.Fatal("procedural filter")
	}
	if !KindMatchesFilter(KindProcedural, KindFilterAny) {
		t.Fatal("any filter")
	}
}
