package service

import (
	"os"
	"strings"
	"testing"
)

func TestChatGo_doesNotWireMemorySidecarsOrToolFamily(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"NotifyMemoryExtractFromTurn",
		"NotifyMemoryGraphFromTurn",
		"BuildToolFamilyIndex",
		"notifyMemoryExtractAfterAssistant",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("default chat path must not contain %q", needle)
		}
	}
}
