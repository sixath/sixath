package service

import (
	"os"
	"strings"
	"testing"
)

func TestChatGo_DoesNotHoldGrowthUC(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "growthUC") {
		t.Fatal("ChatService must not inject unused GrowthUsecase")
	}
}
