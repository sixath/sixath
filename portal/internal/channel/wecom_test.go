package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPushToWeCom_TextSuccess(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	err := PushToWeCom(context.Background(), srv.URL, "hello", "text")
	if err != nil {
		t.Fatalf("PushToWeCom() error = %v", err)
	}
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %v, want text", gotBody["msgtype"])
	}
	text, ok := gotBody["text"].(map[string]any)
	if !ok {
		t.Fatalf("text payload missing: %#v", gotBody)
	}
	if text["content"] != "hello" {
		t.Fatalf("content = %v, want hello", text["content"])
	}
}

func TestPushToWeCom_MarkdownPayload(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	err := PushToWeCom(context.Background(), srv.URL, "# title", "markdown")
	if err != nil {
		t.Fatalf("PushToWeCom() error = %v", err)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", gotBody["msgtype"])
	}
	md, ok := gotBody["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown payload missing: %#v", gotBody)
	}
	if md["content"] != "# title" {
		t.Fatalf("content = %v, want # title", md["content"])
	}
}

func TestPushToWeCom_ErrCodeNonZero(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	t.Cleanup(srv.Close)

	err := PushToWeCom(context.Background(), srv.URL, "hello", "text")
	if err == nil {
		t.Fatal("PushToWeCom() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "errcode=93000") {
		t.Fatalf("error = %q, want errcode=93000", err.Error())
	}
}

func TestPushToWeCom_EmptyURL(t *testing.T) {
	t.Parallel()
	err := PushToWeCom(context.Background(), "", "hello", "text")
	if err != nil {
		t.Fatalf("PushToWeCom() error = %v, want nil", err)
	}
}
