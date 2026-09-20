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
	for _, needle := range []string{"请重新说明", "请重问", "禁止要求用户重述"} {
		if needle == "禁止要求用户重述" {
			if !strings.Contains(got, needle) {
				t.Fatalf("forced summary must forbid restating the question, missing %q in %q", needle, got)
			}
			continue
		}
		if !strings.Contains(got, needle) {
			t.Fatalf("forced summary must explicitly ban %q, got %q", needle, got)
		}
	}
}

func TestAnswerOriginalQuestionPromptWithGoal_embedsQuestion(t *testing.T) {
	q := "为什么这个vmid=199306 上线的时候报操作不合法，需重启实例后才允许上线"
	got := AnswerOriginalQuestionPromptWithGoal(q)
	if !strings.Contains(got, q) {
		t.Fatalf("must embed original question, got %q", got)
	}
	if !strings.Contains(got, "【本轮原始问题】") {
		t.Fatalf("must label original question, got %q", got)
	}
	if strings.Contains(got, "【本轮任务锁】") {
		t.Fatal("must not inject task lock")
	}
}
