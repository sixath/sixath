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

func TestIsPilotAgent(t *testing.T) {
	if IsPilotAgent(nil, "zone-4100-agent", "") {
		t.Fatal("empty pilots")
	}
	pilots := []string{"zone-4100-agent"}
	if !IsPilotAgent(pilots, "uuid", "zone-4100-agent") {
		t.Fatal("name match")
	}
	if !IsPilotAgent(pilots, "zone-4100-agent", "") {
		t.Fatal("id match")
	}
	if IsPilotAgent(pilots, "other", "other-name") {
		t.Fatal("no match")
	}
}
