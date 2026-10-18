package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

const (
	ilinkQRCodeKind     = "ilink_qrcode"
	ilinkQRCodeTTL      = 5 * time.Minute
	ilinkPreviewRunes   = 40
	ilinkRemarkMaxRunes = 64
	// Column widths in channel_ilink_peers; MySQL strict mode rejects longer values with a 500.
	ilinkPeerIDMaxRunes     = 128
	ilinkContextTokenMaxLen = 512
)

// IlinkLoginClient is the QR login subset of the iLink client (*ilink.Client satisfies it).
type IlinkLoginClient interface {
	GetBotQRCode(ctx context.Context) (*ilink.QRCode, error)
	GetQRCodeStatus(ctx context.Context, qrcode string) (*ilink.QRStatus, error)
}

type IlinkLoginStart struct {
	QRCode    string
	QRImage   string
	ExpiresAt time.Time
}

type IlinkLoginPoll struct {
	Status string // wait | scaned | confirmed | expired
	BotID  string
}

// WeixinIlinkUsecase orchestrates weixin_ilink QR login, roster management and Gateway runtime calls.
type WeixinIlinkUsecase struct {
	channels  ChannelRepo
	repo      WeixinIlinkRepo
	ephemeral AuthEphemeralRepo
	login     IlinkLoginClient
	qrImage   func(content string) (string, error)
	now       func() time.Time
}

func NewWeixinIlinkUsecase(channels ChannelRepo, repo WeixinIlinkRepo, ephemeral AuthEphemeralRepo) *WeixinIlinkUsecase {
	return NewWeixinIlinkUsecaseWith(channels, repo, ephemeral, ilink.NewClient(ilink.DefaultBaseURL, ""), ilink.QRCodePNGDataURL)
}

func NewWeixinIlinkUsecaseWith(channels ChannelRepo, repo WeixinIlinkRepo, ephemeral AuthEphemeralRepo, login IlinkLoginClient, qrImage func(string) (string, error)) *WeixinIlinkUsecase {
	return &WeixinIlinkUsecase{
		channels:  channels,
		repo:      repo,
		ephemeral: ephemeral,
		login:     login,
		qrImage:   qrImage,
		now:       time.Now,
	}
}

type ilinkQRCodePayload struct {
	ChannelUUID string `json:"channel_uuid"`
}

func ilinkQRCodeKey(qrcode string) string {
	sum := sha256.Sum256([]byte(qrcode))
	return hex.EncodeToString(sum[:])
}

func ilinkTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ilinkSafeBaseURL keeps the server-provided base URL only if it is https on weixin.qq.com,
// because the bot_token is later sent there; "" means the default base URL.
func ilinkSafeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

func ilinkUpstreamErr(op string, err error) error {
	return kratosErrors.ServiceUnavailable("ILINK_UPSTREAM", op+"失败："+err.Error())
}

func ilinkValidPeerID(peerID string) error {
	if peerID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "peer_id is required")
	}
	if utf8.RuneCountInString(peerID) > ilinkPeerIDMaxRunes {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "peer_id is too long")
	}
	return nil
}

// ilinkChannelGone maps a repo ErrNotFound (channel deleted after the slug lookup) to ErrChannelNotFound.
func ilinkChannelGone(err error) error {
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrChannelNotFound
	}
	return err
}

func (uc *WeixinIlinkUsecase) checkIlink(ch *ChannelMeta, err error) (*ChannelMeta, error) {
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	if ch.Type != ChannelTypeWeixinIlink {
		return nil, ErrIlinkNotIlinkChannel
	}
	return ch, nil
}

func (uc *WeixinIlinkUsecase) channelByID(ctx context.Context, channelUUID string) (*ChannelMeta, error) {
	return uc.checkIlink(uc.channels.GetByID(ctx, strings.TrimSpace(channelUUID)))
}

func (uc *WeixinIlinkUsecase) channelBySlug(ctx context.Context, channelID string) (*ChannelMeta, error) {
	return uc.checkIlink(uc.channels.GetByChannelID(ctx, strings.TrimSpace(channelID)))
}

// StartLogin fetches a QR code and binds it to the channel for ilinkQRCodeTTL.
func (uc *WeixinIlinkUsecase) StartLogin(ctx context.Context, channelUUID string) (*IlinkLoginStart, error) {
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	qr, err := uc.login.GetBotQRCode(ctx)
	if err != nil {
		return nil, ilinkUpstreamErr("获取二维码", err)
	}
	img, err := uc.qrImage(qr.ImgContent)
	if err != nil {
		return nil, kratosErrors.InternalServer("ILINK_QRCODE_RENDER", err.Error())
	}
	payload, err := json.Marshal(ilinkQRCodePayload{ChannelUUID: ch.ID})
	if err != nil {
		return nil, err
	}
	expires := uc.now().Add(ilinkQRCodeTTL)
	if err := uc.ephemeral.Put(ctx, ilinkQRCodeKey(qr.QRCode), ilinkQRCodeKind, string(payload), expires); err != nil {
		return nil, err
	}
	return &IlinkLoginStart{QRCode: qr.QRCode, QRImage: img, ExpiresAt: expires}, nil
}

