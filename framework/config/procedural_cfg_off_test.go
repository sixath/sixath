package config

import (
	"os"
	"strings"
	"testing"
)

func TestConfigGo_omitsMemoryProceduralRepair(t *testing.T) {
	b, err := os.ReadFile("tool_guardrails.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"MemoryProceduralRepair", "procedural_repair"} {
		if strings.Contains(src, needle) {
			t.Errorf("config must not define %s", needle)
		}
	}
}
