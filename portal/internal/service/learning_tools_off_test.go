package service

import (
	"os"
	"strings"
	"testing"
)

func TestChatGo_DoesNotRegisterLearningTools(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "RegisterLearningTools") {
		t.Fatal("default chat path must not register append_learning")
	}
}

func TestAgentGo_DoesNotRegisterLearningTools(t *testing.T) {
	b, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "RegisterLearningTools") {
		t.Fatal("shortcut chat must not register append_learning")
	}
}
