package server

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// WeixinIlinkHandlers serves /api/v1/channels/{id}/ilink/*: login only, same as the Channel admin APIs.
type WeixinIlinkHandlers struct {
	uc *biz.WeixinIlinkUsecase
}

func NewWeixinIlinkHandlers(uc *biz.WeixinIlinkUsecase) *WeixinIlinkHandlers {
	return &WeixinIlinkHandlers{uc: uc}
}

type ilinkPeerView struct {
	PeerID          string `json:"peer_id"`
	Allowed         bool   `json:"allowed"`
	Remark          string `json:"remark"`
	HasContextToken bool   `json:"has_context_token"`
	ContextTokenAt  string `json:"context_token_at,omitempty"`
	LastInboundAt   string `json:"last_inbound_at,omitempty"`
	LastPreview     string `json:"last_preview"`
}

type ilinkPeerPatchRequest struct {
	Allowed *bool   `json:"allowed"`
	Remark  *string `json:"remark"`
}

func ilinkPeerToView(p *biz.IlinkPeer) ilinkPeerView {
	v := ilinkPeerView{
		PeerID:          p.PeerID,
		Allowed:         p.Allowed,
		Remark:          p.Remark,
		HasContextToken: p.ContextToken != "",
		LastPreview:     p.LastPreview,
	}
	if p.ContextTokenAt != nil {
		v.ContextTokenAt = p.ContextTokenAt.UTC().Format(time.RFC3339)
	}
	if p.LastInboundAt != nil {
		v.LastInboundAt = p.LastInboundAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (h *WeixinIlinkHandlers) serve(ctx kratoshttp.Context, fn func(context.Context) (any, error)) error {
	if h == nil || h.uc == nil {
		return kratosErrors.ServiceUnavailable("ILINK_DISABLED", "weixin_ilink is not configured")
	}
	out, err := runWithMiddleware(ctx, fn)
	if err != nil {
		return err
	}
	return ctx.JSON(200, out)
}

// POST /api/v1/channels/{id}/ilink/login
func (h *WeixinIlinkHandlers) StartLogin() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			res, err := h.uc.StartLogin(c, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"qrcode":       res.QRCode,
				"qrcode_image": res.QRImage,
				"expires_at":   res.ExpiresAt.UTC().Format(time.RFC3339),
			}, nil
		})
	}
}

// GET /api/v1/channels/{id}/ilink/login/status?qrcode=
func (h *WeixinIlinkHandlers) LoginStatus() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		qrcode := ctx.Request().URL.Query().Get("qrcode")
		return h.serve(ctx, func(c context.Context) (any, error) {
			res, err := h.uc.PollLogin(c, id, qrcode)
			if err != nil {
				return nil, err
			}
			return map[string]any{"status": res.Status, "bot_id": res.BotID}, nil
		})
	}
}

// GET /api/v1/channels/{id}/ilink/peers
func (h *WeixinIlinkHandlers) ListPeers() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			peers, err := h.uc.ListPeers(c, id)
			if err != nil {
				return nil, err
			}
			items := make([]ilinkPeerView, 0, len(peers))
			for _, p := range peers {
				items = append(items, ilinkPeerToView(p))
			}
			return map[string]any{"items": items}, nil
		})
	}
}

// PATCH /api/v1/channels/{id}/ilink/peers/{peer_id}
func (h *WeixinIlinkHandlers) PatchPeer() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		peerID := ctx.Vars().Get("peer_id")
		var req ilinkPeerPatchRequest
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			p, err := h.uc.UpdatePeer(c, id, peerID, biz.IlinkPeerPatch{Allowed: req.Allowed, Remark: req.Remark})
			if err != nil {
				return nil, err
			}
			return ilinkPeerToView(p), nil
		})
	}
}

// DELETE /api/v1/channels/{id}/ilink/peers/{peer_id}
func (h *WeixinIlinkHandlers) DeletePeer() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		peerID := ctx.Vars().Get("peer_id")
		return h.serve(ctx, func(c context.Context) (any, error) {
			if err := h.uc.DeletePeer(c, id, peerID); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
	}
}
