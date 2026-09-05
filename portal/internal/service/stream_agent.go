package service

import (
	"context"
	"errors"
	"strings"

	"backend/internal/chat"

	"github.com/sixath/framework/agent"
)

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
