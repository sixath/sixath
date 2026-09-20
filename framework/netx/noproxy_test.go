package netx

import "testing"

func TestMatchNoProxy(t *testing.T) {
	cases := []struct {
		host  string
		rules []string
		want  bool
	}{
		{"es.local", []string{"es.local"}, true},
		{"ES.LOCAL", []string{"es.local"}, true},
		{"a.example.com", []string{".example.com"}, true},
		{"example.com", []string{".example.com"}, false},
		{"8.8.8.8", []string{"8.8.8.0/24"}, true},
		{"unresolvable.invalid", []string{"10.0.0.0/8"}, false},
		{"any.host", []string{"*"}, true},
		{"keep.proxy", []string{"  ", "other"}, false},
	}
	for _, c := range cases {
		if got := MatchNoProxy(c.host, c.rules); got != c.want {
			t.Fatalf("host=%s rules=%v got=%v want=%v", c.host, c.rules, got, c.want)
		}
	}
}
