package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"
)

type fakeGatewayChannels struct {
	list []*biz.ChannelMeta
	byID map[string]*biz.ChannelMeta
	err  error
}

func (f *fakeGatewayChannels) GetByChannelID(_ context.Context, channelID string) (*biz.ChannelMeta, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch, ok := f.byID[channelID]
	if !ok {
		return nil, biz.ErrChannelNotFound
	}
	return ch, nil
}

func (f *fakeGatewayChannels) ListGatewayChannels(context.Context) ([]*biz.ChannelMeta, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

type fakeChannelRuntimeRepo struct {
	byID map[string]*biz.RuntimeStatusRow
}

func (f *fakeChannelRuntimeRepo) Get(_ context.Context, channelID string) (*biz.RuntimeStatusRow, error) {
	row, ok := f.byID[channelID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakeChannelRuntimeRepo) Upsert(_ context.Context, channelID string, patch biz.RuntimeStatusPatch) error {
	if channelID == "" || patch.State == "" {
		return pkgErrors.ErrNotFound
	}
	now := time.Now().UTC()
	row, ok := f.byID[channelID]
	if !ok {
		row = &biz.RuntimeStatusRow{ChannelID: channelID}
		f.byID[channelID] = row
	}
	row.State = patch.State
	row.LastHeartbeatAt = now
	row.UpdatedAt = now
	if patch.LastError != nil {
		row.LastError = *patch.LastError
	}
	if patch.ReconnectAttempt != nil {
		row.ReconnectAttempt = *patch.ReconnectAttempt
	}
	if patch.ReconnectInMs != nil {
		row.ReconnectInMs = *patch.ReconnectInMs
	}
	if patch.GatewayInstanceID != nil {
		row.GatewayInstanceID = *patch.GatewayInstanceID
	}
	if patch.State == "connected" {
		row.LastError = ""
		row.ReconnectAttempt = 0
		row.ReconnectInMs = 0
	}
	return nil
}

func TestGatewayListChannels_IncludesDisabled(t *testing.T) {
	updated := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	channels := &fakeGatewayChannels{
		list: []*biz.ChannelMeta{
			{
				ChannelID:        "demo-webhook",
				Type:             "webhook",
				Enabled:          false,
				WebhookSecret:    "wh-secret",
				IPWhitelist:      []string{},
				DefaultReplyMode: "async",
				UpdatedAt:        updated,
			},
			{
				ChannelID: "sixath4",
				Type:      "wecom_bot",
				Enabled:   true,
				BotID:     "bot-1",
				BotSecret: "bot-secret",
				BotNames:  []string{"sixath"},
				WSURL:     "wss://openws.work.weixin.qq.com",
				UpdatedAt: updated,
			},
		},
		byID: map[string]*biz.ChannelMeta{},
	}
	svc := newTestService(nil, nil, nil)
	svc.channels = channels
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodGet, "/runtime/v1/gateway/channels", "", "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Channels []map[string]any `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(body.Channels) != 2 {
		t.Fatalf("channels len = %d, want 2; body=%s", len(body.Channels), rec.Body.String())
	}

	wh := body.Channels[0]
	if wh["id"] != "demo-webhook" || wh["type"] != "webhook" || wh["enabled"] != false {
		t.Fatalf("webhook item = %+v", wh)
	}
	if wh["webhook_secret"] != "wh-secret" {
		t.Fatalf("expected plaintext webhook_secret, got %#v", wh["webhook_secret"])
	}
	if wh["default_reply_mode"] != "async" {
		t.Fatalf("default_reply_mode = %#v", wh["default_reply_mode"])
	}

	bot := body.Channels[1]
	if bot["id"] != "sixath4" || bot["type"] != "wecom_bot" || bot["enabled"] != true {
		t.Fatalf("wecom_bot item = %+v", bot)
	}
	if bot["secret"] != "bot-secret" || bot["bot_id"] != "bot-1" {
		t.Fatalf("expected plaintext bot fields, got %+v", bot)
	}
}

func TestGatewayListChannels_RequiresToken(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.channels = &fakeGatewayChannels{list: nil, byID: map[string]*biz.ChannelMeta{}}
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodGet, "/runtime/v1/gateway/channels", "", "", false)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostChannelStatus_UnknownChannel404(t *testing.T) {
	channels := &fakeGatewayChannels{byID: map[string]*biz.ChannelMeta{}}
	runtimeRepo := &fakeChannelRuntimeRepo{byID: map[string]*biz.RuntimeStatusRow{}}
	svc := newTestService(nil, nil, nil)
	svc.channels = channels
	svc.runtimeStatus = runtimeRepo
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodPost, "/runtime/v1/gateway/channels/missing/status",
		`{"state":"connected"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if len(runtimeRepo.byID) != 0 {
		t.Fatalf("expected no ghost runtime rows, got %+v", runtimeRepo.byID)
	}
}

func TestPostChannelStatus_ConnectedClearsError(t *testing.T) {
	channels := &fakeGatewayChannels{byID: map[string]*biz.ChannelMeta{
		"sixath4": {ChannelID: "sixath4", Type: "wecom_bot", Enabled: true},
	}}
	runtimeRepo := &fakeChannelRuntimeRepo{byID: map[string]*biz.RuntimeStatusRow{
		"sixath4": {
			ChannelID:        "sixath4",
			State:            "reconnecting",
			LastError:        "boom",
			ReconnectAttempt: 3,
			ReconnectInMs:    8000,
		},
	}}
	svc := newTestService(nil, nil, nil)
	svc.channels = channels
	svc.runtimeStatus = runtimeRepo
	srv := testRuntimeServer(t, svc)

	req := runtimeReq(http.MethodPost, "/runtime/v1/gateway/channels/sixath4/status",
		`{"state":"connected","gateway_instance_id":"gw-1"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	row, err := runtimeRepo.Get(context.Background(), "sixath4")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.State != "connected" {
		t.Fatalf("state = %q, want connected", row.State)
	}
	if row.LastError != "" || row.ReconnectAttempt != 0 || row.ReconnectInMs != 0 {
		t.Fatalf("expected cleared error/reconnect, got last_error=%q attempt=%d in_ms=%d",
			row.LastError, row.ReconnectAttempt, row.ReconnectInMs)
	}
	if row.GatewayInstanceID != "gw-1" {
		t.Fatalf("gateway_instance_id = %q, want gw-1", row.GatewayInstanceID)
	}
}
