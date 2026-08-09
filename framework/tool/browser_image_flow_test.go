package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool/browser"
)

func TestBrowserNavigate_NoConfirmByDefault(t *testing.T) {
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.PageSnapshot = browser.Snapshot{Title: "ok", URL: "https://example.com/", Refs: map[string]string{}}
	if err := RegisterBrowserToolsWithConfig(reg, browser.NewSessionStore(), func() (browser.Backend, error) {
		return fb, nil
	}, &BrowserToolsConfig{
		PendingStore: NewInMemoryBrowserPendingStore(),
		TokenGen:     &fakeTokenGen{next: "tok"},
		// ConfirmNavigate defaults false
	}); err != nil {
		t.Fatal(err)
	}
	nav, _ := reg.Get("browser_navigate")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	out, err := nav.Execute(ctx, map[string]any{"url": "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["status"] == "pending" {
		t.Fatal("navigate must not pending when ConfirmNavigate=false")
	}
	if m["ok"] != true {
		t.Fatalf("%#v", m)
	}
	if len(fb.NavigatedURLs) != 1 {
		t.Fatalf("navigated=%v", fb.NavigatedURLs)
	}
}

func TestBrowserGetImages_FiltersUIChrome(t *testing.T) {
	reg := NewRegistry()
	fb := browser.NewFakeBackend()
	fb.Images = []browser.ImageInfo{
		{URL: "https://r.bing.com/rp/icon.svg", Alt: "ui"},
		{URL: "https://cdn.example.com/photo.jpg", Alt: "hero", Width: 800, Height: 600},
		{URL: "data:image/png;base64,aaa", Alt: "inline"},
		{URL: "https://img.example.com/p.webp", Alt: "shot", Width: 400, Height: 300},
	}
	if err := RegisterBrowserTools(reg, browser.NewSessionStore(), func() (browser.Backend, error) { return fb, nil }); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("browser_get_images")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s")
	out, err := tl.Execute(ctx, map[string]any{"limit": 10, "min_width": 80})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["count"] != 2 {
		t.Fatalf("want 2 content images, got %#v", m)
	}
	imgs := m["images"].([]map[string]any)
	for _, im := range imgs {
		u := im["url"].(string)
		if strings.Contains(u, ".svg") || strings.HasPrefix(u, "data:") {
			t.Fatalf("filtered badly: %s", u)
		}
	}
}

func TestBrowserSnapshot_ChallengeWarning(t *testing.T) {
	out := browserSnapshotResult(browser.Snapshot{
		URL:   "https://sec.douban.com/c?r=https%3A%2F%2Fmovie.douban.com",
		Title: "豆瓣",
		Text:  "载入中",
		Refs:  map[string]string{},
	})
	if out["warning"] != "anti_bot_or_captcha" {
		t.Fatalf("%#v", out)
	}
}

func TestIsContentImageURL(t *testing.T) {
	if isContentImageURL("https://r.bing.com/rp/x.svg") {
		t.Fatal("svg ui")
	}
	if !isContentImageURL("https://img3.doubanio.com/view/photo/l/public/p1.webp") {
		t.Fatal("douban photo")
	}
}
