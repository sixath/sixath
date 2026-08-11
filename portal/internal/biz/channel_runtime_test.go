package biz

import (
	"testing"
	"time"
)

func TestDeriveRuntimeStatus_NonWecomBot(t *testing.T) {
	ch := &ChannelMeta{Type: "webhook", Enabled: true}
	row := &RuntimeStatusRow{
		State:           "connected",
		LastHeartbeatAt: time.Now(),
	}
	got := DeriveRuntimeStatus(ch, row, time.Now())
	if got != nil {
		t.Fatalf("DeriveRuntimeStatus(webhook) = %#v, want nil", got)
	}
}

func TestDeriveRuntimeStatus_Disabled(t *testing.T) {
	ch := &ChannelMeta{Type: "wecom_bot", Enabled: false}
	row := &RuntimeStatusRow{
		State:           "connected",
		LastHeartbeatAt: time.Now(),
	}
	got := DeriveRuntimeStatus(ch, row, time.Now())
	if got == nil {
		t.Fatal("DeriveRuntimeStatus(disabled) = nil, want disabled view")
	}
	if got.State != "disabled" {
		t.Fatalf("state = %q, want disabled", got.State)
	}
}

func TestDeriveRuntimeStatus_StaleUnknown(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	ch := &ChannelMeta{Type: "wecom_bot", Enabled: true}
	row := &RuntimeStatusRow{
		State:           "connected",
		LastHeartbeatAt: now.Add(-(RuntimeStatusStaleAfter + time.Second)),
	}
	got := DeriveRuntimeStatus(ch, row, now)
	if got == nil {
		t.Fatal("DeriveRuntimeStatus(stale) = nil, want unknown view")
	}
	if got.State != "unknown" {
		t.Fatalf("state = %q, want unknown", got.State)
	}

	gotNilRow := DeriveRuntimeStatus(ch, nil, now)
	if gotNilRow == nil || gotNilRow.State != "unknown" {
		t.Fatalf("DeriveRuntimeStatus(nil row) = %#v, want unknown", gotNilRow)
	}
}

func TestDeriveRuntimeStatus_FreshConnected(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	hb := now.Add(-30 * time.Second)
	ch := &ChannelMeta{Type: "wecom_bot", Enabled: true}
	row := &RuntimeStatusRow{
		ChannelID:         "ch-1",
		State:             "connected",
		LastHeartbeatAt:   hb,
		LastError:         "",
		ReconnectAttempt:  0,
		ReconnectInMs:     0,
		GatewayInstanceID: "gw-1",
	}
	got := DeriveRuntimeStatus(ch, row, now)
	if got == nil {
		t.Fatal("DeriveRuntimeStatus(fresh) = nil, want connected view")
	}
	if got.State != "connected" {
		t.Fatalf("state = %q, want connected", got.State)
	}
	if !got.LastHeartbeatAt.Equal(hb) {
		t.Fatalf("last_heartbeat_at = %v, want %v", got.LastHeartbeatAt, hb)
	}
	if got.GatewayInstanceID != "gw-1" {
		t.Fatalf("gateway_instance_id = %q, want gw-1", got.GatewayInstanceID)
	}
}
