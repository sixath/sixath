package anyx

import (
	"encoding/json"
	"testing"
)

func TestInt64FromAny(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   int64
		wantOK bool
	}{
		{"int", int(42), 42, true},
		{"int32", int32(42), 42, true},
		{"int64", int64(42), 42, true},
		{"uint32", uint32(42), 42, true},
		{"uint64", uint64(42), 42, true},
		{"float64 (json default)", float64(42), 42, true},
		{"float32", float32(42), 42, true},
		{"json.Number", json.Number("42"), 42, true},
		{"string fails", "42", 0, false},
		{"nil fails", nil, 0, false},
		{"bool fails", true, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Int64FromAny(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Int64FromAny(%v) = (%d, %v), want (%d, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
