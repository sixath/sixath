package biz

import (
	"strings"
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

func TestDeriveRuntimeStatus_WeixinIlinkWithoutToken(t *testing.T) {
	now := time.Now()
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true}
	got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now)
	if got == nil || got.State != "login_required" || !strings.Contains(got.LastError, "未登录") {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkExpiredBeatsFreshHeartbeat(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Minute)
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkSessionExpiredAt: &expired}
	got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now)
	if got == nil || got.State != "login_required" || !strings.Contains(got.LastError, "过期") {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkDisabledFirst(t *testing.T) {
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: false}
	got := DeriveRuntimeStatus(ch, nil, time.Now())
	if got == nil || got.State != "disabled" {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkConnectedAndStale(t *testing.T) {
	now := time.Now()
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt"}
	if got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now); got == nil || got.State != "connected" {
		t.Fatalf("fresh = %#v", got)
	}
	stale := &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now.Add(-(RuntimeStatusStaleAfter + time.Second))}
	if got := DeriveRuntimeStatus(ch, stale, now); got == nil || got.State != "unknown" {
		t.Fatalf("stale = %#v", got)
	}
}
