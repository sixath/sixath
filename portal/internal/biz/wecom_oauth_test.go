package biz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWeComOAuth_GetUserID_Success(t *testing.T) {
	var getTokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/gettoken"):
			getTokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "tok-abc",
				"expires_in":   7200,
			})
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/auth/getuserinfo"):
			if got := r.URL.Query().Get("access_token"); got != "tok-abc" {
				t.Errorf("access_token = %q, want tok-abc", got)
			}
			if got := r.URL.Query().Get("code"); got != "auth-code-1" {
				t.Errorf("code = %q, want auth-code-1", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"errmsg":  "ok",
				"userid":  "zhangsan",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := newWeComOAuthClientForTest("corp-id", "corp-secret", srv.Client(), srv.URL)
	userid, err := client.GetUserID(context.Background(), "auth-code-1")
	if err != nil {
		t.Fatalf("GetUserID() err = %v", err)
	}
	if userid != "zhangsan" {
		t.Fatalf("userid = %q, want zhangsan", userid)
	}
	if got := getTokenCalls.Load(); got != 1 {
		t.Fatalf("gettoken calls = %d, want 1", got)
	}
}

func TestWeComOAuth_GetUserID_CachesAccessToken(t *testing.T) {
	var getTokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/gettoken"):
			getTokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "tok-cached",
				"expires_in":   7200,
			})
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/auth/getuserinfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"errmsg":  "ok",
				"userid":  "lisi",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := newWeComOAuthClientForTest("corp-id", "corp-secret", srv.Client(), srv.URL)
	if _, err := client.GetUserID(context.Background(), "code-1"); err != nil {
		t.Fatalf("first GetUserID() err = %v", err)
	}
	if _, err := client.GetUserID(context.Background(), "code-2"); err != nil {
		t.Fatalf("second GetUserID() err = %v", err)
	}
	if got := getTokenCalls.Load(); got != 1 {
		t.Fatalf("gettoken calls = %d, want 1 (token should be cached)", got)
	}
}

func TestWeComOAuth_GetUserID_GetTokenErrcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": 40013,
			"errmsg":  "invalid corpid",
		})
	}))
	defer srv.Close()

	client := newWeComOAuthClientForTest("bad-corp", "bad-secret", srv.Client(), srv.URL)
	_, err := client.GetUserID(context.Background(), "code-1")
	if err == nil {
		t.Fatal("GetUserID() err = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid corpid") {
		t.Fatalf("GetUserID() err = %q, want invalid corpid", err)
	}
}

func TestWeComOAuth_GetUserID_GetUserInfoErrcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/gettoken"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "tok-abc",
				"expires_in":   7200,
			})
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/auth/getuserinfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 40029,
				"errmsg":  "invalid code",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := newWeComOAuthClientForTest("corp-id", "corp-secret", srv.Client(), srv.URL)
	_, err := client.GetUserID(context.Background(), "bad-code")
	if err == nil {
		t.Fatal("GetUserID() err = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid code") {
		t.Fatalf("GetUserID() err = %q, want invalid code", err)
	}
}

func TestWeComOAuth_GetUserID_MissingUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/gettoken"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"errmsg":       "ok",
				"access_token": "tok-abc",
				"expires_in":   7200,
			})
		case strings.HasSuffix(r.URL.Path, "/cgi-bin/auth/getuserinfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"errmsg":  "ok",
				"openid":  "external-openid",
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := newWeComOAuthClientForTest("corp-id", "corp-secret", srv.Client(), srv.URL)
	_, err := client.GetUserID(context.Background(), "code-1")
	if !errors.Is(err, ErrWeComNoUserID) {
		t.Fatalf("GetUserID() err = %v, want ErrWeComNoUserID", err)
	}
}
