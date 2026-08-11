package adapter // same package as wecom_progress.go (white-box)

import (
	"strings"
	"testing"
	"time"
)

func TestFormatProgressText_Skeleton(t *testing.T) {
	st := NewProgressState(time.Unix(0, 0))
	got := FormatProgressText(st, time.Unix(0, 0))
	for _, want := range []string{"处理中…", "耗时 00:00", "阶段 思考中", "工具 —", "已完成 0 步"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestFormatProgressText_ElapsedAndFields(t *testing.T) {
	st := NewProgressState(time.Unix(100, 0))
	st.Stage = "调用工具"
	st.ToolName = "kubectl_logs"
	st.StepsDone = 2
	got := FormatProgressText(st, time.Unix(142, 0))
	if !strings.Contains(got, "耗时 00:42") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "工具 kubectl_logs") || !strings.Contains(got, "已完成 2 步") {
		t.Fatalf("got %q", got)
	}
}
