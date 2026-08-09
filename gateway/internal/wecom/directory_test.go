package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatWeComDisplayName(t *testing.T) {
	if got := formatWeComDisplayName("金伟", "Jas Jin"); got != "Jas Jin(金伟)" {
		t.Fatalf("got %q", got)
	}
	if got := formatWeComDisplayName("金伟", ""); got != "金伟" {
		t.Fatalf("got %q", got)
	}
	if got := formatWeComDisplayName("金伟", "金伟"); got != "金伟" {
		t.Fatalf("got %q", got)
	}
}

func TestDirectoryResolveDisplayName_PlainUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/gettoken"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "errmsg": "ok", "access_token": "TOK", "expires_in": 7200,
			})
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/user/get"):
			if r.URL.Query().Get("userid") != "alice" {
				t.Errorf("userid=%q", r.URL.Query().Get("userid"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "errmsg": "ok", "name": "金伟", "alias": "Jas Jin",
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := NewDirectory(DirectoryConfig{
		CorpID:  "CORP",
		Secret:  "SEC",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	got := dir.ResolveDisplayName(context.Background(), "alice")
	if got != "Jas Jin(金伟)" {
		t.Fatalf("got %q", got)
	}
	// cache hit — second call must not break even if server gone
	srv.Close()
	if got2 := dir.ResolveDisplayName(context.Background(), "alice"); got2 != got {
		t.Fatalf("cache miss: %q", got2)
	}
}

func TestDirectoryResolveDisplayName_OpenUserID(t *testing.T) {
	openID := "woudFdDgAAnOIR6h1juihhoLmJ2HZ4mQ"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/gettoken"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "access_token": "TOK", "expires_in": 7200,
			})
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/user/get"):
			uid := r.URL.Query().Get("userid")
			if uid == openID {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errcode": 60111, "errmsg": "userid not found",
				})
				return
			}
			if uid != "jasjin" {
				t.Errorf("userid=%q", uid)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "name": "金伟", "alias": "Jas Jin",
			})
		case strings.HasPrefix(r.URL.Path, "/cgi-bin/batch/openuserid_to_userid"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"userid_list": []map[string]string{
					{"open_userid": openID, "userid": "jasjin"},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := NewDirectory(DirectoryConfig{
		CorpID:  "CORP",
		Secret:  "SEC",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	got := dir.ResolveDisplayName(context.Background(), openID)
	if got != "Jas Jin(金伟)" {
		t.Fatalf("got %q", got)
	}
}

func TestDirectoryResolveDisplayName_FallsBackToUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 40001, "errmsg": "invalid credential"})
	}))
	t.Cleanup(srv.Close)

	dir := NewDirectory(DirectoryConfig{
		CorpID:  "CORP",
		Secret:  "BAD",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})
	got := dir.ResolveDisplayName(context.Background(), "alice")
	if got != "alice" {
		t.Fatalf("got %q want alice fallback", got)
	}
}

func TestNewDirectory_RequiresCreds(t *testing.T) {
	if NewDirectory(DirectoryConfig{}) != nil {
		t.Fatal("expected nil without creds")
	}
}

func TestNormalizeMsgBody_UsesFromName(t *testing.T) {
	body := []byte(`{"msgid":"M1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice","name":"金伟","alias":"Jas Jin"},"msgtype":"text","text":{"content":"hi"}}`)
	n, err := NormalizeMsgBody(body, NormalizeOpts{BotID: "BOT"})
	if err != nil {
		t.Fatal(err)
	}
	if n.AskerName != "Jas Jin(金伟)" {
		t.Fatalf("AskerName=%q", n.AskerName)
	}
}

func TestWithAskerName(t *testing.T) {
	n := Normalized{AskerID: "alice", AskerName: "alice", QuestionText: "hi"}
	n = n.WithAskerName("Jas Jin(金伟)")
	if n.AskerName != "Jas Jin(金伟)" {
		t.Fatal(n.AskerName)
	}
	want := "[企微] 发起人=Jas Jin(金伟)(alice)\n问题：hi"
	if n.RuntimeContent != want {
		t.Fatalf("RuntimeContent=%q want %q", n.RuntimeContent, want)
	}
}
