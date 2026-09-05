package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChatGo_DoesNotStreamMEA(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{"streamWithRulesMEA", "MEAEnabledForAgent", "AgentMEAEnabled"} {
		if strings.Contains(src, needle) {
			t.Errorf("chat.go must not contain %q", needle)
		}
	}
}

func TestWebClientTs_omitsMeaEnabledRuntimeTool(t *testing.T) {
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
	if !strings.Contains(src, "RUNTIME_TOOL_FIELDS") {
		t.Fatal("expected RUNTIME_TOOL_FIELDS in client.ts")
	}
	if strings.Contains(src, "key: 'mea_enabled'") || strings.Contains(src, `key: "mea_enabled"`) {
		t.Fatal("Agent runtime tool fields must not list mea_enabled")
	}
}
