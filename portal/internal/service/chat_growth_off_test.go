package service

import (
	"os"
	"strings"
	"testing"
)

func TestChatGo_DoesNotCallGrowthReActOptions(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "growthReActOptions") {
		t.Fatal("default chat path must not call growthReActOptions")
	}
}
