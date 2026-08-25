package agent

import "testing"

func TestIsBoundEvidenceTool(t *testing.T) {
	for _, name := range []string{
		"es_log_query", "jaeger_trace", "execute_read", "list_tables",
		"describe_table", "rca_read", "rca_grep", "rca_glob", "rca_symbol",
	} {
		if !IsBoundEvidenceTool(name) {
			t.Fatalf("want bound evidence: %s", name)
		}
	}
	if IsBoundEvidenceTool("skills_list") {
		t.Fatal("skills_list is not bound evidence")
	}
	if IsBoundEvidenceTool("") {
		t.Fatal("empty name is not bound evidence")
	}
}

func TestIsSkillsFamilyToolName(t *testing.T) {
	for _, name := range []string{
		"skills_list", "load_skill", "skill_view", "skill_manage",
		"read_skill_file", "execute_skill_script",
	} {
		if !IsSkillsFamilyToolName(name) {
			t.Fatalf("want skills family: %s", name)
		}
	}
	if IsSkillsFamilyToolName("es_log_query") {
		t.Fatal("es_log_query is not skills family")
	}
}

func TestHasSuccessfulBoundEvidence_nilTrace(t *testing.T) {
	if HasSuccessfulBoundEvidence(nil) {
		t.Fatal("nil trace must be false")
	}
}

func TestHasSuccessfulBoundEvidence_failedErrorNotSuccess(t *testing.T) {
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Error: "timeout",
	}}}
	if HasSuccessfulBoundEvidence(trace) {
		t.Fatal("Error != \"\" is not success")
	}
}

func TestHasSuccessfulBoundEvidence_esLogQuerySuccess(t *testing.T) {
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Allowed: true, Result: map[string]any{"ok": true},
	}}}
	if !HasSuccessfulBoundEvidence(trace) {
		t.Fatal("es_log_query success must be true")
	}
}
