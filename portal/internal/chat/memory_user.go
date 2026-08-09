package chat

import (
	"context"
	"strings"

	"backend/internal/biz"
)

// ResolveMemoryUserID picks the user id for MemoryStore scope=user.
// Prefers the chat session owner; falls back to the authenticated caller.
func ResolveMemoryUserID(ctx context.Context, session *biz.ChatSession) string {
	if session != nil {
		if id := strings.TrimSpace(session.UserID); id != "" {
			return id
		}
	}
	if id, ok := biz.CallerUserID(ctx); ok {
		return strings.TrimSpace(id)
	}
	return ""
}
