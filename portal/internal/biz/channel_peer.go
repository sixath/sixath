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

var (
	ErrAgentNotAllowed = kratosErrors.Forbidden("AGENT_NOT_ALLOWED", "agent not allowed for channel")
	ErrAgentBound      = kratosErrors.Conflict("AGENT_BOUND", "peer already bound to another agent; use force_new")
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
	Upsert(ctx context.Context, row *ChannelPeerSession) error
	Delete(ctx context.Context, channelID, peerID string) error
}

// ChannelPeerResolveInput is the input for Resolve.
type ChannelPeerResolveInput struct {
	ChannelID string
	PeerID    string
	AgentID   string // optional; empty uses channel.DefaultAgent
	ForceNew  bool
	Reason    string
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
	channelRepo ChannelRepo
}

// NewChannelPeerUsecase creates a ChannelPeerUsecase.
func NewChannelPeerUsecase(peerRepo ChannelPeerSessionRepo, sessionRepo ChatSessionRepo, channelRepo ChannelRepo) *ChannelPeerUsecase {
	return &ChannelPeerUsecase{peerRepo: peerRepo, sessionRepo: sessionRepo, channelRepo: channelRepo}
}

// Resolve returns the session for channel_id+peer_id using the agent allowlist / force_new decision table.
func (uc *ChannelPeerUsecase) Resolve(ctx context.Context, in ChannelPeerResolveInput) (*ChannelPeerResolveResult, error) {
	channelID := strings.TrimSpace(in.ChannelID)
	peerID := strings.TrimSpace(in.PeerID)
	if channelID == "" || peerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel_id and peer_id are required")
	}

	ch, err := uc.channelRepo.GetByChannelID(ctx, channelID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, err
	}

	agentID := strings.TrimSpace(in.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(ch.DefaultAgent)
	}
	if agentID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id is required")
	}
	if !agentAllowed(ch, agentID) {
		return nil, ErrAgentNotAllowed
	}

	existing, err := uc.peerRepo.Get(ctx, channelID, peerID)
	if err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, err
	}
	if err == nil && existing != nil && !in.ForceNew {
		if existing.AgentID == agentID {
			return &ChannelPeerResolveResult{
				SessionID: existing.SessionID,
				AgentID:   existing.AgentID,
				Created:   false,
			}, nil
		}
		return nil, ErrAgentBound
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

	if existing != nil {
		if err := uc.peerRepo.Upsert(ctx, row); err != nil {
			return nil, err
		}
		return &ChannelPeerResolveResult{
			SessionID: session.ID,
			AgentID:   agentID,
			Created:   true,
		}, nil
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

// DeleteBinding removes the channel+peer session mapping (does not delete the chat session).
func (uc *ChannelPeerUsecase) DeleteBinding(ctx context.Context, channelID, peerID string) error {
	return uc.peerRepo.Delete(ctx, strings.TrimSpace(channelID), strings.TrimSpace(peerID))
}

// GetBinding returns the existing channel+peer mapping without creating one.
func (uc *ChannelPeerUsecase) GetBinding(ctx context.Context, channelID, peerID string) (*ChannelPeerSession, error) {
	channelID = strings.TrimSpace(channelID)
	peerID = strings.TrimSpace(peerID)
	if channelID == "" || peerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel_id and peer_id are required")
	}
	row, err := uc.peerRepo.Get(ctx, channelID, peerID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, pkgErrors.ErrNotFound
		}
		return nil, err
	}
	return row, nil
}

func agentAllowed(ch *ChannelMeta, agentID string) bool {
	if len(ch.AllowedAgents) == 0 {
		return agentID == ch.DefaultAgent
	}
	for _, id := range ch.AllowedAgents {
		if id == agentID {
			return true
		}
	}
	return false
}

// PeerUserID derives a stable chat_sessions.user_id (≤36 chars) for webhook peers.
// Full peer_id remains in channel_peer_sessions; this only satisfies the column width.
func PeerUserID(channelID, peerID string) string {
	sum := sha256.Sum256([]byte(channelID + "\x00" + peerID))
	return "p" + hex.EncodeToString(sum[:])[:35]
}
