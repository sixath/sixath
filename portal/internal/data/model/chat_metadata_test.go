package model

import (
	"encoding/json"
	"testing"
)

func TestJSONMap_ValueAndScan(t *testing.T) {
	m := JSONMap{"timeline": []any{map[string]any{"kind": "tool", "id": "c1"}}}
	v, err := m.Value()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		t.Fatalf("Value: %#v", v)
	}
	var scanned JSONMap
	if err := scanned.Scan([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if scanned["timeline"] == nil {
		t.Fatalf("scan lost timeline: %#v", scanned)
	}
	// round-trip via encoding/json for sanity
	b, _ := json.Marshal(scanned)
	if !json.Valid(b) {
		t.Fatal("invalid json")
	}
}

func TestJSONMap_ScanNil(t *testing.T) {
	var m JSONMap
	if err := m.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if m != nil {
		t.Fatalf("want nil, got %#v", m)
	}
}
