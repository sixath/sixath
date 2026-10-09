package tool

import (
	"path/filepath"
	"testing"
)

func TestNamedRCARoots_BasenameWhenUnique(t *testing.T) {
	got := NamedRCARoots([]string{"/codes/a/svc-a", "/codes/b/svc-b"})
	want := []RCARoot{
		{Name: "svc-a", Path: filepath.Clean("/codes/a/svc-a")},
		{Name: "svc-b", Path: filepath.Clean("/codes/b/svc-b")},
	}
	assertRCARoots(t, got, want)
}

func TestNamedRCARoots_QualifiesCollidingBasenames(t *testing.T) {
	got := NamedRCARoots([]string{"/codes/cloudgame/gateway", "/codes/migu/gateway", "/codes/x/solo"})
	want := []RCARoot{
		{Name: "cloudgame/gateway", Path: filepath.Clean("/codes/cloudgame/gateway")},
		{Name: "migu/gateway", Path: filepath.Clean("/codes/migu/gateway")},
		{Name: "solo", Path: filepath.Clean("/codes/x/solo")},
	}
	assertRCARoots(t, got, want)
}

func TestNamedRCARoots_QualifiesUntilUnique(t *testing.T) {
	got := NamedRCARoots([]string{"/p/team/gw", "/q/team/gw"})
	if got[0].Name != "p/team/gw" || got[1].Name != "q/team/gw" {
		t.Fatalf("names = %q, %q", got[0].Name, got[1].Name)
	}
}

func TestNamedRCARoots_FallsBackToFullPathWhenSegmentsIdentical(t *testing.T) {
	got := NamedRCARoots([]string{"/a/gw", "a/gw"})
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	want0 := filepath.ToSlash(filepath.Clean("/a/gw"))
	want1 := filepath.ToSlash(filepath.Clean("a/gw"))
	if got[0].Name != want0 || got[1].Name != want1 {
		t.Fatalf("names = %q, %q; want %q, %q", got[0].Name, got[1].Name, want0, want1)
	}
	if err := validateRCARoots(got); err != nil {
		t.Fatalf("validateRCARoots: %v", err)
	}
}

func TestNamedRCARoots_DropsEmptyAndDuplicatePaths(t *testing.T) {
	got := NamedRCARoots([]string{"", "  ", "/codes/a", "/codes/a/", "/codes/a"})
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %#v", got)
	}
}

func TestValidateRCARoots(t *testing.T) {
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: "/x/a"}, {Name: "b", Path: "/x/b"}}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: "/x/a"}, {Name: "a", Path: "/y/a"}}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	if err := validateRCARoots([]RCARoot{{Name: "", Path: "/x/a"}}); err == nil {
		t.Fatal("expected empty name error")
	}
	if err := validateRCARoots([]RCARoot{{Name: "a", Path: " "}}); err == nil {
		t.Fatal("expected empty path error")
	}
}

func assertRCARoots(t *testing.T, got, want []RCARoot) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
