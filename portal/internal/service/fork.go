package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/chat"
	"backend/internal/data"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/sixath/framework/tool"
	"gorm.io/gorm"
)

// ForkResult is returned by POST /sessions/{id}/fork.
type ForkResult struct {
	SessionID         string `json:"session_id"`
	ParentSessionID   string `json:"parent_session_id"`
	Title             string `json:"title"`
	CopiedMessages    int    `json:"copied_messages"`
	CopiedTraces      int    `json:"copied_traces"`
	CopiedMemoryUnits int    `json:"copied_memory_units"`
	CopiedTodos       int    `json:"copied_todos"`
}

var (
	ErrForkMessageNotFound = kratosErrors.NotFound("FORK_MESSAGE_NOT_FOUND", "message not found in session")
	ErrForkInactive        = kratosErrors.BadRequest("FORK_INACTIVE", "message is already inactive")
)

// ForkToMessage copies the active prefix through the anchor into a new child session.
// The parent session stays writable; this path never SoftDeactivateAfter or MarkReadonly.
func (s *ChatService) ForkToMessage(ctx context.Context, sessionID, messageID string) (*ForkResult, error) {
	if s == nil || s.chatUC == nil {
		return nil, fmt.Errorf("chat service unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	messageID = strings.TrimSpace(messageID)
	if sessionID == "" || messageID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "session_id and message_id required")
	}

	sess, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	msg, err := s.chatUC.GetMessageByID(ctx, messageID)
	if err != nil {
		return nil, ErrForkMessageNotFound
	}
	if msg.SessionID != sessionID {
		return nil, ErrForkMessageNotFound
	}
	if !msg.Active {
		return nil, ErrForkInactive
	}

	if s.db == nil {
		return nil, fmt.Errorf("fork: database unavailable")
	}
	var snap data.ForkSnapshotResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		snap, e = data.ForkSnapshot(ctx, tx, data.ForkSnapshotInput{Parent: sess, Anchor: msg})
		return e
	})
	if err != nil {
		if errors.Is(err, data.ErrForkAnchorMissing) || strings.Contains(err.Error(), "anchor not in active prefix") {
			return nil, ErrForkMessageNotFound
		}
		return nil, err
	}
	if snap.Child == nil {
		return nil, fmt.Errorf("fork: child session missing after snapshot")
	}

	copiedTodos := 0
	if items := tool.DefaultTodoStore.Copy(sessionID, snap.Child.ID); items != nil {
		copiedTodos = len(items)
	}
	for _, m := range snap.CopiedMessageRows {
		if m == nil {
			continue
		}
		if m.Role == "user" || m.Role == "assistant" {
			chat.NotifySessionMessageIndexed(ctx, s.chatUC, snap.Child.ID, m)
		}
	}

	return &ForkResult{
		SessionID:         snap.Child.ID,
		ParentSessionID:   snap.Child.ParentSessionID,
		Title:             snap.Child.Title,
		CopiedMessages:    snap.CopiedMessages,
		CopiedTraces:      snap.CopiedTraces,
		CopiedMemoryUnits: snap.CopiedMemoryUnits,
		CopiedTodos:       copiedTodos,
	}, nil
}
