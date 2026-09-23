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
		{"D:", "D:\\", true},
		{"d:\\", "D:\\", true},
		{"D:\\logs", "D:\\logs", true},
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
		{"C:\\", "D:", "cd /d D:\\", "D:\\"},
		{"C:\\Users", "D:\\", "cd /d D:\\", "D:\\"},
		{"D:\\", "cd Program Files", "cd /d D:\\Program Files", "D:\\Program Files"},
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

func TestFormatRunCmdOutput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty success",
			raw:  `{"codeDesc":"","retCode":0}`,
			want: "",
		},
		{
			name: "tasklist table",
			raw:   "{\"codeDesc\":\"\\r\\n映像名称                       PID\\r\\nSystem Idle Process              0\\r\\n\",\"retCode\":0}",
			want:  "映像名称                       PID\nSystem Idle Process              0",
		},
		{
			name: "nonzero exit",
			raw:  `{"codeDesc":"The system cannot find the path specified.","retCode":1}`,
			want: "The system cannot find the path specified.\n[exit 1]",
		},
		{
			name: "plain text passthrough",
			raw:  "hello\nworld",
			want: "hello\nworld",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRunCmdOutput(tt.raw)
			if got != tt.want {
				t.Fatalf("formatRunCmdOutput() = %q, want %q", got, tt.want)
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