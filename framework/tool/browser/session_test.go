package browser

import (
	"context"
	"errors"
	"testing"
)

func TestSessionStore_GetOrCreate_reusesSameBackend(t *testing.T) {
	store := NewSessionStore()
	factoryCalls := 0
	factory := func() (Backend, error) {
		factoryCalls++
		return NewFakeBackend(), nil
	}

	b1, err := store.GetOrCreate("sess-1", factory)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	b2, err := store.GetOrCreate("sess-1", factory)
	if err != nil {
		t.Fatalf("GetOrCreate second: %v", err)
	}
	if b1 != b2 {
		t.Fatal("expected same backend instance for same sessionID")
	}
	if factoryCalls != 1 {
		t.Fatalf("factory called %d times, want 1", factoryCalls)
	}
}

func TestSessionStore_CloseThenGetOrCreate_createsNew(t *testing.T) {
	store := NewSessionStore()
	factoryCalls := 0
	factory := func() (Backend, error) {
		factoryCalls++
		return NewFakeBackend(), nil
	}

	b1, err := store.GetOrCreate("sess-1", factory)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if err := store.Close("sess-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b2, err := store.GetOrCreate("sess-1", factory)
	if err != nil {
		t.Fatalf("GetOrCreate after close: %v", err)
	}
	if b1 == b2 {
		t.Fatal("expected new backend instance after Close")
	}
	if factoryCalls != 2 {
		t.Fatalf("factory called %d times, want 2", factoryCalls)
	}
}

func TestFakeBackend_Navigate_recordsURL(t *testing.T) {
	fb := NewFakeBackend()
	fb.PageSnapshot = Snapshot{URL: "https://example.com", Title: "Example"}

	snap, err := fb.Navigate(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if snap.URL != "https://example.com" {
		t.Fatalf("snapshot URL = %q, want https://example.com", snap.URL)
	}
	if len(fb.NavigatedURLs) != 1 || fb.NavigatedURLs[0] != "https://example.com" {
		t.Fatalf("NavigatedURLs = %#v, want [https://example.com]", fb.NavigatedURLs)
	}
}

func TestSessionStore_CloseAll_closesAll(t *testing.T) {
	store := NewSessionStore()
	fb1 := NewFakeBackend()
	fb2 := NewFakeBackend()

	_, err := store.GetOrCreate("sess-a", func() (Backend, error) { return fb1, nil })
	if err != nil {
		t.Fatalf("GetOrCreate sess-a: %v", err)
	}
	_, err = store.GetOrCreate("sess-b", func() (Backend, error) { return fb2, nil })
	if err != nil {
		t.Fatalf("GetOrCreate sess-b: %v", err)
	}

	if err := store.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if !fb1.Closed() {
		t.Fatal("fb1 not closed")
	}
	if !fb2.Closed() {
		t.Fatal("fb2 not closed")
	}
}

func TestFakeBackend_Healthy_returnsInjectedError(t *testing.T) {
	fb := NewFakeBackend()
	want := errors.New("cdp unavailable")
	fb.HealthyErr = want

	if err := fb.Healthy(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Healthy() = %v, want %v", err, want)
	}
}

func TestFakeBackend_Close_marksClosed(t *testing.T) {
	fb := NewFakeBackend()
	if err := fb.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fb.Closed() {
		t.Fatal("expected closed after Close")
	}
}

func TestFakeBackend_ClickAndType_recordCalls(t *testing.T) {
	fb := NewFakeBackend()
	fb.PageSnapshot = Snapshot{URL: "https://example.com", Refs: map[string]string{"@e1": "button"}}

	if _, err := fb.Click(context.Background(), "@e1"); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if _, err := fb.Type(context.Background(), "@e1", "hello"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if len(fb.ClickedRefs) != 1 || fb.ClickedRefs[0] != "@e1" {
		t.Fatalf("ClickedRefs = %#v, want [@e1]", fb.ClickedRefs)
	}
	if len(fb.TypedInputs) != 1 || fb.TypedInputs[0] != (TypedInput{Ref: "@e1", Text: "hello"}) {
		t.Fatalf("TypedInputs = %#v, want [{@e1 hello}]", fb.TypedInputs)
	}
}
