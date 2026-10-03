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
}

func TestLevenshteinRunes(t *testing.T) {
	if d := Levenshtein("日志表", "日志"); d != 1 {
		t.Fatalf("got %d", d)
	}
	if d := Levenshtein("kitten", "sitting"); d != 3 {
		t.Fatalf("got %d", d)
	}
}
