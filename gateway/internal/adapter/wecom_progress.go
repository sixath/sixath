package adapter

import (
	"fmt"
	"time"
)

const (
	progressStageThinking = "思考中"
	progressStageTool     = "调用工具"
	progressStageReply    = "生成回复"
)

type ProgressState struct {
	StartedAt time.Time
	Stage     string
	ToolName  string
	StepsDone int
	Failed    bool
	HITL      bool
	ErrMsg    string

	doneToolIDs    map[string]struct{}
	doneModelSteps map[int]struct{}
	toolInFlight   bool
}

func NewProgressState(startedAt time.Time) *ProgressState {
	return &ProgressState{
		StartedAt:      startedAt,
		Stage:          progressStageThinking,
		doneToolIDs:    map[string]struct{}{},
		doneModelSteps: map[int]struct{}{},
	}
}

func FormatProgressText(st *ProgressState, now time.Time) string {
	tool := st.ToolName
	if tool == "" {
		tool = "—"
	}
	d := now.Sub(st.StartedAt)
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	mm, ss := sec/60, sec%60
	return fmt.Sprintf("处理中…\n耗时 %02d:%02d\n阶段 %s\n工具 %s\n已完成 %d 步",
		mm, ss, st.Stage, tool, st.StepsDone)
}
