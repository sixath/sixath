package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

type ilinkFakeChannels struct {
	ChannelRepo
	byID map[string]*ChannelMeta
}

func (f *ilinkFakeChannels) GetByID(_ context.Context, id string) (*ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return ch, nil
}

func (f *ilinkFakeChannels) GetByChannelID(_ context.Context, slug string) (*ChannelMeta, error) {
	for _, ch := range f.byID {
		if ch.ChannelID == slug {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

type ilinkFakeRepo struct {
	WeixinIlinkRepo
	confirmed   []IlinkLoginResult
	cursor      *IlinkCursor
	reports     []IlinkPeerReport
	reportGens  []int
	expiredGens []int
}

func (f *ilinkFakeRepo) ConfirmLogin(_ context.Context, channelUUID string, in IlinkLoginResult, _ time.Time) (*ChannelMeta, error) {
	f.confirmed = append(f.confirmed, in)
	return &ChannelMeta{ID: channelUUID, Type: ChannelTypeWeixinIlink, IlinkBotID: in.BotID}, nil
}

func (f *ilinkFakeRepo) GetCursor(context.Context, string) (*IlinkCursor, error) {
	if f.cursor == nil {
		return &IlinkCursor{}, nil
	}
	cp := *f.cursor
	return &cp, nil
}

func (f *ilinkFakeRepo) UpsertPeerReport(_ context.Context, _ string, generation int, in IlinkPeerReport) (*IlinkPeer, error) {
	f.reports = append(f.reports, in)
	f.reportGens = append(f.reportGens, generation)
	return &IlinkPeer{PeerID: in.PeerID}, nil
}

func (f *ilinkFakeRepo) MarkSessionExpired(_ context.Context, _ string, generation int, _ time.Time) (bool, error) {
	f.expiredGens = append(f.expiredGens, generation)
	return true, nil
}

type ilinkFakeLogin struct {
	qr       *ilink.QRCode
	statuses []*ilink.QRStatus
}

func (f *ilinkFakeLogin) GetBotQRCode(context.Context) (*ilink.QRCode, error) { return f.qr, nil }

func (f *ilinkFakeLogin) GetQRCodeStatus(context.Context, string) (*ilink.QRStatus, error) {
	if len(f.statuses) == 0 {
		return &ilink.QRStatus{Status: "wait"}, nil
	}
	s := f.statuses[0]
	f.statuses = f.statuses[1:]
	return s, nil
}

var ilinkTestNow = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

func newIlinkUsecaseForTest(login *ilinkFakeLogin) (*WeixinIlinkUsecase, *ilinkFakeRepo, *authEphemeralFake) {
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":   {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkGeneration: 3},
		"uuid-2":   {ID: "uuid-2", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-web": {ID: "uuid-web", ChannelID: "web-1", Type: "web"},
	}}
	repo := &ilinkFakeRepo{}
	eph := newAuthEphemeralFake()
	uc := NewWeixinIlinkUsecaseWith(channels, repo, eph, login, func(content string) (string, error) {
		return "data:image/png;base64,FAKE-" + content, nil
	})
	uc.now = func() time.Time { return ilinkTestNow }
	return uc, repo, eph
}

func newQRLogin(statuses ...*ilink.QRStatus) *ilinkFakeLogin {
	return &ilinkFakeLogin{qr: &ilink.QRCode{QRCode: "qrc-1", ImgContent: "https://liteapp.weixin.qq.com/q/x"}, statuses: statuses}
}

func TestIlinkStartLogin_StoresQRCodeBinding(t *testing.T) {
	uc, _, eph := newIlinkUsecaseForTest(newQRLogin())

	res, err := uc.StartLogin(context.Background(), "uuid-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if res.QRCode != "qrc-1" || !strings.HasPrefix(res.QRImage, "data:image/png;base64,") {
		t.Fatalf("res = %+v", res)
	}
	if !res.ExpiresAt.Equal(ilinkTestNow.Add(ilinkQRCodeTTL)) {
		t.Fatalf("ExpiresAt = %v", res.ExpiresAt)
	}
	item, ok := eph.get(ilinkQRCodeKey("qrc-1"))
	if !ok || item.kind != ilinkQRCodeKind || !strings.Contains(item.payload, "uuid-1") {
		t.Fatalf("ephemeral = %+v ok=%v", item, ok)
	}
}

func TestIlinkStartLogin_RejectsNonIlinkChannel(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	if _, err := uc.StartLogin(context.Background(), "uuid-web"); !errors.Is(err, ErrIlinkNotIlinkChannel) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.StartLogin(context.Background(), "missing"); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestIlinkPollLogin_WaitKeepsBinding(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "wait"}, &ilink.QRStatus{Status: "scaned"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	for _, want := range []string{"wait", "scaned", "wait"} {
		res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
		if err != nil || res.Status != want {
			t.Fatalf("PollLogin = %+v, %v; want %s", res, err, want)
		}
	}
	if len(repo.confirmed) != 0 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
}

func TestIlinkPollLogin_ConfirmedPersistsAndConsumes(t *testing.T) {
	uc, repo, eph := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{
		Status: "confirmed", BotToken: "bt", BotID: "b@im.bot", UserID: "u@im.wechat", BaseURL: "https://ilinkai.weixin.qq.com",
	}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
	if err != nil || res.Status != "confirmed" || res.BotID != "b@im.bot" {
		t.Fatalf("PollLogin = %+v, %v", res, err)
	}
	if len(repo.confirmed) != 1 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
	got := repo.confirmed[0]
	if got.BotToken != "bt" || got.BotID != "b@im.bot" || got.OwnerUserID != "u@im.wechat" || got.BaseURL != "https://ilinkai.weixin.qq.com" {
		t.Fatalf("confirm input = %+v", got)
	}
	if item, _ := eph.get(ilinkQRCodeKey("qrc-1")); !item.consumed {
		t.Fatal("qrcode binding not consumed")
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("poll after confirm err = %v", err)
	}
}

func TestIlinkPollLogin_ConfirmedWithoutTokenFails(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "confirmed", BotID: "b@im.bot"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); err == nil {
		t.Fatal("want error when bot_token is missing")
	}
	if len(repo.confirmed) != 0 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
}

func TestIlinkPollLogin_QRCodeOfOtherChannelNotFound(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-2", "qrc-1"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "never-issued"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("unknown qrcode err = %v", err)
	}
}

func TestIlinkPollLogin_ExpiredConsumes(t *testing.T) {
	uc, _, eph := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "expired"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
	if err != nil || res.Status != "expired" {
		t.Fatalf("PollLogin = %+v, %v", res, err)
	}
	if item, _ := eph.get(ilinkQRCodeKey("qrc-1")); !item.consumed {
		t.Fatal("expired qrcode binding not consumed")
	}
}

func TestIlinkGetCursor_GenerationMismatchReturnsEmpty(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()

	repo.cursor = &IlinkCursor{Generation: 2, GetUpdatesBuf: "old"}
	cur, err := uc.GetCursor(ctx, "wx-1")
	if err != nil || cur.Generation != 3 || cur.GetUpdatesBuf != "" {
		t.Fatalf("stale cursor = %+v, %v", cur, err)
	}
	repo.cursor = &IlinkCursor{Generation: 3, GetUpdatesBuf: "cur"}
	cur, err = uc.GetCursor(ctx, "wx-1")
	if err != nil || cur.GetUpdatesBuf != "cur" {
		t.Fatalf("current cursor = %+v, %v", cur, err)
	}
}

func TestIlinkReportPeer_TruncatesPreviewAndDefaultsTime(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())

	_, err := uc.ReportPeer(context.Background(), "wx-1", 3, IlinkPeerReport{PeerID: " p1 ", Preview: strings.Repeat("字", 100)})
	if err != nil {
		t.Fatalf("ReportPeer: %v", err)
	}
	got := repo.reports[0]
	if got.PeerID != "p1" || utf8.RuneCountInString(got.Preview) != ilinkPreviewRunes || !got.At.Equal(ilinkTestNow) {
		t.Fatalf("report = %+v", got)
	}
	if repo.reportGens[0] != 3 {
		t.Fatalf("generation = %d", repo.reportGens[0])
	}
}

