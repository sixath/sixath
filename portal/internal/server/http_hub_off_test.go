package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryHubServerFileRemoved(t *testing.T) {
	_, err := os.Stat("memory_hub.go")
	if err == nil {
		t.Fatal("memory_hub.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestHTTP_OmitsHubRoutes(t *testing.T) {
	b, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"/hub/", "memory-hub"} {
		if strings.Contains(src, needle) {
			t.Errorf("http.go must not contain %q", needle)
		}
	}
}

func TestWebAgentFormOmitsHubGovernance(t *testing.T) {
	p := filepath.Join("..", "..", "..", "web", "src", "pages", "AgentForm.tsx")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hub-governance") {
		t.Fatal("AgentForm.tsx must not contain hub-governance")
	}
}

func TestWebAgentDetailOmitsHubGovernance(t *testing.T) {
	p := filepath.Join("..", "..", "..", "web", "src", "pages", "AgentDetail.tsx")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "hub-governance-display") {
		t.Fatal("AgentDetail.tsx must not contain hub-governance-display")
	}
}
