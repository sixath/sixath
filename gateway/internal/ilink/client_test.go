package ilink

import (
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

func checkCommonHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
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
}

func TestGetUpdatesBodyAndCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getupdates" {
			t.Errorf("path = %s", r.URL.Path)
		}
		checkCommonHeaders(t, r)
		var body struct {
			Buf      string            `json:"get_updates_buf"`
			BaseInfo map[string]string `json:"base_info"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Buf != "buf-1" || body.BaseInfo["channel_version"] != channelVersion {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[{"message_id":7517413341670998920,"from_user_id":"u@im.wechat","message_type":1,"context_token":"ctx","create_time_ms":1760000000000,"item_list":[{"type":1,"text_item":{"text":"hi"}}]}],"get_updates_buf":"buf-2","sync_buf":"ignored"}`))
	}))
	defer srv.Close()

	upd, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "buf-1")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if upd.GetUpdatesBuf != "buf-2" || len(upd.Msgs) != 1 {
		t.Fatalf("got %+v", upd)
	}
	in := Normalize(upd.Msgs[0])
	if in.MessageID != "7517413341670998920" || in.Text != "hi" || in.ContextToken != "ctx" || in.NonText {
		t.Fatalf("inbound = %+v", in)
	}
	if !upd.Msgs[0].IsUserMessage() {
		t.Fatal("message_type 1 should be a user message")
	}
}

func TestGetUpdatesEmptyBufKeepsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[]}`))
	}))
	defer srv.Close()

	upd, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "keep")
	if err != nil || upd.GetUpdatesBuf != "keep" {
		t.Fatalf("upd=%+v err=%v", upd, err)
	}
}

func TestGetUpdatesClientTimeoutIsEmptyBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	c.PollTimeout = 50 * time.Millisecond
	upd, err := c.GetUpdates(context.Background(), "buf-1")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(upd.Msgs) != 0 || upd.GetUpdatesBuf != "buf-1" {
		t.Fatalf("got %+v", upd)
	}
}

func TestGetUpdatesParentCanceledIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := NewClient(srv.URL, "tok").GetUpdates(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSessionExpiredRetOrErrcode(t *testing.T) {
	for _, body := range []string{`{"ret":-14}`, `{"errcode":-14,"errmsg":"session timeout"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
		srv.Close()
		if !IsSessionExpired(err) {
			t.Fatalf("body %s: err = %v, want session expired", body, err)
		}
	}
}

func TestHTTPErrorIsNotSessionExpiredAndMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"context_token":"leak-ctx"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	if err == nil || IsSessionExpired(err) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "leak-ctx") {
		t.Fatalf("error leaks secret: %v", err)
	}
}

func TestSendTextBodyAndMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		checkCommonHeaders(t, r)
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Msg map[string]any `json:"msg"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
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
		if !strings.Contains(string(raw), `"text_item":{"text":"浣犲ソ"}`) {
			t.Errorf("item_list missing text: %s", raw)
		}
		_, _ = w.Write([]byte(`{"message_id":7517413341670998920}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "浣犲ソ")
	if err != nil || id != "7517413341670998920" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestNetworkErrorNamesMethodAndPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	_, err := NewClient(base, "tok").SendText(context.Background(), "u", "c", "x")
	if err == nil {
		t.Fatal("want error for closed port")
	}
	if !strings.Contains(err.Error(), "ilink POST /ilink/bot/sendmessage") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.BaseURL != DefaultBaseURL || c.PollTimeout != DefaultPollTimeout {
		t.Fatalf("got base=%q poll=%s", c.BaseURL, c.PollTimeout)
	}
	if NewClient("https://x/", "").BaseURL != "https://x" {
		t.Fatal("trailing slash not trimmed")
	}
}
