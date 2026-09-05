package service

import (
	"context"

	"backend/internal/chat"

	agent "github.com/sixath/framework/harness"
)

// registerProcessSessionHooks kills background terminal processes for the chat session.
func (s *ChatService) registerProcessSessionHooks() {
	if s == nil || s.sessionHooks == nil {
		return
	}
	s.sessionHooks.Register(agent.ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		chat.ProcessRegistryForHooks().KillChatSession(sessionID)
		return nil
	}))
}
