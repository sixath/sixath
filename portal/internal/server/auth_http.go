package server

import (
	"net/url"
	"strings"

	"backend/internal/biz"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Invite   string `json:"invite"`
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

type orgMembershipResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type authSessionResponse struct {
	Token         string                  `json:"token"`
	UserID        string                  `json:"user_id"`
	Email         string                  `json:"email"`
	Orgs          []orgMembershipResponse `json:"orgs"`
	EmailVerified bool                    `json:"email_verified"`
}

func toAuthSessionResponse(session *biz.AuthSession) authSessionResponse {
	orgs := make([]orgMembershipResponse, len(session.Orgs))
	for i, org := range session.Orgs {
		orgs[i] = orgMembershipResponse{ID: org.OrgID, Name: org.Name, Role: org.Role}
	}
	return authSessionResponse{
		Token:         session.Token,
		UserID:        session.UserID,
		Email:         session.Email,
		Orgs:          orgs,
		EmailVerified: session.EmailVerified,
	}
}

func LoginHandler(uc *biz.AuthUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body loginRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		session, err := uc.Login(ctx, strings.TrimSpace(body.Email), body.Password)
		if err != nil {
			return err
		}
		return ctx.JSON(200, toAuthSessionResponse(session))
	}
}

func RegisterHandler(uc *biz.AuthUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body registerRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		session, err := uc.Register(ctx, strings.TrimSpace(body.Email), body.Password, strings.TrimSpace(body.Invite))
		if err != nil {
			return err
		}
		return ctx.JSON(200, toAuthSessionResponse(session))
	}
}

func PreviewInviteHandler(uc *biz.AuthUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		preview, err := uc.PreviewInvite(ctx, ctx.Vars().Get("token"))
		if err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{
			"org_name": preview.OrgName,
			"valid":    preview.Valid,
		})
	}
}

func VerifyEmailHandler(uc *biz.AuthUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body verifyEmailRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		if err := uc.VerifyEmail(ctx, strings.TrimSpace(body.Token)); err != nil {
			return err
		}
		return ctx.JSON(200, map[string]any{"ok": true})
	}
}

func invitePath(plainToken string) string {
	return "/register?invite=" + url.QueryEscape(plainToken)
}
