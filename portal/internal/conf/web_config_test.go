package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWebToolsFromConfigPath_growthLLMWeb(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `
growth:
  llm:
    provider: openai
    model: test
    web:
      search_backend: bocha
      bocha_api_key: "sk-test-bocha"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := LoadWebToolsFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if wt == nil || wt.BochaAPIKey != "sk-test-bocha" {
		t.Fatalf("got %#v", wt)
	}
	if wt.SearchBackend != "bocha" {
		t.Fatalf("backend=%q", wt.SearchBackend)
	}
}
