package chat

import (
	"context"
	"log"
	"os"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/agent"
)

// CompactForkSessionEnabled is the stand-in for compact.fork_session_on_l2 (default false).
func CompactForkSessionEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("SATH_COMPACT_FORK_SESSION")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ForkSessionOnCompactIfEnabled creates a child session after L2 compact when
// SATH_COMPACT_FORK_SESSION is on. Marks the old session readonly and seeds the
// new session with the compact summary. Returns new session id or "".
func ForkSessionOnCompactIfEnabled(ctx context.Context, chatUC *biz.ChatUsecase, oldSessionID string, tr *agent.RunTrace) string {
	if !CompactForkSessionEnabled() || chatUC == nil || tr == nil || strings.TrimSpace(tr.LastL2Summary) == "" {
		return ""
	}
	old, err := chatUC.GetSession(ctx, oldSessionID)
	if err != nil || old == nil {
		if err != nil {
			log.Printf("fork_on_compact get session: %v", err)
		}
		return ""
	}
	title := old.Title
	if title == "" {
		title = "archived"
	}
	title = "fork: " + title
	child, err := chatUC.CreateSession(ctx, old.AgentID, title, oldSessionID)
	if err != nil {
		log.Printf("fork_on_compact create: %v", err)
		return ""
	}
	if err := chatUC.MarkSessionReadonly(ctx, oldSessionID); err != nil {
		log.Printf("fork_on_compact mark readonly: %v", err)
	}
	summary := strings.TrimSpace(tr.LastL2Summary)
	if summary != "" {
		_, _ = chatUC.CreateMessageWithMetadata(ctx, child.ID, "system", summary, map[string]any{
			"sixath.origin": "compact_boundary",
			"forked_from":   oldSessionID,
		})
	}
	return child.ID
}
