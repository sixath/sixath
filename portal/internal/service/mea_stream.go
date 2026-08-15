package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/chat"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/mea"
	"github.com/sixath/framework/model"
)

func countMEAStatuses(st mea.TaskState) (pending, completed int) {
	for _, r := range st.Records {
		switch r.Status {
		case mea.StatusPending:
			pending++
		case mea.StatusCompleted:
			completed++
		}
	}
	return pending, completed
}

func messagesForMEAContract(base []model.Message, c mea.Contract) []model.Message {
	out := append([]model.Message(nil), base...)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(c.Goal))
	if len(c.AcceptanceChecks) > 0 {
		b.WriteString("\n\n[MEA acceptance — produce environment state that passes these checks]\n")
		for _, ck := range c.AcceptanceChecks {
			fmt.Fprintf(&b, "- type=%s path=%s pattern=%s json_path=%s equals=%s\n",
				ck.Type, ck.Path, ck.Pattern, ck.JSONPath, ck.Equals)
		}
	} else if len(c.Acceptance) > 0 {
		b.WriteString("\n\n[MEA acceptance — satisfy these observable criteria]\n")
		for _, line := range c.Acceptance {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	prompt := b.String()
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" {
			out[i].Content = prompt
			return out
		}
	}
	return append(out, model.Message{Role: "user", Content: prompt})
}

// streamAgentEvents runs one agent episode and forwards events to ch.
// Returns a short summary for MEA ExecutionReport (best-effort from deltas).
func (s *ChatService) streamAgentEvents(
	runCtx context.Context,
	sessionID, agentID, workspace string,
	a agent.Agent,
	req *agent.Request,
	provider *chat.ChatTranscriptProvider,
	ch chan<- ChatStreamEvent,
) (summary string, err error) {
	var summaryBuilder strings.Builder

	ea, ok := a.(agent.EventStreamableAgent)
	if !ok {
		resp, runErr := a.Run(runCtx, req)
		if runErr != nil {
			s.handleStreamRunError(runCtx, sessionID, agentID, provider, ch, runErr)
			return "", runErr
		}
		tr := chat.RunTraceFromMetadata(resp.Metadata)
		s.persistTurnTrace(runCtx, sessionID, agentID, tr)
		s.persistCompactBoundary(runCtx, sessionID, tr)
		s.afterTurnBackgroundReview(runCtx, sessionID, agentID, workspace, resp.Messages, tr)
		for _, event := range streamEventsFromResponse(resp) {
			if event.Type == ChatStreamEventChunk {
				summaryBuilder.WriteString(event.Content)
			}
			ch <- event
		}
		return summaryBuilder.String(), nil
	}

	evCh, runErr := ea.RunEvents(runCtx, req)
	if runErr != nil {
		ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: runErr.Error()}
		return "", runErr
	}
	for ev := range evCh {
		switch ev.Type {
		case agent.StreamEventDelta:
			if ev.Text != "" {
				summaryBuilder.WriteString(ev.Text)
				ch <- ChatStreamEvent{Type: ChatStreamEventChunk, Content: ev.Text}
			}
		case agent.StreamEventToolStarted:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "started")}
			}
		case agent.StreamEventToolCompleted:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "completed")}
			}
		case agent.StreamEventToolFailed, agent.StreamEventPermissionDenied, agent.StreamEventHookBlocked:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "failed")}
			}
		case agent.StreamEventError:
			s.handleStreamRunError(runCtx, sessionID, agentID, provider, ch, errors.New(ev.Error))
			return summaryBuilder.String(), errors.New(ev.Error)
		case agent.StreamEventDone:
			if ev.Trace != nil {
				s.persistTurnTrace(runCtx, sessionID, agentID, ev.Trace)
				s.persistCompactBoundary(runCtx, sessionID, ev.Trace)
				s.afterTurnBackgroundReview(runCtx, sessionID, agentID, workspace, ev.Messages, ev.Trace)
				resp := &agent.Response{Metadata: map[string]any{"trace": ev.Trace}}
				for _, item := range inputRequestsFromResponse(resp) {
					input := item
					ch <- ChatStreamEvent{Type: ChatStreamEventInputRequired, Input: &input}
				}
				for _, item := range confirmationRequestsFromResponse(resp) {
					confirmation := item
					if confirmation.Kind == "skill_manage" {
						store := chat.SkillManagePendingStore()
						if store != nil {
							p, _ := store.GetPending(runCtx, sessionID, confirmation.Token)
							if p == nil {
								continue
							}
						}
					}
					ch <- ChatStreamEvent{Type: ChatStreamEventConfirmRequired, Confirmation: &confirmation}
				}
			}
		}
	}
	return summaryBuilder.String(), nil
}

func (s *ChatService) streamWithRulesMEA(
	runCtx context.Context,
	sessionID, agentID, workspace, goal string,
	checks []mea.AcceptanceCheck,
	acceptance []string,
	agentMEAEnabled bool,
	auditorModel model.Model,
	a agent.Agent,
	baseMessages []model.Message,
	baseMeta map[string]any,
	provider *chat.ChatTranscriptProvider,
	ch chan<- ChatStreamEvent,
) {
	ch <- ChatStreamEvent{Type: ChatStreamEventMEA, MEA: &MEAStreamPayload{
		Phase: "started",
		Goal:  goal,
	}}

	round := 0
	exec := mea.ExecutorFunc(func(ctx context.Context, st mea.TaskState, c mea.Contract) (mea.ExecutionReport, error) {
		round++
		msgs := messagesForMEAContract(baseMessages, c)
		req := &agent.Request{Messages: msgs, Metadata: baseMeta}
		summary, err := s.streamAgentEvents(ctx, sessionID, agentID, workspace, a, req, provider, ch)
		pending, completed := countMEAStatuses(st)
		ch <- ChatStreamEvent{Type: ChatStreamEventMEA, MEA: &MEAStreamPayload{
			Phase:     "round",
			Round:     round,
			Pending:   pending,
			Completed: completed,
			Goal:      c.Goal,
		}}
		if err != nil {
			return mea.ExecutionReport{
				Round:         c.Round,
				Summary:       summary,
				Issues:        []string{err.Error()},
				ClaimComplete: false,
			}, err
		}
		return mea.ExecutionReport{
			Round:         c.Round,
			Summary:       summary,
			ClaimComplete: true,
		}, nil
	})

	res, err := chat.RunRulesMEA(runCtx, chat.RulesMEAInput{
		SessionID:       sessionID,
		AgentID:         agentID,
		AgentMEAEnabled: agentMEAEnabled,
		Goal:            goal,
		WorkDir:         workspace,
		Checks:          checks,
		Acceptance:      acceptance,
		AuditorModel:    auditorModel,
		Executor:        exec,
	})
	if err != nil {
		ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: err.Error()}
		return
	}
	pending, completed := countMEAStatuses(res.State)
	ch <- ChatStreamEvent{Type: ChatStreamEventMEA, MEA: &MEAStreamPayload{
		Phase:     "finished",
		Reason:    res.Reason,
		Pending:   pending,
		Completed: completed,
		Goal:      goal,
	}}
}
