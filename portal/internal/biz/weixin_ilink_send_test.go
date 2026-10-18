package biz

import (
	"context"
	"strings"
	"testing"
)

type sendFakeChannels struct {
	*ilinkFakeChannels
	byAgent map[string][]*ChannelMeta
}

func (f *sendFakeChannels) ListIlinkByAgent(_ context.Context, agentID string) ([]*ChannelMeta, error) {
	return f.byAgent[agentID], nil
}

func newSendUsecaseForTest() *WeixinIlinkUsecase {
	wx1 := &ChannelMeta{ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true}
	wx2 := &ChannelMeta{ID: "uuid-2", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true}
	wx3 := &ChannelMeta{ID: "uuid-3", ChannelID: "wx-3", Type: ChannelTypeWeixinIlink, Enabled: true}
	channels := &sendFakeChannels{
		ilinkFakeChannels: &ilinkFakeChannels{byID: map[string]*ChannelMeta{
			"uuid-1": wx1, "uuid-2": wx2, "uuid-3": wx3,
			"uuid-bot": {ID: "uuid-bot", ChannelID: "bot-1", Type: "wecom_bot", Enabled: true},
		}},
		byAgent: map[string][]*ChannelMeta{"solo": {wx2}, "multi": {wx1, wx2}},
	}
	peers := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{
		peerKey("wx-3", "user:p3"): {ChannelID: "wx-3", PeerID: "user:p3", SessionID: "s-wx3"},
		peerKey("bot-1", "user:u"): {ChannelID: "bot-1", PeerID: "user:u", SessionID: "s-bot"},
	}}
	uc := NewWeixinIlinkUsecaseWith(channels, &ilinkFakeRepo{}, newAuthEphemeralFake(), newQRLogin(), nil)
	uc.SetPeerSessions(peers)
	return uc
}

func TestIlinkResolveSendTarget(t *testing.T) {
	uc := newSendUsecaseForTest()
	cases := []struct {
		agent, session, slug, peer string
		wantChannel, wantPeer      string
		wantErr                    string
	}{
		{"multi", "s-wx3", "", "", "wx-3", "p3", ""},
		{"multi", "s-wx3", "", "px", "wx-3", "px", ""},
		{"multi", "s-wx3", "wx-3", "", "wx-3", "p3", ""},
		{"solo", "", "", " p1 ", "wx-2", "p1", ""},
		{"solo", "s-bot", "", "p1", "wx-2", "p1", ""},
		{"multi", "", "wx-1", "p1", "wx-1", "p1", ""},
		{"multi", "s-wx3", "wx-2", "p1", "wx-2", "p1", ""},
		{"solo", "", "", "", "", "", "peer_id"},
		{"multi", "s-wx3", "wx-2", "", "", "", "peer_id"},
		{"multi", "", "", "p1", "", "", "wx-1、wx-2"},
		{"multi", "", "wx-9", "p1", "", "", "wx-9 不可用"},
		{"nobody", "s-bot", "", "p1", "", "", "未关联"},
	}
	for _, c := range cases {
		got, err := uc.ResolveSendTarget(context.Background(), c.agent, c.session, c.slug, c.peer)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%+v: err = %v, want %q", c, err, c.wantErr)
			}
			continue
		}
		if err != nil || got.Channel.ChannelID != c.wantChannel || got.PeerID != c.wantPeer {
			t.Errorf("%+v: got %+v, %v", c, got, err)
		}
	}
}
