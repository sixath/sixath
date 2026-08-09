package growth

import "testing"

func TestDetectOneToOneRename(t *testing.T) {
	renames, ok := DetectOneToOneRename(
		[]string{"alpha", "beta"},
		[]string{"alpha", "gamma"},
	)
	if !ok {
		t.Fatal("expected ok")
	}
	if renames["beta"] != "gamma" || len(renames) != 1 {
		t.Fatalf("renames = %#v", renames)
	}
}

func TestDetectOneToOneRename_noChange(t *testing.T) {
	_, ok := DetectOneToOneRename([]string{"a", "b"}, []string{"b", "a"})
	if ok {
		t.Fatal("expected ok=false for identical sets")
	}
}

func TestDetectOneToOneRename_multiChange(t *testing.T) {
	_, ok := DetectOneToOneRename(
		[]string{"a", "b"},
		[]string{"c", "d"},
	)
	if ok {
		t.Fatal("expected ok=false for many-to-many")
	}
}

func TestDetectOneToOneRename_onlyRemoved(t *testing.T) {
	_, ok := DetectOneToOneRename([]string{"a", "b"}, []string{"a"})
	if ok {
		t.Fatal("expected ok=false when only removed")
	}
}

func TestDetectOneToOneRename_onlyAdded(t *testing.T) {
	_, ok := DetectOneToOneRename([]string{"a"}, []string{"a", "b"})
	if ok {
		t.Fatal("expected ok=false when only added")
	}
}

func TestDetectOneToOneRename_emptyAndDuplicates(t *testing.T) {
	renames, ok := DetectOneToOneRename(
		[]string{"old", "old", ""},
		[]string{"new", "new"},
	)
	if !ok || renames["old"] != "new" {
		t.Fatalf("got renames=%#v ok=%v", renames, ok)
	}
}
