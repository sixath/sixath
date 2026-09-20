package biz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionWorkspaceDir(t *testing.T) {
	got := SessionWorkspaceDir("/ws", "sess-1")
	want := filepath.Join("/ws", "sessions", "sess-1")
	if got != want {
		t.Fatalf("SessionWorkspaceDir = %q, want %q", got, want)
	}
	if SessionWorkspaceDir("", "sess-1") != "" {
		t.Fatal("expected empty dir when workspace missing")
	}
	if SessionWorkspaceDir("/ws", "") != "" {
		t.Fatal("expected empty dir when session id missing")
	}
}

func TestRemoveSessionWorkspaceDir(t *testing.T) {
	ws := t.TempDir()
	sessionID := "sess-rm-1"
	uploads := filepath.Join(ws, "sessions", sessionID, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	file := filepath.Join(uploads, "att-1_shot.png")
	if err := os.WriteFile(file, []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RemoveSessionWorkspaceDir(ws, sessionID); err != nil {
		t.Fatalf("RemoveSessionWorkspaceDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "sessions", sessionID)); !os.IsNotExist(err) {
		t.Fatalf("session dir still exists: stat err=%v", err)
	}
}
