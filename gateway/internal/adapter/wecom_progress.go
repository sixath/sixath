package adapter

import (
	"encoding/json"
	"fmt"
	"time"
)

const hitlNoSurfaceMsg = "hitl required but reply_mode=final has no interactive surface"

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

func (st *ProgressState) ApplySSEEvent(event string, data []byte) {
	switch event {
	case "model_call":
		var payload struct {
			ModelCall struct {
				Step  int    `json:"step"`
				Phase string `json:"phase"`
			} `json:"model_call"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		switch payload.ModelCall.Phase {
		case "invoked":
			if !st.toolInFlight {
				st.Stage = progressStageThinking
			}
		case "responded":
			if _, ok := st.doneModelSteps[payload.ModelCall.Step]; !ok {
				st.doneModelSteps[payload.ModelCall.Step] = struct{}{}
				st.StepsDone++
			}
			if st.Stage != progressStageReply {
				st.Stage = progressStageThinking
			}
		}
	case "tool_call":
		var payload struct {
			ToolCall struct {
				ID       string `json:"id"`
				Phase    string `json:"phase"`
				ToolName string `json:"tool_name"`
			} `json:"tool_call"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		switch payload.ToolCall.Phase {
		case "started":
			st.toolInFlight = true
			st.Stage = progressStageTool
			if payload.ToolCall.ToolName != "" {
				st.ToolName = payload.ToolCall.ToolName
			}
		case "completed":
			if payload.ToolCall.ID != "" {
				if _, ok := st.doneToolIDs[payload.ToolCall.ID]; !ok {
					st.doneToolIDs[payload.ToolCall.ID] = struct{}{}
					st.StepsDone++
				}
			}
			st.toolInFlight = false
			if payload.ToolCall.ToolName != "" {
				st.ToolName = payload.ToolCall.ToolName
			}
		}
	case "chunk":
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		if payload.Content != "" {
			st.Stage = progressStageReply
		}
	case "input_required", "confirm_required":
		st.Failed = true
		st.HITL = true
		st.ErrMsg = hitlNoSurfaceMsg
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return
		}
		st.Failed = true
		st.ErrMsg = payload.Error
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
