package biz

import "testing"

func TestValidRCAFuncPath(t *testing.T) {
	for _, fp := range []string{"rca_code", "rca_symbol", "jaeger_trace", "es_log_query"} {
		if !ValidRCAFuncPath(fp) {
			t.Fatalf("%q should be valid", fp)
		}
	}
	for _, fp := range []string{"", "rca_grep", "unknown"} {
		if ValidRCAFuncPath(fp) {
			t.Fatalf("%q should be invalid", fp)
		}
	}
}

func TestToolType_RCAIsValid(t *testing.T) {
	if !IsValidToolType(string(ToolTypeRCA)) {
		t.Fatal("rca must be a valid tool type")
	}
	if IsValidToolType("bogus") {
		t.Fatal("bogus must be invalid")
	}
}
