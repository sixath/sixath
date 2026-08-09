package service

import (
	"context"
	"fmt"
	"strings"

	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/sixath/framework/config"
	"github.com/sixath/framework/sessionsearch"
)

// RewindResult is returned by POST /sessions/{id}/rewind.
type RewindResult struct {
	SessionID            string   `json:"session_id"`
	RewindCount          int      `json:"rewind_count"`
	DeactivatedMessages  []string `json:"deactivated_messages"`
	DeactivatedTraceReqs []string `json:"deactivated_traces"`
}

var (
	ErrRewindMessageNotFound = kratosErrors.NotFound("REWIND_MESSAGE_NOT_FOUND", "message not found in session")
	ErrRewindInactive        = kratosErrors.BadRequest("REWIND_INACTIVE", "message is already inactive")
)

// RewindToMessage soft-hides the anchor message and all later chat messages,
// deactivates turn_traces from the anchor time, and removes matching FTS rows.
// Does not roll back Skills. Remaining transcript ends before the anchor.
func (s *ChatService) RewindToMessage(ctx context.Context, sessionID, messageID string) (*RewindResult, error) {
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
		return nil, ErrRewindMessageNotFound
	}
	if msg.SessionID != sessionID {
		return nil, ErrRewindMessageNotFound
	}
	if !msg.Active {
		return nil, ErrRewindInactive
	}

	deactivatedMsgs, err := s.chatUC.SoftDeactivateAfter(ctx, sessionID, msg.CreatedAt, messageID)
	if err != nil {
		return nil, err
	}

	var deactivatedTraces []string
	if s.turnTraceStore != nil {
		deactivatedTraces, err = s.turnTraceStore.DeactivateAfter(ctx, sessionID, msg.CreatedAt)
		if err != nil {
			s.log.Warnf("rewind DeactivateAfter traces session_id=%s err=%v", sessionID, err)
			deactivatedTraces = nil
		}
	}

	s.rewindCleanupFTS(ctx, sess.AgentID, sessionID, deactivatedMsgs, deactivatedTraces)

	if err := s.chatUC.BumpRewindCount(ctx, sessionID); err != nil {
		return nil, err
	}
	updated, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &RewindResult{
		SessionID:            sessionID,
		RewindCount:          updated.RewindCount,
		DeactivatedMessages:  deactivatedMsgs,
		DeactivatedTraceReqs: deactivatedTraces,
	}, nil
}

func (s *ChatService) rewindCleanupFTS(ctx context.Context, agentID, sessionID string, messageIDs, requestIDs []string) {
	if agentID == "" {
		return
	}
	cfg := config.Config{SessionSearch: chat.DefaultSessionSearchConfig}
	if !cfg.SessionSearch.Enabled {
		return
	}
	mgr, err := sessionsearch.GetSessionSearchManager(cfg, agentID)
	if err != nil || mgr == nil {
		if err != nil && s.log != nil {
			s.log.Warnf("rewind FTS manager session_id=%s err=%v", sessionID, err)
		}
		return
	}
	if len(messageIDs) > 0 {
		if err := mgr.RemoveMessages(ctx, messageIDs); err != nil && s.log != nil {
			s.log.Warnf("rewind RemoveMessages session_id=%s err=%v", sessionID, err)
		}
	}
	for _, rid := range requestIDs {
		if err := mgr.RemoveTraceProjections(ctx, sessionID, rid); err != nil && s.log != nil {
			s.log.Warnf("rewind RemoveTraceProjections session_id=%s request_id=%s err=%v", sessionID, rid, err)
		}
	}
}
