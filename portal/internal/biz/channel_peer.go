package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ChannelPeerSession is the domain mapping of channel+peer → chat session.
type ChannelPeerSession struct {
	ChannelID string
	PeerID    string
	SessionID string
	AgentID   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChannelPeerSessionRepo persists channel+peer → session mappings.
type ChannelPeerSessionRepo interface {
	Get(ctx context.Context, channelID, peerID string) (*ChannelPeerSession, error)
	Create(ctx context.Context, row *ChannelPeerSession) error
}

// ChannelPeerResolveResult is the outcome of Resolve.
type ChannelPeerResolveResult struct {
	SessionID string
	AgentID   string
	Created   bool
}

// ChannelPeerUsecase resolves webhook/IM peers onto Portal chat sessions.
type ChannelPeerUsecase struct {
	peerRepo    ChannelPeerSessionRepo
	sessionRepo ChatSessionRepo
}

// NewChannelPeerUsecase creates a ChannelPeerUsecase.
func NewChannelPeerUsecase(peerRepo ChannelPeerSessionRepo, sessionRepo ChatSessionRepo) *ChannelPeerUsecase {
	return &ChannelPeerUsecase{peerRepo: peerRepo, sessionRepo: sessionRepo}
}

// Resolve returns the session for channel_id+peer_id.
// If a mapping exists, it is returned as-is (new agentID is ignored).
// Otherwise a chat session is created and the mapping is inserted.
func (uc *ChannelPeerUsecase) Resolve(ctx context.Context, channelID, peerID, agentID string) (*ChannelPeerResolveResult, error) {
	channelID = strings.TrimSpace(channelID)
	peerID = strings.TrimSpace(peerID)
	agentID = strings.TrimSpace(agentID)
	if channelID == "" || peerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel_id and peer_id are required")
	}
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id is required")
	}

	existing, err := uc.peerRepo.Get(ctx, channelID, peerID)
	if err == nil && existing != nil {
		return &ChannelPeerResolveResult{
			SessionID: existing.SessionID,
			AgentID:   existing.AgentID,
			Created:   false,
		}, nil
	}
	if err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, err
	}

	title := fmt.Sprintf("channel:%s peer:%s", channelID, peerID)
	session, err := uc.sessionRepo.Create(ctx, PeerUserID(channelID, peerID), agentID, title, "")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	row := &ChannelPeerSession{
		ChannelID: channelID,
		PeerID:    peerID,
		SessionID: session.ID,
		AgentID:   agentID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := uc.peerRepo.Create(ctx, row); err != nil {
		if !errors.Is(err, pkgErrors.ErrConflict) {
			return nil, err
		}
		// Another Resolve won the race; return the persisted mapping.
		won, getErr := uc.peerRepo.Get(ctx, channelID, peerID)
		if getErr != nil {
			return nil, getErr
		}
		if won.SessionID != session.ID {
			_ = uc.sessionRepo.Delete(ctx, session.ID) // best-effort orphan cleanup
		}
		return &ChannelPeerResolveResult{
			SessionID: won.SessionID,
			AgentID:   won.AgentID,
			Created:   false,
		}, nil
	}

	return &ChannelPeerResolveResult{
		SessionID: session.ID,
		AgentID:   agentID,
		Created:   true,
	}, nil
}

// PeerUserID derives a stable chat_sessions.user_id (≤36 chars) for webhook peers.
// Full peer_id remains in channel_peer_sessions; this only satisfies the column width.
func PeerUserID(channelID, peerID string) string {
	sum := sha256.Sum256([]byte(channelID + "\x00" + peerID))
	return "p" + hex.EncodeToString(sum[:])[:35]
}
