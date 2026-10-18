package ilink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSendTextHeadersBodyAndMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth headers = %q / %q", r.Header.Get("AuthorizationType"), r.Header.Get("Authorization"))
		}
		uin, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WECHAT-UIN"))
		if err != nil {
			t.Errorf("X-WECHAT-UIN not base64: %v", err)
		} else if _, err := strconv.ParseUint(string(uin), 10, 32); err != nil {
			t.Errorf("X-WECHAT-UIN not uint32 decimal: %q", uin)
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Msg      map[string]any    `json:"msg"`
			BaseInfo map[string]string `json:"base_info"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.BaseInfo["channel_version"] != channelVersion {
			t.Errorf("base_info = %v", body.BaseInfo)
		}
		m := body.Msg
		if v, ok := m["from_user_id"]; !ok || v != "" {
			t.Errorf("from_user_id = %v (present=%v)", v, ok)
		}
		if m["to_user_id"] != "u@im.wechat" || m["context_token"] != "ctx-1" {
			t.Errorf("to/context_token = %v / %v", m["to_user_id"], m["context_token"])
		}
		if m["message_type"] != float64(2) || m["message_state"] != float64(2) {
			t.Errorf("message_type/state = %v / %v", m["message_type"], m["message_state"])
		}
		if id, _ := m["client_id"].(string); !strings.HasPrefix(id, "sixath:") {
			t.Errorf("client_id = %v", m["client_id"])
		}
		if !strings.Contains(string(raw), `"text_item":{"text":"你好"}`) {
			t.Errorf("item_list missing text: %s", raw)
		}
		_, _ = w.Write([]byte(`{"message_id":7517413341670998920}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "你好")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if id != "7517413341670998920" {
		t.Fatalf("message id = %q", id)
	}
}

func TestSendTextStringMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message_id":"abc"}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err != nil || id != "abc" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestSendTextNullMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message_id":null}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestNetworkErrorDoesNotLeakQRCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	_, err := NewClient(base, "").GetQRCodeStatus(context.Background(), "secret-qr")
	if err == nil {
		t.Fatal("want error for closed port")
	}
	if strings.Contains(err.Error(), "secret-qr") {
		t.Fatalf("error leaks qrcode: %v", err)
	}
	if !strings.Contains(err.Error(), "/ilink/bot/get_qrcode_status") {
		t.Fatalf("error lost path: %v", err)
	}
}

func TestSendTextEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestSessionExpiredErrcodeOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if !IsSessionExpired(err) {
		t.Fatalf("err = %v, want session expired", err)
	}
}

func TestSessionExpiredRet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if !IsSessionExpired(err) {
		t.Fatalf("err = %v, want session expired", err)
	}
}

func TestHTTPErrorMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"bot_token":"leak-bt","context_token":"leak-ctx"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err == nil {
		t.Fatal("want error for 502")
	}
	if IsSessionExpired(err) {
		t.Fatalf("502 must not be session expired: %v", err)
	}
	if strings.Contains(err.Error(), "leak-") {
		t.Fatalf("error leaks secrets: %v", err)
	}
}

func TestGetBotQRCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/get_bot_qrcode" || r.URL.Query().Get("bot_type") != "3" {
			t.Errorf("url = %s", r.URL.String())
		}
		if r.Header.Get("iLink-App-ClientVersion") != "1" {
			t.Errorf("missing iLink-App-ClientVersion")
		}
		_, _ = w.Write([]byte(`{"qrcode":"qrc-1","qrcode_img_content":"https://liteapp.weixin.qq.com/q/x","ret":0}`))
	}))
	defer srv.Close()

	qr, err := NewClient(srv.URL, "").GetBotQRCode(context.Background())
	if err != nil {
		t.Fatalf("GetBotQRCode: %v", err)
	}
	if qr.QRCode != "qrc-1" || qr.ImgContent != "https://liteapp.weixin.qq.com/q/x" {
		t.Fatalf("got %+v", qr)
	}
}

func TestGetQRCodeStatusConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qrcode") != "qrc 1" {
			t.Errorf("qrcode = %q", r.URL.Query().Get("qrcode"))
		}
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bt","ilink_bot_id":"b@im.bot","ilink_user_id":"u@im.wechat","baseurl":"https://x","ret":0}`))
	}))
	defer srv.Close()

	s, err := NewClient(srv.URL, "").GetQRCodeStatus(context.Background(), "qrc 1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "confirmed" || s.BotToken != "bt" || s.BotID != "b@im.bot" || s.UserID != "u@im.wechat" || s.BaseURL != "https://x" {
		t.Fatalf("got %+v", s)
	}
}

func TestGetQRCodeStatusClientTimeoutIsWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	c.PollTimeout = 50 * time.Millisecond
	s, err := c.GetQRCodeStatus(context.Background(), "qrc-1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "wait" || !s.ClientTimeout {
		t.Fatalf("got %+v", s)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.BaseURL != DefaultBaseURL || c.PollTimeout != DefaultLoginPollTimeout {
		t.Fatalf("got base=%q poll=%s", c.BaseURL, c.PollTimeout)
	}
	if NewClient("https://x/", "").BaseURL != "https://x" {
		t.Fatal("trailing slash not trimmed")
	}
}
