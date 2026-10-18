package service

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

type fakeWeixinSender struct {
	byAgent map[string][]*biz.ChannelMeta
}

func (f *fakeWeixinSender) ChannelsForAgent(_ context.Context, agentID string) ([]*biz.ChannelMeta, error) {
	return f.byAgent[agentID], nil
}

func (f *fakeWeixinSender) ResolveSendTarget(context.Context, string, string, string, string) (*biz.IlinkSendTarget, error) {
	return nil, nil
}

func (f *fakeWeixinSender) Push(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

func TestRegisterWeixinToolForAgent(t *testing.T) {
	sender := &fakeWeixinSender{byAgent: map[string][]*biz.ChannelMeta{
		"a1": {{ID: "u1", ChannelID: "wx-1"}, {ID: "u2", ChannelID: "wx-2"}},
	}}

	reg := tool.NewRegistry()
	bindings := registerWeixinToolForAgent(context.Background(), sender, nil, reg, &biz.AgentMeta{ID: "a1"})
	if _, ok := reg.Get("send_to_weixin"); !ok {
		t.Fatal("send_to_weixin not registered")
	}
	if len(bindings) != 1 || bindings[0].ChannelID != "wx-1,wx-2" || bindings[0].ChannelType != biz.ChannelTypeWeixinIlink {
		t.Fatalf("bindings = %+v", bindings)
	}

	reg = tool.NewRegistry()
	if b := registerWeixinToolForAgent(context.Background(), sender, nil, reg, &biz.AgentMeta{ID: "a2"}); b != nil {
		t.Fatalf("bindings without channels = %+v", b)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered for an agent without weixin_ilink channels")
	}
	if b := registerWeixinToolForAgent(context.Background(), nil, nil, reg, &biz.AgentMeta{ID: "a1"}); b != nil {
		t.Fatalf("bindings without sender = %+v", b)
	}
}
