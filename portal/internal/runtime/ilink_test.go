package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/ilink"
)

type fakeIlinkBackend struct {
	allowed     map[string][]string
	reportSlug  string
	reportGen   int
	report      biz.IlinkPeerReport
	reportErr   error
	cursor      *biz.IlinkCursor
	putSlug     string
	put         *biz.IlinkCursor
	putErr      error
	expiredSlug string
	expiredGen  int
}

func (f *fakeIlinkBackend) AllowedPeers(_ context.Context, channelUUID string) ([]string, error) {
	return f.allowed[channelUUID], nil
}

func (f *fakeIlinkBackend) ReportPeer(_ context.Context, channelID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error) {
	f.reportSlug, f.reportGen, f.report = channelID, generation, in
	if f.reportErr != nil {
		return nil, f.reportErr
	}
	return &biz.IlinkPeer{PeerID: in.PeerID, Allowed: true}, nil
}

func (f *fakeIlinkBackend) GetCursor(context.Context, string) (*biz.IlinkCursor, error) {
	return f.cursor, nil
}

func (f *fakeIlinkBackend) PutCursor(_ context.Context, channelID string, cur biz.IlinkCursor) error {
	f.putSlug = channelID
	f.put = &cur
	return f.putErr
}

func (f *fakeIlinkBackend) ReportSessionExpired(_ context.Context, channelID string, generation int) error {
	f.expiredSlug, f.expiredGen = channelID, generation
	return nil
}

func serveIlinkRuntime(t *testing.T, svc *Service, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := testRuntimeServer(t, svc)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, runtimeReq(method, path, body, "", true))
	return rec
}

func TestGatewayListChannels_WeixinIlinkFields(t *testing.T) {
	expired := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	svc := newTestService(nil, nil, nil)
	svc.channels = &fakeGatewayChannels{byID: map[string]*biz.ChannelMeta{}, list: []*biz.ChannelMeta{{
		ID: "uuid-1", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink, Enabled: true,
		IlinkBotToken: "bt", IlinkGeneration: 4, IlinkSessionExpiredAt: &expired,
	}}}
	svc.SetIlink(&fakeIlinkBackend{allowed: map[string][]string{"uuid-1": {"p1@im.wechat"}}})

	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Channels []map[string]any `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Channels[0]
	if item["id"] != "wx-1" || item["bot_token"] != "bt" || item["base_url"] != ilink.DefaultBaseURL {
		t.Fatalf("item = %+v", item)
	}
	if item["generation"] != float64(4) || item["session_expired"] != true {
		t.Fatalf("generation/session_expired = %v / %v", item["generation"], item["session_expired"])
	}
	peers, _ := item["allowed_peers"].([]any)
	if len(peers) != 1 || peers[0] != "p1@im.wechat" {
		t.Fatalf("allowed_peers = %v", item["allowed_peers"])
	}
}

func TestIlinkReportPeer_PassesThrough(t *testing.T) {
	backend := &fakeIlinkBackend{}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers",
		`{"generation":4,"peer_id":"p1","context_token":"c","preview":"hi","at":"2026-10-18T02:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.reportSlug != "wx-1" || backend.reportGen != 4 || backend.report.PeerID != "p1" || backend.report.ContextToken != "c" {
		t.Fatalf("backend got slug=%q gen=%d report=%+v", backend.reportSlug, backend.reportGen, backend.report)
	}
	if !backend.report.At.Equal(time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("at = %v", backend.report.At)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["allowed"] != true {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestIlinkReportPeer_GenerationMismatchIs409(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{reportErr: biz.ErrIlinkGenerationMismatch})

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", `{"generation":1,"peer_id":"p1"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestIlinkReportPeer_BadAt(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{})

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", `{"peer_id":"p1","at":"yesterday"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIlinkCursor_GetAndPut(t *testing.T) {
	backend := &fakeIlinkBackend{cursor: &biz.IlinkCursor{Generation: 4, GetUpdatesBuf: "buf-1"}}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["generation"] != float64(4) || got["get_updates_buf"] != "buf-1" {
		t.Fatalf("GET body = %s", rec.Body.String())
	}

	rec = serveIlinkRuntime(t, svc, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", `{"generation":4,"get_updates_buf":"buf-2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.putSlug != "wx-1" || backend.put == nil || backend.put.Generation != 4 || backend.put.GetUpdatesBuf != "buf-2" {
		t.Fatalf("put = %q %+v", backend.putSlug, backend.put)
	}

	backend.putErr = biz.ErrIlinkGenerationMismatch
	rec = serveIlinkRuntime(t, svc, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", `{"generation":3,"get_updates_buf":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want 409", rec.Code)
	}
}

func TestIlinkSessionExpired(t *testing.T) {
	backend := &fakeIlinkBackend{}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/session-expired", `{"generation":4,"reason":"-14"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.expiredSlug != "wx-1" || backend.expiredGen != 4 {
		t.Fatalf("expired = %q %d", backend.expiredSlug, backend.expiredGen)
	}
}

func TestIlinkEndpoints_UnavailableWithoutBackend(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestIlinkEndpoints_RequireToken(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{})
	srv := testRuntimeServer(t, svc)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, runtimeReq(http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "", "", false))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
