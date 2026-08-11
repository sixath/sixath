package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
)

type wecomStreamTurnResult struct {
	Content string
	Failed  bool
	ErrMsg  string
}

type sseEvent struct {
	event string
	data  []byte
}

func consumeWecomTurnStream(
	ctx context.Context,
	conn WecomConn,
	reqID, streamID string,
	body io.Reader,
	startedAt time.Time,
	tick time.Duration,
) wecomStreamTurnResult {
	if tick <= 0 {
		tick = 5 * time.Second
	}

	st := NewProgressState(startedAt)
	if err := conn.RespondStream(ctx, reqID, streamID, FormatProgressText(st, time.Now()), false); err != nil {
		log.Printf("wecom progress skeleton: %v", err)
	}

	events := make(chan sseEvent, 16)
	go scanSSEEvents(body, events)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	var full strings.Builder
	streamDone := false
	for !streamDone {
		select {
		case <-ctx.Done():
			streamDone = true
		case <-ticker.C:
			if err := conn.RespondStream(ctx, reqID, streamID, FormatProgressText(st, time.Now()), false); err != nil {
				log.Printf("wecom progress tick: %v", err)
			}
		case ev, ok := <-events:
			if !ok {
				streamDone = true
				break
			}
			if ev.event == "done" {
				streamDone = true
				break
			}
			st.ApplySSEEvent(ev.event, ev.data)
			if ev.event == "chunk" {
				var payload struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal(ev.data, &payload); err == nil && payload.Content != "" {
					full.WriteString(payload.Content)
				}
			}
		}
	}

	content := strings.TrimSpace(full.String())
	res := wecomStreamTurnResult{Content: content}
	if st.Failed {
		res.Failed = true
		res.ErrMsg = st.ErrMsg
		if res.ErrMsg == "" {
			res.ErrMsg = "turn failed"
		}
		return res
	}
	if err := ctx.Err(); err != nil {
		res.Failed = true
		res.ErrMsg = err.Error()
		return res
	}
	if content == "" {
		res.Failed = true
		res.ErrMsg = "turn failed"
		return res
	}
	return res
}

func scanSSEEvents(body io.Reader, out chan<- sseEvent) {
	defer close(out)
	sc := bufio.NewScanner(body)
	// Allow larger SSE data lines (chunk payloads).
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var event string
	var data strings.Builder
	flush := func() {
		if event == "" && data.Len() == 0 {
			return
		}
		out <- sseEvent{event: event, data: []byte(data.String())}
		event = ""
		data.Reset()
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
}

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
	if st.Failed {
		return
	}
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
