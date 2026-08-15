package mea

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	st := NewFileStore(dir)
	s := TaskState{Version: 1, SessionID: "abc", Goal: "g", UpdatedAt: time.Now().UTC()}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load("abc")
	if err != nil || got.Goal != "g" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "abc.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFileStore_RejectUnsafeSessionID(t *testing.T) {
	st := NewFileStore(t.TempDir())
	err := st.Save(TaskState{SessionID: "../x", Version: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileStore_LoadMissing_ErrNotFound(t *testing.T) {
	st := NewFileStore(t.TempDir())
	_, err := st.Load("missing-session")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
