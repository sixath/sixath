package tool

import (
	"strings"
	"testing"
)

func TestSplitScriptOutput_DropsBlankAndTrailing(t *testing.T) {
	lines := splitScriptOutput("a\n\nb\n")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("%q", lines)
	}
}

func TestRowsFromScriptLines_AllJSONObjects(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, `{"b":2}`})
	if len(rows) != 2 {
		t.Fatalf("%v", rows)
	}
	if _, ok := rows[0]["line"]; ok {
		t.Fatal("must not wrap pure json objects")
	}
	if rows[0]["a"] != float64(1) {
		t.Fatalf("%v", rows[0])
	}
}

func TestRowsFromScriptLines_WrapsText(t *testing.T) {
	rows := rowsFromScriptLines([]string{"hello", "world"})
	if rows[0]["line"] != "hello" || rows[1]["line"] != "world" {
		t.Fatalf("%v", rows)
	}
}

func TestRowsFromScriptLines_MixedGoesWrap(t *testing.T) {
	rows := rowsFromScriptLines([]string{`{"a":1}`, "not-json"})
	if rows[0]["line"] != `{"a":1}` || rows[1]["line"] != "not-json" {
		t.Fatalf("%v", rows)
	}
}

func TestTruncateUTF8Bytes(t *testing.T) {
	s := strings.Repeat("x", 9000)
	got := truncateUTF8Bytes(s, 8192)
	if len(got) != 8192 {
		t.Fatalf("len=%d", len(got))
	}
	s2 := strings.Repeat("你", 100)
	got2 := truncateUTF8Bytes(s2, 4)
	if got2 != "你" {
		t.Fatalf("%q", got2)
	}
}