func TestIlinkReportPeer_Validation(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.ReportPeer(ctx, "missing", 0, IlinkPeerReport{PeerID: "p1"}); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("unknown slug err = %v", err)
	}
	if _, err := uc.ReportPeer(ctx, "web-1", 0, IlinkPeerReport{PeerID: "p1"}); !errors.Is(err, ErrIlinkNotIlinkChannel) {
		t.Fatalf("non-ilink err = %v", err)
	}
	if _, err := uc.ReportPeer(ctx, "wx-1", 3, IlinkPeerReport{PeerID: "  "}); err == nil {
		t.Fatal("want error for empty peer_id")
	}
}

func TestIlinkReportSessionExpired_PassesGeneration(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())
	if err := uc.ReportSessionExpired(context.Background(), "wx-1", 3); err != nil {
		t.Fatalf("ReportSessionExpired: %v", err)
	}
	if len(repo.expiredGens) != 1 || repo.expiredGens[0] != 3 {
		t.Fatalf("expiredGens = %v", repo.expiredGens)
	}
}

func TestIlinkSafeBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"https://ilinkai.weixin.qq.com":      "https://ilinkai.weixin.qq.com",
		"https://ilinkai.weixin.qq.com/":     "https://ilinkai.weixin.qq.com",
		"https://weixin.qq.com":              "https://weixin.qq.com",
		"http://ilinkai.weixin.qq.com":       "",
		"https://evil.example.com":           "",
		"https://weixin.qq.com.evil.com":     "",
		"https://user@ilinkai.weixin.qq.com": "",
		"::not a url":                        "",
	}
	for in, want := range cases {
		if got := ilinkSafeBaseURL(in); got != want {
			t.Errorf("ilinkSafeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIlinkPollLogin_UntrustedBaseURLFallsBackToDefault(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{
		Status: "confirmed", BotToken: "bt", BotID: "b@im.bot", UserID: "u@im.wechat", BaseURL: "https://evil.example.com",
	}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); err != nil {
		t.Fatalf("PollLogin: %v", err)
	}
	if got := repo.confirmed[0].BaseURL; got != "" {
		t.Fatalf("BaseURL = %q, want empty (default)", got)
	}
}

func TestIlinkUpdatePeer_Validation(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.UpdatePeer(ctx, "uuid-1", "p1", IlinkPeerPatch{}); err == nil {
		t.Fatal("want error for empty patch")
	}
	long := strings.Repeat("名", ilinkRemarkMaxRunes+1)
	if _, err := uc.UpdatePeer(ctx, "uuid-1", "p1", IlinkPeerPatch{Remark: &long}); err == nil {
		t.Fatal("want error for long remark")
	}
}
