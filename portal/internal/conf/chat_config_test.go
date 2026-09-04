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

func TestLoadChatFromConfigPath_turnToolSurfaceDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
chat:
  public_inbound_enabled: true
  turn_tool_surface_enabled: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	t.Setenv("SATH_CHAT_PUBLIC_INBOUND_ENABLED", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TurnToolSurfaceEnabled == nil || *cfg.TurnToolSurfaceEnabled {
		t.Fatalf("got TurnToolSurfaceEnabled=%v, want false", cfg.TurnToolSurfaceEnabled)
	}
}

func TestLoadChatFromConfigPath_turnToolSurfaceEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
chat:
  turn_tool_surface_enabled: true
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_TURN_TOOL_SURFACE", "0")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TurnToolSurfaceEnabled == nil || *cfg.TurnToolSurfaceEnabled {
		t.Fatalf("env 0 should force false, got %v", cfg.TurnToolSurfaceEnabled)
	}
}

func TestLoadChatFromConfigPath_investigationGatesDefaultOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  http:\n    addr: \":8000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatalf("got %q, want off", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesGarbageOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatalf("got %q", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesOn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "on" {
		t.Fatalf("got %q", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "off")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatal("env must override yaml")
	}
}
