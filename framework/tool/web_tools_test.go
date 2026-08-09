package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sixath/framework/tool/web"
)

func TestValidateOutboundURL_BlocksPrivateIP(t *testing.T) {
	if err := ValidateOutboundURL("http://127.0.0.1/secret"); err == nil {
		t.Fatal("expected block for loopback")
	}
	if err := ValidateOutboundURL("http://localhost/secret"); err == nil {
		t.Fatal("expected block for localhost")
	}
}

func TestWebSearchTool_UsesBackend(t *testing.T) {
	backend := web.NewBochaBackend(web.BochaConfig{APIKey: "k"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"webPages":{"value":[{"name":"Hit","url":"https://x.test","snippet":"s"}]}}`))
	}))
	defer srv.Close()
	backend = web.NewBochaBackend(web.BochaConfig{
		APIKey:     "k",
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})

	reg := NewRegistry()
	if err := RegisterWebTools(reg, &WebToolsConfig{SearchBackend: backend}); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("web_search")
	if !ok {
		t.Fatal("missing web_search")
	}
	if err := tl.CheckFn(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	resp := res.(*web.SearchResponse)
	if len(resp.Results) != 1 || resp.Results[0].Title != "Hit" {
		t.Fatalf("%#v", res)
	}
}

func TestWebSearchTool_HiddenWithoutAPIKey(t *testing.T) {
	reg := NewRegistry()
	_ = RegisterWebTools(reg, &WebToolsConfig{
		SearchBackend: web.NewBochaBackend(web.BochaConfig{}),
	})
	tl, _ := reg.Get("web_search")
	if tl.CheckFn == nil {
		t.Fatal("expected check fn")
	}
	if err := tl.CheckFn(context.Background()); err == nil {
		t.Fatal("expected check failure")
	}
}

func TestNewWebSearchBackend_ExplicitKey(t *testing.T) {
	b := NewWebSearchBackend("bocha", "yaml-key", "")
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.Name() != "bocha" {
		t.Fatalf("name: %s", b.Name())
	}
}

func TestWebExtractTool_RejectsLoopback(t *testing.T) {
	reg := NewRegistry()
	_ = RegisterWebTools(reg, &WebToolsConfig{
		SearchBackend: web.NewBochaBackend(web.BochaConfig{APIKey: "k"}),
	})
	tl, _ := reg.Get("web_extract")
	res, err := tl.Execute(context.Background(), map[string]any{
		"urls": []any{"http://127.0.0.1/page"},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := res.(map[string]any)["results"].([]map[string]any)
	if results[0]["error"] == nil {
		t.Fatalf("expected ssrf error, got %#v", results[0])
	}
}

func TestHtmlToMarkdown_Basic(t *testing.T) {
	got := htmlToMarkdown("<html><body><p>Hello web</p></body></html>")
	if !strings.Contains(got, "Hello web") {
		t.Fatalf("got %q", got)
	}
}
