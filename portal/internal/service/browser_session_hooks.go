package service

import (
	"context"

	"backend/internal/chat"

	agent "github.com/sixath/framework/harness"
)

// registerBrowserSessionHooks closes the process-level browser SessionStore entry
// when a chat session ends (DeleteSession → ChatSessionHooks).
// Close errors are Warn-only and do not fail the hook chain.
func (s *ChatService) registerBrowserSessionHooks() {
	if s == nil || s.sessionHooks == nil {
		return
	}
	s.sessionHooks.Register(agent.ChatSessionHookFunc(func(_ context.Context, sessionID string) error {
		if err := chat.BrowserSessionStore().Close(sessionID); err != nil && s.log != nil {
			s.log.Warnf("browser session close: session_id=%s err=%v", sessionID, err)
		}
		return nil
	}))
}
