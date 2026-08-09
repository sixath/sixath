package server

import (
	"context"
	"strings"

	"backend/internal/biz"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type orgMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type resourceGrantRequest struct {
	GranteeType string `json:"grantee_type"`
	GranteeID   string `json:"grantee_id"`
	Perm        string `json:"perm"`
}

func AddOrgMemberHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body orgMemberRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		orgID := ctx.Vars().Get("id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.AddOrgMember(c, orgID, strings.TrimSpace(body.UserID), strings.TrimSpace(body.Role))
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func CreateResourceGrantHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body resourceGrantRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		resourceID := ctx.Vars().Get("id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.CreateGrant(c, resourceID, strings.TrimSpace(body.GranteeType), strings.TrimSpace(body.GranteeID), biz.Perm(strings.TrimSpace(body.Perm)))
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func IssueUserTokenHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		userID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.IssueUserToken(c, userID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]string{"token": out.(string)})
	}
}
