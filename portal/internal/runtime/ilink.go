package runtime

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/ilink"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// ilinkBackend is the weixin_ilink surface used by the Gateway; channelID arguments are channel slugs.
type ilinkBackend interface {
	AllowedPeers(ctx context.Context, channelUUID string) ([]string, error)
	ReportPeer(ctx context.Context, channelID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error)
	GetCursor(ctx context.Context, channelID string) (*biz.IlinkCursor, error)
	PutCursor(ctx context.Context, channelID string, cur biz.IlinkCursor) error
	ReportSessionExpired(ctx context.Context, channelID string, generation int) error
}

var _ ilinkBackend = (*biz.WeixinIlinkUsecase)(nil)

// SetIlink wires the weixin_ilink backend; without it the ilink endpoints return 500.
func (s *Service) SetIlink(b ilinkBackend) {
	if s != nil {
		s.ilink = b
	}
}

type ilinkPeerReportRequest struct {
	Generation   int    `json:"generation"`
	PeerID       string `json:"peer_id"`
	ContextToken string `json:"context_token"`
	Preview      string `json:"preview"`
	At           string `json:"at"`
}

type ilinkCursorBody struct {
	Generation    int    `json:"generation"`
	GetUpdatesBuf string `json:"get_updates_buf"`
}

type ilinkSessionExpiredRequest struct {
	Generation int    `json:"generation"`
	Reason     string `json:"reason"`
}

func (s *Service) ilinkBackendOrErr() (ilinkBackend, error) {
	if s.ilink == nil {
		return nil, kratosErrors.InternalServer("UNAVAILABLE", "weixin_ilink backend unavailable")
	}
	return s.ilink, nil
}

// fillIlinkGatewayItem adds weixin_ilink fields to a gateway channel item; allowlist errors fail the sync.
func (s *Service) fillIlinkGatewayItem(ctx context.Context, ch *biz.ChannelMeta, item *gatewayChannelItem) error {
	item.BotToken = ch.IlinkBotToken
	item.BaseURL = ch.IlinkBaseURL
	if item.BaseURL == "" {
		item.BaseURL = ilink.DefaultBaseURL
	}
	item.Generation = ch.IlinkGeneration
	item.SessionExpired = ch.IlinkSessionExpiredAt != nil
	if s.ilink == nil {
		return nil
	}
	peers, err := s.ilink.AllowedPeers(ctx, ch.ID)
	if err != nil {
		return err
	}
	item.AllowedPeers = peers
	return nil
}

func (s *Service) handleIlinkReportPeer(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkPeerReportRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	in := biz.IlinkPeerReport{PeerID: req.PeerID, ContextToken: req.ContextToken, Preview: req.Preview}
	if strings.TrimSpace(req.At) != "" {
		at, err := time.Parse(time.RFC3339, req.At)
		if err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "at must be RFC3339")
		}
		in.At = at
	}
	peer, err := b.ReportPeer(ctx, hctx.Vars().Get("channel_id"), req.Generation, in)
	if err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"allowed": peer.Allowed})
}

func (s *Service) handleIlinkGetCursor(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	cur, err := b.GetCursor(ctx, hctx.Vars().Get("channel_id"))
	if err != nil {
		return err
	}
	return hctx.JSON(200, ilinkCursorBody{Generation: cur.Generation, GetUpdatesBuf: cur.GetUpdatesBuf})
}

func (s *Service) handleIlinkPutCursor(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkCursorBody
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	if err := b.PutCursor(ctx, hctx.Vars().Get("channel_id"), biz.IlinkCursor{Generation: req.Generation, GetUpdatesBuf: req.GetUpdatesBuf}); err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"ok": true})
}

func (s *Service) handleIlinkSessionExpired(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkSessionExpiredRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	if err := b.ReportSessionExpired(ctx, hctx.Vars().Get("channel_id"), req.Generation); err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"ok": true})
}
