package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWebClientTs_omitsCodeModelFields(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	clientPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "web", "src", "api", "client.ts"))
	b, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "export interface ModelConfig") {
		t.Fatal("expected ModelConfig in client.ts")
	}
	for _, needle := range []string{"code_provider", "code_model", "code_api_key", "code_base_url"} {
		if strings.Contains(src, needle) {
			t.Errorf("ModelConfig must not map %q", needle)
		}
	}
}
