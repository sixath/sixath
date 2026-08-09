package server

import "testing"

func TestParseBoolQuery(t *testing.T) {
	if !parseBoolQuery("", true) {
		t.Fatal("default true")
	}
	if parseBoolQuery("", false) {
		t.Fatal("default false")
	}
	for _, v := range []string{"1", "true", "YES", "on"} {
		if !parseBoolQuery(v, false) {
			t.Fatalf("%q should be true", v)
		}
	}
	for _, v := range []string{"0", "false", "NO", "off"} {
		if parseBoolQuery(v, true) {
			t.Fatalf("%q should be false", v)
		}
	}
}

func TestParseIntQuery(t *testing.T) {
	if parseIntQuery("", 5) != 5 {
		t.Fatal("default")
	}
	if parseIntQuery("7", 5) != 7 {
		t.Fatal("parse")
	}
	if parseIntQuery("x", 5) != 5 {
		t.Fatal("invalid fallback")
	}
}
