package server

import (
	"context"
	"strconv"
	"strings"
	"time"

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

type memberInfoResponse struct {
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

func ListOrgMembersHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		orgID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			members, err := uc.ListOrgMembers(c, orgID)
			if err != nil {
				return nil, err
			}
			items := make([]memberInfoResponse, len(members))
			for i, m := range members {
				items[i] = memberInfoResponse{UserID: m.UserID, UserName: m.UserName, Role: m.Role, CreatedAt: m.CreatedAt.Format(time.RFC3339)}
			}
			return map[string]any{"members": items}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func RemoveOrgMemberHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		orgID := ctx.Vars().Get("id")
		userID := ctx.Vars().Get("user_id")
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.RemoveOrgMember(c, orgID, userID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

func UpdateMemberRoleHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		orgID := ctx.Vars().Get("id")
		userID := ctx.Vars().Get("user_id")
		var body updateMemberRoleRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.UpdateMemberRole(c, orgID, userID, body.Role)
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

type userSummaryResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

func ListUsersHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		q := strings.TrimSpace(ctx.Query().Get("q"))
		limit := 0
		if raw := strings.TrimSpace(ctx.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				limit = n
			}
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.ListUsers(c, q, limit)
		})
		if err != nil {
			return err
		}
		users := out.([]biz.UserSummary)
		items := make([]userSummaryResponse, len(users))
		for i, u := range users {
			items[i] = userSummaryResponse{ID: u.ID, Name: u.Name, Email: u.Email}
		}
		return ctx.JSON(200, map[string]any{"users": items})
	}
}

type grantResponse struct {
	GranteeType string `json:"grantee_type"`
	GranteeID   string `json:"grantee_id"`
	Perm        string `json:"perm"`
}

func ListGrantsHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		resourceID := ctx.Vars().Get("id")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			grants, err := uc.ListGrants(c, resourceID)
			if err != nil {
				return nil, err
			}
			items := make([]grantResponse, len(grants))
			for i, g := range grants {
				items[i] = grantResponse{GranteeType: g.GranteeType, GranteeID: g.GranteeID, Perm: string(g.Perm)}
			}
			return map[string]any{"grants": items}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func DeleteGrantHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		resourceID := ctx.Vars().Get("id")
		r, _ := kratoshttp.RequestFromServerContext(ctx)
		granteeType := ""
		granteeID := ""
		if r != nil {
			granteeType = r.URL.Query().Get("grantee_type")
			granteeID = r.URL.Query().Get("grantee_id")
		}
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, uc.DeleteGrant(c, resourceID, granteeType, granteeID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func GetResourceByPayloadHandler(uc *biz.ACLAPIUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		resourceType := ctx.Vars().Get("type")
		payloadRef := ctx.Vars().Get("ref")
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return uc.GetResourceByPayload(c, biz.ResourceType(resourceType), payloadRef)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}