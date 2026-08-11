package biz

import (
	"context"
	"time"
)

// RuntimeStatusStaleAfter is the heartbeat freshness window for derived status.
const RuntimeStatusStaleAfter = 90 * time.Second

// RuntimeStatusRow is the persisted gateway-reported status for a channel.
type RuntimeStatusRow struct {
	ChannelID         string
	State             string
	LastHeartbeatAt   time.Time
	LastError         string
	ReconnectAttempt  int
	ReconnectInMs     int
	GatewayInstanceID string
	UpdatedAt         time.Time
}

// RuntimeStatusView is the Admin-facing derived runtime status.
type RuntimeStatusView struct {
	State             string
	LastHeartbeatAt   time.Time
	LastError         string
	ReconnectAttempt  int
	ReconnectInMs     int
	GatewayInstanceID string
}

// RuntimeStatusPatch is a partial update for ChannelRuntimeRepo.Upsert.
// Pointer fields: nil = preserve existing; non-nil = overwrite.
type RuntimeStatusPatch struct {
	State             string // required
	LastError         *string
	ReconnectAttempt  *int
	ReconnectInMs     *int
	GatewayInstanceID *string
}

// ChannelRuntimeRepo persists gateway channel runtime status.
type ChannelRuntimeRepo interface {
	Get(ctx context.Context, channelID string) (*RuntimeStatusRow, error)
	Upsert(ctx context.Context, channelID string, patch RuntimeStatusPatch) error
}

// DeriveRuntimeStatus applies spec §3.5 read-path derivation.
func DeriveRuntimeStatus(ch *ChannelMeta, row *RuntimeStatusRow, now time.Time) *RuntimeStatusView {
	if ch == nil || ch.Type != "wecom_bot" {
		return nil
	}
	if !ch.Enabled {
		return &RuntimeStatusView{State: "disabled"}
	}
	if row == nil || now.Sub(row.LastHeartbeatAt) > RuntimeStatusStaleAfter {
		v := &RuntimeStatusView{State: "unknown"}
		if row != nil {
			v.LastHeartbeatAt = row.LastHeartbeatAt
			v.LastError = row.LastError
			v.ReconnectAttempt = row.ReconnectAttempt
			v.ReconnectInMs = row.ReconnectInMs
			v.GatewayInstanceID = row.GatewayInstanceID
		}
		return v
	}
	return &RuntimeStatusView{
		State:             row.State,
		LastHeartbeatAt:   row.LastHeartbeatAt,
		LastError:         row.LastError,
		ReconnectAttempt:  row.ReconnectAttempt,
		ReconnectInMs:     row.ReconnectInMs,
		GatewayInstanceID: row.GatewayInstanceID,
	}
}
