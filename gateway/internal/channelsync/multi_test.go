package channelsync_test

import (
	"context"
	"testing"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/channelsync"
)

func TestMultiReconciler_FansOutSameSnapshots(t *testing.T) {
	a, b := &fakeManager{}, &fakeManager{}
	reg := channel.NewRegistry()
	lister := &fakeLister{chs: []channel.Channel{
		{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"},
		{ID: "wx1", Type: "weixin_ilink", Enabled: true, BotToken: "t", Generation: 1},
	}}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  channelsync.MultiReconciler{a, nil, b},
	})
	if err := r.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	for name, m := range map[string]*fakeManager{"a": a, "b": b} {
		if m.callCount() != 1 {
			t.Fatalf("%s calls=%d want 1", name, m.callCount())
		}
		if len(m.calls[0].next) != 2 || len(m.calls[0].prev) != 0 {
			t.Fatalf("%s call=%+v", name, m.calls[0])
		}
	}
}
