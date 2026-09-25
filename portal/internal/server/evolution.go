package server

import (
	"strings"

	"backend/internal/biz"
	chat "backend/internal/chat"
	"github.com/sixath/framework/config"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// EvolutionHandlers provides HTTP handlers for the evolution review API.
type EvolutionHandlers struct {
	uc *biz.EvolutionUsecase
}

func NewEvolutionHandlers(uc *biz.EvolutionUsecase) *EvolutionHandlers {
	return &EvolutionHandlers{uc: uc}
}

func (h *EvolutionHandlers) ListProposals() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		page := int32(parseIntQuery(ctx.Query().Get("page"), 1))
		pageSize := int32(parseIntQuery(ctx.Query().Get("page_size"), 20))
		status := strings.TrimSpace(ctx.Query().Get("status"))
		items, total, err := h.uc.List(ctx, page, pageSize, status)
		if err != nil {
			return ctx.JSON(500, map[string]any{"error": err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret":   map[string]any{"code": 0, "message": "ok"},
			"items": items,
			"total": total,
		})
	}
}

func (h *EvolutionHandlers) GetProposal() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		item, err := h.uc.Get(ctx, id)
		if err != nil {
			return ctx.JSON(404, map[string]any{"error": "not found"})
		}
		return ctx.JSON(200, map[string]any{
			"ret":  map[string]any{"code": 0, "message": "ok"},
			"item": item,
		})
	}
}

func (h *EvolutionHandlers) ApproveProposal() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		reviewer := ctx.Request().Header.Get("X-User-Email")
		if reviewer == "" {
			reviewer = "unknown"
		}
		if err := h.uc.Approve(ctx, id, reviewer); err != nil {
			return ctx.JSON(500, map[string]any{"error": err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret": map[string]any{"code": 0, "message": "ok"},
		})
	}
}

func (h *EvolutionHandlers) RejectProposal() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		reviewer := ctx.Request().Header.Get("X-User-Email")
		if reviewer == "" {
			reviewer = "unknown"
		}
		var body struct {
			Comment string `json:"comment"`
		}
		if err := ctx.Bind(&body); err != nil {
			// Allow empty body — binding may fail when body is absent.
		}
		if err := h.uc.Reject(ctx, id, reviewer, body.Comment); err != nil {
			return ctx.JSON(500, map[string]any{"error": err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret": map[string]any{"code": 0, "message": "ok"},
		})
	}
}

func (h *EvolutionHandlers) PatchProposal() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var body struct {
			ProposedContent string `json:"proposed_content"`
		}
		if err := ctx.Bind(&body); err != nil {
			return ctx.JSON(400, map[string]any{"error": err.Error()})
		}
		if err := h.uc.Patch(ctx, id, body.ProposedContent); err != nil {
			return ctx.JSON(500, map[string]any{"error": err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret": map[string]any{"code": 0, "message": "ok"},
		})
	}
}

func (h *EvolutionHandlers) CountPending() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		count, err := h.uc.CountPending(ctx)
		if err != nil {
			return ctx.JSON(500, map[string]any{"error": err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret":   map[string]any{"code": 0, "message": "ok"},
			"count": count,
		})
	}
}

// GetConfig returns the current evolution config.
func (h *EvolutionHandlers) GetConfig() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		cfg := chat.EvolutionConfig()
		enabled := chat.EvolutionEnabled()
		return ctx.JSON(200, map[string]any{
			"ret":     map[string]any{"code": 0, "message": "ok"},
			"enabled": enabled,
			"config":  cfg,
		})
	}
}

// PutConfig updates the evolution config in memory and persists to agent_extra.yaml.
func (h *EvolutionHandlers) PutConfig() func(khttp.Context) error {
	return func(ctx khttp.Context) error {
		var body config.EvolutionConfig
		if err := ctx.Bind(&body); err != nil {
			return ctx.JSON(400, map[string]any{"error": err.Error()})
		}
		if err := chat.UpdateEvolutionConfig(&body); err != nil {
			return ctx.JSON(500, map[string]any{"error": "persist failed: " + err.Error()})
		}
		return ctx.JSON(200, map[string]any{
			"ret":     map[string]any{"code": 0, "message": "ok"},
			"enabled": body.Enabled,
		})
	}
}