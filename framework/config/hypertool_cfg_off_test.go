package config

import (
	"os"
	"strings"
	"testing"
)

func TestConfigGo_omitsHyperTool(t *testing.T) {
	b, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"HyperToolConfig", "HyperTool "} {
		if strings.Contains(src, needle) {
			t.Errorf("config.go must not contain %q", needle)
		}
	}
}
