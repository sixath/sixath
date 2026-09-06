package toolskill

import (
	"testing"
	"time"
)

func TestRuntimeWriteLease_Contention(t *testing.T) {
	l := NewRuntimeWriteLease()
	ws := "/tmp/ws"
	ok, _ := l.TryAcquire(ws, "holder-a", time.Minute)
	if !ok {
		t.Fatal("expected first acquire")
	}
	ok, retry := l.TryAcquire(ws, "holder-b", time.Minute)
	if ok {
		t.Fatal("expected second acquire to fail")
	}
	if retry < 1 {
		t.Fatalf("expected retry_after >= 1, got %d", retry)
	}
	l.Release(ws, "holder-a")
	ok, _ = l.TryAcquire(ws, "holder-b", time.Minute)
	if !ok {
		t.Fatal("expected acquire after release")
	}
}
