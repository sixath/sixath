package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

type pushFakeRepo struct {
	*ilinkFakeRepo
	peers map[string]*IlinkPeer
}

func (f *pushFakeRepo) GetPeer(_ context.Context, _ string, peerID string) (*IlinkPeer, error) {
	p, ok := f.peers[peerID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

type fakeIlinkSender struct {
	baseURL, token string
	sent           []string
	toks           []string
	errs           []error
}

func (f *fakeIlinkSender) SendText(_ context.Context, to, contextToken, text string) (string, error) {
	i := len(f.sent)
	f.sent = append(f.sent, to+"|"+text)
	f.toks = append(f.toks, contextToken)
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	return "mid-" + string(rune('a'+i)), nil
}

func newPushUsecaseForTest() (*WeixinIlinkUsecase, *pushFakeRepo, *fakeIlinkSender, *int) {
	expired := ilinkTestNow.Add(-time.Hour)
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":       {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkBaseURL: "https://x.weixin.qq.com", IlinkGeneration: 3},
		"uuid-nologin": {ID: "uuid-nologin", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-expired": {ID: "uuid-expired", ChannelID: "wx-3", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkSessionExpiredAt: &expired},
		"uuid-off":     {ID: "uuid-off", ChannelID: "wx-4", Type: ChannelTypeWeixinIlink, IlinkBotToken: "bt"},
		"uuid-web":     {ID: "uuid-web", ChannelID: "web-1", Type: "web"},
	}}
	repo := &pushFakeRepo{ilinkFakeRepo: &ilinkFakeRepo{}, peers: map[string]*IlinkPeer{
		"p1":      {PeerID: "p1", Allowed: true, ContextToken: "ctx-1"},
		"blocked": {PeerID: "blocked", ContextToken: "ctx-2"},
		"silent":  {PeerID: "silent", Allowed: true},
	}}
	sender := &fakeIlinkSender{}
	created := 0
	uc := NewWeixinIlinkUsecaseWith(channels, repo, newAuthEphemeralFake(), newQRLogin(), nil)
	uc.now = func() time.Time { return ilinkTestNow }
	uc.newSender = func(baseURL, token string) IlinkSender {
		created++
		sender.baseURL, sender.token = baseURL, token
		return sender
	}
	return uc, repo, sender, &created
}

func TestIlinkPush_SplitsAndSends(t *testing.T) {
	uc, _, sender, _ := newPushUsecaseForTest()
	text := "# 日报\n" + strings.Repeat("字", ilink.DefaultSplitRunes)

	ids, err := uc.Push(context.Background(), "uuid-1", " p1 ", text)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(ids) != 2 || ids[0] != "mid-a" || ids[1] != "mid-b" {
		t.Fatalf("ids = %v", ids)
	}
	if sender.baseURL != "https://x.weixin.qq.com" || sender.token != "bt" {
		t.Fatalf("sender = %q / %q", sender.baseURL, sender.token)
	}
	if !strings.HasPrefix(sender.sent[0], "p1|日报\n") || sender.toks[0] != "ctx-1" || sender.toks[1] != "ctx-1" {
		t.Fatalf("sent = %q toks = %v", sender.sent[0][:20], sender.toks)
	}
}

func TestIlinkPush_RejectsWithoutCallingIlink(t *testing.T) {
	uc, _, _, created := newPushUsecaseForTest()
	ctx := context.Background()
	cases := []struct {
		channel, peer string
		check         func(error) bool
	}{
		{"uuid-1", "p9", func(err error) bool {
			return errors.Is(err, ErrIlinkPeerNotAllowed) && err.Error() == "目标 p9 未在白名单"
		}},
		{"uuid-1", "blocked", func(err error) bool { return errors.Is(err, ErrIlinkPeerNotAllowed) }},
		{"uuid-1", "silent", func(err error) bool { return errors.Is(err, ErrIlinkNoContextToken) }},
		{"uuid-nologin", "p1", func(err error) bool { return errors.Is(err, ErrIlinkLoginRequired) }},
		{"uuid-expired", "p1", func(err error) bool { return errors.Is(err, ErrIlinkLoginRequired) }},
		{"uuid-off", "p1", func(err error) bool { return err != nil && strings.Contains(err.Error(), "已停用") }},
		{"uuid-web", "p1", func(err error) bool { return errors.Is(err, ErrIlinkNotIlinkChannel) }},
		{"missing", "p1", func(err error) bool { return errors.Is(err, ErrChannelNotFound) }},
		{"uuid-1", " ", func(err error) bool { return isIlinkInvalidArgument(err) }},
	}
	for _, c := range cases {
		if _, err := uc.Push(ctx, c.channel, c.peer, "hi"); !c.check(err) {
			t.Errorf("Push(%s, %q) err = %v", c.channel, c.peer, err)
		}
	}
	if *created != 0 {
		t.Fatalf("iLink client created %d times", *created)
	}
	if _, err := uc.Push(ctx, "uuid-1", "p1", " \n "); err == nil {
		t.Fatal("want error for blank text")
	}
}

func TestIlinkPush_SessionExpiredMarksChannel(t *testing.T) {
	uc, repo, sender, _ := newPushUsecaseForTest()
	sender.errs = []error{&ilink.APIError{ErrCode: -14, ErrMsg: "session timeout"}}

	ids, err := uc.Push(context.Background(), "uuid-1", "p1", strings.Repeat("字", ilink.DefaultSplitRunes+1))
	if !errors.Is(err, ErrIlinkLoginRequired) || len(ids) != 0 {
		t.Fatalf("Push = %v, %v", ids, err)
	}
	if len(repo.expiredGens) != 1 || repo.expiredGens[0] != 3 {
		t.Fatalf("expiredGens = %v", repo.expiredGens)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d parts after -14", len(sender.sent))
	}
}

func TestIlinkPush_NoRetryOnFailure(t *testing.T) {
	uc, repo, sender, _ := newPushUsecaseForTest()
	sender.errs = []error{nil, errors.New("boom")}

	ids, err := uc.Push(context.Background(), "uuid-1", "p1", strings.Repeat("字", ilink.DefaultSplitRunes*3))
	if err == nil || !strings.Contains(err.Error(), "2/3") || len(ids) != 1 {
		t.Fatalf("Push = %v, %v", ids, err)
	}
	if len(sender.sent) != 2 || len(repo.expiredGens) != 0 {
		t.Fatalf("sent = %d expired = %v", len(sender.sent), repo.expiredGens)
	}
}
