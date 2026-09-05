package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBackgroundReviewGo_NoAfterTurnHook(t *testing.T) {
	b, err := os.ReadFile("background_review.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{"afterTurnBackgroundReview", "SetBackgroundReviewer", "func (w *GrowthWorker) SpawnBackgroundReview"} {
		if strings.Contains(s, needle) {
			t.Fatalf("C3 ChatService hook must be removed: %s", needle)
		}
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

func TestProvideGrowthWorkerSource_NilWhenDisabled(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	mainPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "cmd", "backend", "main.go"))
	b, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "if !bool(workerEnabled)") || !strings.Contains(s, "return nil") {
		t.Fatal("provideGrowthWorker must return nil when worker_enabled is false")
	}
}
