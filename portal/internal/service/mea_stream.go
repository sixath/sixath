package service

import (
	"context"
	"errors"
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
	if prompt := chat.MEAAcceptancePrompt(c.AcceptanceChecks, c.Acceptance); prompt != "" {
		b.WriteString("\n\n")
		b.WriteString(prompt)
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

type streamEpisode struct {
	Summary   string
	FinalText string
	Trace     *agent.RunTrace
}

// streamAgentEvents runs one agent episode and forwards events to ch.
// Returns episode (FinalText from Done.Text / last assistant / resp.Text, not deltas).
func (s *ChatService) streamAgentEvents(
	runCtx context.Context,
	sessionID, agentID, workspace string,
	a agent.Agent,
	req *agent.Request,
	provider *chat.ChatTranscriptProvider,
	ch chan<- ChatStreamEvent,
) (streamEpisode, error) {
	var summaryBuilder strings.Builder
	var ep streamEpisode

	ea, ok := a.(agent.EventStreamableAgent)
	if !ok {
		resp, runErr := a.Run(runCtx, req)
		if runErr != nil {
			s.handleStreamRunError(runCtx, sessionID, agentID, provider, ch, runErr)
			return streamEpisode{}, runErr
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
		return streamEpisode{
			Summary:   summaryBuilder.String(),
			FinalText: strings.TrimSpace(resp.Text),
			Trace:     tr,
		}, nil
	}

	evCh, runErr := ea.RunEvents(runCtx, req)
	if runErr != nil {
		ch <- ChatStreamEvent{Type: ChatStreamEventError, Error: runErr.Error()}
		return streamEpisode{}, runErr
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
				// Surface ask_user cards as soon as the tool returns pending (before turn Done).
				if item := inputRequestFromToolRecord(*ev.ToolCall); item != nil {
					input := *item
					ch <- ChatStreamEvent{Type: ChatStreamEventInputRequired, Input: &input}
				}
			}
		case agent.StreamEventToolFailed, agent.StreamEventPermissionDenied, agent.StreamEventHookBlocked:
			if ev.ToolCall != nil {
				ch <- ChatStreamEvent{Type: ChatStreamEventToolCall, ToolCall: toolCallPayloadFromRecord(*ev.ToolCall, "failed")}
			}
		case agent.StreamEventError:
			s.handleStreamRunError(runCtx, sessionID, agentID, provider, ch, errors.New(ev.Error))
			ep.Summary = summaryBuilder.String()
			if ev.Trace != nil {
				ep.Trace = ev.Trace
			}
			return ep, errors.New(ev.Error)
		case agent.StreamEventDone:
			ep.FinalText = chat.FinalTextFromDone(ev.Text, ev.Messages)
			ep.Trace = ev.Trace
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
	ep.Summary = summaryBuilder.String()
	return ep, nil
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
		ep, err := s.streamAgentEvents(ctx, sessionID, agentID, workspace, a, req, provider, ch)
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
				Summary:       ep.Summary,
				Issues:        []string{err.Error()},
				ClaimComplete: false,
				FinalText:     ep.FinalText,
				ToolHits:      chat.ToolHitsFromTrace(ep.Trace),
			}, err
		}
		return mea.ExecutionReport{
			Round:         c.Round,
			Summary:       ep.Summary,
			ClaimComplete: true,
			FinalText:     ep.FinalText,
			ToolHits:      chat.ToolHitsFromTrace(ep.Trace),
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
