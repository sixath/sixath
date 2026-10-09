package model

import "testing"

func TestJSONStrings_ScanNilClears(t *testing.T) {
	s := JSONStrings{"x"}
	if err := (&s).Scan(nil); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(s) != 0 {
		t.Fatalf("after Scan(nil) = %v, want empty", s)
	}
}

func TestJSONStrings_Value(t *testing.T) {
	v, err := JSONStrings(nil).Value()
	if err != nil || v != nil {
		t.Fatalf("nil Value() = %v, %v; want nil, nil", v, err)
	}
	v, err = JSONStrings{}.Value()
	if err != nil || v != "[]" {
		t.Fatalf("empty Value() = %v, %v; want \"[]\"", v, err)
	}
}
