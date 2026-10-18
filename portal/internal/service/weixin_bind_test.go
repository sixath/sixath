package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

type fakeWeixinSender struct {
	byAgent map[string][]*biz.ChannelMeta
	err     error
}

func (f *fakeWeixinSender) ChannelsForAgent(_ context.Context, agentID string) ([]*biz.ChannelMeta, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byAgent[agentID], nil
}

func (f *fakeWeixinSender) ResolveSendTarget(context.Context, string, string, string, string) (*biz.IlinkSendTarget, error) {
	return nil, nil
}

func (f *fakeWeixinSender) Push(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

type captureLogger struct{ lines []string }

func (l *captureLogger) Log(level log.Level, keyvals ...any) error {
	l.lines = append(l.lines, level.String()+" "+fmt.Sprint(keyvals...))
	return nil
}

func TestRegisterWeixinToolForAgent(t *testing.T) {
	sender := &fakeWeixinSender{byAgent: map[string][]*biz.ChannelMeta{
		"a1": {{ID: "u1", ChannelID: "wx-1"}, {ID: "u2", ChannelID: "wx-2"}},
	}}

	reg := tool.NewRegistry()
	bindings := registerWeixinToolForAgent(context.Background(), nil, sender, nil, reg, &biz.AgentMeta{ID: "a1"})
	if _, ok := reg.Get("send_to_weixin"); !ok {
		t.Fatal("send_to_weixin not registered")
	}
	if len(bindings) != 1 || bindings[0].ChannelID != "wx-1,wx-2" || bindings[0].ChannelType != biz.ChannelTypeWeixinIlink {
		t.Fatalf("bindings = %+v", bindings)
	}

	reg = tool.NewRegistry()
	if b := registerWeixinToolForAgent(context.Background(), nil, sender, nil, reg, &biz.AgentMeta{ID: "a2"}); b != nil {
		t.Fatalf("bindings without channels = %+v", b)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered for an agent without weixin_ilink channels")
	}
	if b := registerWeixinToolForAgent(context.Background(), nil, nil, nil, reg, &biz.AgentMeta{ID: "a1"}); b != nil {
		t.Fatalf("bindings without sender = %+v", b)
	}
}

func TestRegisterWeixinToolForAgent_LogsChannelLookupError(t *testing.T) {
	logger := &captureLogger{}
	sender := &fakeWeixinSender{err: errors.New("db down")}
	reg := tool.NewRegistry()

	if b := registerWeixinToolForAgent(context.Background(), log.NewHelper(logger), sender, nil, reg, &biz.AgentMeta{ID: "a1"}); b != nil {
		t.Fatalf("bindings on lookup error = %+v", b)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered despite lookup error")
	}
	if len(logger.lines) != 1 || !strings.HasPrefix(logger.lines[0], "WARN") || !strings.Contains(logger.lines[0], "a1") || !strings.Contains(logger.lines[0], "db down") {
		t.Fatalf("log lines = %q", logger.lines)
	}
}

func TestSetWeixinIlink_TypedNilIsNil(t *testing.T) {
	var uc *biz.WeixinIlinkUsecase
	chatSvc := &ChatService{weixinIlink: &fakeWeixinSender{}}
	chatSvc.SetWeixinIlink(uc)
	if chatSvc.weixinIlink != nil {
		t.Fatalf("ChatService.weixinIlink = %#v, want nil", chatSvc.weixinIlink)
	}
	agentSvc := &AgentService{weixinIlink: &fakeWeixinSender{}}
	agentSvc.SetWeixinIlink(uc)
	if agentSvc.weixinIlink != nil {
		t.Fatalf("AgentService.weixinIlink = %#v, want nil", agentSvc.weixinIlink)
	}
	if b := registerWeixinToolForAgent(context.Background(), nil, chatSvc.weixinIlink, nil, tool.NewRegistry(), &biz.AgentMeta{ID: "a1"}); b != nil {
		t.Fatalf("bindings with typed-nil sender = %+v", b)
	}
}
