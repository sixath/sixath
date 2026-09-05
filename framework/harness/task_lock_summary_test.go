package harness

import (
	"strings"
	"testing"
)

func TestAnswerOriginalQuestionPrompt_noTaskLock(t *testing.T) {
	got := AnswerOriginalQuestionPrompt()
	if got != ForcedFinalSummaryPrompt {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "【本轮任务锁】") {
		t.Fatal("forced summary must not inject task lock")
	}
}
