package biz

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"
)

// OrgInvite is an organization registration invite (never includes plaintext token).
type OrgInvite struct {
	ID        string
	OrgID     string
	CreatedBy string
	MaxUses   int
	UsedCount int
	ExpiresAt *time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// InviteRepo persists org registration invites.
type InviteRepo interface {
	CreateInvite(ctx context.Context, orgID, createdBy string, maxUses int, expiresAt *time.Time) (*OrgInvite, string, error)
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (*OrgInvite, error)
	ListInvitesByOrg(ctx context.Context, orgID string) ([]*OrgInvite, error)
	IncrementInviteUsed(ctx context.Context, id string) error
	RevokeInvite(ctx context.Context, id string) error
}

// GenerateOpaqueToken returns a URL-safe random token suitable for invites and email verify links.
func GenerateOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(raw), "="), nil
}

// InviteUsable reports whether an invite can still accept registrations at now.
func InviteUsable(invite *OrgInvite, now time.Time) bool {
	if invite == nil {
		return false
	}
	if invite.RevokedAt != nil {
		return false
	}
	if invite.ExpiresAt != nil && !invite.ExpiresAt.After(now) {
		return false
	}
	if invite.MaxUses != 0 && invite.UsedCount >= invite.MaxUses {
		return false
	}
	return true
}
