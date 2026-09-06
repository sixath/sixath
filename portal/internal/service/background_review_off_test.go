package service

import (
	"os"
	"strings"
	"testing"
)

func TestBackgroundReviewGoRemoved(t *testing.T) {
	_, err := os.Stat("background_review.go")
	if err == nil {
		t.Fatal("background_review.go must not exist")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestChatGo_DoesNotCallAfterTurnBackgroundReview(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "afterTurnBackgroundReview") || strings.Contains(string(b), "bgReviewer") {
		t.Fatal("default chat path must not wire C3 background review")
	}
}
