package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// weComAuth is the AuthUsecase surface used by WeCom SSO HTTP handlers.
type weComAuth interface {
	WeComEnabled() bool
	PublicBaseURL() string
	StartWeCom(ctx context.Context, next string) (ssoURL string, err error)
	HandleWeComCallback(ctx context.Context, code, state string) (ticket, next string, err error)
	ExchangeWeComTicket(ctx context.Context, ticket string) (*biz.AuthSession, error)
}

type weComExchangeRequest struct {
	Ticket string `json:"ticket"`
}

// WeComStatusHandler GET /api/v1/auth/wecom/status → { "enabled": bool }.
func WeComStatusHandler(uc weComAuth) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		return ctx.JSON(200, map[string]any{"enabled": uc.WeComEnabled()})
	}
}

// WeComStartHandler GET /api/v1/auth/wecom/start?next= → 302 to WeCom SSO URL.
func WeComStartHandler(uc weComAuth) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		next := ctx.Request().URL.Query().Get("next")
		ssoURL, err := uc.StartWeCom(ctx, next)
		if err != nil {
			return err
		}
		w := ctx.Response()
		http.Redirect(w, ctx.Request(), ssoURL, http.StatusFound)
		return nil
	}
}

// WeComCallbackHandler GET /api/v1/auth/wecom/callback?code=&state= → 302 frontend callback.
func WeComCallbackHandler(uc weComAuth) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		q := ctx.Request().URL.Query()
		code := q.Get("code")
		state := q.Get("state")
		ticket, next, err := uc.HandleWeComCallback(ctx, code, state)
		loc := weComFrontendCallbackURL(uc.PublicBaseURL(), ticket, next, err)
		w := ctx.Response()
		http.Redirect(w, ctx.Request(), loc, http.StatusFound)
		return nil
	}
}

// WeComExchangeHandler POST /api/v1/auth/wecom/exchange { "ticket" } → auth session.
func WeComExchangeHandler(uc weComAuth) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var body weComExchangeRequest
		if err := ctx.Bind(&body); err != nil {
			return err
		}
		session, err := uc.ExchangeWeComTicket(ctx, strings.TrimSpace(body.Ticket))
		if err != nil {
			return err
		}
		return ctx.JSON(200, toAuthSessionResponse(session))
	}
}

func weComFrontendCallbackURL(publicBaseURL, ticket, next string, err error) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	path := "/login/wecom/callback"
	if base != "" {
		path = base + path
	}
	q := url.Values{}
	if err != nil {
		reason := errors.FromError(err).Reason
		if reason == "" {
			reason = "INTERNAL"
		}
		q.Set("error", reason)
	} else {
		q.Set("ticket", ticket)
		q.Set("next", next)
	}
	return path + "?" + q.Encode()
}
