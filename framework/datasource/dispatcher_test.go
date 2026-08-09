package datasource

import (
	"context"
	"testing"
)

type stubDS struct {
	id, typ string
}

func (s *stubDS) ID() string                 { return s.id }
func (s *stubDS) Type() string               { return s.typ }
func (s *stubDS) Ping(context.Context) error { return nil }
func (s *stubDS) Close() error               { return nil }

func TestTypedDispatcher_For(t *testing.T) {
	reg := NewRegistry()
	RegisterNoop(reg)
	d := NewTypedDispatcher[string](reg)
	d.Register(TypeNoop, "noop-handler")
	if _, err := reg.Register(Config{ID: "ds1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	got, err := d.For("ds1")
	if err != nil || got != "noop-handler" {
		t.Fatalf("For: got %q err %v", got, err)
	}
	_, err = d.For("missing")
	if err != ErrNotFound {
		t.Fatalf("missing id: %v", err)
	}
}

func TestTypedDispatcher_UnsupportedType(t *testing.T) {
	reg := NewRegistry()
	RegisterNoop(reg)
	d := NewTypedDispatcher[string](reg)
	if _, err := reg.Register(Config{ID: "ds1", Type: "noop"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.For("ds1")
	if err != ErrUnsupportedType {
		t.Fatalf("unregistered type: %v", err)
	}
}
