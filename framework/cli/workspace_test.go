package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixath/framework/config"
)

func TestApplyCLIWorkspace_FillsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cfg := &config.Config{}
	if err := applyCLIWorkspace(cfg); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(dir, ".sath", "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace != want {
		t.Fatalf("workspace = %q want %q", cfg.Workspace, want)
	}
	if st, err := os.Stat(cfg.Workspace); err != nil || !st.IsDir() {
		t.Fatalf("stat: %v", err)
	}
}

func TestInitMainGo_WiresWorkspace(t *testing.T) {
	if !strings.Contains(initMainGo, "EnsureCLIRoot") || !strings.Contains(initMainGo, "WithChatWorkspace") {
		t.Fatal("init main.go template must EnsureCLIRoot and WithChatWorkspace")
	}
}
