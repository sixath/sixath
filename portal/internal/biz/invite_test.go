package biz

import (
	"testing"
	"time"
)

func TestInviteUsable(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	revoked := now.Add(-time.Minute)

	tests := []struct {
		name   string
		invite *OrgInvite
		want   bool
	}{
		{name: "nil invite", invite: nil, want: false},
		{
			name: "valid single use unused",
			invite: &OrgInvite{MaxUses: 1, UsedCount: 0},
			want: true,
		},
		{
			name: "valid unlimited",
			invite: &OrgInvite{MaxUses: 0, UsedCount: 99},
			want: true,
		},
		{
			name: "valid finite remaining",
			invite: &OrgInvite{MaxUses: 3, UsedCount: 2},
			want: true,
		},
		{
			name: "exhausted single use",
			invite: &OrgInvite{MaxUses: 1, UsedCount: 1},
			want: false,
		},
		{
			name: "exhausted finite",
			invite: &OrgInvite{MaxUses: 2, UsedCount: 2},
			want: false,
		},
		{
			name: "revoked",
			invite: &OrgInvite{MaxUses: 0, RevokedAt: &revoked},
			want: false,
		},
		{
			name: "expired",
			invite: &OrgInvite{MaxUses: 0, ExpiresAt: &past},
			want: false,
		},
		{
			name: "expires exactly now",
			invite: &OrgInvite{MaxUses: 0, ExpiresAt: &now},
			want: false,
		},
		{
			name: "not yet expired",
			invite: &OrgInvite{MaxUses: 1, UsedCount: 0, ExpiresAt: &future},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InviteUsable(tt.invite, now); got != tt.want {
				t.Fatalf("InviteUsable() = %v, want %v", got, tt.want)
			}
		})
	}
}
