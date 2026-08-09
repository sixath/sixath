package server

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type createOrgRequest struct {
	Name string `json:"name"`
}

type createInviteRequest struct {
	MaxUses        int `json:"max_uses"`
	ExpiresInHours int `json:"expires_in_hours"`
}

type orgResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type inviteResponse struct {
	ID        string     `json:"id"`
	MaxUses   int        `json:"max_uses"`
	UsedCount int        `json:"used_count"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type createInviteResponse struct {
	inviteResponse
	InviteToken string `json:"invite_token"`
	InvitePath  string `json:"invite_path"`
}

func toInviteResponse(invite *biz.OrgInvite) inviteResponse {
	return inviteResponse{
		ID:        invite.ID,
		MaxUses:   invite.MaxUses,
		UsedCount: invite.UsedCount,
		ExpiresAt: invite.ExpiresAt,
		RevokedAt: invite.RevokedAt,
		CreatedAt: invite.CreatedAt,
	}
}

func CreateOrgHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createOrgRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.CreateOrg(c, strings.TrimSpace(body.Name))
		})
		if err != nil {
			return err
		}
		org := out.(*biz.Org)
		return ctx.JSON(200, orgResponse{ID: org.ID, Name: org.Name})
	}
}

func ListOrgsHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.ListMyOrgs(c)
		})
		if err != nil {
			return err
		}
		orgs := out.([]biz.OrgMembership)
		items := make([]orgMembershipResponse, len(orgs))
		for i, org := range orgs {
			items[i] = orgMembershipResponse{ID: org.OrgID, Name: org.Name, Role: org.Role}
		}
		return ctx.JSON(200, map[string]any{"orgs": items})
	}
}

func CreateInviteHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body createInviteRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		orgID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			plain, invite, err := uc.CreateInvite(c, orgID, body.MaxUses, body.ExpiresInHours)
			if err != nil {
				return nil, err
			}
			return &createInviteResponse{
				inviteResponse: toInviteResponse(invite),
				InviteToken:    plain,
				InvitePath:     invitePath(plain),
			}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func ListInvitesHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		orgID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.ListInvites(c, orgID)
		})
		if err != nil {
			return err
		}
		invites := out.([]*biz.OrgInvite)
		items := make([]inviteResponse, len(invites))
		for i, invite := range invites {
			items[i] = toInviteResponse(invite)
		}
		return ctx.JSON(200, map[string]any{"invites": items})
	}
}

func RevokeInviteHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		orgID := ctx.Vars().Get("id")
		inviteID := ctx.Vars().Get("invite_id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.RevokeInvite(c, orgID, inviteID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}