// PollLogin relays one get_qrcode_status round (the server may hold it ~30s) and persists on confirmed.
func (uc *WeixinIlinkUsecase) PollLogin(ctx context.Context, channelUUID, qrcode string) (*IlinkLoginPoll, error) {
	qrcode = strings.TrimSpace(qrcode)
	if qrcode == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "qrcode is required")
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	key := ilinkQRCodeKey(qrcode)
	raw, err := uc.ephemeral.Get(ctx, key, ilinkQRCodeKind, uc.now())
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrIlinkQRCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	var p ilinkQRCodePayload
	if json.Unmarshal([]byte(raw), &p) != nil || p.ChannelUUID != ch.ID {
		return nil, ErrIlinkQRCodeNotFound
	}

	st, err := uc.login.GetQRCodeStatus(ctx, qrcode)
	if err != nil {
		return nil, ilinkUpstreamErr("查询扫码状态", err)
	}
	switch st.Status {
	case "confirmed":
		if strings.TrimSpace(st.BotToken) == "" {
			return nil, kratosErrors.ServiceUnavailable("ILINK_UPSTREAM", "扫码确认结果缺少 bot_token")
		}
		meta, err := uc.repo.ConfirmLogin(ctx, ch.ID, IlinkLoginResult{
			BotToken:    st.BotToken,
			BotID:       st.BotID,
			OwnerUserID: st.UserID,
			BaseURL:     ilinkSafeBaseURL(st.BaseURL),
		}, uc.now())
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrChannelNotFound
		}
		if err != nil {
			return nil, err
		}
		if _, err := uc.ephemeral.Consume(ctx, key, ilinkQRCodeKind, uc.now()); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, err
		}
		return &IlinkLoginPoll{Status: "confirmed", BotID: meta.IlinkBotID}, nil
	case "expired":
		_, _ = uc.ephemeral.Consume(ctx, key, ilinkQRCodeKind, uc.now())
		return &IlinkLoginPoll{Status: "expired"}, nil
	case "scaned":
		return &IlinkLoginPoll{Status: "scaned"}, nil
	default:
		return &IlinkLoginPoll{Status: "wait"}, nil
	}
}

func (uc *WeixinIlinkUsecase) ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error) {
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	return uc.repo.ListPeers(ctx, ch.ID)
}

func (uc *WeixinIlinkUsecase) UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error) {
	peerID = strings.TrimSpace(peerID)
	if err := ilinkValidPeerID(peerID); err != nil {
		return nil, err
	}
	if patch.Allowed == nil && patch.Remark == nil {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "allowed or remark is required")
	}
	if patch.Remark != nil {
		remark := strings.TrimSpace(*patch.Remark)
		if utf8.RuneCountInString(remark) > ilinkRemarkMaxRunes {
			return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "remark is too long")
		}
		patch.Remark = &remark
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	p, err := uc.repo.UpdatePeer(ctx, ch.ID, peerID, patch)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrIlinkPeerNotFound
	}
	return p, err
}

func (uc *WeixinIlinkUsecase) DeletePeer(ctx context.Context, channelUUID, peerID string) error {
	peerID = strings.TrimSpace(peerID)
	if err := ilinkValidPeerID(peerID); err != nil {
		return err
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return err
	}
	err = uc.repo.DeletePeer(ctx, ch.ID, peerID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrIlinkPeerNotFound
	}
	return err
}

// AllowedPeers lists allowed peer IDs by channels.id (Gateway channel sync).
func (uc *WeixinIlinkUsecase) AllowedPeers(ctx context.Context, channelUUID string) ([]string, error) {
	return uc.repo.ListAllowedPeerIDs(ctx, channelUUID)
}

// ReportPeer records an inbound message; channelID is the channel slug used by the Gateway.
func (uc *WeixinIlinkUsecase) ReportPeer(ctx context.Context, channelID string, generation int, in IlinkPeerReport) (*IlinkPeer, error) {
	in.PeerID = strings.TrimSpace(in.PeerID)
	if err := ilinkValidPeerID(in.PeerID); err != nil {
		return nil, err
	}
	if len(in.ContextToken) > ilinkContextTokenMaxLen {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "context_token is too long")
	}
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if in.At.IsZero() {
		in.At = uc.now()
	}
	in.Preview = ilinkTruncate(strings.TrimSpace(in.Preview), ilinkPreviewRunes)
	p, err := uc.repo.UpsertPeerReport(ctx, ch.ID, generation, in)
	if err != nil {
		return nil, ilinkChannelGone(err)
	}
	return p, nil
}

// GetCursor returns an empty cursor for the current generation when the stored one is stale.
func (uc *WeixinIlinkUsecase) GetCursor(ctx context.Context, channelID string) (*IlinkCursor, error) {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return nil, err
	}
	cur, err := uc.repo.GetCursor(ctx, ch.ID)
	if err != nil {
		return nil, err
	}
	if cur.Generation != ch.IlinkGeneration {
		return &IlinkCursor{Generation: ch.IlinkGeneration}, nil
	}
	return cur, nil
}

func (uc *WeixinIlinkUsecase) PutCursor(ctx context.Context, channelID string, cur IlinkCursor) error {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return err
	}
	return ilinkChannelGone(uc.repo.PutCursor(ctx, ch.ID, cur, uc.now()))
}

// ReportSessionExpired marks the channel login_required; ignored when generation is stale.
func (uc *WeixinIlinkUsecase) ReportSessionExpired(ctx context.Context, channelID string, generation int) error {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return err
	}
	_, err = uc.repo.MarkSessionExpired(ctx, ch.ID, generation, uc.now())
	return ilinkChannelGone(err)
}
