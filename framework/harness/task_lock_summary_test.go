package harness

import (
	"strings"
	"testing"
)

func TestAppendTaskLockToSummaryPrompt(t *testing.T) {
	got := AnswerOriginalQuestionPrompt("原问题XYZ")
	if !strings.Contains(got, "原问题XYZ") {
		t.Fatal(got)
	}
	if !strings.Contains(got, ForcedFinalSummaryPrompt) {
		t.Fatal("shared generator must include forced-summary constraints")
	}
}
