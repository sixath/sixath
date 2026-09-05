package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"backend/internal/biz"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/model"
)

type recordingCompactStore struct {
	mu       sync.Mutex
	messages []*biz.ChatMessage
	creates  int
}

func (r *recordingCompactStore) ListMessages(ctx context.Context, sessionID string, limit int) ([]*biz.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*biz.ChatMessage, 0, len(r.messages))
	for _, m := range r.messages {
		if m.SessionID == sessionID {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *recordingCompactStore) CreateMessageWithMetadata(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*biz.ChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	msg := &biz.ChatMessage{
		ID:        "msg-" + string(rune('a'+r.creates-1)),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Metadata:  metadata,
	}
	r.messages = append(r.messages, msg)
	return msg, nil
}

func TestPersistCompactBoundaryIfNeeded_IdempotentSameRequestID(t *testing.T) {
	store := &recordingCompactStore{}
	tr := &agent.RunTrace{
		RequestID:           "req-compact-1",
		LastL2Summary:       "conversation compressed summary",
		LastL2MiddleRemoved: 4,
	}
	ctx := context.Background()
	PersistCompactBoundaryIfNeeded(ctx, store, "sess-1", tr)
	PersistCompactBoundaryIfNeeded(ctx, store, "sess-1", tr)

	if store.creates != 1 {
		t.Fatalf("creates=%d want 1 (idempotent)", store.creates)
	}
	if len(store.messages) != 1 {
		t.Fatalf("messages=%d want 1", len(store.messages))
	}
	msg := store.messages[0]
	if msg.Role != "system" {
		t.Fatalf("role=%q want system", msg.Role)
	}
	if msg.Metadata[model.MetadataKeySixathOrigin] != model.OriginCompactBoundary {
		t.Fatalf("origin=%v want %q", msg.Metadata[model.MetadataKeySixathOrigin], model.OriginCompactBoundary)
	}
	if msg.Metadata["request_id"] != "req-compact-1" {
		t.Fatalf("request_id=%v", msg.Metadata["request_id"])
	}
	if msg.Metadata["middle_removed"] != 4 {
		t.Fatalf("middle_removed=%v want 4", msg.Metadata["middle_removed"])
	}
	if !strings.Contains(msg.Content, "conversation compressed summary") {
		t.Fatalf("content=%q missing summary", msg.Content)
	}
	if !strings.Contains(msg.Content, "middle_removed=4") {
		t.Fatalf("content=%q missing middle_removed note", msg.Content)
	}
}

func TestPersistCompactBoundaryIfNeeded_EmptySummaryNoOp(t *testing.T) {
	store := &recordingCompactStore{}
	PersistCompactBoundaryIfNeeded(context.Background(), store, "sess-1", &agent.RunTrace{
		RequestID: "r1",
	})
	if store.creates != 0 {
		t.Fatalf("creates=%d want 0", store.creates)
	}
}

func TestPersistCompactBoundaryIfNeeded_DifferentRequestIDs(t *testing.T) {
	store := &recordingCompactStore{}
	ctx := context.Background()
	PersistCompactBoundaryIfNeeded(ctx, store, "sess-1", &agent.RunTrace{
		RequestID:     "r1",
		LastL2Summary: "s1",
	})
	PersistCompactBoundaryIfNeeded(ctx, store, "sess-1", &agent.RunTrace{
		RequestID:     "r2",
		LastL2Summary: "s2",
	})
	if store.creates != 2 {
		t.Fatalf("creates=%d want 2", store.creates)
	}
}

func TestPersistCompactBoundaryIfNeeded_NilSafe(t *testing.T) {
	PersistCompactBoundaryIfNeeded(context.Background(), nil, "s", &agent.RunTrace{LastL2Summary: "x", RequestID: "r"})
	store := &recordingCompactStore{}
	PersistCompactBoundaryIfNeeded(context.Background(), store, "s", nil)
	if store.creates != 0 {
		t.Fatalf("creates=%d want 0", store.creates)
	}
}
