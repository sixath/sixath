package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeFromConfigPath_serviceToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
runtime:
  service_token: "from-yaml-token"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_RUNTIME_TOKEN", "")
	rt, err := LoadRuntimeFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rt == nil || rt.ServiceToken != "from-yaml-token" {
		t.Fatalf("got %#v", rt)
	}
}

func TestLoadRuntimeFromConfigPath_envOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
runtime:
  service_token: "from-yaml-token"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_RUNTIME_TOKEN", "from-env-token")
	rt, err := LoadRuntimeFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rt.ServiceToken != "from-env-token" {
		t.Fatalf("service_token=%q, want from-env-token", rt.ServiceToken)
	}
}

func TestLoadRuntimeFromConfigPath_emptyStaysEmpty(t *testing.T) {
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
	t.Setenv("SATH_RUNTIME_TOKEN", "")
	rt, err := LoadRuntimeFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rt.ServiceToken != "" {
		t.Fatalf("service_token=%q, want empty (no hardcoded default)", rt.ServiceToken)
	}
}
