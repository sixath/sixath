package biz

import (
	"context"
	"time"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ChannelTypeWeixinIlink is the channel type for WeChat iLink bots.
const ChannelTypeWeixinIlink = "weixin_ilink"

var (
	ErrIlinkNotIlinkChannel    = kratosErrors.BadRequest("ILINK_NOT_ILINK_CHANNEL", "channel type is not weixin_ilink")
	ErrIlinkQRCodeNotFound     = kratosErrors.NotFound("ILINK_QRCODE_NOT_FOUND", "二维码不存在或已过期，请重新获取")
	ErrIlinkGenerationMismatch = kratosErrors.Conflict("ILINK_GENERATION_MISMATCH", "weixin_ilink generation mismatch")
	ErrIlinkPeerNotFound       = kratosErrors.NotFound("ILINK_PEER_NOT_FOUND", "peer not found")
)

// IlinkPeer is one roster row of a weixin_ilink channel.
type IlinkPeer struct {
	ChannelUUID    string
	PeerID         string
	Allowed        bool
	Remark         string
	ContextToken   string
	ContextTokenAt *time.Time
	LastInboundAt  *time.Time
	LastPreview    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type IlinkPeerPatch struct {
	Allowed *bool
	Remark  *string
}

// IlinkPeerReport is what the Gateway reports for each inbound message.
type IlinkPeerReport struct {
	PeerID       string
	ContextToken string
	Preview      string
	At           time.Time
}

type IlinkCursor struct {
	Generation    int
	GetUpdatesBuf string
}

type IlinkLoginResult struct {
	BotToken    string
	BotID       string
	OwnerUserID string
	BaseURL     string
}

// WeixinIlinkRepo persists weixin_ilink login state, roster and cursor. channelUUID is channels.id.
type WeixinIlinkRepo interface {
	// ConfirmLogin writes credentials, bumps ilink_generation, clears the expiry mark and cursor,
	// clears every roster context_token (each scan yields a new bot) and allows the owner, in one transaction.
	ConfirmLogin(ctx context.Context, channelUUID string, in IlinkLoginResult, now time.Time) (*ChannelMeta, error)
	// MarkSessionExpired applies only when generation matches; reports whether it applied.
	MarkSessionExpired(ctx context.Context, channelUUID string, generation int, now time.Time) (bool, error)
	// UpsertPeerReport returns ErrIlinkGenerationMismatch when generation is stale; new peers start not allowed.
	UpsertPeerReport(ctx context.Context, channelUUID string, generation int, in IlinkPeerReport) (*IlinkPeer, error)
	ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error)
	// GetPeer returns ErrNotFound when the peer is not in the roster.
	GetPeer(ctx context.Context, channelUUID, peerID string) (*IlinkPeer, error)
	UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error)
	DeletePeer(ctx context.Context, channelUUID, peerID string) error
	ListAllowedPeerIDs(ctx context.Context, channelUUID string) ([]string, error)
	// GetCursor returns an empty cursor (generation 0) when none is stored.
	GetCursor(ctx context.Context, channelUUID string) (*IlinkCursor, error)
	// PutCursor returns ErrIlinkGenerationMismatch unless cur.Generation equals channels.ilink_generation.
	PutCursor(ctx context.Context, channelUUID string, cur IlinkCursor, now time.Time) error
}
