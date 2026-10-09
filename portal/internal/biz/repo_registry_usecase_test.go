package biz

import (
	"path/filepath"
	"testing"
)

func TestMapTargetToConfiguredRoot(t *testing.T) {
	cfg := filepath.Join(string(filepath.Separator), "srv", "code")
	real := filepath.Join(string(filepath.Separator), "data", "code")
	other := filepath.Join(string(filepath.Separator), "opt", "src")
	roots := []repoCodeRoot{{path: cfg, resolved: real}, {path: other, resolved: other}}

	cases := []struct{ in, want string }{
		{filepath.Join(real, "cg", "svc"), filepath.Join(cfg, "cg", "svc")},
		{real, cfg},
		{filepath.Join(cfg, "cg"), filepath.Join(cfg, "cg")},
		{filepath.Join(other, "x"), filepath.Join(other, "x")},
		{filepath.Join(string(filepath.Separator), "data", "codex", "a"), filepath.Join(string(filepath.Separator), "data", "codex", "a")},
		{filepath.Join(string(filepath.Separator), "elsewhere"), filepath.Join(string(filepath.Separator), "elsewhere")},
	}
	for _, c := range cases {
		if got := mapTargetToConfiguredRoot(c.in, roots); got != c.want {
			t.Errorf("mapTargetToConfiguredRoot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPathWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "data", "code")
	cases := []struct {
		p    string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "a", "b"), true},
		{filepath.Join(string(filepath.Separator), "data", "codex"), false},
		{filepath.Join(string(filepath.Separator), "data"), false},
	}
	for _, c := range cases {
		if got := pathWithin(root, c.p); got != c.want {
			t.Errorf("pathWithin(%q) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestCleanCodeRootsAbsAndDedupe(t *testing.T) {
	dir := t.TempDir()
	got := cleanCodeRoots([]string{dir, " " + dir + string(filepath.Separator) + " ", ""})
	if len(got) != 1 || got[0].path != filepath.Clean(dir) || got[0].resolved == "" {
		t.Fatalf("cleanCodeRoots = %#v", got)
	}
	rel := cleanCodeRoots([]string{"rel-root-does-not-exist"})
	if len(rel) != 1 || !filepath.IsAbs(rel[0].path) || rel[0].resolved != rel[0].path {
		t.Fatalf("relative root = %#v", rel)
	}
}
