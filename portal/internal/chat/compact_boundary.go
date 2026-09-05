package chat

import (
	"context"
	"fmt"
	"log"
	"strings"

	"backend/internal/biz"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

// CompactBoundaryStore is the message surface needed to persist L2 compact
// boundaries. *biz.ChatUsecase satisfies this interface.
type CompactBoundaryStore interface {
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*biz.ChatMessage, error)
	CreateMessageWithMetadata(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*biz.ChatMessage, error)
}

// compactBoundaryListLimit is how many session messages to scan for idempotency.
// ListBySession is oldest-first; a high limit covers typical sessions so a
// just-written boundary is still visible on retry.
const compactBoundaryListLimit = 10000

// PersistCompactBoundaryIfNeeded writes a system message with
// sixath.origin=compact_boundary when tr.LastL2Summary is non-empty.
// Idempotent per (session_id, request_id, origin): skips if one already exists.
// Failures are logged only — never returned to the caller.
func PersistCompactBoundaryIfNeeded(ctx context.Context, store CompactBoundaryStore, sessionID string, tr *agent.RunTrace) {
	if store == nil || tr == nil || sessionID == "" {
		return
	}
	summary := strings.TrimSpace(tr.LastL2Summary)
	if summary == "" {
		return
	}
	requestID := TurnTraceRequestID(ctx, tr)
	if requestID == "" {
		log.Printf("compact_boundary skip: empty request_id session_id=%s", sessionID)
		return
	}
	if compactBoundaryExists(ctx, store, sessionID, requestID) {
		return
	}
	content := summary
	if tr.LastL2MiddleRemoved > 0 {
		content = fmt.Sprintf("%s\n\n[middle_removed=%d]", summary, tr.LastL2MiddleRemoved)
	}
	meta := map[string]any{
		model.MetadataKeySixathOrigin: model.OriginCompactBoundary,
		"middle_removed":              tr.LastL2MiddleRemoved,
		"request_id":                  requestID,
	}
	if _, err := store.CreateMessageWithMetadata(ctx, sessionID, "system", content, meta); err != nil {
		log.Printf("compact_boundary create failed: session_id=%s request_id=%s err=%v", sessionID, requestID, err)
	}
}

func compactBoundaryExists(ctx context.Context, store CompactBoundaryStore, sessionID, requestID string) bool {
	msgs, err := store.ListMessages(ctx, sessionID, compactBoundaryListLimit)
	if err != nil {
		log.Printf("compact_boundary list failed: session_id=%s err=%v", sessionID, err)
		return false
	}
	for _, m := range msgs {
		if isCompactBoundaryForRequest(m, requestID) {
			return true
		}
	}
	return false
}

func isCompactBoundaryForRequest(m *biz.ChatMessage, requestID string) bool {
	if m == nil || m.Role != "system" || m.Metadata == nil || requestID == "" {
		return false
	}
	origin, _ := m.Metadata[model.MetadataKeySixathOrigin].(string)
	if origin != model.OriginCompactBoundary {
		return false
	}
	rid, _ := m.Metadata["request_id"].(string)
	return rid == requestID
}
