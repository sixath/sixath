package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPostSetsHeadersAndBaseInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
			t.Errorf("AuthorizationType = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q", got)
		}
		uin, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WECHAT-UIN"))
		if err != nil {
			t.Errorf("X-WECHAT-UIN not base64: %v", err)
		}
		if _, err := strconv.ParseUint(string(uin), 10, 32); err != nil {
			t.Errorf("X-WECHAT-UIN not uint32 decimal: %q", uin)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		info, _ := body["base_info"].(map[string]any)
		if info["channel_version"] != channelVersion {
			t.Errorf("base_info = %v", body["base_info"])
		}
		if body["get_updates_buf"] != "buf-0" {
			t.Errorf("get_updates_buf = %v", body["get_updates_buf"])
		}
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[],"get_updates_buf":"buf-1"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok-1")
	up, err := c.GetUpdates(context.Background(), "buf-0")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if up.GetUpdatesBuf != "buf-1" {
		t.Fatalf("cursor = %q, want buf-1", up.GetUpdatesBuf)
	}
}

func TestGetUpdatesSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14,"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.SessionExpired() {
		t.Fatalf("err = %v, want session expired APIError", err)
	}
}

func TestErrcodeOnlySessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.SessionExpired() {
		t.Fatalf("err = %v, want session expired APIError", err)
	}
}

func TestGetUpdatesClientTimeoutReturnsEmptyBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	c.PollTimeout = 50 * time.Millisecond
	up, err := c.GetUpdates(context.Background(), "keep-me")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(up.Msgs) != 0 || up.GetUpdatesBuf != "keep-me" {
		t.Fatalf("got %+v, want empty batch with same cursor", up)
	}
}

func TestGetQRCodeStatusClientTimeoutIsWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("iLink-App-ClientVersion") != "1" {
			t.Errorf("missing iLink-App-ClientVersion")
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	c.PollTimeout = 50 * time.Millisecond
	s, err := c.GetQRCodeStatus(context.Background(), "qrc_1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "wait" || !s.ClientTimeout {
		t.Fatalf("got %+v, want wait with ClientTimeout", s)
	}
}

func TestGetQRCodeStatusConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qrcode") != "qrc_1" {
			t.Errorf("qrcode = %q", r.URL.Query().Get("qrcode"))
		}
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bt","ilink_bot_id":"b@im.bot","ilink_user_id":"u@im.wechat","baseurl":"https://x"}`))
	}))
	defer srv.Close()

	s, err := NewClient(srv.URL, "").GetQRCodeStatus(context.Background(), "qrc_1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "confirmed" || s.BotToken != "bt" || s.BotID != "b@im.bot" || s.UserID != "u@im.wechat" || s.BaseURL != "https://x" {
		t.Fatalf("got %+v", s)
	}
}

func TestSendTextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _, _ = w.Write([]byte(`{}`)) }()
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body struct {
			Msg map[string]any `json:"msg"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		m := body.Msg
		if v, ok := m["from_user_id"]; !ok || v != "" {
			t.Errorf("from_user_id = %v (present=%v), want empty string", v, ok)
		}
		if m["to_user_id"] != "u@im.wechat" || m["context_token"] != "ctx-1" {
			t.Errorf("to/context_token = %v / %v", m["to_user_id"], m["context_token"])
		}
		if m["message_type"] != float64(2) || m["message_state"] != float64(2) {
			t.Errorf("message_type/state = %v / %v", m["message_type"], m["message_state"])
		}
		items, ok := m["item_list"].([]any)
		if !ok || len(items) != 1 {
			t.Errorf("item_list = %v", m["item_list"])
			return
		}
		item, ok := items[0].(map[string]any)
		if !ok {
			t.Errorf("item = %v", items[0])
			return
		}
		textItem, ok := item["text_item"].(map[string]any)
		if !ok || item["type"] != float64(1) || textItem["text"] != "你好" {
			t.Errorf("item = %v", item)
		}
	}))
	defer srv.Close()

	if err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "你好"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	if err == nil {
		t.Fatal("want error for 502")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("502 should not be APIError: %v", err)
	}
}

func TestRawLogMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"secret-bt","msgs":[{"context_token": "secret-ctx"}]}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c := NewClient(srv.URL, "")
	c.RawLog = &buf
	if _, err := c.GetQRCodeStatus(context.Background(), "q"); err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "secret-bt") || strings.Contains(out, "secret-ctx") {
		t.Fatalf("raw log leaked secrets: %s", out)
	}
	if !strings.Contains(out, `"bot_token":"***"`) || !strings.Contains(out, "get_qrcode_status") {
		t.Fatalf("raw log = %s", out)
	}
}

func TestMessageText(t *testing.T) {
	m := Message{ItemList: []MessageItem{
		{Type: itemTypeText, TextItem: &TextItem{Text: "a"}},
		{Type: 2},
		{Type: itemTypeText, TextItem: &TextItem{Text: "b"}},
	}}
	if got := m.Text(); got != "a\nb" {
		t.Fatalf("Text() = %q", got)
	}
	if got := m.ItemTypes(); len(got) != 3 || got[1] != 2 {
		t.Fatalf("ItemTypes() = %v", got)
	}
}
