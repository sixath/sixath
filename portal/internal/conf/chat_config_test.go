package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadChatFromConfigPath_publicInboundEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
chat:
  public_inbound_enabled: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_CHAT_PUBLIC_INBOUND_ENABLED", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || !cfg.PublicInboundEnabled {
		t.Fatalf("got %#v, want public_inbound_enabled=true", cfg)
	}
}

func TestLoadChatFromConfigPath_defaultFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
server:
  http:
    addr: ":8000"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_CHAT_PUBLIC_INBOUND_ENABLED", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicInboundEnabled {
		t.Fatalf("public_inbound_enabled=%v, want false by default", cfg.PublicInboundEnabled)
	}
}

func TestLoadChatFromConfigPath_envOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
chat:
  public_inbound_enabled: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_CHAT_PUBLIC_INBOUND_ENABLED", "true")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PublicInboundEnabled {
		t.Fatal("env should override yaml to true")
	}
}
