package tool

import (
	"reflect"
	"testing"
)

func TestSuggest(t *testing.T) {
	cands := []string{"game_flow", "game_flow_all", "backend", "Service.Name", "vm_state"}
	cases := []struct {
		in   string
		n    int
		want []string
	}{
		{"service.name", 3, []string{"Service.Name"}},
		{"game_flo", 3, []string{"game_flow", "game_flow_all"}},
		{"flow", 3, []string{"game_flow", "game_flow_all"}},
		{"vm_stat", 3, []string{"vm_state"}},
		{"zzzzzz", 3, nil},
		{"", 3, nil},
		{"game_flow", 1, []string{"game_flow"}},
	}
	for _, tc := range cases {
		if got := Suggest(tc.in, cands, tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Suggest(%q)=%v want %v", tc.in, got, tc.want)
		}
	}

	extra := []struct {
		name  string
		in    string
		cands []string
		n     int
		want  []string
	}{
		{"duplicates collapsed", "ab", []string{"abc", "abc", "abd"}, 3, []string{"abc", "abd"}},
		{"n zero", "abc", []string{"abc"}, 0, nil},
		{"n negative", "abc", []string{"abc"}, -1, nil},
		{"same rank and dist keeps input order", "ga", []string{"gaz", "gay"}, 3, []string{"gaz", "gay"}},
		{"prefix dist counts runes", "日志", []string{"日志ab", "日志表"}, 3, []string{"日志表", "日志ab"}},
	}
	for _, tc := range extra {
		if got := Suggest(tc.in, tc.cands, tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Suggest(%q)=%v want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestLevenshteinRunes(t *testing.T) {
	if d := Levenshtein("日志表", "日志"); d != 1 {
		t.Fatalf("got %d", d)
	}
	if d := Levenshtein("kitten", "sitting"); d != 3 {
		t.Fatalf("got %d", d)
	}
}
