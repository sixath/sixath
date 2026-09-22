package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"backend/internal/biz"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

type weComAuthFake struct {
	enabled       bool
	publicBaseURL string
	startURL      string
	startErr      error
	ticket        string
	next          string
	callbackErr   error
	session       *biz.AuthSession
	exchangeErr   error
	lastTicket    string
}

func (f *weComAuthFake) WeComEnabled() bool { return f.enabled }
func (f *weComAuthFake) PublicBaseURL() string {
	return f.publicBaseURL
}
func (f *weComAuthFake) StartWeCom(_ context.Context, _ string) (string, error) {
	return f.startURL, f.startErr
}
func (f *weComAuthFake) HandleWeComCallback(_ context.Context, _, _ string) (string, string, error) {
	return f.ticket, f.next, f.callbackErr
}
func (f *weComAuthFake) ExchangeWeComTicket(_ context.Context, ticket string) (*biz.AuthSession, error) {
	f.lastTicket = ticket
	return f.session, f.exchangeErr
}

func TestWeComStatus_ReturnsEnabledJSON(t *testing.T) {
	uc := &weComAuthFake{enabled: true}
	srv := khttp.NewServer()
	r := srv.Route("/")
	r.GET("/api/v1/auth/wecom/status", WeComStatusHandler(uc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/wecom/status", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if !body.Enabled {
		t.Fatalf("enabled = false, want true")
	}
}

func TestWeComStart_DisabledReturns400(t *testing.T) {
	uc := &weComAuthFake{startErr: biz.ErrWeComLoginDisabled}
	srv := khttp.NewServer(
		khttp.ErrorEncoder(errorEncoder),
	)
	r := srv.Route("/")
	r.GET("/api/v1/auth/wecom/start", WeComStartHandler(uc))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/wecom/start?next=/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var rsp struct {
		Ret struct {
			Reason string `json:"reason"`
		} `json:"ret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rsp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if rsp.Ret.Reason != "WECOM_LOGIN_DISABLED" {
		t.Fatalf("reason = %q, want WECOM_LOGIN_DISABLED body=%s", rsp.Ret.Reason, rec.Body.String())
	}
}

func TestWeComExchange_HappyPath(t *testing.T) {
	uc := &weComAuthFake{
		session: &biz.AuthSession{
			Token:         "tok-1",
			UserID:        "user-1",
			Email:         "",
			Orgs:          nil,
			EmailVerified: true,
		},
	}
	srv := khttp.NewServer()
	r := srv.Route("/")
	r.POST("/api/v1/auth/wecom/exchange", WeComExchangeHandler(uc))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wecom/exchange", strings.NewReader(`{"ticket":"ticket-abc"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if uc.lastTicket != "ticket-abc" {
		t.Fatalf("ticket passed = %q, want ticket-abc", uc.lastTicket)
	}
	var body authSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.Token != "tok-1" || body.UserID != "user-1" || !body.EmailVerified {
		t.Fatalf("session = %+v, want tok-1/user-1/verified", body)
	}
}

func TestWeComFrontendCallbackURL(t *testing.T) {
	t.Parallel()

	got := weComFrontendCallbackURL("https://portal.example.com/", "t1", "/chat", nil)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Scheme != "https" || u.Host != "portal.example.com" || u.Path != "/login/wecom/callback" {
		t.Fatalf("url = %q", got)
	}
	if u.Query().Get("ticket") != "t1" || u.Query().Get("next") != "/chat" {
		t.Fatalf("query = %v", u.Query())
	}

	rel := weComFrontendCallbackURL("", "t2", "/", nil)
	ru, err := url.Parse(rel)
	if err != nil {
		t.Fatalf("parse relative: %v", err)
	}
	if ru.Path != "/login/wecom/callback" || ru.Query().Get("ticket") != "t2" {
		t.Fatalf("relative = %q", rel)
	}

	errLoc := weComFrontendCallbackURL("", "", "", biz.ErrWeComLoginDisabled)
	eu, err := url.Parse(errLoc)
	if err != nil {
		t.Fatalf("parse error loc: %v", err)
	}
	if eu.Query().Get("error") != "WECOM_LOGIN_DISABLED" {
		t.Fatalf("error loc = %q", errLoc)
	}

	internal := weComFrontendCallbackURL("", "", "", context.Canceled)
	iu, err := url.Parse(internal)
	if err != nil {
		t.Fatalf("parse internal: %v", err)
	}
	if iu.Query().Get("error") == "" {
		t.Fatalf("internal loc missing error: %q", internal)
	}
}
