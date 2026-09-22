package terminal

import (
	"testing"
	"time"
)

func TestParseCDCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantDir  string
		wantIsCD bool
	}{
		{"cd D:\\logs", "D:\\logs", true},
		{"cd /d D:\\logs", "D:\\logs", true},
		{"cd C:\\Users", "C:\\Users", true},
		{"dir", "", false},
		{"type file.txt", "", false},
		{"cd", "", true},
		{"  cd  D:\\data  ", "D:\\data", true},
		{"CD D:\\LOGS", "D:\\LOGS", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotDir, gotIsCD := parseCDCommand(tt.input)
			if gotDir != tt.wantDir || gotIsCD != tt.wantIsCD {
				t.Fatalf("parseCDCommand(%q) = (%q, %v), want (%q, %v)", tt.input, gotDir, gotIsCD, tt.wantDir, tt.wantIsCD)
			}
		})
	}
}

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		workDir string
		input   string
		want    string
		wantDir string
	}{
		{"D:\\logs", "dir", "cd /d D:\\logs && dir", "D:\\logs"},
		{"C:\\", "type file.txt", "cd /d C:\\ && type file.txt", "C:\\"},
		{"D:\\logs", "cd D:\\data", "cd /d D:\\data", "D:\\data"},
		{"D:\\logs", "cd /d E:\\work", "cd /d E:\\work", "E:\\work"},
		{"C:\\", "cd", "cd /d C:\\", "C:\\"},
		{"D:\\a", "cd b", "cd /d D:\\a\\b", "D:\\a\\b"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, gotDir := buildCommand(tt.workDir, tt.input)
			if got != tt.want || gotDir != tt.wantDir {
				t.Fatalf("buildCommand(%q, %q) = (%q, %q), want (%q, %q)", tt.workDir, tt.input, got, gotDir, tt.want, tt.wantDir)
			}
		})
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	mgr := NewManager(nil, 0)
	mgr.sessions["s1"] = &Session{ID: "s1", LastUsed: time.Now().Add(-1 * time.Hour)}
	mgr.sessions["s2"] = &Session{ID: "s2", LastUsed: time.Now()}
	mgr.cleanupExpired()
	if _, ok := mgr.sessions["s2"]; ok {
		t.Fatal("s2 should not be cleaned up since TTL=0 means immediate expiry")
	}
}